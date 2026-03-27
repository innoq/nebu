package grpc

import (
	"context"

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
func FormatUserID(sub, serverName string) string {
	if sub == "" {
		return ""
	}
	return "@" + sub + ":" + serverName
}
