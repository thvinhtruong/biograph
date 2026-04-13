package search

import (
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/storage"
)

// SearchResult is a ranked result from Bleve.
type SearchResult struct {
	ID          string
	DisplayName string
	Definition  string
	Course      string
	Category    string
	Score       float64
}

// Index wraps a Bleve index for entity search.
type Index struct {
	bleveIdx bleve.Index
	cfg      *config.Config
}

// entityDoc is the document structure indexed by Bleve.
type entityDoc struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Definition  string `json:"definition"`
	Category    string `json:"category"`
	Course      string `json:"course"`
}

// OpenIndex opens an existing Bleve index, or creates one if it doesn't exist.
func OpenIndex(cfg *config.Config) (*Index, error) {
	path := cfg.Search.BleveIndexPath
	idx, err := bleve.Open(path)
	if err == bleve.ErrorIndexPathDoesNotExist {
		idx, err = createIndex(path)
		if err != nil {
			return nil, fmt.Errorf("create bleve index: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("open bleve index: %w", err)
	}
	return &Index{bleveIdx: idx, cfg: cfg}, nil
}

func createIndex(path string) (bleve.Index, error) {
	mapping := bleve.NewIndexMapping()
	entityMapping := bleve.NewDocumentMapping()

	// Boost display_name 2× for title matches
	nameField := bleve.NewTextFieldMapping()
	nameField.Analyzer = "standard"
	entityMapping.AddFieldMappingsAt("display_name", nameField)

	defField := bleve.NewTextFieldMapping()
	defField.Analyzer = "standard"
	entityMapping.AddFieldMappingsAt("definition", defField)

	catField := bleve.NewKeywordFieldMapping()
	entityMapping.AddFieldMappingsAt("category", catField)

	courseField := bleve.NewKeywordFieldMapping()
	entityMapping.AddFieldMappingsAt("course", courseField)

	mapping.AddDocumentMapping("entity", entityMapping)
	return bleve.New(path, mapping)
}

// Close closes the Bleve index.
func (idx *Index) Close() error {
	return idx.bleveIdx.Close()
}

// IndexNode adds or updates a node in the Bleve index.
func (idx *Index) IndexNode(n *storage.Node) error {
	doc := entityDoc{
		ID:          n.ID,
		DisplayName: n.DisplayName,
		Definition:  n.Definition,
		Category:    n.Category,
		Course:      n.Course,
	}
	return idx.bleveIdx.Index(n.ID, doc)
}

// Search performs a fuzzy BM25 query and returns ranked results.
func (idx *Index) Search(term, course string, limit int) ([]SearchResult, error) {
	query := bleve.NewMatchQuery(term)
	query.Fuzziness = 1

	searchReq := bleve.NewSearchRequestOptions(query, limit, 0, false)
	searchReq.Fields = []string{"id", "display_name", "definition", "category", "course"}

	searchResult, err := idx.bleveIdx.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("bleve search: %w", err)
	}

	var results []SearchResult
	for _, hit := range searchResult.Hits {
		r := SearchResult{
			ID:    hit.ID,
			Score: hit.Score,
		}
		if v, ok := hit.Fields["display_name"].(string); ok {
			r.DisplayName = v
		}
		if v, ok := hit.Fields["definition"].(string); ok {
			r.Definition = v
		}
		if v, ok := hit.Fields["category"].(string); ok {
			r.Category = v
		}
		if v, ok := hit.Fields["course"].(string); ok {
			r.Course = v
		}
		// Course filter (post-filter since Bleve keyword fields aren't always filtered)
		if course != "" && r.Course != course {
			continue
		}
		results = append(results, r)
	}
	return results, nil
}

// TopScore returns the highest score from a search, or 0 if no results.
func (idx *Index) TopScore(term string) (float64, error) {
	results, err := idx.Search(term, "", 1)
	if err != nil {
		return 0, err
	}
	if len(results) == 0 {
		return 0, nil
	}
	return results[0].Score, nil
}
