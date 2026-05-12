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

// FormatUserIDFromClaims builds a Matrix user ID using the configured claim name.
// It looks up claims[claimName], sanitises the value via sanitiseLocalpart, and
// uses the result as the localpart. If the result is empty (claim absent, not a
// string, or sanitises to ""), it falls back to FormatUserID(sub, serverName)
// where sub = claims["sub"].(string).
//
// AC6 (Story 11-10): new signature — claimName + full claims map replace the old
// (sub, name string) pair. All call sites must pass the DB-loaded claim name.
//
// Security: claims[claimName] is used only as a map key lookup (no SQL) and is
// sanitised before use — injection risk is nil.
func FormatUserIDFromClaims(claimName string, claims map[string]interface{}, serverName string) string {
	// Extract configured claim value as string (non-string types fall through to fallback).
	if claimValue, ok := claims[claimName].(string); ok {
		if safe := sanitiseLocalpart(claimValue); safe != "" {
			return "@" + safe + ":" + serverName
		}
	}
	// Fallback: use SHA-256 of sub claim.
	sub, _ := claims["sub"].(string)
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
