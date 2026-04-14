package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"unicode"

	"google.golang.org/grpc/metadata"
)

const (
	MetadataKeyUserID     = "x-user-id"
	MetadataKeySystemRole = "x-system-role"
)

// WithUserMetadata returns ctx with x-user-id and x-system-role set as outgoing gRPC metadata.
// userID must already be formatted as "@{localpart}:{serverName}" via FormatUserID.
func WithUserMetadata(ctx context.Context, userID, systemRole string) context.Context {
	md := metadata.Pairs(
		MetadataKeyUserID, userID,
		MetadataKeySystemRole, systemRole,
	)
	return metadata.NewOutgoingContext(ctx, md)
}

// FormatUserID builds a Matrix user ID from an OIDC sub claim and the server name.
// Returns "" if sub is empty (unauthenticated context).
//
// Localpart derivation (priority order — Story 7-15 will make this configurable):
//  1. name claim: if provided and Matrix-safe, use directly (e.g. "alex" → "@alex:server")
//  2. Fallback: lowercase(base64url(SHA-256(sub))[:12]) — opaque but stable
func FormatUserID(sub, serverName string) string {
	if sub == "" {
		return ""
	}
	h := sha256.Sum256([]byte(sub))
	localpart := strings.ToLower(base64.RawURLEncoding.EncodeToString(h[:])[:12])
	return "@" + localpart + ":" + serverName
}

// FormatUserIDFromClaims builds a Matrix user ID preferring the human-readable
// name claim as the localpart. Falls back to FormatUserID(sub, serverName).
//
// The name claim is sanitised to Matrix-safe characters: [a-z0-9._\-].
// If the sanitised name is empty, the SHA-256 fallback is used.
func FormatUserIDFromClaims(sub, name, serverName string) string {
	if sub == "" {
		return ""
	}
	if safe := sanitiseLocalpart(name); safe != "" {
		return "@" + safe + ":" + serverName
	}
	return FormatUserID(sub, serverName)
}

// sanitiseLocalpart lowercases s and keeps only Matrix-safe characters.
// Returns "" if the result is empty.
func sanitiseLocalpart(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else if unicode.IsSpace(r) {
			b.WriteRune('_')
		}
		// drop all other characters
	}
	return b.String()
}
