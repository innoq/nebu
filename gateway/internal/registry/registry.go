package registry

import (
	"sync"
	"time"
)

// NodeEntry represents a registered Elixir core node.
type NodeEntry struct {
	Addr         string    `json:"addr"`
	RegisteredAt time.Time `json:"registered_at"`
}

// Registry is a thread-safe in-memory store for registered Elixir core nodes.
type Registry struct {
	mu    sync.RWMutex
	nodes map[string]NodeEntry
}

// New creates a new empty Registry.
func New() *Registry {
	return &Registry{nodes: make(map[string]NodeEntry)}
}

// Register adds or updates a node entry keyed by addr.
func (r *Registry) Register(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nodes[addr] = NodeEntry{Addr: addr, RegisteredAt: time.Now().UTC()}
}

// List returns a snapshot of all registered node entries.
func (r *Registry) List() []NodeEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := make([]NodeEntry, 0, len(r.nodes))
	for _, e := range r.nodes {
		entries = append(entries, e)
	}
	return entries
}
