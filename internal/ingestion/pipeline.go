package ingestion

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/schollz/progressbar/v3"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/llm"
	"github.com/truongvinh/biograph/internal/storage"
)

// Options controls a single ingestion run.
type Options struct {
	PDFPath  string
	Course   string
	ExamDate string
	Config   *config.Config
}

// Result summarises what was produced.
type Result struct {
	PagesProcessed int
	VLMPages       int
	NodesStored    int
	OutputPath     string
}

// Pipeline orchestrates the tiered ingestion process.
type Pipeline struct {
	db  *storage.DB
	cfg *config.Config
}

func NewPipeline(db *storage.DB, cfg *config.Config) *Pipeline {
	return &Pipeline{db: db, cfg: cfg}
}

// Run executes the full ingestion for a single PDF.
// Flow: extract text → classify pages → VLM complex pages → synthesize → store nodes → write ledger.
func (p *Pipeline) Run(opts Options) (*Result, error) {
	result := &Result{}
	pdfName := filepath.Base(opts.PDFPath)

	// Step 1: Extract text from all pages
	log.Info().Str("pdf", pdfName).Msg("extracting text")
	extractor := NewTextExtractor(p.cfg)
	pageTexts, err := extractor.ExtractAll(opts.PDFPath)
	if err != nil {
		return nil, fmt.Errorf("text extraction: %w", err)
	}
	result.PagesProcessed = len(pageTexts)

	// Step 2: Classify pages
	classifier := NewPageClassifier()
	var simplePages, complexPages []PageText
	for _, pt := range pageTexts {
		if classifier.IsSimple(pt) {
			simplePages = append(simplePages, pt)
		} else {
			complexPages = append(complexPages, pt)
		}
	}
	result.VLMPages = len(complexPages)
	log.Info().Int("simple", len(simplePages)).Int("complex", len(complexPages)).Msg("page classification done")

	// Step 3: Enrich complex pages via VLM (fan-out/fan-in)
	bar := progressbar.Default(int64(len(pageTexts)), "processing pages")
	allPageContent := make([]string, 0, len(pageTexts))

	// Simple pages: use raw text directly
	pageMap := make(map[int]string, len(pageTexts))
	for _, pt := range simplePages {
		pageMap[pt.PageNumber] = pt.Text
		bar.Add(1)
	}

	// Complex pages: VLM enrichment
	if len(complexPages) > 0 {
		vlmWorker := NewVLMWorker(p.cfg)
		enriched, err := vlmWorker.ProcessPages(complexPages, opts.PDFPath, opts.Course)
		if err != nil {
			log.Warn().Err(err).Msg("VLM processing failed, falling back to raw text")
			for _, pt := range complexPages {
				pageMap[pt.PageNumber] = pt.Text
			}
		} else {
			for _, ec := range enriched {
				parts := []string{ec.ContentSummary}
				if ec.VisualContext != "" {
					parts = append(parts, "[Visual: "+ec.VisualContext+"]")
				}
				if len(ec.RawLatex) > 0 {
					parts = append(parts, "[LaTeX: "+strings.Join(ec.RawLatex, " | ")+"]")
				}
				pageMap[ec.PageNumber] = strings.Join(parts, "\n")
			}
		}
		bar.Add(len(complexPages))
	}
	bar.Finish()

	// Assemble pages in order
	for i := 1; i <= len(pageTexts); i++ {
		if text, ok := pageMap[i]; ok {
			allPageContent = append(allPageContent, fmt.Sprintf("[Page %d]\n%s", i, text))
		}
	}

	// Step 4: Single synthesis LLM call → nodes + First Thoughts markdown
	log.Info().Msg("synthesising lecture content")
	client := llm.NewClient(p.cfg)
	synthesis, err := client.Synthesize(allPageContent, opts.Course, opts.ExamDate, pdfName)
	if err != nil {
		return nil, fmt.Errorf("synthesis failed: %w", err)
	}

	// Step 5: Store atomic nodes in SQLite (powers `ask` command)
	for _, draft := range synthesis.Nodes {
		node := &storage.Node{
			ID:          draft.ID,
			DisplayName: draft.DisplayName,
			Definition:  draft.Definition,
			Category:    draft.Category,
			Course:      opts.Course,
			ExamDate:    opts.ExamDate,
			RawLatex:    draft.RawLatex,
			Sources: []storage.SourceRef{{
				PDF:  pdfName,
				Page: 0,
			}},
		}
		if err := p.db.UpsertNode(node); err != nil {
			log.Warn().Err(err).Str("node", node.ID).Msg("failed to upsert node")
			continue
		}
		result.NodesStored++
	}

	// Step 6: Write First Thoughts markdown ledger
	writer := NewMarkdownWriter(p.cfg)
	outPath, err := writer.WriteLecture(opts.Course, pdfName, synthesis.Markdown)
	if err != nil {
		return nil, fmt.Errorf("write ledger: %w", err)
	}
	result.OutputPath = outPath

	log.Info().
		Int("nodes", result.NodesStored).
		Str("output", outPath).
		Msg("ingestion complete")

	return result, nil
}
