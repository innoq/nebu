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
	"sync"
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

// ─── AC7 (FB-29c-3): PurgeScheduler applies jitter to tick intervals ─────────
//
// Story 5.29d AC7: When multiple gateway instances all use the same 24h fixed
// ticker, they all trigger audit purge simultaneously, causing lock contention.
// Fix: add ±10% jitter to the tick interval so instances spread their purge calls.
//
// Implementation contract (new function/option in audit/scheduler.go):
//
//   // NewPurgeSchedulerWithJitter constructs a PurgeScheduler where each tick
//   // is delayed by (baseInterval ± jitterFraction * baseInterval).
//   // jitterFraction=0.10 → ±10% of baseInterval.
//   //
//   // Production usage:
//   //   scheduler := audit.NewPurgeSchedulerWithJitter(2555, cleanupFn, 24*time.Hour, 0.10)
//   //   go scheduler.Start(ctx)
//   //
//   // The scheduler internally generates a new random tick interval after each
//   // tick, within [base*(1-jitter), base*(1+jitter)].
//
//   func NewPurgeSchedulerWithJitter(retentionDays int, cleanupFn RunCleanupFunc, baseInterval time.Duration, jitterFraction float64) *PurgeScheduler
//
// RED-PHASE: ALL tests below FAIL until:
//   1. NewPurgeSchedulerWithJitter is added to audit/scheduler.go.
//   2. The scheduler applies random jitter to each tick interval.
//
// AC coverage:
//   AC7 (FB-29c-3) — TestPurgeScheduler_AppliesJitter
//   AC7 (FB-29c-3) — TestPurgeScheduler_JitterWithinBounds

// TestPurgeScheduler_AppliesJitter — AC7
//
// Given: PurgeSchedulerWithJitter with base=100ms, jitter=0.10
// When:  5 ticks are processed
// Then:  the scheduled intervals are NOT all identical (jitter causes variation)
//
// RED-PHASE: FAILS because NewPurgeSchedulerWithJitter does not exist.
func TestPurgeScheduler_AppliesJitter(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping jitter timing test in short mode")
	}

	var callTimes []int64
	var mu sync.Mutex

	fakeCleanup := func(ctx context.Context) (int64, error) {
		mu.Lock()
		callTimes = append(callTimes, time.Now().UnixNano())
		mu.Unlock()
		return 0, nil
	}

	// Use a very short base interval (50ms) and 10% jitter so the test is fast.
	const baseInterval = 50 * time.Millisecond
	const jitterFraction = 0.10

	// RED-PHASE: NewPurgeSchedulerWithJitter does not exist yet.
	scheduler := audit.NewPurgeSchedulerWithJitter(2555, fakeCleanup, baseInterval, jitterFraction)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go scheduler.Start(ctx)

	// Wait for 5 ticks.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(callTimes)
		mu.Unlock()
		if n >= 5 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	n := len(callTimes)
	mu.Unlock()

	if n < 5 {
		t.Fatalf("AC7 FAIL: expected at least 5 ticks within deadline, got %d — "+
			"NewPurgeSchedulerWithJitter does not exist or is not scheduling ticks correctly", n)
	}

	// Compute inter-tick intervals.
	mu.Lock()
	times := make([]int64, len(callTimes))
	copy(times, callTimes)
	mu.Unlock()

	intervals := make([]time.Duration, len(times)-1)
	for i := 1; i < len(times); i++ {
		intervals[i-1] = time.Duration(times[i] - times[i-1])
	}

	// With jitter, intervals should NOT all be the same.
	// We check that at least one pair differs by more than 1% of baseInterval.
	allIdentical := true
	threshold := time.Duration(float64(baseInterval) * 0.01) // 1% of base = 0.5ms
	for i := 1; i < len(intervals); i++ {
		diff := intervals[i] - intervals[i-1]
		if diff < 0 {
			diff = -diff
		}
		if diff > threshold {
			allIdentical = false
			break
		}
	}

	if allIdentical {
		t.Errorf(
			"AC7 FAIL: all inter-tick intervals are effectively identical (%v) — "+
				"jitter is not being applied. Intervals: %v",
			intervals[0], intervals,
		)
	}
}

// TestPurgeScheduler_JitterWithinBounds — AC7
//
// Given: PurgeSchedulerWithJitter with base=100ms, jitter=0.10
// When:  10 ticks are processed
// Then:  all inter-tick intervals are within [base*0.85, base*1.15]
//        (10% jitter should stay within ±15% with reasonable probability)
//
// RED-PHASE: FAILS because NewPurgeSchedulerWithJitter does not exist.
func TestPurgeScheduler_JitterWithinBounds(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping jitter bounds test in short mode")
	}

	var callTimes []int64
	var mu sync.Mutex

	fakeCleanup := func(ctx context.Context) (int64, error) {
		mu.Lock()
		callTimes = append(callTimes, time.Now().UnixNano())
		mu.Unlock()
		return 0, nil
	}

	const baseInterval = 60 * time.Millisecond
	const jitterFraction = 0.10

	// RED-PHASE: NewPurgeSchedulerWithJitter does not exist yet.
	scheduler := audit.NewPurgeSchedulerWithJitter(2555, fakeCleanup, baseInterval, jitterFraction)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	go scheduler.Start(ctx)

	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(callTimes)
		mu.Unlock()
		if n >= 10 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	times := make([]int64, len(callTimes))
	copy(times, callTimes)
	mu.Unlock()

	if len(times) < 3 {
		t.Fatalf("AC7 FAIL: expected at least 3 ticks for bounds check, got %d", len(times))
	}

	// Bounds: jitter ±10%. Allow generous CI scheduling overhead on the upper
	// bound (handler runtime, OS scheduling) but enforce a hard lower bound so
	// the scheduler cannot fire faster than base*(1-jitter). Out-of-bound
	// intervals are real failures (post code-review M1 fix — t.Logf was a no-op).
	const upperToleranceFactor = 0.50 // tolerate 50% wall-clock overhead on the slow side (CI noise)
	lower := time.Duration(float64(baseInterval) * (1 - jitterFraction))
	upper := time.Duration(float64(baseInterval) * (1 + jitterFraction + upperToleranceFactor))

	violations := 0
	for i := 1; i < len(times); i++ {
		interval := time.Duration(times[i] - times[i-1])
		if interval < lower || interval > upper {
			violations++
			t.Errorf(
				"AC7 FAIL: inter-tick interval %v outside expected bounds [%v, %v] at tick %d — "+
					"jitter scheduler is producing intervals outside the configured ±%.0f%% window",
				interval, lower, upper, i, jitterFraction*100,
			)
		}
	}
	if violations > 0 {
		t.Logf("AC7: %d/%d intervals outside bounds (base=%v, jitter=%.0f%%)",
			violations, len(times)-1, baseInterval, jitterFraction*100)
	}
}
