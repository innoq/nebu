// Package buffer provides a per-user in-memory ring buffer for EventBus events.
// It absorbs event spikes during burst load and delivers them to /sync long-poll
// handlers at a controlled rate (Story 4-16).
//
// Thread-safe: all public methods lock the internal mutex before accessing shared state.
package buffer

import (
	"context"
	"sync"

	pb "github.com/nebu/nebu/internal/grpc/pb"
	"github.com/prometheus/client_golang/prometheus"
)

// MessageBuffer provides per-user ring buffers for EventBus events.
// Each user gets an independent slice (ring buffer via re-slicing) that drops the
// oldest event when capacity is exceeded.
//
// Thread-safe; all methods are safe for concurrent use.
type MessageBuffer struct {
	mu       sync.Mutex
	capacity int
	buffers  map[string][]*pb.Event   // per-user event slice
	notify   map[string]chan struct{}  // per-user notify channels (closed to wake WaitFor)
	overflow prometheus.Counter
}

// NewMessageBuffer creates a MessageBuffer with the given capacity per user.
// reg is the Prometheus registerer (use prometheus.DefaultRegisterer in production,
// prometheus.NewRegistry() in tests to avoid global state pollution).
func NewMessageBuffer(capacity int, reg prometheus.Registerer) *MessageBuffer {
	overflow := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "nebu_buffer_overflow_total",
		Help: "Total number of events dropped from the per-user ring buffer due to overflow.",
	})
	reg.MustRegister(overflow)

	return &MessageBuffer{
		capacity: capacity,
		buffers:  make(map[string][]*pb.Event),
		notify:   make(map[string]chan struct{}),
		overflow: overflow,
	}
}

// Put appends event to the ring buffer for userID.
// If the user's buffer is at capacity, the oldest event is dropped and the
// overflow counter is incremented.
// After appending, all callers blocked in WaitFor are unblocked.
func (m *MessageBuffer) Put(userID string, event *pb.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()

	buf := m.buffers[userID]
	if len(buf) >= m.capacity {
		// Drop oldest (index 0) to make room
		buf = buf[1:]
		m.overflow.Inc()
	}
	m.buffers[userID] = append(buf, event)

	// Notify waiters: swap channel before closing to prevent double-close on
	// concurrent Puts (the mutex guarantees only one Put runs at a time, but
	// the swap pattern is the canonical safe approach).
	oldCh := m.notifyFor(userID)
	m.notify[userID] = make(chan struct{}) // new channel for future waiters
	close(oldCh)                           // wake all current waiters — safe, mutex held
}

// DrainFor returns up to maxEvents events from userID's buffer in FIFO order and
// removes them. Non-blocking; returns nil if no events are present for userID.
func (m *MessageBuffer) DrainFor(userID string, maxEvents int) []*pb.Event {
	m.mu.Lock()
	defer m.mu.Unlock()

	buf := m.buffers[userID]
	if len(buf) == 0 {
		return nil
	}

	n := len(buf)
	if maxEvents < n {
		n = maxEvents
	}

	result := make([]*pb.Event, n)
	copy(result, buf[:n])
	m.buffers[userID] = buf[n:]
	return result
}

// WaitFor returns a channel that is closed when at least one event is available
// for userID, or when ctx is cancelled. The caller must never close the returned
// channel. After WaitFor returns, call DrainFor to retrieve events.
//
// Goroutine-safe: WaitFor spawns one lightweight goroutine per call that exits
// as soon as either the notify channel or ctx.Done() fires.
func (m *MessageBuffer) WaitFor(ctx context.Context, userID string) <-chan struct{} {
	m.mu.Lock()
	ch := m.notifyFor(userID)
	m.mu.Unlock()

	// combined closes when either an event arrives (ch) or ctx is cancelled.
	combined := make(chan struct{})
	go func() {
		defer close(combined)
		select {
		case <-ch:
		case <-ctx.Done():
		}
	}()
	return combined
}

// notifyFor returns the current notify channel for userID, creating it if absent.
// Must be called with m.mu held.
func (m *MessageBuffer) notifyFor(userID string) chan struct{} {
	ch, ok := m.notify[userID]
	if !ok {
		ch = make(chan struct{})
		m.notify[userID] = ch
	}
	return ch
}
