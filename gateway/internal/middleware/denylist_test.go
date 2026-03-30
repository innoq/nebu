package middleware_test

import (
	"testing"
	"time"

	"github.com/nebu/nebu/internal/middleware"
)

func TestDenylist_ContainsMissingToken(t *testing.T) {
	d := middleware.NewDenylist()
	if d.Contains("unknown-token") {
		t.Error("expected false for unknown token")
	}
}

func TestDenylist_AddAndContains(t *testing.T) {
	d := middleware.NewDenylist()
	d.Add("my-token", time.Now().Add(time.Hour))
	if !d.Contains("my-token") {
		t.Error("expected true after Add")
	}
}

func TestDenylist_ExpiredEntry(t *testing.T) {
	d := middleware.NewDenylist()
	d.Add("expired-token", time.Now().Add(-time.Second))
	if d.Contains("expired-token") {
		t.Error("expected false for expired token")
	}
}

func TestDenylist_TwoDistinctTokens(t *testing.T) {
	d := middleware.NewDenylist()
	d.Add("token-a", time.Now().Add(time.Hour))
	if d.Contains("token-b") {
		t.Error("adding token-a should not deny token-b")
	}
}

func TestDenylist_RepeatedAdd(t *testing.T) {
	d := middleware.NewDenylist()
	expiry := time.Now().Add(time.Hour)
	d.Add("repeat-token", expiry)
	d.Add("repeat-token", expiry)
	if !d.Contains("repeat-token") {
		t.Error("expected true after repeated Add")
	}
}
