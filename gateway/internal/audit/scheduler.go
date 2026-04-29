package audit

// scheduler.go — Story 5.29c: FB-E5-07 — Audit Purge Scheduler (AC5)
// Story 5.29d: AC7 (FB-29c-3) — NewPurgeSchedulerWithJitter for multi-instance spread.
//
// PurgeScheduler runs RunCleanup on every tick delivered via its tickCh channel.
// In production the tick channel comes from time.NewTicker(24 * time.Hour).Tick().
// In tests the tick channel is a controllable chan time.Time so no real time passes.
//
// RunCleanupFunc is the injectable cleanup function signature. Production code wires
// RunCleanup from audit.go; tests inject a lightweight fake.
//
// Usage in main.go (fixed-interval, single instance):
//
//	cleanupFn := func(ctx context.Context) (int64, error) {
//	    return audit.RunCleanup(ctx, db, retentionDays)
//	}
//	ticker := time.NewTicker(24 * time.Hour)
//	scheduler := audit.NewPurgeScheduler(retentionDays, cleanupFn, ticker.C)
//	go scheduler.Start(ctx)
//
// Usage in main.go (jitter, multi-instance — preferred):
//
//	scheduler := audit.NewPurgeSchedulerWithJitter(retentionDays, cleanupFn, 24*time.Hour, 0.10)
//	go scheduler.Start(ctx)

import (
	"context"
	"log/slog"
	"math/rand"
	"time"
)

// RunCleanupFunc is the injectable cleanup function type.
// It is called on each tick; the scheduler does not pass DB or retentionDays directly —
// those are closed over by the production wrapper in main.go.
type RunCleanupFunc func(ctx context.Context) (int64, error)

// PurgeScheduler runs RunCleanup on each tick until the context is cancelled.
// Errors from RunCleanup are logged as warnings; the scheduler continues running.
//
// Two modes:
//   - External ticker mode (tickCh != nil): ticks come from an external channel
//     (production: ticker.C; tests: manual chan). Used by NewPurgeScheduler.
//   - Jitter mode (tickCh == nil): the scheduler generates its own sleeping goroutine
//     with a randomised interval per tick. Used by NewPurgeSchedulerWithJitter.
type PurgeScheduler struct {
	retentionDays  int
	cleanupFn      RunCleanupFunc
	tickCh         <-chan time.Time // nil in jitter mode
	baseInterval   time.Duration   // non-zero in jitter mode
	jitterFraction float64         // non-zero in jitter mode
}

// NewPurgeScheduler constructs a PurgeScheduler driven by an external ticker channel.
//
//   - retentionDays: logged on each tick for observability; not passed to cleanupFn
//     (cleanupFn closes over it in production).
//   - cleanupFn: the function to call on each tick (injectable for tests).
//   - tickCh: a channel of time.Time ticks (production: ticker.C; tests: manual chan).
func NewPurgeScheduler(retentionDays int, cleanupFn RunCleanupFunc, tickCh <-chan time.Time) *PurgeScheduler {
	return &PurgeScheduler{
		retentionDays: retentionDays,
		cleanupFn:     cleanupFn,
		tickCh:        tickCh,
	}
}

// NewPurgeSchedulerWithJitter constructs a PurgeScheduler where each tick interval is
// randomised by ±jitterFraction of baseInterval. This spreads simultaneous purge calls
// across multiple gateway instances so they don't all hit the DB at the same time.
//
//   - retentionDays: logged on each tick for observability.
//   - cleanupFn: the function to call on each tick.
//   - baseInterval: the nominal interval between purge runs (e.g. 24*time.Hour).
//   - jitterFraction: the relative jitter applied to each interval (0.10 = ±10%).
//     Each tick sleeps for base + jitter, where jitter ∈ [-jitterFraction*base, +jitterFraction*base].
//
// Production usage:
//
//	scheduler := audit.NewPurgeSchedulerWithJitter(retentionDays, cleanupFn, 24*time.Hour, 0.10)
//	go scheduler.Start(ctx)
func NewPurgeSchedulerWithJitter(retentionDays int, cleanupFn RunCleanupFunc, baseInterval time.Duration, jitterFraction float64) *PurgeScheduler {
	return &PurgeScheduler{
		retentionDays:  retentionDays,
		cleanupFn:      cleanupFn,
		baseInterval:   baseInterval,
		jitterFraction: jitterFraction,
	}
}

// nextInterval returns a randomised sleep duration for the jitter-mode scheduler.
// The result is in [base*(1-jitter), base*(1+jitter)].
func (p *PurgeScheduler) nextInterval() time.Duration {
	// rand.Float64() ∈ [0.0, 1.0) → jitterOffset ∈ (-jitterFraction, +jitterFraction)
	jitterOffset := (rand.Float64()*2 - 1) * p.jitterFraction
	return time.Duration(float64(p.baseInterval) * (1 + jitterOffset))
}

// Start begins the scheduler loop. It blocks until ctx is cancelled.
// Call as "go scheduler.Start(ctx)" in main.go.
//
// In external-ticker mode (NewPurgeScheduler): waits for ticks on tickCh.
// In jitter mode (NewPurgeSchedulerWithJitter): self-schedules each tick with
// a randomised interval using time.Sleep.
func (p *PurgeScheduler) Start(ctx context.Context) {
	if p.tickCh != nil {
		// External ticker mode (tests and legacy production usage).
		p.runExternalTicker(ctx)
		return
	}
	// Jitter mode: self-scheduled with randomised intervals.
	p.runJitterMode(ctx)
}

// runExternalTicker drives the scheduler from an external tick channel.
func (p *PurgeScheduler) runExternalTicker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.tickCh:
			p.runOnce(ctx)
		}
	}
}

// runJitterMode drives the scheduler with self-generated jittered intervals.
func (p *PurgeScheduler) runJitterMode(ctx context.Context) {
	for {
		interval := p.nextInterval()
		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
			p.runOnce(ctx)
		}
	}
}

// runOnce calls cleanupFn and logs the result.
func (p *PurgeScheduler) runOnce(ctx context.Context) {
	deleted, err := p.cleanupFn(ctx)
	if err != nil {
		slog.Warn("audit: purge tick failed", "retention_days", p.retentionDays, "err", err)
		return
	}
	slog.Info("audit: purge tick completed", "deleted_rows", deleted, "retention_days", p.retentionDays)
}
