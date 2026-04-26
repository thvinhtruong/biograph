package ingestion

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/truongvinh/biograph/internal/config"
)

const scratchpadDelimiter = "<!-- biograph:scratchpad -->"

// MarkdownWriter writes and safely updates First Thoughts lecture documents.
type MarkdownWriter struct {
	cfg *config.Config
}

func NewMarkdownWriter(cfg *config.Config) *MarkdownWriter {
	return &MarkdownWriter{cfg: cfg}
}

// WriteLecture writes the First Thoughts document for a lecture.
// On re-ingest, the LLM-generated section is replaced while the scratchpad
// (everything after the delimiter) is preserved unchanged.
func (w *MarkdownWriter) WriteLecture(course, filename, markdown string) (string, error) {
	dir := filepath.Join(w.cfg.Output.ContentDir, sanitiseCourse(course))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}

	slug := toSlug(filename)
	path := filepath.Join(dir, slug+".md")

	newContent := ensureScratchpad(markdown)

	existing, err := os.ReadFile(path)
	if err == nil {
		// File exists — preserve the scratchpad section
		newContent = mergeWithExisting(newContent, string(existing))
	}

	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// mergeWithExisting replaces the LLM section of an existing file while
// keeping everything the user wrote below the scratchpad delimiter.
func mergeWithExisting(newContent, existing string) string {
	idx := strings.Index(existing, scratchpadDelimiter)
	if idx < 0 {
		// No delimiter found — overwrite entirely (first ingest or corrupted file)
		return newContent
	}

	// Everything from the delimiter onward is the user's scratchpad
	userSection := existing[idx+len(scratchpadDelimiter):]

	// Replace the delimiter and below in newContent with the saved user section
	newIdx := strings.Index(newContent, scratchpadDelimiter)
	if newIdx < 0 {
		return newContent + "\n\n" + scratchpadDelimiter + userSection
	}
	return newContent[:newIdx+len(scratchpadDelimiter)] + userSection
}

// ensureScratchpad guarantees the delimiter exists in the content.
func ensureScratchpad(markdown string) string {
	if strings.Contains(markdown, scratchpadDelimiter) {
		return markdown
	}
	return markdown + "\n\n" + scratchpadDelimiter + "\n\n> _Space reserved for personal notes, coding implementations, and questions._\n"
}

// sanitiseCourse makes a course name safe for use as a directory name.
func sanitiseCourse(course string) string {
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_")
	return strings.ToLower(replacer.Replace(course))
}

// toSlug converts a filename to a URL/filesystem-safe slug.
func toSlug(name string) string {
	// Strip extension
	name = strings.TrimSuffix(name, filepath.Ext(name))
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_")
	return replacer.Replace(name)
}
