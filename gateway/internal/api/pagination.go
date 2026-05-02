//go:build go1.22

package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

// ErrInvalidCursor is returned by DecodeCursor when the cursor is malformed,
// contains invalid base64, invalid JSON, or is missing required fields.
// Callers should use errors.Is(err, ErrInvalidCursor) to check for this condition.
var ErrInvalidCursor = errors.New("invalid cursor")

// cursorPayload is the internal JSON structure of a pagination cursor.
type cursorPayload struct {
	AfterID        string `json:"after_id"`
	AfterCreatedAt string `json:"after_created_at"`
}

// EncodeCursor encodes a pagination position into an opaque, URL-safe cursor token.
// The token is Base64URLNoPad(json({"after_id":"<uuid>","after_created_at":"<ISO8601>"})).
func EncodeCursor(afterID, afterCreatedAt string) string {
	payload := cursorPayload{
		AfterID:        afterID,
		AfterCreatedAt: afterCreatedAt,
	}
	b, _ := json.Marshal(payload)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor decodes an opaque cursor token produced by EncodeCursor.
// Returns the afterID and afterCreatedAt values encoded in the cursor.
// Returns ErrInvalidCursor if the token is malformed, not valid base64url,
// not valid JSON, or is missing required fields.
func DecodeCursor(cursor string) (afterID, afterCreatedAt string, err error) {
	if cursor == "" {
		return "", "", ErrInvalidCursor
	}

	raw, decErr := base64.RawURLEncoding.DecodeString(cursor)
	if decErr != nil {
		return "", "", ErrInvalidCursor
	}

	var payload cursorPayload
	if jsonErr := json.Unmarshal(raw, &payload); jsonErr != nil {
		return "", "", ErrInvalidCursor
	}

	if payload.AfterID == "" || payload.AfterCreatedAt == "" {
		return "", "", ErrInvalidCursor
	}

	return payload.AfterID, payload.AfterCreatedAt, nil
}
