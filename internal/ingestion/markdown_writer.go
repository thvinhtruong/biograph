package ingestion

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/storage"
)

// MarkdownWriter generates Obsidian-compatible markdown notes.
type MarkdownWriter struct {
	cfg *config.Config
}

func NewMarkdownWriter(cfg *config.Config) *MarkdownWriter {
	return &MarkdownWriter{cfg: cfg}
}

// WriteEntity writes a node's entity note into the Obsidian vault.
func (w *MarkdownWriter) WriteEntity(node *storage.Node, rels []RelationDraft) error {
	dir := filepath.Join(w.cfg.Vault.Path, w.cfg.Vault.EntityDir, node.Category)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	path := filepath.Join(dir, node.ID+".md")
	content := w.renderEntityNote(node, rels)
	return os.WriteFile(path, []byte(content), 0644)
}

// WriteSource writes a source PDF metadata note.
func (w *MarkdownWriter) WriteSource(pdfName, course, examDate string, pageCount int) error {
	dir := filepath.Join(w.cfg.Vault.Path, w.cfg.Vault.SourceDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	slug := strings.TrimSuffix(pdfName, filepath.Ext(pdfName))
	slug = strings.ReplaceAll(slug, " ", "_")
	path := filepath.Join(dir, slug+".md")

	content := fmt.Sprintf(`---
pdf: "%s"
course: %s
exam_date: %s
page_count: %d
ingested_at: %s
tags:
  - source
  - %s
---

# %s

- **Course:** %s
- **Pages:** %d
- **Exam:** %s
- **Ingested:** %s
`,
		pdfName, course, examDate, pageCount,
		time.Now().Format("2006-01-02"),
		course,
		slug,
		course, pageCount, examDate,
		time.Now().Format("2006-01-02 15:04"),
	)

	return os.WriteFile(path, []byte(content), 0644)
}

// SyncEntity re-writes an entity note from the current DB state (vault sync).
func (w *MarkdownWriter) SyncEntity(node *storage.Node) error {
	return w.WriteEntity(node, nil)
}

func (w *MarkdownWriter) renderEntityNote(node *storage.Node, rels []RelationDraft) string {
	var sb strings.Builder

	// YAML frontmatter
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("id: %s\n", node.ID))
	sb.WriteString(fmt.Sprintf("display_name: \"%s\"\n", node.DisplayName))
	sb.WriteString(fmt.Sprintf("category: %s\n", node.Category))
	sb.WriteString(fmt.Sprintf("course: %s\n", node.Course))
	if node.ExamDate != "" {
		sb.WriteString(fmt.Sprintf("exam_date: %s\n", node.ExamDate))
	}
	sb.WriteString(fmt.Sprintf("last_updated: %s\n", time.Now().Format("2006-01-02")))
	sb.WriteString("tags:\n")
	sb.WriteString("  - entity\n")
	if node.Category != "" {
		sb.WriteString(fmt.Sprintf("  - %s\n", node.Category))
	}
	if node.Course != "" {
		sb.WriteString(fmt.Sprintf("  - %s\n", node.Course))
	}

	// Sources as YAML list
	if len(node.Sources) > 0 {
		sb.WriteString("sources:\n")
		for _, s := range node.Sources {
			sb.WriteString(fmt.Sprintf("  - pdf: \"%s\"\n    page: %d\n", s.PDF, s.Page))
		}
	}
	sb.WriteString("---\n\n")

	// Title
	sb.WriteString(fmt.Sprintf("# %s\n\n", node.DisplayName))

	// Definition
	sb.WriteString(node.Definition + "\n\n")

	// LaTeX equations
	if len(node.RawLatex) > 0 {
		sb.WriteString("## Key Equations\n\n")
		for _, eq := range node.RawLatex {
			sb.WriteString(fmt.Sprintf("$$%s$$\n\n", eq))
		}
	}

	// Connections (wikilinks for Obsidian graph view)
	if len(rels) > 0 {
		sb.WriteString("## Connections\n\n")
		for _, r := range rels {
			if r.Source == node.ID {
				sb.WriteString(fmt.Sprintf("- [[%s]] — %s (strength: %.2f)\n", r.Target, r.Type, r.Strength))
			} else if r.Target == node.ID {
				sb.WriteString(fmt.Sprintf("- [[%s]] — %s (strength: %.2f)\n", r.Source, r.Type, r.Strength))
			}
		}
		sb.WriteString("\n")
	}

	// Source references
	if len(node.Sources) > 0 {
		sb.WriteString("## Sources\n\n")
		for _, s := range node.Sources {
			sb.WriteString(fmt.Sprintf("- %s, Page %d\n", s.PDF, s.Page))
		}
	}

	return sb.String()
}
