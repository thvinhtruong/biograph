package ingestion

import (
	"regexp"
	"strings"
)

var (
	reLatex     = regexp.MustCompile(`(\\frac|\\sum|\\int|\\begin\{equation|\\begin\{align|\$\$|\\\[)`)
	reTable     = regexp.MustCompile(`(\|{2,}|\\begin\{tabular)`)
	reMultiCol  = regexp.MustCompile(`(?m)^.{1,40}\s{5,}.{1,40}$`) // two short columns separated by wide gap
)

const minTextLength = 50 // pages with fewer chars are flagged as complex (image-heavy)

// PageClassifier decides whether a page needs VLM processing.
type PageClassifier struct{}

func NewPageClassifier() *PageClassifier {
	return &PageClassifier{}
}

// IsSimple returns true if the page can be handled by text alone (no VLM needed).
func (c *PageClassifier) IsSimple(pt PageText) bool {
	text := pt.Text

	// Signal 1: too little text → likely image/diagram page
	if len(strings.TrimSpace(text)) < minTextLength {
		return false
	}

	// Signal 2: LaTeX math detected
	if reLatex.MatchString(text) {
		return false
	}

	// Signal 3: table structure detected
	if reTable.MatchString(text) {
		return false
	}

	// Signal 4: multi-column garble (two columns of short lines)
	if reMultiCol.MatchString(text) {
		lines := strings.Split(text, "\n")
		shortLines := 0
		for _, l := range lines {
			if len(strings.TrimSpace(l)) < 60 && len(strings.TrimSpace(l)) > 5 {
				shortLines++
			}
		}
		// If more than 60% of non-empty lines are short → multi-column layout
		nonEmpty := 0
		for _, l := range lines {
			if strings.TrimSpace(l) != "" {
				nonEmpty++
			}
		}
		if nonEmpty > 0 && float64(shortLines)/float64(nonEmpty) > 0.6 {
			return false
		}
	}

	return true
}

// textToExtractedContent converts a simple (text-only) page into ExtractedContent.
// For text-only pages, we do minimal entity extraction without VLM.
func textToExtractedContent(pt PageText, course, examDate string) ExtractedContent {
	return ExtractedContent{
		PageNumber:    pt.PageNumber,
		ContentSummary: truncateText(pt.Text, 500),
		Entities:      nil, // populated by VLM; text-only pages skip entity extraction
		Relationships: nil,
		RawLatex:      nil,
		VisualContext: "",
	}
}

func truncateText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
