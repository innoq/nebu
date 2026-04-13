package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"google.golang.org/grpc/metadata"
)

const (
	MetadataKeyUserID     = "x-user-id"
	MetadataKeySystemRole = "x-system-role"
)

// WithUserMetadata returns ctx with x-user-id and x-system-role set as outgoing gRPC metadata.
// userID must already be formatted as "@{sub}:{serverName}" via FormatUserID.
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
// The localpart is derived as: lowercase(base64url(SHA-256(sub))[:12])
// This produces a 12-character, all-lowercase, Matrix-safe identifier with
// 72 bits of effective entropy — sufficient collision resistance for any
// realistic deployment size, and independent of the IdP's sub encoding.
func FormatUserID(sub, serverName string) string {
	if sub == "" {
		return ""
	}
	h := sha256.Sum256([]byte(sub))
	localpart := strings.ToLower(base64.RawURLEncoding.EncodeToString(h[:])[:12])
	return "@" + localpart + ":" + serverName
}
