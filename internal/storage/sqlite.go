package storage

import (
	"database/sql"
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

// Stats holds aggregate statistics about the knowledge graph.
type Stats struct {
	NodeCount     int
	EdgeCount     int
	CourseCount   int
	QueryCount    int
	TopNodes      []NodeSummary
	UpcomingExams []ExamSummary
}

type NodeSummary struct {
	ID          string
	DisplayName string
	Course      string
	Weight      float64
}

type ExamSummary struct {
	Course    string
	ExamDate  string
	DaysUntil int
}

// Stats returns aggregate statistics.
func (db *DB) Stats() (*Stats, error) {
	s := &Stats{}

	row := db.sql.QueryRow(`SELECT COUNT(*) FROM nodes`)
	if err := row.Scan(&s.NodeCount); err != nil {
		return nil, err
	}
	row = db.sql.QueryRow(`SELECT COUNT(*) FROM edges`)
	if err := row.Scan(&s.EdgeCount); err != nil {
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

	// Top 10 nodes by avg outbound edge weight
	rows, err := db.sql.Query(`
		SELECT n.id, n.display_name, n.course,
		       COALESCE(AVG(e.weight), 0.5) AS avg_weight
		FROM nodes n
		LEFT JOIN edges e ON e.source_id = n.id
		GROUP BY n.id
		ORDER BY avg_weight DESC
		LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ns NodeSummary
		if err := rows.Scan(&ns.ID, &ns.DisplayName, &ns.Course, &ns.Weight); err != nil {
			return nil, err
		}
		s.TopNodes = append(s.TopNodes, ns)
	}

	// Upcoming exams (within 60 days, not yet passed)
	examRows, err := db.sql.Query(`
		SELECT course, exam_date,
		       CAST(julianday(exam_date) - julianday('now') AS INTEGER) AS days_until
		FROM nodes
		WHERE exam_date != '' AND exam_date > date('now')
		GROUP BY course, exam_date
		ORDER BY exam_date ASC
		LIMIT 10`)
	if err != nil {
		return nil, err
	}
	defer examRows.Close()
	for examRows.Next() {
		var es ExamSummary
		if err := examRows.Scan(&es.Course, &es.ExamDate, &es.DaysUntil); err != nil {
			return nil, err
		}
		s.UpcomingExams = append(s.UpcomingExams, es)
	}

	return s, nil
}

// LogQuery records a query and the activated node IDs for Hebbian learning.
func (db *DB) LogQuery(queryText string, activatedNodes []string) error {
	nodesJSON := marshalStringSlice(activatedNodes)
	_, err := db.sql.Exec(
		`INSERT INTO query_log (query_text, activated_nodes, timestamp) VALUES (?, ?, ?)`,
		queryText, nodesJSON, time.Now().UTC().Format(time.RFC3339),
	)
	return err
}
