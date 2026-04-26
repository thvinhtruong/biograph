package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/truongvinh/biograph/internal/config"
)

// DB wraps a SQLite connection with all biograph operations.
type DB struct {
	sql *sql.DB
	cfg *config.Config
}

// Open opens (or creates) the SQLite database and runs migrations.
func Open(cfg *config.Config) (*DB, error) {
	dsn := cfg.Database.Path + "?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000"
	sqlDB, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db := &DB{sql: sqlDB, cfg: cfg}
	if err := db.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migration: %w", err)
	}
	return db, nil
}

// Close closes the underlying database connection.
func (db *DB) Close() error {
	return db.sql.Close()
}

// Stats holds aggregate statistics about the knowledge base.
type Stats struct {
	NodeCount   int
	CourseCount int
	QueryCount  int
}

// Stats returns aggregate statistics.
func (db *DB) Stats() (*Stats, error) {
	s := &Stats{}

	row := db.sql.QueryRow(`SELECT COUNT(*) FROM nodes`)
	if err := row.Scan(&s.NodeCount); err != nil {
		return nil, err
	}
	row = db.sql.QueryRow(`SELECT COUNT(DISTINCT course) FROM nodes WHERE course != ''`)
	if err := row.Scan(&s.CourseCount); err != nil {
		return nil, err
	}
	row = db.sql.QueryRow(`SELECT COUNT(*) FROM query_log`)
	if err := row.Scan(&s.QueryCount); err != nil {
		return nil, err
	}

	return s, nil
}

// LogQuery records a query for audit history.
func (db *DB) LogQuery(queryText string, matchedNodes []string) error {
	nodesJSON := marshalStringSlice(matchedNodes)
	_, err := db.sql.Exec(
		`INSERT INTO query_log (query_text, matched_nodes, timestamp) VALUES (?, ?, ?)`,
		queryText, nodesJSON, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func marshalStringSlice(ss []string) string {
	b, _ := json.Marshal(ss)
	return string(b)
}
