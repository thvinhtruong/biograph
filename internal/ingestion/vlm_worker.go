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
type ExtractedContent struct {
	PageNumber     int             `json:"page_number"`
	ContentSummary string          `json:"content_summary"`
	Entities       []EntityDraft   `json:"entities"`
	Relationships  []RelationDraft `json:"relationships"`
	RawLatex       []string        `json:"raw_latex"`
	VisualContext  string          `json:"visual_context"`
}

// EntityDraft is a VLM-extracted entity before storage.
type EntityDraft struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Definition  string `json:"definition"`
	Category    string `json:"category"`
}

// RelationDraft is a VLM-extracted relationship before storage.
type RelationDraft struct {
	Source   string  `json:"source"`
	Target   string  `json:"target"`
	Type     string  `json:"type"`
	Strength float64 `json:"strength"`
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

// ProcessPages runs the fan-out/fan-in VLM pipeline.
// pdfPath is the source PDF file — each complex page is extracted as a
// single-page PDF byte slice and sent natively to the vision model.
func (w *VLMWorker) ProcessPages(pages []PageText, pdfPath, course, examDate string) ([]ExtractedContent, error) {
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
	// Extract this page as a standalone PDF byte slice (no external tool needed)
	pageBytes, err := ExtractPagePDF(pdfPath, pt.PageNumber)
	if err != nil {
		log.Warn().Err(err).Int("page", pt.PageNumber).Msg("page PDF extraction failed, falling back to text prompt")
		// Graceful fallback: send text-based prompt
		return w.processPageText(pt, course)
	}

	prompt := buildExtractionPrompt(pt.Text, course)
	response, err := w.client.ExtractPage(pageBytes, pt.PageNumber, prompt)
	if err != nil {
		return ExtractedContent{PageNumber: pt.PageNumber}, err
	}
	return parseExtractionResponse(response, pt.PageNumber), nil
}

// processPageText is the text-only fallback for when PDF byte extraction fails.
func (w *VLMWorker) processPageText(pt PageText, course string) (ExtractedContent, error) {
	prompt := buildExtractionPrompt(pt.Text, course)
	response, err := w.client.Extract(prompt)
	if err != nil {
		return ExtractedContent{PageNumber: pt.PageNumber}, err
	}
	return parseExtractionResponse(response, pt.PageNumber), nil
}

func parseExtractionResponse(response string, pageNum int) ExtractedContent {
	var ec ExtractedContent
	if err := json.Unmarshal([]byte(response), &ec); err != nil {
		return ExtractedContent{
			PageNumber:     pageNum,
			ContentSummary: truncateText(response, 500),
		}
	}
	ec.PageNumber = pageNum
	return ec
}

func buildExtractionPrompt(pageText, course string) string {
	return fmt.Sprintf(`You are an expert academic knowledge extractor. Extract entities and relationships from this lecture page.

Course: %s
Page content (text layer):
---
%s
---

Respond ONLY with valid JSON matching this exact schema:
{
  "page_number": 0,
  "content_summary": "One paragraph summary of the page",
  "entities": [
    {
      "name": "snake_case_id",
      "display_name": "Human Readable Name",
      "definition": "Precise academic definition",
      "category": "algorithm|theorem|concept|mathematical_concept|model|technique"
    }
  ],
  "relationships": [
    {
      "source": "entity_id",
      "target": "other_entity_id",
      "type": "depends_on|is_part_of|related_to|extends|contradicts|uses",
      "strength": 0.85
    }
  ],
  "raw_latex": ["\\frac{...}{...}"],
  "visual_context": "Description of any diagrams, figures, or charts on the page"
}

Rules:
- Use snake_case for entity names (e.g., "gradient_descent" not "Gradient Descent")
- Return 0-8 entities per page, only the most important ones
- Strength values: 0.9 (strong dependency) to 0.5 (loose relation)
- If there are no meaningful entities, return empty arrays`, course, pageText)
}
