package main

// claim_loader_test.go — Story 11-10 MAJOR-3 fix: TTL-cached claimLoader unit tests.
//
// Verifies that:
//   - Two rapid successive calls only invoke loadFn once (cache hit on second call).
//   - After TTL expiry a third call invokes loadFn again.
//   - Stale value is returned on loadFn error (fail-open).

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestClaimLoader_CachesValueWithinTTL verifies that two rapid successive calls to
// claimLoader.get() only invoke loadFn once — the second call is served from the cache.
// MAJOR-3 fix (Story 11-10): per-request DB queries replaced by TTL-cached loader.
func TestClaimLoader_CachesValueWithinTTL(t *testing.T) {
	var callCount atomic.Int32
	loader := &claimLoader{
		ttl: 60 * time.Second,
		loadFn: func(_ context.Context) (string, error) {
			callCount.Add(1)
			return "preferred_username", nil
		},
	}

	ctx := context.Background()

	v1 := loader.get(ctx)
	if v1 != "preferred_username" {
		t.Errorf("first get: expected 'preferred_username', got %q", v1)
	}

	v2 := loader.get(ctx)
	if v2 != "preferred_username" {
		t.Errorf("second get: expected 'preferred_username', got %q", v2)
	}

	// loadFn must have been called exactly once — the second call is a cache hit.
	if n := callCount.Load(); n != 1 {
		t.Errorf("expected loadFn called once, got %d", n)
	}
}

// TestClaimLoader_RefreshesAfterTTLExpiry verifies that once the TTL has elapsed the
// next call to get() invokes loadFn again to refresh the cached value.
func TestClaimLoader_RefreshesAfterTTLExpiry(t *testing.T) {
	var callCount atomic.Int32
	loader := &claimLoader{
		ttl: 10 * time.Millisecond, // very short TTL for test
		loadFn: func(_ context.Context) (string, error) {
			callCount.Add(1)
			return "sub", nil
		},
	}

	ctx := context.Background()

	_ = loader.get(ctx) // first call — fills cache
	time.Sleep(20 * time.Millisecond) // wait for TTL to expire
	_ = loader.get(ctx) // second call — should refresh

	if n := callCount.Load(); n != 2 {
		t.Errorf("expected loadFn called twice (once per TTL window), got %d", n)
	}
}

// TestClaimLoader_ReturnsStaleOnError verifies that when loadFn returns an error the
// cached (stale) value is returned (fail-open). The error is logged internally by get().
func TestClaimLoader_ReturnsStaleOnError(t *testing.T) {
	callCount := 0
	loader := &claimLoader{
		ttl: 10 * time.Millisecond,
		loadFn: func(_ context.Context) (string, error) {
			callCount++
			if callCount == 1 {
				return "name", nil // first call succeeds — populates cache
			}
			return "", errors.New("db connection lost")
		},
	}

	ctx := context.Background()

	v1 := loader.get(ctx) // first call — cache populated with "name"
	if v1 != "name" {
		t.Fatalf("first call: expected 'name', got %q", v1)
	}

	time.Sleep(20 * time.Millisecond) // expire TTL

	// second call — loadFn errors; stale value returned (fail-open).
	// Error is logged internally by get(); callers do not receive it.
	v2 := loader.get(ctx)
	// Stale value "name" must be returned (fail-open — don't block auth on transient DB error).
	if v2 != "name" {
		t.Errorf("expected stale value 'name' on error, got %q", v2)
	}
}
