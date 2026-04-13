package storage

import (
	"fmt"
	"math"
	"time"
)

// Edge represents a synaptic connection between two nodes.
type Edge struct {
	SourceID  string
	TargetID  string
	Weight    float64
	EdgeType  string
	LastFired string
	CreatedAt string
}

// NeighborResult is a neighbor node with its edge metadata.
type NeighborResult struct {
	NodeID    string
	Weight    float64
	EdgeType  string
	Definition string
}

// UpsertEdge inserts an edge or updates its weight/type if it already exists.
func (db *DB) UpsertEdge(e *Edge) error {
	_, err := db.sql.Exec(`
		INSERT INTO edges (source_id, target_id, weight, edge_type)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(source_id, target_id) DO UPDATE SET
		    weight    = MAX(weight, excluded.weight),
		    edge_type = excluded.edge_type`,
		e.SourceID, e.TargetID, e.Weight, e.EdgeType,
	)
	return err
}

// GetEdge returns a specific edge between two nodes, or nil if missing.
func (db *DB) GetEdge(sourceID, targetID string) (*Edge, error) {
	row := db.sql.QueryRow(`
		SELECT source_id, target_id, weight, edge_type, last_fired, created_at
		FROM edges WHERE source_id = ? AND target_id = ?`, sourceID, targetID)

	e := &Edge{}
	err := row.Scan(&e.SourceID, &e.TargetID, &e.Weight, &e.EdgeType, &e.LastFired, &e.CreatedAt)
	if err != nil {
		return nil, nil // not found
	}
	return e, nil
}

// GetNeighbors returns all neighbors of a node sorted by descending weight.
// Bidirectional: returns nodes where nodeID is source OR target.
func (db *DB) GetNeighbors(nodeID string) ([]NeighborResult, error) {
	rows, err := db.sql.Query(`
		SELECT
		    CASE WHEN e.source_id = ? THEN e.target_id ELSE e.source_id END AS neighbor_id,
		    e.weight,
		    e.edge_type,
		    n.definition
		FROM edges e
		JOIN nodes n ON n.id = CASE WHEN e.source_id = ? THEN e.target_id ELSE e.source_id END
		WHERE e.source_id = ? OR e.target_id = ?
		ORDER BY e.weight DESC`,
		nodeID, nodeID, nodeID, nodeID,
	)
	if err != nil {
		return nil, fmt.Errorf("get neighbors: %w", err)
	}
	defer rows.Close()

	var results []NeighborResult
	for rows.Next() {
		var r NeighborResult
		if err := rows.Scan(&r.NodeID, &r.Weight, &r.EdgeType, &r.Definition); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// UpdateEdgeWeight applies a Hebbian sigmoid update to a specific edge.
// w_new = 1 / (1 + exp(-(w_old + alpha - center)))
func (db *DB) UpdateEdgeWeight(sourceID, targetID string, alpha, center float64) error {
	_, err := db.sql.Exec(`
		UPDATE edges
		SET weight     = 1.0 / (1.0 + exp(-(weight + ? - ?))),
		    last_fired = ?
		WHERE source_id = ? AND target_id = ?`,
		alpha, center,
		time.Now().UTC().Format(time.RFC3339),
		sourceID, targetID,
	)
	return err
}

// DecayEdgesForCourse multiplies all edge weights for a course by decayFactor.
func (db *DB) DecayEdgesForCourse(course string, decayFactor float64) error {
	_, err := db.sql.Exec(`
		UPDATE edges
		SET weight = MAX(0.01, weight * ?)
		WHERE source_id IN (SELECT id FROM nodes WHERE course = ?)`,
		decayFactor, course,
	)
	return err
}

// DecayAllEdges applies exam-aware decay across all courses.
func (db *DB) DecayAllEdges() error {
	// Fetch distinct courses with exam dates
	rows, err := db.sql.Query(`
		SELECT course, exam_date
		FROM nodes
		WHERE exam_date != ''
		GROUP BY course, exam_date`)
	if err != nil {
		return err
	}
	defer rows.Close()

	now := time.Now()
	for rows.Next() {
		var course, examDateStr string
		if err := rows.Scan(&course, &examDateStr); err != nil {
			continue
		}
		examDate, err := time.Parse("2006-01-02", examDateStr)
		if err != nil {
			continue
		}

		daysUntil := examDate.Sub(now).Hours() / 24
		factor := decayFactor(daysUntil)
		if err := db.DecayEdgesForCourse(course, factor); err != nil {
			return err
		}
	}
	return nil
}

func decayFactor(daysUntilExam float64) float64 {
	_ = math.Exp // ensure math is used
	switch {
	case daysUntilExam > 30:
		return 0.999   // long-term retention
	case daysUntilExam > 7:
		return 0.9999  // exam prep — near-zero decay
	case daysUntilExam > 0:
		return 1.0     // exam imminent — freeze weights
	case daysUntilExam > -7:
		return 0.995   // post-exam cooldown
	default:
		return 0.98    // aggressive post-exam decay
	}
}
