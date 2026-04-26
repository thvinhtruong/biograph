package graph

import (
	"math"
	"testing"
)

func TestSigmoidUpdate(t *testing.T) {
	alpha := 0.05
	center := 0.5
	// Fixed point of sigmoid(w + alpha - center): where w_new == w_old.
	// For alpha=0.05, center=0.5 this sits near 0.51.
	// Below the fixed point weights rise; above it they fall back — bounded Hebbian.

	t.Run("low weight increases toward attractor", func(t *testing.T) {
		got := SigmoidUpdate(0.0, alpha, center)
		if got <= 0.0 {
			t.Errorf("SigmoidUpdate(0.0) = %v: expected positive value", got)
		}
		// sigmoid(-0.45) ≈ 0.389
		if got < 0.37 || got > 0.41 {
			t.Errorf("SigmoidUpdate(0.0) = %v: want ~0.389", got)
		}
	})

	t.Run("mid weight increases past center", func(t *testing.T) {
		got := SigmoidUpdate(0.5, alpha, center)
		if got <= 0.5 {
			t.Errorf("SigmoidUpdate(0.5) = %v: expected > 0.5 (below fixed point)", got)
		}
		// sigmoid(0.05) ≈ 0.512
		if got < 0.50 || got > 0.52 {
			t.Errorf("SigmoidUpdate(0.5) = %v: want ~0.512", got)
		}
	})

	t.Run("high weight pulled back toward attractor", func(t *testing.T) {
		got := SigmoidUpdate(0.9, alpha, center)
		// sigmoid(0.45) ≈ 0.611 — weight decreases from 0.9 toward the fixed point
		if got >= 0.9 {
			t.Errorf("SigmoidUpdate(0.9) = %v: expected pull-back below 0.9", got)
		}
		if got < 0.60 || got > 0.62 {
			t.Errorf("SigmoidUpdate(0.9) = %v: want ~0.611", got)
		}
	})

	t.Run("output strictly in (0,1)", func(t *testing.T) {
		for _, w := range []float64{0.0, 0.25, 0.5, 0.75, 1.0} {
			got := SigmoidUpdate(w, alpha, center)
			if got <= 0 || got >= 1 {
				t.Errorf("SigmoidUpdate(%v) = %v: must be in open interval (0,1)", w, got)
			}
		}
	})

	t.Run("monotone in w_old", func(t *testing.T) {
		// sigmoid(w + alpha - center) is strictly increasing — higher w_old always gives higher w_new
		prev := SigmoidUpdate(0.0, alpha, center)
		for _, w := range []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9} {
			got := SigmoidUpdate(w, alpha, center)
			if got <= prev {
				t.Errorf("SigmoidUpdate not monotone: f(%v)=%v not > f(prev)=%v", w, got, prev)
			}
			prev = got
		}
	})

	t.Run("repeated application converges to fixed point, never reaches 1", func(t *testing.T) {
		for _, start := range []float64{0.01, 0.5, 0.99} {
			w := start
			for range 1000 {
				w = SigmoidUpdate(w, alpha, center)
			}
			if w >= 1.0 {
				t.Errorf("start=%v: weight reached 1.0 after 1000 updates: %v", start, w)
			}
			if math.IsNaN(w) || math.IsInf(w, 0) {
				t.Errorf("start=%v: weight became non-finite: %v", start, w)
			}
			// All starting points should converge near the fixed point ~0.51
			if w < 0.50 || w > 0.52 {
				t.Errorf("start=%v: converged to %v, expected near fixed point ~0.51", start, w)
			}
		}
	})
}

func TestEnergyMapConcurrent(t *testing.T) {
	const goroutines = 100
	const delta = 0.1

	em := newEnergyMap()

	done := make(chan struct{})
	for range goroutines {
		go func() {
			em.add("node-a", delta)
			done <- struct{}{}
		}()
	}
	for range goroutines {
		<-done
	}

	got := em.get("node-a")
	want := float64(goroutines) * delta
	// allow 1e-9 tolerance for float64 summation
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("concurrent adds: got %v, want %v", got, want)
	}
}

func TestEnergyMapCollectAbove(t *testing.T) {
	em := newEnergyMap()
	em.seed("high", 0.8)
	em.seed("mid", 0.05)
	em.seed("low", 0.01)

	above := em.collectAbove(0.05)
	found := map[string]bool{}
	for _, id := range above {
		found[id] = true
	}

	if !found["high"] {
		t.Error("expected 'high' (0.8) to be above threshold 0.05")
	}
	if !found["mid"] {
		t.Error("expected 'mid' (0.05) to be at/above threshold 0.05")
	}
	if found["low"] {
		t.Error("expected 'low' (0.01) to be pruned by threshold 0.05")
	}
}

func TestEnergyMapSeed(t *testing.T) {
	em := newEnergyMap()
	em.seed("n1", 1.0)
	em.seed("n1", 0.5) // overwrite

	if got := em.get("n1"); got != 0.5 {
		t.Errorf("seed overwrite: got %v, want 0.5", got)
	}

	if got := em.get("missing"); got != 0 {
		t.Errorf("missing key: got %v, want 0", got)
	}
}
