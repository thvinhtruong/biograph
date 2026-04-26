package ingestion

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/llm"
)

// ExtractedContent is the structured output from a VLM call for one page.
// It captures the enriched text content that feeds the final synthesis pass.
type ExtractedContent struct {
	PageNumber     int      `json:"page_number"`
	ContentSummary string   `json:"content_summary"`
	RawLatex       []string `json:"raw_latex"`
	VisualContext  string   `json:"visual_context"`
}

// VLMWorker manages a pool of concurrent VLM API calls.
type VLMWorker struct {
	cfg    *config.Config
	client *llm.Client
}

func NewVLMWorker(cfg *config.Config) *VLMWorker {
	return &VLMWorker{
		cfg:    cfg,
		client: llm.NewClient(cfg),
	}
}

type pageJob struct {
	page    PageText
	pdfPath string
	course  string
}

type pageResult struct {
	content ExtractedContent
	err     error
}

// ProcessPages runs the fan-out/fan-in VLM pipeline over complex pages.
// Returns enriched content for each page, sorted by page number.
func (w *VLMWorker) ProcessPages(pages []PageText, pdfPath, course string) ([]ExtractedContent, error) {
	numWorkers := w.cfg.Ingestion.Workers
	if numWorkers <= 0 {
		numWorkers = 4
	}

	jobs := make(chan pageJob, len(pages))
	results := make(chan pageResult, len(pages))

	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				ec, err := w.processPage(job.page, job.pdfPath, job.course)
				results <- pageResult{content: ec, err: err}
			}
		}()
	}

	for _, p := range pages {
		jobs <- pageJob{page: p, pdfPath: pdfPath, course: course}
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	contents := make([]ExtractedContent, 0, len(pages))
	var lastErr error
	for r := range results {
		if r.err != nil {
			log.Warn().Err(r.err).Msg("VLM page processing failed")
			lastErr = r.err
			continue
		}
		contents = append(contents, r.content)
	}

	sort.Slice(contents, func(i, j int) bool {
		return contents[i].PageNumber < contents[j].PageNumber
	})

	if len(contents) == 0 && lastErr != nil {
		return nil, fmt.Errorf("all VLM calls failed: %w", lastErr)
	}
	return contents, nil
}

func (w *VLMWorker) processPage(pt PageText, pdfPath, course string) (ExtractedContent, error) {
	pageBytes, err := ExtractPagePDF(pdfPath, pt.PageNumber)
	if err != nil {
		log.Warn().Err(err).Int("page", pt.PageNumber).Msg("page PDF extraction failed, falling back to text")
		return w.processPageText(pt, course)
	}

	prompt := buildPagePrompt(pt.Text, course)
	response, err := w.client.ExtractPage(pageBytes, pt.PageNumber, prompt)
	if err != nil {
		return ExtractedContent{PageNumber: pt.PageNumber, ContentSummary: pt.Text}, err
	}
	return parsePageResponse(response, pt.PageNumber), nil
}

func (w *VLMWorker) processPageText(pt PageText, course string) (ExtractedContent, error) {
	prompt := buildPagePrompt(pt.Text, course)
	response, err := w.client.Extract(prompt)
	if err != nil {
		return ExtractedContent{PageNumber: pt.PageNumber, ContentSummary: pt.Text}, err
	}
	return parsePageResponse(response, pt.PageNumber), nil
}

func parsePageResponse(response string, pageNum int) ExtractedContent {
	var ec ExtractedContent
	if err := json.Unmarshal([]byte(response), &ec); err != nil {
		return ExtractedContent{
			PageNumber:     pageNum,
			ContentSummary: truncateText(response, 800),
		}
	}
	ec.PageNumber = pageNum
	return ec
}

func buildPagePrompt(pageText, course string) string {
	return fmt.Sprintf(`You are extracting structured content from a lecture page for a %s course.

Page content:
---
%s
---

Respond ONLY with valid JSON:
{
  "page_number": 0,
  "content_summary": "Full text/content summary of this page, preserving all important details",
  "raw_latex": ["any LaTeX equations found, verbatim"],
  "visual_context": "Description of diagrams, figures, tables, or charts on this page (empty string if none)"
}

Rules:
- content_summary should be comprehensive — it feeds a later synthesis step
- Preserve all mathematical notation in raw_latex
- If no visual elements, set visual_context to ""`, course, pageText)
}
