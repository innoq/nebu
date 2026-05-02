package matrix

// ─── Story 7-26: User Interactive Authentication (UIA) ───────────────────────
//
// UIA is a Matrix challenge-response mechanism required before destructive
// operations such as device deletion.
//
// Nebu is OIDC-only, so the only supported UIA stage is "m.login.sso".
//
// Protocol:
//  1. Handler calls RequireUIA(w, r, userID).
//     - If no "auth" field in request body → write 401 challenge and return (false, "").
//     - If "auth.session" present but not completed for userID → write 401 and return (false, "").
//     - If completed → consume session and return (true, sessionID).
//  2. After real OIDC callback:
//     - The SSO callback handler calls CompleteUIASession(sessionID, userID) to mark done.
//
// Security:
//   - UIA session UUIDs are tied to a specific userID — cross-user reuse returns 401.
//   - TTL is 5 minutes; expired sessions are garbage-collected lazily.
//   - Session state is in-memory: acceptable for MVP. Phase 2: persistent store.
//
// Reusability:
//   - This module is designed to be reused by future endpoints (account deactivation etc.).
//   - approveUIASession (exported as package-internal) is used only by tests and
//     the SSO callback handler.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// uiaSessionTTL is the maximum lifetime of a pending UIA session.
const uiaSessionTTL = 5 * time.Minute

// uiaSessionEntry holds state for a pending or completed UIA session.
type uiaSessionEntry struct {
	userID    string
	completed bool
	exp       time.Time
}

// uiaStore is the global in-memory store for pending UIA sessions.
// Keyed by the opaque session UUID returned in the 401 challenge.
var uiaStore = &uiaSessionStore{sessions: make(map[string]uiaSessionEntry)}

type uiaSessionStore struct {
	mu       sync.Mutex
	sessions map[string]uiaSessionEntry
}

// newSessionID generates a cryptographically random 16-byte hex session ID.
func newSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// createSession allocates a new UIA session for userID and returns its ID.
func (s *uiaSessionStore) createSession(userID string) (string, error) {
	id, err := newSessionID()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Evict expired sessions lazily before adding a new one.
	now := time.Now()
	for k, v := range s.sessions {
		if now.After(v.exp) {
			delete(s.sessions, k)
		}
	}
	s.sessions[id] = uiaSessionEntry{
		userID:    userID,
		completed: false,
		exp:       now.Add(uiaSessionTTL),
	}
	return id, nil
}

// complete marks a session as completed. Returns false if the session doesn't
// exist, has expired, or belongs to a different user.
func (s *uiaSessionStore) complete(sessionID, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.sessions[sessionID]
	if !ok {
		return false
	}
	if time.Now().After(entry.exp) {
		delete(s.sessions, sessionID)
		return false
	}
	if entry.userID != userID {
		return false
	}
	entry.completed = true
	s.sessions[sessionID] = entry
	return true
}

// check returns true (and consumes the session) if the session is completed
// for the given userID. Returns false if unknown, expired, wrong user, or
// not yet completed.
func (s *uiaSessionStore) check(sessionID, userID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.sessions[sessionID]
	if !ok {
		return false
	}
	if time.Now().After(entry.exp) {
		delete(s.sessions, sessionID)
		return false
	}
	if entry.userID != userID {
		return false
	}
	if !entry.completed {
		return false
	}
	// Single-use: consume after successful check.
	delete(s.sessions, sessionID)
	return true
}

// approveUIASession creates a pre-approved UIA session for userID with the
// given sessionID. Used by test helpers and the SSO callback handler to
// signal that the user has completed re-authentication.
//
// Unlike the normal flow (client receives session from 401 challenge), this
// directly inserts a completed session into the store — enabling tests to
// bypass the UIA challenge step and test subsequent behavior.
func approveUIASession(sessionID, userID string) {
	uiaStore.mu.Lock()
	defer uiaStore.mu.Unlock()
	uiaStore.sessions[sessionID] = uiaSessionEntry{
		userID:    userID,
		completed: true,
		exp:       time.Now().Add(uiaSessionTTL),
	}
}

// uiaChallenge is the JSON body returned in a 401 UIA challenge response.
type uiaChallenge struct {
	Flows   []uiaFlow   `json:"flows"`
	Session string      `json:"session"`
	Params  interface{} `json:"params"`
}

type uiaFlow struct {
	Stages []string `json:"stages"`
}

// uiaAuthBody is the parsed "auth" field from the client's request body.
type uiaAuthBody struct {
	Auth *uiaAuth `json:"auth,omitempty"`
}

type uiaAuth struct {
	Type    string `json:"type"`
	Session string `json:"session"`
}

// writeUIAChallenge writes a 401 UIA challenge response and allocates a new
// session UUID tied to userID.
func writeUIAChallenge(w http.ResponseWriter, userID string) {
	sessionID, err := uiaStore.createSession(userID)
	if err != nil {
		writeMatrixError(w, http.StatusInternalServerError, "M_UNKNOWN", "Internal server error")
		return
	}
	challenge := uiaChallenge{
		Flows: []uiaFlow{
			{Stages: []string{"m.login.sso"}},
		},
		Session: sessionID,
		Params:  struct{}{},
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(challenge)
}

// checkUIACompleted inspects the request body for a completed UIA auth object.
//
// Returns (true, remainingBody) when UIA is satisfied — the handler may proceed.
// Returns (false, _) when UIA is not satisfied — the 401 challenge has already
// been written to w (or an error was written).
//
// The request body is read and buffered so the caller can re-read it.
// remainingBody is the full parsed body (auth field stripped out is irrelevant —
// callers should parse the original body separately).
func checkUIACompleted(w http.ResponseWriter, r *http.Request, userID string) (completed bool, parsedBody []byte) {
	// Read the body once.
	var bodyBuf []byte
	if r.Body != nil {
		decoder := json.NewDecoder(r.Body)
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			// No valid JSON body → UIA challenge.
			writeUIAChallenge(w, userID)
			return false, nil
		}
		bodyBuf = raw
	}

	if len(bodyBuf) == 0 {
		writeUIAChallenge(w, userID)
		return false, nil
	}

	// Try to parse the auth field.
	var authBody uiaAuthBody
	_ = json.Unmarshal(bodyBuf, &authBody)

	if authBody.Auth == nil || authBody.Auth.Session == "" {
		// No auth object or no session → issue challenge.
		writeUIAChallenge(w, userID)
		return false, bodyBuf
	}

	// Validate the auth session.
	if !uiaStore.check(authBody.Auth.Session, userID) {
		// Session not completed or doesn't belong to this user.
		writeUIAChallenge(w, userID)
		return false, bodyBuf
	}

	return true, bodyBuf
}
