package storage

// migrate runs the full schema DDL on first run (idempotent via IF NOT EXISTS).
func (db *DB) migrate() error {
	schema := `
-- Core entity nodes
CREATE TABLE IF NOT EXISTS nodes (
    id           TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    definition   TEXT NOT NULL,
    category     TEXT NOT NULL,
    course       TEXT NOT NULL DEFAULT '',
    exam_date    TEXT NOT NULL DEFAULT '',
    raw_latex    TEXT NOT NULL DEFAULT '[]',
    sources      TEXT NOT NULL DEFAULT '[]',
    created_at   TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_nodes_course ON nodes(course);

-- FTS5 virtual table for full-text search
CREATE VIRTUAL TABLE IF NOT EXISTS nodes_fts USING fts5(
    id,
    display_name,
    definition,
    category,
    course,
    content='nodes',
    content_rowid='rowid'
);

-- Sync triggers: keep FTS5 in sync with nodes table
CREATE TRIGGER IF NOT EXISTS nodes_ai AFTER INSERT ON nodes BEGIN
    INSERT INTO nodes_fts(rowid, id, display_name, definition, category, course)
    VALUES (new.rowid, new.id, new.display_name, new.definition, new.category, new.course);
END;

CREATE TRIGGER IF NOT EXISTS nodes_ad AFTER DELETE ON nodes BEGIN
    INSERT INTO nodes_fts(nodes_fts, rowid, id, display_name, definition, category, course)
    VALUES ('delete', old.rowid, old.id, old.display_name, old.definition, old.category, old.course);
END;

CREATE TRIGGER IF NOT EXISTS nodes_au AFTER UPDATE ON nodes BEGIN
    INSERT INTO nodes_fts(nodes_fts, rowid, id, display_name, definition, category, course)
    VALUES ('delete', old.rowid, old.id, old.display_name, old.definition, old.category, old.course);
    INSERT INTO nodes_fts(rowid, id, display_name, definition, category, course)
    VALUES (new.rowid, new.id, new.display_name, new.definition, new.category, new.course);
END;

-- Query history
CREATE TABLE IF NOT EXISTS query_log (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    query_text     TEXT NOT NULL,
    matched_nodes  TEXT NOT NULL DEFAULT '[]',
    timestamp      TEXT NOT NULL DEFAULT (datetime('now'))
);
`
	_, err := db.sql.Exec(schema)
	return err
}
