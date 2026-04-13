package storage

import (
	"encoding/json"
	"fmt"
	"time"
)

// Node represents an entity in the knowledge graph.
type Node struct {
	ID          string       `json:"id"`
	DisplayName string       `json:"display_name"`
	Definition  string       `json:"definition"`
	Category    string       `json:"category"`
	Course      string       `json:"course"`
	ExamDate    string       `json:"exam_date"`
	RawLatex    []string     `json:"raw_latex"`
	Sources     []SourceRef  `json:"sources"`
	CreatedAt   string       `json:"created_at"`
	UpdatedAt   string       `json:"updated_at"`
}

// SourceRef points to the PDF page where a node was extracted.
type SourceRef struct {
	PDF  string `json:"pdf"`
	Page int    `json:"page"`
}

// UpsertNode inserts a new node or merges with an existing one.
// Last-write-wins for definition; sources are appended.
func (db *DB) UpsertNode(n *Node) error {
	existing, err := db.GetNode(n.ID)
	if err != nil {
		return err
	}

	latexJSON, err := json.Marshal(n.RawLatex)
	if err != nil {
		return err
	}

	if existing == nil {
		// Fresh insert
		sourcesJSON, _ := json.Marshal(n.Sources)
		_, err = db.sql.Exec(`
			INSERT INTO nodes (id, display_name, definition, category, course, exam_date, raw_latex, sources, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.ID, n.DisplayName, n.Definition, n.Category,
			n.Course, n.ExamDate, string(latexJSON), string(sourcesJSON),
			time.Now().UTC().Format(time.RFC3339),
		)
		return err
	}

	// Merge: update definition + append sources
	merged := append(existing.Sources, n.Sources...)
	sourcesJSON, _ := json.Marshal(merged)

	_, err = db.sql.Exec(`
		UPDATE nodes
		SET definition = ?, display_name = ?, category = ?,
		    exam_date = ?, raw_latex = ?, sources = ?, updated_at = ?
		WHERE id = ?`,
		n.Definition, n.DisplayName, n.Category,
		n.ExamDate, string(latexJSON), string(sourcesJSON),
		time.Now().UTC().Format(time.RFC3339),
		n.ID,
	)
	return err
}

// GetNode retrieves a node by ID. Returns nil if not found.
func (db *DB) GetNode(id string) (*Node, error) {
	row := db.sql.QueryRow(`
		SELECT id, display_name, definition, category, course, exam_date, raw_latex, sources, created_at, updated_at
		FROM nodes WHERE id = ?`, id)

	n := &Node{}
	var rawLatexJSON, sourcesJSON string
	err := row.Scan(&n.ID, &n.DisplayName, &n.Definition, &n.Category,
		&n.Course, &n.ExamDate, &rawLatexJSON, &sourcesJSON, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, nil // not found
	}

	if err := json.Unmarshal([]byte(rawLatexJSON), &n.RawLatex); err != nil {
		n.RawLatex = nil
	}
	if err := json.Unmarshal([]byte(sourcesJSON), &n.Sources); err != nil {
		n.Sources = nil
	}
	return n, nil
}

// GetNodes retrieves multiple nodes by ID.
func (db *DB) GetNodes(ids []string) ([]*Node, error) {
	nodes := make([]*Node, 0, len(ids))
	for _, id := range ids {
		n, err := db.GetNode(id)
		if err != nil {
			return nil, err
		}
		if n != nil {
			nodes = append(nodes, n)
		}
	}
	return nodes, nil
}

// FTSSearch performs a full-text search using the FTS5 virtual table.
func (db *DB) FTSSearch(query string, limit int) ([]*Node, error) {
	rows, err := db.sql.Query(`
		SELECT n.id, n.display_name, n.definition, n.category, n.course, n.exam_date, n.raw_latex, n.sources
		FROM nodes_fts f
		JOIN nodes n ON n.id = f.id
		WHERE nodes_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("fts search: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		n := &Node{}
		var rawLatexJSON, sourcesJSON string
		if err := rows.Scan(&n.ID, &n.DisplayName, &n.Definition, &n.Category,
			&n.Course, &n.ExamDate, &rawLatexJSON, &sourcesJSON); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(rawLatexJSON), &n.RawLatex)
		json.Unmarshal([]byte(sourcesJSON), &n.Sources)
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// ListTopNodes returns the top N nodes sorted by average edge weight.
func (db *DB) ListTopNodes(limit int) ([]*Node, error) {
	rows, err := db.sql.Query(`
		SELECT n.id, n.display_name, n.definition, n.category, n.course, n.exam_date, n.raw_latex, n.sources
		FROM nodes n
		LEFT JOIN edges e ON e.source_id = n.id
		GROUP BY n.id
		ORDER BY COALESCE(AVG(e.weight), 0.5) DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		n := &Node{}
		var rawLatexJSON, sourcesJSON string
		if err := rows.Scan(&n.ID, &n.DisplayName, &n.Definition, &n.Category,
			&n.Course, &n.ExamDate, &rawLatexJSON, &sourcesJSON); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(rawLatexJSON), &n.RawLatex)
		json.Unmarshal([]byte(sourcesJSON), &n.Sources)
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// marshalStringSlice encodes a string slice to JSON.
func marshalStringSlice(ss []string) string {
	b, _ := json.Marshal(ss)
	return string(b)
}
