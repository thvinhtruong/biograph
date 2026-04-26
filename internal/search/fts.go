package search

import (
	"fmt"
	"strings"

	"github.com/truongvinh/biograph/internal/storage"
)

const defaultLimit = 10

// Search performs FTS5 full-text search and returns matching nodes.
// The query is sanitised to avoid FTS5 syntax errors from raw user input.
func Search(db *storage.DB, query, course string, limit int) ([]*storage.Node, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	safe := sanitise(query)
	if safe == "" {
		return nil, fmt.Errorf("query is empty after sanitisation")
	}
	return db.FTSSearch(safe, course, limit)
}

// sanitise strips FTS5 special characters from user input so a plain
// keyword search never causes a syntax error.
func sanitise(q string) string {
	// Remove FTS5 operators/punctuation that cause parse errors
	replacer := strings.NewReplacer(
		`"`, ``,
		`*`, ``,
		`(`, ``,
		`)`, ``,
		`:`, ``,
		`-`, ` `,
	)
	return strings.TrimSpace(replacer.Replace(q))
}
