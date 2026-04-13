package search

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/llm"
	"github.com/truongvinh/biograph/internal/storage"
)

// Router decides which starting nodes to use for spreading activation.
type Router struct {
	db  *storage.DB
	cfg *config.Config
}

func NewRouter(db *storage.DB, cfg *config.Config) *Router {
	return &Router{db: db, cfg: cfg}
}

// Route returns up to 5 starting node IDs for a query.
// Uses Bleve first; falls back to LLM router if confidence is low.
func (r *Router) Route(query, course string) ([]string, error) {
	// Step 1: Try Bleve BM25 search
	idx, err := OpenIndex(r.cfg)
	if err != nil {
		log.Warn().Err(err).Msg("bleve unavailable, falling back to LLM router")
		return r.llmRoute(query, course)
	}
	defer idx.Close()

	results, err := idx.Search(query, course, 5)
	if err == nil && len(results) > 0 {
		topScore := results[0].Score
		if topScore >= r.cfg.Search.BleveConfidenceThreshold {
			ids := make([]string, 0, len(results))
			for _, res := range results {
				ids = append(ids, res.ID)
			}
			log.Debug().Strs("nodes", ids).Float64("score", topScore).Msg("bleve route")
			return ids, nil
		}
	}

	// Step 2: Bleve confidence too low → LLM router
	if r.cfg.Search.LLMFallbackEnabled {
		return r.llmRoute(query, course)
	}

	// Step 3: FTS5 fallback
	return r.ftsRoute(query)
}

func (r *Router) llmRoute(query, course string) ([]string, error) {
	// Collect a sample of top entity IDs to help the LLM
	topNodes, err := r.db.ListTopNodes(50)
	if err != nil || len(topNodes) == 0 {
		return nil, fmt.Errorf("no nodes in graph yet")
	}

	samples := make([]string, 0, len(topNodes))
	for _, n := range topNodes {
		if course == "" || n.Course == course {
			samples = append(samples, n.ID)
		}
	}
	if len(samples) == 0 {
		for _, n := range topNodes {
			samples = append(samples, n.ID)
		}
	}

	prompt := fmt.Sprintf(`You are a knowledge graph router. Given an academic question, extract the core concepts and map them to entity IDs.

Available entity IDs:
%s

User query: "%s"

Respond with a JSON array of entity IDs that are most relevant (1-5 IDs maximum):
["entity_id_1", "entity_id_2"]

Rules:
- Use snake_case for entity IDs
- Only return IDs from the provided list
- Return [] if nothing matches`, strings.Join(samples, ", "), query)

	client := llm.NewClient(r.cfg)
	response, err := client.Route(prompt)
	if err != nil {
		return r.ftsRoute(query)
	}

	// Parse JSON array
	response = strings.TrimSpace(response)
	// Extract JSON array if wrapped in other text
	start := strings.Index(response, "[")
	end := strings.LastIndex(response, "]")
	if start != -1 && end != -1 && end > start {
		response = response[start : end+1]
	}

	var ids []string
	if err := json.Unmarshal([]byte(response), &ids); err != nil {
		log.Warn().Err(err).Str("response", response).Msg("LLM router parse failed")
		return r.ftsRoute(query)
	}

	// Validate: only return IDs that actually exist
	valid := make([]string, 0, len(ids))
	for _, id := range ids {
		node, _ := r.db.GetNode(id)
		if node != nil {
			valid = append(valid, id)
		}
	}

	if len(valid) == 0 {
		return r.ftsRoute(query)
	}

	log.Debug().Strs("nodes", valid).Msg("LLM route")
	return valid, nil
}

func (r *Router) ftsRoute(query string) ([]string, error) {
	nodes, err := r.db.FTSSearch(query, 5)
	if err != nil || len(nodes) == 0 {
		return nil, fmt.Errorf("no matching nodes found for query: %q", query)
	}
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids, nil
}
