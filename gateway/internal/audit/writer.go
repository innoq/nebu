package audit

// writer.go — Story 5-2: generic audit log event sender via gRPC.
//
// Never-raise policy: if the gRPC call fails, a warning is logged and nil is
// returned. Audit failures must never block the primary operation path.

import (
	"context"
	"encoding/json"
	"log/slog"

	pb "github.com/nebu/nebu/internal/grpc/pb"
)

// MaxMetadataJSONBytes caps the serialised metadata payload per audit entry.
// Kassandra MEDIUM-1 (2026-04-23): gRPC's default 4 MiB message cap is far too
// loose for a per-event JSONB payload and invites DoS. 16 KiB is generous for
// the structured metadata we actually emit (a handful of short strings).
const MaxMetadataJSONBytes = 16 * 1024

// LogEvent sends one audit log event to the Elixir core via gRPC.
// metadata is serialised to JSON bytes before the call; payloads exceeding
// MaxMetadataJSONBytes are replaced with "{}" plus a warning log entry so
// the audit row still lands without shipping an unbounded blob.
// If the gRPC call fails, a slog.Warn is emitted and nil is returned —
// audit failures must never block the primary operation.
func LogEvent(
	ctx context.Context,
	client pb.CoreServiceClient,
	actorUserID, action, targetType, targetID string,
	metadata map[string]any,
	outcome, errorDetail string,
) error {
	metaJSON := []byte("{}")
	if len(metadata) > 0 {
		if b, err := json.Marshal(metadata); err == nil {
			if len(b) > MaxMetadataJSONBytes {
				slog.Warn("audit: metadata JSON exceeds size limit; dropping payload",
					"action", action, "size_bytes", len(b), "limit_bytes", MaxMetadataJSONBytes)
			} else {
				metaJSON = b
			}
		} else {
			// Surface the failure so silently-dropped metadata doesn't hide bugs;
			// we still send {} so the audit row itself is preserved.
			slog.Warn("audit: metadata JSON marshal failed; sending empty object", "action", action, "err", err)
		}
	}

	_, err := client.WriteAuditLog(ctx, &pb.WriteAuditLogRequest{
		ActorUserId:  actorUserID,
		Action:       action,
		TargetType:   targetType,
		TargetId:     targetID,
		MetadataJson: metaJSON,
		Outcome:      outcome,
		ErrorDetail:  errorDetail,
	})
	if err != nil {
		slog.Warn("audit: WriteAuditLog gRPC failed", "action", action, "err", err)
	}
	// Always return nil — audit failure does not propagate to the caller.
	return nil
}
