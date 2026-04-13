package ingestion

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/truongvinh/biograph/internal/config"
)

// PageText holds the raw text content of a single PDF page.
type PageText struct {
	PageNumber int
	Text       string
}

// TextExtractor uses pdftotext (poppler) to extract text from a PDF.
type TextExtractor struct {
	cfg *config.Config
}

func NewTextExtractor(cfg *config.Config) *TextExtractor {
	return &TextExtractor{cfg: cfg}
}

// ExtractAll extracts text from every page of a PDF.
// Uses pdftotext -f <page> -l <page> to process page by page.
func (e *TextExtractor) ExtractAll(pdfPath string) ([]PageText, error) {
	// First, get page count
	nPages, err := e.pageCount(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("page count: %w", err)
	}

	pages := make([]PageText, 0, nPages)
	for i := 1; i <= nPages; i++ {
		text, err := e.extractPage(pdfPath, i)
		if err != nil {
			// Non-fatal: include empty text so classifier can flag it
			pages = append(pages, PageText{PageNumber: i, Text: ""})
			continue
		}
		pages = append(pages, PageText{PageNumber: i, Text: text})
	}
	return pages, nil
}

func (e *TextExtractor) extractPage(pdfPath string, page int) (string, error) {
	pdftotext := e.cfg.Ingestion.PDFToTextPath
	if pdftotext == "" {
		pdftotext = "pdftotext"
	}

	pageStr := strconv.Itoa(page)
	// -f first page, -l last page, - for stdout
	cmd := exec.Command(pdftotext, "-f", pageStr, "-l", pageStr, "-enc", "UTF-8", pdfPath, "-")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (e *TextExtractor) pageCount(pdfPath string) (int, error) {
	// Use pdfinfo if available, fall back to pdftotext page scan
	cmd := exec.Command("pdfinfo", pdfPath)
	out, err := cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Pages:") {
				parts := strings.Fields(line)
				if len(parts) == 2 {
					return strconv.Atoi(parts[1])
				}
			}
		}
	}

	// Fallback: try pdfcpu
	return e.pageCountViaPDFCPU(pdfPath)
}

func (e *TextExtractor) pageCountViaPDFCPU(pdfPath string) (int, error) {
	cmd := exec.Command("pdfcpu", "info", pdfPath)
	out, err := cmd.Output()
	if err != nil {
		// Last resort: assume 1
		return 1, nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Page count") {
			parts := strings.Fields(line)
			for _, p := range parts {
				if n, err := strconv.Atoi(p); err == nil {
					return n, nil
				}
			}
		}
	}
	return 1, nil
}
