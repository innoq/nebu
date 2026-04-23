package matrix

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
)

const maxMatrixIDBytes = 512

var (
	reRoomID  = regexp.MustCompile(`^![A-Za-z0-9._=-]{1,63}:[A-Za-z0-9.-]{1,255}$`)
	reUserID  = regexp.MustCompile(`^@[A-Za-z0-9._=/-]{1,63}:[A-Za-z0-9.-]{1,255}$`)
	reEventID = regexp.MustCompile(`^\$[A-Za-z0-9+/=_-]{1,64}(?::[A-Za-z0-9.-]{1,255})?$`)
)

// ValidateMatrixRoomID returns an error if s is not a valid Matrix Room ID.
// Format: ^![A-Za-z0-9._=-]{1,63}:[A-Za-z0-9.-]{1,255}$
// Total length is capped at 512 bytes before regex evaluation.
func ValidateMatrixRoomID(s string) error {
	if len(s) > maxMatrixIDBytes {
		return errors.New("room ID exceeds 512 bytes")
	}
	if !reRoomID.MatchString(s) {
		return errors.New("invalid Matrix room ID format")
	}
	return nil
}

// ValidateMatrixUserID returns an error if s is not a valid Matrix User ID.
// Format: ^@[A-Za-z0-9._=/-]{1,63}:[A-Za-z0-9.-]{1,255}$
// Total length is capped at 512 bytes before regex evaluation.
func ValidateMatrixUserID(s string) error {
	if len(s) > maxMatrixIDBytes {
		return errors.New("user ID exceeds 512 bytes")
	}
	if !reUserID.MatchString(s) {
		return errors.New("invalid Matrix user ID format")
	}
	return nil
}

// ValidateMatrixEventID returns an error if s is not a valid Matrix Event ID.
// Accepts rooms v3+ hash form ($<base64url>{1,64}) and legacy form ($<local>:<server>).
// Total length is capped at 512 bytes before regex evaluation.
func ValidateMatrixEventID(s string) error {
	if len(s) > maxMatrixIDBytes {
		return errors.New("event ID exceeds 512 bytes")
	}
	if !reEventID.MatchString(s) {
		return errors.New("invalid Matrix event ID format")
	}
	return nil
}

// requireJSON checks that the request Content-Type is application/json.
// Returns true when the check passes. On mismatch it writes 415 M_UNSUPPORTED_MEDIA_TYPE
// and returns false — the caller must return immediately.
func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	// Allow "application/json" with or without a charset parameter.
	if strings.HasPrefix(ct, "application/json") {
		return true
	}
	writeMatrixError(w, http.StatusUnsupportedMediaType, "M_UNSUPPORTED_MEDIA_TYPE",
		"Content-Type must be application/json")
	return false
}
