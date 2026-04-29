package audit

// scheduler.go — Story 5.29c: FB-E5-07 — Audit Purge Scheduler (AC5)
//
// PurgeScheduler runs RunCleanup on every tick delivered via its tickCh channel.
// In production the tick channel comes from time.NewTicker(24 * time.Hour).Tick().
// In tests the tick channel is a controllable chan time.Time so no real time passes.
//
// RunCleanupFunc is the injectable cleanup function signature. Production code wires
// RunCleanup from audit.go; tests inject a lightweight fake.
//
// Usage in main.go:
//
//	cleanupFn := func(ctx context.Context) (int64, error) {
//	    return audit.RunCleanup(ctx, db, retentionDays)
//	}
//	ticker := time.NewTicker(24 * time.Hour)
//	scheduler := audit.NewPurgeScheduler(retentionDays, cleanupFn, ticker.C)
//	go scheduler.Start(ctx)

import (
	"context"
	"log/slog"
	"time"
)

// RunCleanupFunc is the injectable cleanup function type.
// It is called on each tick; the scheduler does not pass DB or retentionDays directly —
// those are closed over by the production wrapper in main.go.
type RunCleanupFunc func(ctx context.Context) (int64, error)

// PurgeScheduler runs RunCleanup on each tick from tickCh until the context is cancelled.
// Errors from RunCleanup are logged as warnings; the scheduler continues running.
type PurgeScheduler struct {
	retentionDays int
	cleanupFn     RunCleanupFunc
	tickCh        <-chan time.Time
}

// NewPurgeScheduler constructs a PurgeScheduler.
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

// Start begins the scheduler loop. It blocks until ctx is cancelled.
// Call as "go scheduler.Start(ctx)" in main.go.
func (p *PurgeScheduler) Start(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.tickCh:
			deleted, err := p.cleanupFn(ctx)
			if err != nil {
				slog.Warn("audit: purge tick failed", "retention_days", p.retentionDays, "err", err)
				continue
			}
			slog.Info("audit: purge tick completed", "deleted_rows", deleted, "retention_days", p.retentionDays)
		}
	}
}
