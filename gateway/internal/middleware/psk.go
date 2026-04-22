package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
)

// pskDomain is a compile-time domain-separation constant used when hashing PSK
// values before comparison. It prevents precomputed-hash attacks.
const pskDomain = "nebu/psk/v1"

// hashForCompare derives a fixed-length HMAC-SHA256 digest from a value using
// the pskDomain as the key. Both sides of a comparison are hashed before
// calling subtle.ConstantTimeCompare, making the final comparison
// length-agnostic in timing.
func hashForCompare(value []byte) []byte {
	mac := hmac.New(sha256.New, []byte(pskDomain))
	mac.Write(value)
	return mac.Sum(nil)
}

// constantTimeEqualHashed compares two byte slices in constant time by hashing
// both to a fixed-length digest first. This prevents timing attacks that could
// otherwise leak the length of the expected secret when inputs differ in length.
func constantTimeEqualHashed(a, b []byte) bool {
	ha := hashForCompare(a)
	hb := hashForCompare(b)
	return subtle.ConstantTimeCompare(ha, hb) == 1
}

// PSKMiddleware returns an HTTP middleware that validates a pre-shared key
// from the Authorization header (Bearer scheme). Uses hash-then-compare to
// prevent timing attacks that could leak PSK length. Returns 401 with empty
// body on mismatch.
func PSKMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			expected := "Bearer " + secret
			if !constantTimeEqualHashed([]byte(authHeader), []byte(expected)) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
