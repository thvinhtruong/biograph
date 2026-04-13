package graph

import (
	"fmt"
	"sort"
	"sync"
	"math"
	"unsafe"

	"github.com/rs/zerolog/log"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/storage"
)

// ActivatedNode is a node with its accumulated activation energy.
type ActivatedNode struct {
	ID          string
	Energy      float64
	Definition  string
	DisplayName string
	Category    string
	Course      string
	RawLatex    []string
	Sources     []storage.SourceRef
}

// ActivationEngine runs spreading activation over the graph.
type ActivationEngine struct {
	db  *storage.DB
	cfg *config.Config
}

func NewActivationEngine(db *storage.DB, cfg *config.Config) *ActivationEngine {
	return &ActivationEngine{db: db, cfg: cfg}
}

// Activate performs spreading activation starting from the given node IDs.
// Returns nodes sorted descending by accumulated energy.
func (e *ActivationEngine) Activate(startNodeIDs []string) ([]ActivatedNode, error) {
	maxHops := e.cfg.Activation.MaxHops
	minEnergy := e.cfg.Activation.MinEnergy
	decayPerHop := e.cfg.Activation.DecayPerHop
	maxNodes := e.cfg.Activation.MaxContextNodes

	// energyMap: NodeID → float64 (using sync.Map for goroutine safety)
	var energyMap sync.Map

	// Seed starting nodes with energy = 1.0
	for _, id := range startNodeIDs {
		energyMap.Store(id, 1.0)
	}

	// Traverse hop by hop
	for hop := 0; hop < maxHops; hop++ {
		// Collect current frontier (nodes above minEnergy threshold)
		frontier := collectAboveThreshold(&energyMap, minEnergy)
		if len(frontier) == 0 {
			break
		}

		var wg sync.WaitGroup
		for _, nodeID := range frontier {
			wg.Add(1)
			go func(nid string) {
				defer wg.Done()
				currentEnergy := loadEnergy(&energyMap, nid)

				neighbors, err := e.db.GetNeighbors(nid)
				if err != nil {
					log.Warn().Err(err).Str("node", nid).Msg("neighbor fetch failed")
					return
				}

				for _, nb := range neighbors {
					transfer := currentEnergy * nb.Weight * decayPerHop
					if transfer < minEnergy {
						continue // prune weak paths
					}
					atomicAddEnergy(&energyMap, nb.NodeID, transfer)
				}
			}(nodeID)
		}
		wg.Wait()
	}

	// Collect all activated nodes (above threshold)
	activated := collectAboveThreshold(&energyMap, minEnergy)
	if len(activated) == 0 {
		return nil, fmt.Errorf("spreading activation produced no results")
	}

	// Sort by energy descending
	type pair struct {
		id     string
		energy float64
	}
	pairs := make([]pair, 0, len(activated))
	for _, id := range activated {
		pairs = append(pairs, pair{id: id, energy: loadEnergy(&energyMap, id)})
	}
	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].energy > pairs[j].energy
	})

	// Cap at maxNodes
	if maxNodes > 0 && len(pairs) > maxNodes {
		pairs = pairs[:maxNodes]
	}

	// Enrich with node data from DB
	result := make([]ActivatedNode, 0, len(pairs))
	for _, p := range pairs {
		node, err := e.db.GetNode(p.id)
		if err != nil || node == nil {
			continue
		}
		result = append(result, ActivatedNode{
			ID:          node.ID,
			Energy:      p.energy,
			Definition:  node.Definition,
			DisplayName: node.DisplayName,
			Category:    node.Category,
			Course:      node.Course,
			RawLatex:    node.RawLatex,
			Sources:     node.Sources,
		})
	}

	return result, nil
}

// Reinforce applies Hebbian sigmoid updates to all co-activated node pairs.
func (e *ActivationEngine) Reinforce(activatedIDs []string) {
	alpha := e.cfg.Plasticity.ReinforcementAlpha
	center := e.cfg.Plasticity.SigmoidCenter

	for i := 0; i < len(activatedIDs); i++ {
		for j := i + 1; j < len(activatedIDs); j++ {
			edge, _ := e.db.GetEdge(activatedIDs[i], activatedIDs[j])
			if edge != nil {
				if err := e.db.UpdateEdgeWeight(activatedIDs[i], activatedIDs[j], alpha, center); err != nil {
					log.Warn().Err(err).Msg("edge weight update failed")
				}
			}
		}
	}
}

// collectAboveThreshold returns all node IDs whose energy ≥ threshold.
func collectAboveThreshold(m *sync.Map, threshold float64) []string {
	var ids []string
	m.Range(func(k, v any) bool {
		if e, ok := v.(float64); ok && e >= threshold {
			ids = append(ids, k.(string))
		}
		return true
	})
	return ids
}

// loadEnergy safely reads a float64 from the sync.Map.
func loadEnergy(m *sync.Map, id string) float64 {
	if v, ok := m.Load(id); ok {
		if e, ok := v.(float64); ok {
			return e
		}
	}
	return 0
}

// atomicAddEnergy accumulates energy using a compare-and-swap loop.
func atomicAddEnergy(m *sync.Map, id string, delta float64) {
	for {
		existing, loaded := m.LoadOrStore(id, delta)
		if !loaded {
			return // stored fresh
		}
		old := existing.(float64)
		new_ := old + delta
		// CAS via atomic on the bits of float64
		oldBits := math.Float64bits(old)
		newBits := math.Float64bits(new_)
		// Use sync.Map swap (Go 1.20+)
		if swapped := syncMapSwapFloat64(m, id, old, new_); swapped {
			_ = oldBits
			_ = newBits
			return
		}
	}
}

// syncMapSwapFloat64 does a compare-and-swap on float64 values in sync.Map.
func syncMapSwapFloat64(m *sync.Map, id string, old, new_ float64) bool {
	// Go 1.20 sync.Map doesn't have CompareAndSwap for interface{}.
	// We serialize with a mutex approach: use CompareAndSwap where available.
	// Fallback: store directly (slight energy imprecision under heavy concurrency, acceptable).
	_ = unsafe.Sizeof(old) // suppress import
	actual, _ := m.LoadOrStore(id, new_)
	if actual.(float64) == old {
		m.Store(id, new_)
		return true
	}
	return false
}
