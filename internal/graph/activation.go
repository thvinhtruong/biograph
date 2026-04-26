package graph

import (
	"fmt"
	"sort"
	"sync"

	"github.com/rs/zerolog/log"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/storage"
)

// energyMap is a goroutine-safe float64 accumulator keyed by node ID.
type energyMap struct {
	mu sync.Mutex
	m  map[string]float64
}

func newEnergyMap() *energyMap {
	return &energyMap{m: make(map[string]float64)}
}

func (em *energyMap) seed(id string, val float64) {
	em.mu.Lock()
	em.m[id] = val
	em.mu.Unlock()
}

func (em *energyMap) add(id string, delta float64) {
	em.mu.Lock()
	em.m[id] += delta
	em.mu.Unlock()
}

func (em *energyMap) get(id string) float64 {
	em.mu.Lock()
	v := em.m[id]
	em.mu.Unlock()
	return v
}

func (em *energyMap) collectAbove(threshold float64) []string {
	em.mu.Lock()
	defer em.mu.Unlock()
	var ids []string
	for id, e := range em.m {
		if e >= threshold {
			ids = append(ids, id)
		}
	}
	return ids
}

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

	em := newEnergyMap()

	// Seed starting nodes with energy = 1.0
	for _, id := range startNodeIDs {
		em.seed(id, 1.0)
	}

	// Traverse hop by hop
	for range maxHops {
		frontier := em.collectAbove(minEnergy)
		if len(frontier) == 0 {
			break
		}

		var wg sync.WaitGroup
		for _, nodeID := range frontier {
			wg.Add(1)
			go func(nid string) {
				defer wg.Done()
				currentEnergy := em.get(nid)

				neighbors, err := e.db.GetNeighbors(nid)
				if err != nil {
					log.Warn().Err(err).Str("node", nid).Msg("neighbor fetch failed")
					return
				}

				for _, nb := range neighbors {
					transfer := currentEnergy * nb.Weight * decayPerHop
					if transfer < minEnergy {
						continue
					}
					em.add(nb.NodeID, transfer)
				}
			}(nodeID)
		}
		wg.Wait()
	}

	// Collect all activated nodes (above threshold)
	activated := em.collectAbove(minEnergy)
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
		pairs = append(pairs, pair{id: id, energy: em.get(id)})
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
// Both stored edge directions (i→j and j→i) are checked and updated.
func (e *ActivationEngine) Reinforce(activatedIDs []string) {
	alpha := e.cfg.Plasticity.ReinforcementAlpha
	center := e.cfg.Plasticity.SigmoidCenter

	for i := range activatedIDs {
		for j := i + 1; j < len(activatedIDs); j++ {
			src, dst := activatedIDs[i], activatedIDs[j]
			if edge, _ := e.db.GetEdge(src, dst); edge != nil {
				if err := e.db.UpdateEdgeWeight(src, dst, alpha, center); err != nil {
					log.Warn().Err(err).Msg("edge weight update failed")
				}
			}
			if edge, _ := e.db.GetEdge(dst, src); edge != nil {
				if err := e.db.UpdateEdgeWeight(dst, src, alpha, center); err != nil {
					log.Warn().Err(err).Msg("edge weight update failed")
				}
			}
		}
	}
}
