package graph

import (
	"math"

	"github.com/rs/zerolog/log"
	"github.com/truongvinh/biograph/internal/config"
	"github.com/truongvinh/biograph/internal/storage"
)

// PlasticityManager handles Hebbian reinforcement and exam-aware temporal decay.
type PlasticityManager struct {
	db  *storage.DB
	cfg *config.Config
}

func NewPlasticityManager(db *storage.DB, cfg *config.Config) *PlasticityManager {
	return &PlasticityManager{db: db, cfg: cfg}
}

// SigmoidUpdate computes the new edge weight using bounded Hebbian learning.
// w_new = 1 / (1 + exp(-(w_old + alpha - center)))
func SigmoidUpdate(wOld, alpha, center float64) float64 {
	return 1.0 / (1.0 + math.Exp(-(wOld + alpha - center)))
}

// RunDecay applies exam-aware temporal decay to all edges across all courses.
func (pm *PlasticityManager) RunDecay() error {
	if !pm.cfg.Plasticity.DecayEnabled {
		return nil
	}
	if err := pm.db.DecayAllEdges(); err != nil {
		log.Error().Err(err).Msg("temporal decay failed")
		return err
	}
	log.Info().Msg("temporal decay applied")
	return nil
}
