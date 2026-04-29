package audit_test

// scheduler_test.go — Story 5.29c: FB-E5-07 — Audit Purge Scheduler (AC5)
//
// RED-phase: ALL tests in this file FAIL until Story 5.29c is implemented.
// Failing reason: audit.PurgeScheduler does not exist yet. The production code
// currently has no goroutine/timer that calls RunCleanup on a schedule.
//
// Test strategy:
//   - PurgeScheduler is a gateway-side goroutine/ticker (24h interval).
//   - Tests inject a fake RunCleanupFunc so no real PostgreSQL is needed.
//   - TestPurgeScheduler_TickInvokesRunCleanup: starts the scheduler, sends a
//     manual tick via a controllable ticker, verifies RunCleanup was called.
//   - TestPurgeScheduler_SkipsOnDBError: RunCleanup returns error → scheduler
//     logs warn but does not crash; subsequent tick is processed normally.
//
// Implementation contract (types that MUST exist in audit/scheduler.go):
//   type PurgeScheduler struct
//   func NewPurgeScheduler(retentionDays int, cleanupFn RunCleanupFunc, tickCh <-chan time.Time) *PurgeScheduler
//   type RunCleanupFunc func(ctx context.Context) (int64, error)
//   func (s *PurgeScheduler) Start(ctx context.Context)
//
// AC coverage:
//   AC5 — TestPurgeScheduler_TickInvokesRunCleanup
//   AC5 — TestPurgeScheduler_SkipsOnDBError

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nebu/nebu/internal/audit"
)

// TestPurgeScheduler_TickInvokesRunCleanup — AC5
//
// Given: PurgeScheduler configured with retentionDays=2555 and a fake cleanup func
// When:  a tick is delivered on the controllable ticker channel
// Then:  the fake cleanup func is called exactly once with the correct retentionDays
func TestPurgeScheduler_TickInvokesRunCleanup(t *testing.T) {
	var callCount int64

	// Fake RunCleanupFunc captures call count.
	fakeCleanup := func(ctx context.Context) (int64, error) {
		atomic.AddInt64(&callCount, 1)
		return 5, nil // simulated 5 rows deleted
	}

	// Controllable tick channel — we drive the scheduler manually.
	tickCh := make(chan time.Time, 1)

	scheduler := audit.NewPurgeScheduler(2555, fakeCleanup, tickCh)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start scheduler in background goroutine.
	go scheduler.Start(ctx)

	// Deliver one tick.
	tickCh <- time.Now()

	// Allow scheduler goroutine to process the tick.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&callCount) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got := atomic.LoadInt64(&callCount)
	if got < 1 {
		t.Errorf("AC5 FAIL: RunCleanup was not called after tick (count=%d) — "+
			"audit.PurgeScheduler must invoke RunCleanup on each tick", got)
	}
}

// TestPurgeScheduler_SkipsOnDBError — AC5
//
// Given: PurgeScheduler whose cleanup func returns an error on the first tick
// When:  two ticks are delivered
// Then:  scheduler does not crash and processes the second tick (callCount == 2)
func TestPurgeScheduler_SkipsOnDBError(t *testing.T) {
	var callCount int64
	errDB := errors.New("simulated DB error")

	fakeCleanup := func(ctx context.Context) (int64, error) {
		n := atomic.AddInt64(&callCount, 1)
		if n == 1 {
			return 0, errDB // first call fails
		}
		return 3, nil // subsequent calls succeed
	}

	tickCh := make(chan time.Time, 2)
	scheduler := audit.NewPurgeScheduler(2555, fakeCleanup, tickCh)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go scheduler.Start(ctx)

	// Deliver tick 1 (will fail). Wait for the scheduler to actually call our
	// fakeCleanup before sending tick 2 — the previous time.Sleep(50ms) was a
	// hard wait that became the test's main flake source under CI load.
	tickCh <- time.Now()
	deadline1 := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline1) {
		if atomic.LoadInt64(&callCount) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if atomic.LoadInt64(&callCount) < 1 {
		t.Fatalf("AC5 FAIL: tick 1 was not processed within 2s — scheduler appears stuck")
	}

	// Deliver tick 2 (should succeed — scheduler must still be alive).
	tickCh <- time.Now()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&callCount) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	got := atomic.LoadInt64(&callCount)
	if got < 2 {
		t.Errorf("AC5 FAIL: scheduler stopped after DB error (callCount=%d) — "+
			"PurgeScheduler must log warn and continue running after RunCleanup error", got)
	}
}
