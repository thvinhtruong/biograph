package ingestion

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/ledongthuc/pdf"
	pdfcpuapi "github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/truongvinh/biograph/internal/config"
)

// PageText holds the raw text content of a single PDF page.
type PageText struct {
	PageNumber int
	Text       string
}

// TextExtractor uses pure Go libraries to extract text from a PDF.
// No external binaries required — ledongthuc/pdf for text, pdfcpu for page splitting.
type TextExtractor struct {
	cfg *config.Config
}

func NewTextExtractor(cfg *config.Config) *TextExtractor {
	return &TextExtractor{cfg: cfg}
}

// ExtractAll extracts text from every page of a PDF.
func (e *TextExtractor) ExtractAll(pdfPath string) ([]PageText, error) {
	f, r, err := pdf.Open(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	defer f.Close()

	numPages := r.NumPage()
	pages := make([]PageText, 0, numPages)
	for i := 1; i <= numPages; i++ {
		pages = append(pages, PageText{
			PageNumber: i,
			Text:       extractPageText(r, i),
		})
	}
	return pages, nil
}

// ExtractPagePDF extracts a single page from a PDF as a self-contained PDF
// byte slice using pdfcpu. This is sent to vision models instead of a
// rendered PNG — Claude and Gemini both accept raw PDF content natively.
func ExtractPagePDF(pdfPath string, pageNum int) ([]byte, error) {
	f, err := os.Open(pdfPath)
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	pages := []string{fmt.Sprintf("%d", pageNum)}
	if err := pdfcpuapi.Trim(f, &buf, pages, nil); err != nil {
		return nil, fmt.Errorf("extract page %d: %w", pageNum, err)
	}
	return buf.Bytes(), nil
}

// extractPageText pulls plain text from one page via ledongthuc/pdf.
func extractPageText(r *pdf.Reader, pageNum int) string {
	page := r.Page(pageNum)
	if page.V.IsNull() {
		return ""
	}

	// GetTextByRow preserves reading order better than GetPlainText
	rows, err := page.GetTextByRow()
	if err != nil {
		text, _ := page.GetPlainText(nil)
		return strings.TrimSpace(text)
	}

	var sb strings.Builder
	for _, row := range rows {
		for _, word := range row.Content {
			sb.WriteString(word.S)
			sb.WriteString(" ")
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}
