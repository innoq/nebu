package middleware_test

import (
	"testing"
	"time"

	"github.com/nebu/nebu/internal/middleware"
)

func TestDenylist_IsInvalidated_UnknownToken(t *testing.T) {
	d := middleware.NewDenylist()
	if d.IsInvalidated("unknown-token") {
		t.Error("expected false for unknown token")
	}
}

func TestDenylist_InvalidateAndContains(t *testing.T) {
	d := middleware.NewDenylist()
	if err := d.Invalidate("my-token", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Invalidate returned unexpected error: %v", err)
	}
	if !d.IsInvalidated("my-token") {
		t.Error("expected true after Invalidate")
	}
}

func TestDenylist_ExpiredEntry(t *testing.T) {
	d := middleware.NewDenylist()
	_ = d.Invalidate("expired-token", time.Now().Add(-time.Second))
	if d.IsInvalidated("expired-token") {
		t.Error("expected false for expired token")
	}
}

func TestDenylist_TwoDistinctTokens(t *testing.T) {
	d := middleware.NewDenylist()
	_ = d.Invalidate("token-a", time.Now().Add(time.Hour))
	if d.IsInvalidated("token-b") {
		t.Error("invalidating token-a should not affect token-b")
	}
}

func TestDenylist_RepeatedInvalidate(t *testing.T) {
	d := middleware.NewDenylist()
	expiry := time.Now().Add(time.Hour)
	_ = d.Invalidate("repeat-token", expiry)
	_ = d.Invalidate("repeat-token", expiry)
	if !d.IsInvalidated("repeat-token") {
		t.Error("expected true after repeated Invalidate")
	}
}
