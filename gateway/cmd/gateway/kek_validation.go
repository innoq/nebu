package main

// kek_validation.go — Story 5.29d AC5 (FB-29c-1)
//
// validateKEKConfig enforces that the NEBU_KEY_ENCRYPTION_KEY is set when running
// in a production environment. A zero-byte (all-zeroes) KEK is effectively no
// encryption — acceptable for local dev but not for production deployments.
//
// Rules:
//   1. env=="production" AND kekHex=="" AND allowInsecure!="true" → error (hard-fail)
//   2. env!="production" AND kekHex=="" → no error (dev zero-default is fine)
//   3. env=="production" AND allowInsecure=="true" → no error (explicit opt-in, warn)
//   4. kekHex!="" → no error (key is set; always acceptable regardless of env)
//
// Integration in main():
//   After cfg.Load(), call validateKEKConfig(kekHex, cfg.Env, cfg.AllowInsecureKEK).
//   If it returns an error, log and os.Exit(1).

import (
	"fmt"
	"log/slog"
)

// validateKEKConfig checks the NEBU_KEY_ENCRYPTION_KEY configuration against the
// current deployment environment and returns an error if the combination is unsafe.
//
//   - kekHex       — value of NEBU_KEY_ENCRYPTION_KEY (empty = not set)
//   - env          — value of NEBU_ENV ("production", "dev", "staging", etc.)
//   - allowInsecure — value of NEBU_ALLOW_INSECURE_KEK ("true" = opt-in)
func validateKEKConfig(kekHex, env, allowInsecure string) error {
	if kekHex != "" {
		// Key is explicitly set — always safe regardless of env.
		return nil
	}

	// kekHex is empty (not set).
	if env == "production" {
		if allowInsecure == "true" {
			// Operator explicitly opted in — allow but warn loudly.
			slog.Warn("NEBU_KEY_ENCRYPTION_KEY is not set in production — using insecure zero-byte default (NEBU_ALLOW_INSECURE_KEK=true opt-in active)")
			return nil
		}
		// Hard-fail: production without a KEK and without the explicit opt-in.
		return fmt.Errorf(
			"NEBU_KEY_ENCRYPTION_KEY must be set in production (NEBU_ENV=production). " +
				"Using the zero-byte default KEK is not acceptable in production because it " +
				"provides no encryption protection for stored compliance signing keys. " +
				"Set NEBU_KEY_ENCRYPTION_KEY to a 64-hex-char (32-byte) random value, or " +
				"set NEBU_ALLOW_INSECURE_KEK=true to explicitly accept the risk (not recommended)",
		)
	}

	// Non-production environment — zero-default is acceptable; log a warning.
	slog.Warn("NEBU_KEY_ENCRYPTION_KEY not set — using dev-only zero-byte default (NOT safe for production)")
	return nil
}
