package matrix

import (
	"context"
	"encoding/json"
	"net/http"
)

// UserExistenceChecker checks whether a Matrix user ID exists in the users table.
// Defined by the consumer (this handler) per Go interface convention (ADR-009).
// The real implementation queries PostgreSQL; tests supply a fake.
type UserExistenceChecker interface {
	UserExists(ctx context.Context, userID string) (bool, error)
}

// KeysQueryConfig holds dependencies for NewKeysQueryHandler.
type KeysQueryConfig struct {
	UserChecker UserExistenceChecker
}

// KeysQueryHandler handles POST /_matrix/client/v3/keys/query.
//
// Story 5-29e scope: improved stub that distinguishes known users from unknown ones.
// For each queried userId that exists in the DB: an empty device_keys entry is returned
// (signals "user known, no devices registered" — allows clients to proceed with DM creation).
// For unknown users: they are omitted from device_keys (and left absent from failures too,
// matching spec behaviour for "user not found" in non-federated servers).
//
// Full E2EE device key storage is a future story.
//
// Source: tmp/test-findings.md 2026-04-23 — DM creation hangs because keys/query
// returns an empty device_keys map that omits all users, making FluffyChat unable
// to distinguish "user exists, no devices" from "user not found".
type KeysQueryHandler struct {
	checker UserExistenceChecker
}

// NewKeysQueryHandler constructs a KeysQueryHandler from the provided config.
func NewKeysQueryHandler(cfg KeysQueryConfig) *KeysQueryHandler {
	return &KeysQueryHandler{checker: cfg.UserChecker}
}

// PostKeysQuery handles POST /_matrix/client/v3/keys/query.
//
// Flow (5-29e improved stub):
//  1. requireJSON (415 on wrong Content-Type) — consistent with other POST handlers.
//  2. Decode request body: {"device_keys": {"<userId>": [<deviceIds>...]}}.
//  3. For each requested userId:
//     - If user exists in DB → include empty inner map in response device_keys.
//     - If user not found → omit from device_keys (unknown user).
//  4. Return 200 {"device_keys": {...}, "failures": {}}.
func (h *KeysQueryHandler) PostKeysQuery(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	var body struct {
		DeviceKeys map[string][]string `json:"device_keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	deviceKeys := make(map[string]map[string]any)
	failures := make(map[string]any)

	for userID := range body.DeviceKeys {
		exists, err := h.checker.UserExists(r.Context(), userID)
		if err != nil {
			// On DB error: treat as failure for this user rather than crashing.
			failures[userID] = map[string]any{
				"errcode": "M_UNKNOWN",
				"error":   "Internal server error checking user",
			}
			continue
		}
		if exists {
			// User known, no devices registered yet (E2EE stub).
			deviceKeys[userID] = map[string]any{}
		}
		// Unknown users are silently omitted from device_keys per non-federated spec behaviour.
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"device_keys": deviceKeys,
		"failures":    failures,
	})
}
