package storage

import (
	"encoding/json"
	"fmt"
	"time"
)

// Node represents an extracted concept stored in the knowledge base.
type Node struct {
	ID          string      `json:"id"`
	DisplayName string      `json:"display_name"`
	Definition  string      `json:"definition"`
	Category    string      `json:"category"`
	Course      string      `json:"course"`
	ExamDate    string      `json:"exam_date"`
	RawLatex    []string    `json:"raw_latex"`
	Sources     []SourceRef `json:"sources"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
}

// SourceRef points to the PDF page where a node was extracted.
type SourceRef struct {
	PDF  string `json:"pdf"`
	Page int    `json:"page"`
}

// UpsertNode inserts a new node or merges with an existing one.
// Definition is overwritten; sources are appended (deduplication by PDF+page).
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

	// Merge: overwrite definition, append new sources
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

// GetNode retrieves a node by ID. Returns nil, nil if not found.
func (db *DB) GetNode(id string) (*Node, error) {
	row := db.sql.QueryRow(`
		SELECT id, display_name, definition, category, course, exam_date, raw_latex, sources, created_at, updated_at
		FROM nodes WHERE id = ?`, id)

	n := &Node{}
	var rawLatexJSON, sourcesJSON string
	err := row.Scan(&n.ID, &n.DisplayName, &n.Definition, &n.Category,
		&n.Course, &n.ExamDate, &rawLatexJSON, &sourcesJSON, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		return nil, nil
	}
	json.Unmarshal([]byte(rawLatexJSON), &n.RawLatex)
	json.Unmarshal([]byte(sourcesJSON), &n.Sources)
	return n, nil
}

// FTSSearch performs full-text search using the FTS5 virtual table.
// Optionally filtered by course if course != "".
func (db *DB) FTSSearch(query, course string, limit int) ([]*Node, error) {
	var rows interface {
		Next() bool
		Scan(...any) error
		Close() error
	}
	var err error

	if course != "" {
		rows, err = db.sql.Query(`
			SELECT n.id, n.display_name, n.definition, n.category, n.course, n.exam_date, n.raw_latex, n.sources
			FROM nodes_fts f
			JOIN nodes n ON n.id = f.id
			WHERE nodes_fts MATCH ? AND n.course = ?
			ORDER BY rank
			LIMIT ?`, query, course, limit)
	} else {
		rows, err = db.sql.Query(`
			SELECT n.id, n.display_name, n.definition, n.category, n.course, n.exam_date, n.raw_latex, n.sources
			FROM nodes_fts f
			JOIN nodes n ON n.id = f.id
			WHERE nodes_fts MATCH ?
			ORDER BY rank
			LIMIT ?`, query, limit)
	}
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

// ListNodes returns all nodes for a course, ordered by recency.
func (db *DB) ListNodes(course string, limit int) ([]*Node, error) {
	rows, err := db.sql.Query(`
		SELECT id, display_name, definition, category, course, exam_date, raw_latex, sources, created_at, updated_at
		FROM nodes
		WHERE course = ? OR ? = ''
		ORDER BY updated_at DESC
		LIMIT ?`, course, course, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		n := &Node{}
		var rawLatexJSON, sourcesJSON string
		if err := rows.Scan(&n.ID, &n.DisplayName, &n.Definition, &n.Category,
			&n.Course, &n.ExamDate, &rawLatexJSON, &sourcesJSON,
			&n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(rawLatexJSON), &n.RawLatex)
		json.Unmarshal([]byte(sourcesJSON), &n.Sources)
		nodes = append(nodes, n)
	}
	return nodes, nil
}
