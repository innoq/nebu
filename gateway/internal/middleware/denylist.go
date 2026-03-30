package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type denylistEntry struct {
	expiresAt time.Time
}

// Denylist is a thread-safe in-memory token denylist keyed by SHA-256 hash.
// Entries expire lazily when Contains is called.
type Denylist struct {
	entries sync.Map
}

func NewDenylist() *Denylist {
	return &Denylist{}
}

// Add registers a token hash with the given expiry. rawToken is hashed before storage.
func (d *Denylist) Add(rawToken string, expiresAt time.Time) {
	d.entries.Store(tokenHash(rawToken), denylistEntry{expiresAt: expiresAt})
}

// Contains returns true if the token is denylisted and not yet expired.
// Expired entries are removed lazily.
func (d *Denylist) Contains(rawToken string) bool {
	hash := tokenHash(rawToken)
	val, ok := d.entries.Load(hash)
	if !ok {
		return false
	}
	entry := val.(denylistEntry)
	if time.Now().After(entry.expiresAt) {
		d.entries.Delete(hash)
		return false
	}
	return true
}

func tokenHash(rawToken string) string {
	h := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(h[:])
}
