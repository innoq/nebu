// Package strategy provides DrainStrategy implementations for the message buffer.
// MVP: LinearStrategy — constant rate regardless of load (ADR-006).
// Phase 2: AIMD strategy (do NOT implement here).
package strategy

// LinearStrategy returns a constant drain rate regardless of load or buffer size.
// It satisfies the buffer.DrainStrategy interface.
//
// Per ADR-006: the Rate() return value is defined for Phase 2 pluggability;
// in MVP, callers drain immediately on WaitFor signal without rate limiting.
type LinearStrategy struct {
	baseRate float64 // events per second (constant)
}

// New creates a LinearStrategy with the given constant baseRate.
func New(baseRate float64) *LinearStrategy {
	return &LinearStrategy{baseRate: baseRate}
}

// Rate returns baseRate regardless of loadFactor or bufferSize.
// Signature matches buffer.DrainStrategy: Rate(loadFactor float64, bufferSize int64) float64.
func (l *LinearStrategy) Rate(loadFactor float64, bufferSize int64) float64 {
	return l.baseRate
}
