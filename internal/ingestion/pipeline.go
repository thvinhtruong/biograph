package ingestion

import (
	"fmt"
	"path/filepath"

	"github.com/rs/zerolog/log"
	"github.com/schollz/progressbar/v3"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/search"
	"github.com/truongvinh/biograph/internal/storage"
)

// Options controls a single ingestion run.
type Options struct {
	PDFPath  string
	Course   string
	ExamDate string
	ForceVLM bool
	Config   *config.Config
}

// Result summarises what was produced.
type Result struct {
	PagesProcessed int
	VLMPages       int
	EntitiesFound  int
	EdgesCreated   int
	NotesWritten   int
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
func (p *Pipeline) Run(opts Options) (*Result, error) {
	result := &Result{}
	pdfName := filepath.Base(opts.PDFPath)

	// Step 1: Extract text from all pages via pdftotext (Pass 1)
	log.Info().Str("pdf", pdfName).Msg("extracting text (pass 1)")
	extractor := NewTextExtractor(p.cfg)
	pageTexts, err := extractor.ExtractAll(opts.PDFPath)
	if err != nil {
		return nil, fmt.Errorf("text extraction: %w", err)
	}
	result.PagesProcessed = len(pageTexts)

	// Step 2: Classify each page
	classifier := NewPageClassifier()
	simplePages := make([]PageText, 0)
	complexPages := make([]PageText, 0)

	for _, pt := range pageTexts {
		if !opts.ForceVLM && classifier.IsSimple(pt) {
			simplePages = append(simplePages, pt)
		} else {
			complexPages = append(complexPages, pt)
		}
	}
	result.VLMPages = len(complexPages)

	log.Info().
		Int("simple", len(simplePages)).
		Int("complex", len(complexPages)).
		Msg("page classification done")

	// Step 3: Process pages → ExtractedContent
	bar := progressbar.Default(int64(len(pageTexts)), "processing pages")
	contents := make([]ExtractedContent, 0, len(pageTexts))

	// Simple pages: convert text directly (no LLM cost)
	for _, pt := range simplePages {
		ec := textToExtractedContent(pt, opts.Course, opts.ExamDate)
		contents = append(contents, ec)
		bar.Add(1)
	}

	// Complex pages: VLM worker pool
	if len(complexPages) > 0 {
		vlmWorker := NewVLMWorker(p.cfg)
		vlmResults, err := vlmWorker.ProcessPages(complexPages, opts.PDFPath, opts.Course, opts.ExamDate)
		if err != nil {
			log.Warn().Err(err).Msg("VLM processing failed, falling back to text")
			// Fallback: treat as simple
			for _, pt := range complexPages {
				vlmResults = append(vlmResults, textToExtractedContent(pt, opts.Course, opts.ExamDate))
			}
		}
		contents = append(contents, vlmResults...)
		bar.Add(len(complexPages))
	}
	bar.Finish()

	// Step 4: Write entities + edges to storage
	log.Info().Msg("writing to graph storage")
	writer := NewMarkdownWriter(p.cfg)

	// Open Bleve index once for the whole ingestion run
	bleveIdx, bleveErr := search.OpenIndex(p.cfg)
	if bleveErr != nil {
		log.Warn().Err(bleveErr).Msg("bleve index unavailable — skipping search indexing")
	}
	defer func() {
		if bleveIdx != nil {
			bleveIdx.Close()
		}
	}()

	for _, ec := range contents {
		for _, entity := range ec.Entities {
			node := &storage.Node{
				ID:          entity.Name,
				DisplayName: entity.DisplayName,
				Definition:  entity.Definition,
				Category:    entity.Category,
				Course:      opts.Course,
				ExamDate:    opts.ExamDate,
				RawLatex:    ec.RawLatex,
				Sources: []storage.SourceRef{{
					PDF:  pdfName,
					Page: ec.PageNumber,
				}},
			}
			if err := p.db.UpsertNode(node); err != nil {
				log.Warn().Err(err).Str("node", node.ID).Msg("failed to upsert node")
				continue
			}
			result.EntitiesFound++

			// Index into Bleve for fast keyword search
			if bleveIdx != nil {
				if err := bleveIdx.IndexNode(node); err != nil {
					log.Warn().Err(err).Str("node", node.ID).Msg("bleve index failed")
				}
			}
		}

		for _, rel := range ec.Relationships {
			edge := &storage.Edge{
				SourceID: rel.Source,
				TargetID: rel.Target,
				Weight:   rel.Strength,
				EdgeType: rel.Type,
			}
			if err := p.db.UpsertEdge(edge); err != nil {
				log.Warn().Err(err).Msg("failed to upsert edge")
				continue
			}
			result.EdgesCreated++
		}

		// Write Obsidian markdown notes
		for _, entity := range ec.Entities {
			node, _ := p.db.GetNode(entity.Name)
			if node == nil {
				continue
			}
			if err := writer.WriteEntity(node, ec.Relationships); err != nil {
				log.Warn().Err(err).Str("node", node.ID).Msg("failed to write entity note")
			} else {
				result.NotesWritten++
			}
		}
	}

	// Write source metadata note
	if err := writer.WriteSource(pdfName, opts.Course, opts.ExamDate, result.PagesProcessed); err != nil {
		log.Warn().Err(err).Msg("failed to write source note")
	}

	return result, nil
}
