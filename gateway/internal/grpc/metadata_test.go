package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestWithUserMetadata_SetsOutgoingMetadata(t *testing.T) {
	tests := []struct {
		name       string
		userID     string
		systemRole string
	}{
		{"regular user", "@alice:example.com", "user"},
		{"instance admin", "@kai:example.com", "instance_admin"},
		{"empty values", "", "user"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := WithUserMetadata(context.Background(), tt.userID, tt.systemRole)
			md, ok := metadata.FromOutgoingContext(ctx)
			if !ok {
				t.Fatal("no outgoing metadata found in context")
			}
			if got := md.Get(MetadataKeyUserID); len(got) == 0 || got[0] != tt.userID {
				t.Errorf("x-user-id = %v, want %q", got, tt.userID)
			}
			if got := md.Get(MetadataKeySystemRole); len(got) == 0 || got[0] != tt.systemRole {
				t.Errorf("x-system-role = %v, want %q", got, tt.systemRole)
			}
		})
	}
}

func TestFormatUserID(t *testing.T) {
	tests := []struct {
		sub        string
		serverName string
		want       string
	}{
		{"abc-uuid-123", "example.com", "@_zbjphk2ji31:example.com"},
		{"", "example.com", ""},
		{"user1", "nebu.internal", "@cgqblglkpkmb:nebu.internal"},
		// Dex local-connector sub: SHA-256 → base64url[:12] → lowercase
		{"CiQwMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDMSBWxvY2Fs", "localhost", "@xph3zgstbkdb:localhost"},
	}
	for _, tt := range tests {
		t.Run(tt.sub+"/"+tt.serverName, func(t *testing.T) {
			got := FormatUserID(tt.sub, tt.serverName)
			if got != tt.want {
				t.Errorf("FormatUserID(%q, %q) = %q, want %q", tt.sub, tt.serverName, got, tt.want)
			}
		})
	}
}
