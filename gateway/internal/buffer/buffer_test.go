package buffer

// Story 4-16: message_buffer Drain Strategy — Linear MVP
//
// These tests are written FIRST (ATDD gate), before implementation exists.
// ALL tests in this file are expected to FAIL until buffer.go is implemented.
//
// Tests cover:
//   - MessageBuffer.Put + DrainFor round-trip: FIFO order
//   - MessageBuffer.Put + DrainFor per-user isolation
//   - MessageBuffer.DrainFor on user with no events returns nil/empty
//   - MessageBuffer.WaitFor is unblocked after Put
//   - MessageBuffer.WaitFor is unblocked on context cancellation
//   - Ring buffer overflow: oldest event dropped, counter incremented

import (
	"context"
	"testing"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/prometheus/client_golang/prometheus"
)

// newTestBuffer creates a MessageBuffer with a fresh isolated Prometheus
// registry (never DefaultRegisterer) to avoid test pollution.
func newTestBuffer(t *testing.T, capacity int) *MessageBuffer {
	t.Helper()
	reg := prometheus.NewRegistry()
	return NewMessageBuffer(capacity, reg)
}

// makeEvent constructs a minimal *pb.Event for use in tests.
func makeEvent(id, roomID string) *pb.Event {
	return &pb.Event{
		EventId: id,
		RoomId:  roomID,
	}
}

// ─── AC #1 (story): Put + DrainFor round-trip — FIFO order ──────────────────

// TestMessageBuffer_Put_DrainFor verifies that events are returned in FIFO
// order and that a subsequent DrainFor returns an empty slice.
//
// Fails until MessageBuffer is implemented in buffer.go.
func TestMessageBuffer_Put_DrainFor(t *testing.T) {
	buf := newTestBuffer(t, 10)
	userA := "@alice:test.local"

	event1 := makeEvent("$ev1", "!room:test.local")
	event2 := makeEvent("$ev2", "!room:test.local")

	buf.Put(userA, event1)
	buf.Put(userA, event2)

	got := buf.DrainFor(userA, 10)

	if len(got) != 2 {
		t.Fatalf("DrainFor: got %d events, want 2", len(got))
	}
	if got[0].EventId != "$ev1" {
		t.Errorf("first event: got %q, want $ev1", got[0].EventId)
	}
	if got[1].EventId != "$ev2" {
		t.Errorf("second event: got %q, want $ev2", got[1].EventId)
	}

	// Second DrainFor must return empty (events consumed).
	second := buf.DrainFor(userA, 10)
	if len(second) != 0 {
		t.Errorf("second DrainFor: got %d events, want 0", len(second))
	}
}

// ─── AC #6 (story): Per-user isolation ───────────────────────────────────────

// TestMessageBuffer_Put_MultipleUsers verifies that events put for user A are
// not returned when DrainFor is called for user B, and vice versa.
//
// Fails until MessageBuffer is implemented in buffer.go.
func TestMessageBuffer_Put_MultipleUsers(t *testing.T) {
	buf := newTestBuffer(t, 10)
	userA := "@alice:test.local"
	userB := "@bob:test.local"

	eventA := makeEvent("$evA", "!room:test.local")
	eventB := makeEvent("$evB", "!room:test.local")

	buf.Put(userA, eventA)
	buf.Put(userB, eventB)

	gotA := buf.DrainFor(userA, 10)
	if len(gotA) != 1 {
		t.Fatalf("DrainFor(A): got %d events, want 1", len(gotA))
	}
	if gotA[0].EventId != "$evA" {
		t.Errorf("DrainFor(A): got event %q, want $evA", gotA[0].EventId)
	}

	// Bob's event must still be present and only Bob's event.
	gotB := buf.DrainFor(userB, 10)
	if len(gotB) != 1 {
		t.Fatalf("DrainFor(B): got %d events, want 1", len(gotB))
	}
	if gotB[0].EventId != "$evB" {
		t.Errorf("DrainFor(B): got event %q, want $evB", gotB[0].EventId)
	}
}

// ─── AC #1 (story): DrainFor on user with no events ─────────────────────────

// TestMessageBuffer_DrainFor_EmptyUser verifies that DrainFor called for a
// user that has never had an event returns nil or an empty slice (not a panic).
//
// Fails until MessageBuffer is implemented in buffer.go.
func TestMessageBuffer_DrainFor_EmptyUser(t *testing.T) {
	buf := newTestBuffer(t, 10)

	got := buf.DrainFor("@ghost:test.local", 10)
	if len(got) != 0 {
		t.Errorf("DrainFor(unknown user): got %d events, want 0", len(got))
	}
}

// ─── AC #3 (story): WaitFor unblocks after Put ───────────────────────────────

// TestMessageBuffer_WaitFor_NotifiesOnPut verifies that a goroutine waiting on
// WaitFor is unblocked (channel closed) after Put is called for that user.
//
// Fails until MessageBuffer is implemented in buffer.go.
func TestMessageBuffer_WaitFor_NotifiesOnPut(t *testing.T) {
	buf := newTestBuffer(t, 10)
	userA := "@alice:test.local"

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// WaitFor before any events are present — must block until Put.
	// Use a ready-channel so Put only fires after WaitFor has registered, eliminating the timing race.
	ready := make(chan struct{})
	go func() {
		<-ready // wait until WaitFor has been called
		buf.Put(userA, makeEvent("$ev1", "!room:test.local"))
	}()
	ch := buf.WaitFor(ctx, userA)
	close(ready) // signal the goroutine to Put

	select {
	case <-ch:
		// channel closed — WaitFor correctly unblocked on Put
	case <-ctx.Done():
		t.Fatal("WaitFor was not unblocked within 500ms after Put")
	}
}

// ─── AC #4 (story): WaitFor respects context cancellation ───────────────────

// TestMessageBuffer_WaitFor_ContextCancel verifies that when the context is
// cancelled before any Put is called, the channel returned by WaitFor is
// closed (caller unblocks without deadlock or goroutine leak).
//
// Fails until MessageBuffer is implemented in buffer.go.
func TestMessageBuffer_WaitFor_ContextCancel(t *testing.T) {
	buf := newTestBuffer(t, 10)
	userA := "@alice:test.local"

	ctx, cancel := context.WithCancel(context.Background())

	ch := buf.WaitFor(ctx, userA)

	// Cancel context immediately — no Put is called.
	cancel()

	// Channel must close promptly (within a reasonable deadline).
	deadline := time.After(500 * time.Millisecond)
	select {
	case <-ch:
		// channel closed on context cancellation — correct
	case <-deadline:
		t.Fatal("WaitFor channel was not closed within 500ms after context cancellation (possible goroutine leak)")
	}
}

// ─── AC #2 (story): Ring buffer overflow drops oldest ────────────────────────

// TestMessageBuffer_RingBuffer_Overflow verifies that when the ring buffer is
// full and a new event is Put, the oldest event is dropped (not the newest),
// and the overflow Prometheus counter is incremented by 1.
//
// Fails until MessageBuffer ring buffer logic and Prometheus counter are
// implemented in buffer.go.
func TestMessageBuffer_RingBuffer_Overflow(t *testing.T) {
	reg := prometheus.NewRegistry()
	// Capacity of 2: after Put(event1), Put(event2), Put(event3) the buffer
	// should contain [event2, event3] and have dropped event1.
	buf := NewMessageBuffer(2, reg)
	userA := "@alice:test.local"

	event1 := makeEvent("$ev1", "!room:test.local")
	event2 := makeEvent("$ev2", "!room:test.local")
	event3 := makeEvent("$ev3", "!room:test.local") // causes overflow

	buf.Put(userA, event1)
	buf.Put(userA, event2)
	buf.Put(userA, event3) // event1 should be dropped here

	got := buf.DrainFor(userA, 10)
	if len(got) != 2 {
		t.Fatalf("DrainFor after overflow: got %d events, want 2", len(got))
	}
	if got[0].EventId != "$ev2" {
		t.Errorf("first event after overflow: got %q, want $ev2 (oldest dropped)", got[0].EventId)
	}
	if got[1].EventId != "$ev3" {
		t.Errorf("second event after overflow: got %q, want $ev3", got[1].EventId)
	}

	// Verify the overflow counter was incremented exactly once.
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error: %v", err)
	}

	var overflowValue float64
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "nebu_buffer_overflow_total" {
			found = true
			overflowValue = mf.GetMetric()[0].GetCounter().GetValue()
			break
		}
	}
	if !found {
		t.Fatal("nebu_buffer_overflow_total counter not found in registry")
	}
	if overflowValue != 1.0 {
		t.Errorf("nebu_buffer_overflow_total: got %v, want 1 (exactly one drop)", overflowValue)
	}
}
