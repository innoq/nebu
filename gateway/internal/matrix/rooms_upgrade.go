package matrix

import (
	"encoding/json"
	"net/http"
)

// UpgradeRoomConfig holds dependencies for NewUpgradeRoomHandler.
// The 5-29e scope is a 501 stub — no gRPC client is needed yet.
type UpgradeRoomConfig struct {
	ServerName string
}

// UpgradeRoomHandler handles POST /_matrix/client/v3/rooms/{roomId}/upgrade.
//
// Story 5-29e scope: returns 501 M_UNRECOGNIZED (stub).
// Full room-upgrade implementation (tombstone + new room + state copy) is a follow-up story.
type UpgradeRoomHandler struct {
	serverName string
}

// NewUpgradeRoomHandler constructs an UpgradeRoomHandler from the provided config.
func NewUpgradeRoomHandler(cfg UpgradeRoomConfig) *UpgradeRoomHandler {
	return &UpgradeRoomHandler{serverName: cfg.ServerName}
}

// upgradeRoomBody is the JSON body for POST /_matrix/client/v3/rooms/{roomId}/upgrade.
type upgradeRoomBody struct {
	NewVersion string `json:"new_version"`
}

// PostUpgradeRoom handles POST /_matrix/client/v3/rooms/{roomId}/upgrade.
//
// Flow (5-29e stub):
//  1. requireJSON (415 on wrong Content-Type).
//  2. ValidateMatrixRoomID(roomId) → 400 M_BAD_JSON if invalid.
//  3. Decode body with DisallowUnknownFields; validate new_version non-empty → 400 M_BAD_JSON.
//  4. Return 501 M_UNRECOGNIZED (room upgrade not yet implemented).
//
// Source: tmp/test-findings.md 2026-04-23 — "Aktualisiere auf die empfohlene Chat-Version" → 404.
// This endpoint was never registered; gateway returned 404 from the default mux fallback.
func (h *UpgradeRoomHandler) PostUpgradeRoom(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	roomID := r.PathValue("roomId")
	if err := ValidateMatrixRoomID(roomID); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Invalid room ID: "+err.Error())
		return
	}

	var body upgradeRoomBody
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "Request body is not valid JSON")
		return
	}

	if body.NewVersion == "" {
		writeMatrixError(w, http.StatusBadRequest, "M_BAD_JSON", "new_version is required")
		return
	}

	// 5-29e scope: stub — full implementation is a follow-up story.
	writeMatrixError(w, http.StatusNotImplemented, "M_UNRECOGNIZED",
		"This server does not yet support room version upgrades. Tracked as a follow-up.")
}
