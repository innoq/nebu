package main

// kek_validation.go — Story 5.29d AC5 (FB-29c-1)
//
// validateKEKConfig enforces that the NEBU_KEY_ENCRYPTION_KEY is set when running
// in a production environment. A zero-byte (all-zeroes) KEK is effectively no
// encryption — acceptable for local dev but not for production deployments.
//
// Rules (fail-closed: unset NEBU_ENV is treated as production):
//   1. kekHex!="" → no error (key is set; always acceptable regardless of env)
//   2. env in {dev,development,test,staging} AND kekHex=="" → no error (warn)
//   3. env not in dev-set AND kekHex=="" AND allowInsecure=="true" → no error (warn loudly)
//   4. env not in dev-set AND kekHex=="" AND allowInsecure!="true" → error (hard-fail)
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

	// kekHex is empty (not set). Fail-closed: anything other than an explicit
	// dev-class env is treated as production.
	if env == "dev" || env == "development" || env == "test" || env == "staging" {
		slog.Warn("NEBU_KEY_ENCRYPTION_KEY not set — using dev-only zero-byte default (NOT safe for production)", "env", env)
		return nil
	}

	if allowInsecure == "true" {
		// Operator explicitly opted in — allow but warn loudly.
		slog.Warn("NEBU_KEY_ENCRYPTION_KEY is not set and NEBU_ENV is not a dev-class env — using insecure zero-byte default (NEBU_ALLOW_INSECURE_KEK=true opt-in active)", "env", env)
		return nil
	}

	// Hard-fail: missing KEK in a non-dev env without the explicit opt-in.
	return fmt.Errorf(
		"NEBU_KEY_ENCRYPTION_KEY must be set when NEBU_ENV is not one of "+
			"dev|development|test|staging (got %q). Using the zero-byte default KEK "+
			"provides no encryption protection for stored compliance signing keys. "+
			"Set NEBU_KEY_ENCRYPTION_KEY to a 64-hex-char (32-byte) random value, or "+
			"set NEBU_ALLOW_INSECURE_KEK=true to explicitly accept the risk (not recommended)",
		env,
	)
}
