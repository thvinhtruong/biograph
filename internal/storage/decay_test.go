package storage

import "testing"

func TestDecayFactor(t *testing.T) {
	tests := []struct {
		name         string
		daysUntil    float64
		wantFactor   float64
		description  string
	}{
		{"long term retention", 60, 0.999, "days > 30 → 0.999"},
		{"exam prep boundary", 31, 0.999, "days > 30 → 0.999"},
		{"exam prep", 14, 0.9999, "7 < days ≤ 30 → 0.9999"},
		{"exam prep boundary low", 7.1, 0.9999, "7 < days ≤ 30 → 0.9999"},
		{"exam imminent high", 6, 1.0, "0 < days ≤ 7 → 1.0 (freeze)"},
		{"exam imminent low", 0.5, 1.0, "0 < days ≤ 7 → 1.0 (freeze)"},
		{"exam day", 0, 0.995, "days = 0 → post-exam cooldown"},
		{"post exam cooldown", -3, 0.995, "-7 < days ≤ 0 → 0.995"},
		{"post exam boundary", -6.9, 0.995, "-7 < days ≤ 0 → 0.995"},
		{"aggressive decay", -7, 0.98, "days ≤ -7 → 0.98"},
		{"long after exam", -30, 0.98, "days ≤ -7 → 0.98"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decayFactor(tc.daysUntil)
			if got != tc.wantFactor {
				t.Errorf("decayFactor(%v) = %v, want %v (%s)", tc.daysUntil, got, tc.wantFactor, tc.description)
			}
		})
	}
}

func TestDecayFactorMonotonicallyIncreasing(t *testing.T) {
	// Decay should be more aggressive the further past the exam we are.
	// Ordering by exam-proximity: far future > near future > exam day > post-exam > far past
	farFuture := decayFactor(60)
	nearFuture := decayFactor(14)
	imminent := decayFactor(3)
	postExam := decayFactor(-3)
	farPast := decayFactor(-30)

	if farFuture > nearFuture {
		t.Errorf("farFuture(%v) > nearFuture(%v): unexpected", farFuture, nearFuture)
	}
	if nearFuture > imminent {
		t.Errorf("nearFuture(%v) > imminent(%v): unexpected", nearFuture, imminent)
	}
	if imminent < postExam {
		t.Errorf("imminent(%v) should be ≥ postExam(%v)", imminent, postExam)
	}
	if postExam < farPast {
		t.Errorf("postExam(%v) should be ≥ farPast(%v)", postExam, farPast)
	}
}

func TestDecayFactorRange(t *testing.T) {
	days := []float64{-60, -14, -7, -3, 0, 1, 4, 7, 14, 30, 60, 365}
	for _, d := range days {
		f := decayFactor(d)
		if f <= 0 || f > 1.0 {
			t.Errorf("decayFactor(%v) = %v: must be in (0, 1]", d, f)
		}
	}
}
