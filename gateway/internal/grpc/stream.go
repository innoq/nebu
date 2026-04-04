package grpc

import (
	"context"
	"log/slog"
	"time"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

const (
	defaultEventBusInitialBackoff = 1 * time.Second
	defaultEventBusMaxBackoff     = 30 * time.Second
)

// EventBusStreamOption configures an EventBusStream.
type EventBusStreamOption func(*EventBusStream)

// WithMinBackoff sets the initial backoff duration for reconnect attempts.
// Default: 1 second. Useful for overriding in tests.
func WithMinBackoff(d time.Duration) EventBusStreamOption {
	return func(s *EventBusStream) {
		s.minBackoff = d
	}
}

// WithMaxBackoff sets the maximum backoff duration cap for reconnect attempts.
// Default: 30 seconds.
func WithMaxBackoff(d time.Duration) EventBusStreamOption {
	return func(s *EventBusStream) {
		s.maxBackoff = d
	}
}

// EventBusStream manages a persistent gRPC server-streaming connection for the
// EventBus RPC (ADR-005: one stream per Go gateway instance, not per client).
//
// On startup (Start), it launches a background goroutine that calls EventBus on
// the CoreServiceClient and enters a Recv loop. When the stream is lost, it
// retries with exponential backoff until the context is cancelled.
// Events are forwarded to a buffered channel returned by Events().
type EventBusStream struct {
	client     pb.CoreServiceClient
	nodeID     string
	events     chan *pb.Event
	minBackoff time.Duration
	maxBackoff time.Duration
}

// NewEventBusStream creates an EventBusStream. Call Start() to begin consuming.
func NewEventBusStream(client pb.CoreServiceClient, nodeID string, opts ...EventBusStreamOption) *EventBusStream {
	s := &EventBusStream{
		client:     client,
		nodeID:     nodeID,
		events:     make(chan *pb.Event, 256), // buffered to absorb bursts
		minBackoff: defaultEventBusInitialBackoff,
		maxBackoff: defaultEventBusMaxBackoff,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Events returns the read-only channel of received events.
// Downstream consumers (e.g., message_buffer, Story 4-16) read from this channel.
func (s *EventBusStream) Events() <-chan *pb.Event {
	return s.events
}

// Start launches the EventBus receive loop in a background goroutine and returns
// immediately. The goroutine reconnects with exponential backoff on stream errors
// and exits when ctx is cancelled.
//
// Logs state transitions:
//   - slog.Info("EventBus stream connected") on successful connect
//   - slog.Warn("EventBus stream lost, retrying", "backoff_ms", ...) on error
func (s *EventBusStream) Start(ctx context.Context) {
	go s.runLoop(ctx)
}

// runLoop is the background goroutine started by Start. It reconnects with
// exponential backoff until ctx is cancelled.
func (s *EventBusStream) runLoop(ctx context.Context) {
	defer close(s.events)

	backoff := s.minBackoff
	for {
		if ctx.Err() != nil {
			return
		}

		connected, err := s.runOnce(ctx)
		if err == nil || ctx.Err() != nil {
			// runOnce returned nil error (ctx cancelled) or ctx is done.
			return
		}

		// Reset backoff after a successful connection — the previous failure
		// streak is over and the next retry should start fresh.
		if connected {
			backoff = s.minBackoff
		}

		slog.Warn("EventBus stream lost, retrying",
			"node_id", s.nodeID,
			"backoff_ms", backoff.Milliseconds(),
			"err", err)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return
		}

		// Double backoff for next attempt, capped at maxBackoff.
		backoff = minDuration(backoff*2, s.maxBackoff)
	}
}

// runOnce opens a single EventBus stream and receives until error or ctx cancel.
// Returns (true, nil) when ctx is cancelled.
// Returns (true, err) when the stream was connected but later disconnected.
// Returns (false, err) when the initial connection attempt failed.
// The boolean indicates whether a connection was established (backoff should reset).
func (s *EventBusStream) runOnce(ctx context.Context) (connected bool, retErr error) {
	stream, err := s.client.EventBus(ctx, &pb.EventBusRequest{NodeId: s.nodeID})
	if err != nil {
		return false, err
	}
	slog.Info("EventBus stream connected", "node_id", s.nodeID)

	for {
		event, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return true, nil
			}
			return true, err
		}

		select {
		case s.events <- event:
		case <-ctx.Done():
			return true, nil
		default:
			slog.Warn("EventBus events channel full, dropping event",
				"room_id", event.RoomId,
				"event_id", event.EventId)
		}
	}
}

// minDuration returns the smaller of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
