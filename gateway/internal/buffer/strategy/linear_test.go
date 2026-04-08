package strategy

// Story 4-16: message_buffer Drain Strategy — Linear MVP
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until linear.go is implemented.
//
// Tests cover:
//   - LinearStrategy.Rate always returns the configured baseRate (constant)
//   - LinearStrategy satisfies the DrainStrategy interface via Rate signature

import (
	"testing"
)

// ─── AC #5 (story): LinearStrategy returns constant rate ─────────────────────

// TestLinearStrategy_Interval verifies that Rate() returns the configured
// baseRate regardless of the loadFactor or bufferSize inputs.
// Per ADR-006 and AC #5: MVP is constant — ignores all inputs.
//
// Fails until LinearStrategy is implemented in linear.go.
func TestLinearStrategy_Interval(t *testing.T) {
	const baseRate = 100.0
	s := New(baseRate)

	cases := []struct {
		name        string
		loadFactor  float64
		bufferSize  int64
		wantRate    float64
	}{
		{name: "zero load", loadFactor: 0.0, bufferSize: 0, wantRate: baseRate},
		{name: "mid load", loadFactor: 0.5, bufferSize: 500, wantRate: baseRate},
		{name: "high load", loadFactor: 0.99, bufferSize: 9999, wantRate: baseRate},
		{name: "full load", loadFactor: 1.0, bufferSize: 10000, wantRate: baseRate},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := s.Rate(tc.loadFactor, tc.bufferSize)
			if got != tc.wantRate {
				t.Errorf("Rate(%v, %v) = %v, want %v", tc.loadFactor, tc.bufferSize, got, tc.wantRate)
			}
		})
	}
}

// TestLinearStrategy_Type verifies that New returns a non-nil *LinearStrategy
// and that its Rate method is callable (satisfies the interface).
//
// Fails until LinearStrategy is implemented in linear.go.
func TestLinearStrategy_Type(t *testing.T) {
	s := New(42.0)
	if s == nil {
		t.Fatal("New() returned nil, want *LinearStrategy")
	}

	// Rate must be callable and return the configured base rate.
	got := s.Rate(0.0, 0)
	if got != 42.0 {
		t.Errorf("Rate() = %v, want 42.0", got)
	}
}
