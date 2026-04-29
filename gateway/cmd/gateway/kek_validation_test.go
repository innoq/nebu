package main

// kek_validation_test.go — Story 5.29d AC5 (FB-29c-1)
//
// RED-PHASE: ALL tests in this file FAIL until Story 5.29d is implemented.
//
// Failing reason:
//   validateKEKConfig does not exist yet. Currently the KEK-loading logic is
//   inlined in main() with no extractable validation function. The production
//   code warns and continues with a zero-key in non-production — but it has no
//   concept of "env=production" vs "env=dev" and no NEBU_ALLOW_INSECURE_KEK opt-in.
//
// Implementation contract (function that MUST exist in package main):
//
//   // validateKEKConfig checks the NEBU_KEY_ENCRYPTION_KEY configuration
//   // against the current deployment environment and returns an error
//   // if the combination is unsafe.
//   //
//   // Parameters:
//   //   kekHex  — value of NEBU_KEY_ENCRYPTION_KEY (empty = not set)
//   //   env     — value of NEBU_ENV ("production", "dev", "staging", etc.)
//   //   allowInsecure — value of NEBU_ALLOW_INSECURE_KEK ("true" = opt-in)
//   //
//   // Rules:
//   //   1. env=="production" AND kekHex=="" AND allowInsecure!="true" → error
//   //   2. env!="production" AND kekHex=="" → no error (dev default allowed)
//   //   3. env=="production" AND allowInsecure=="true" → no error (explicit opt-in, warning logged)
//   //   4. kekHex!="" → no error (key is set, always ok)
//
//   func validateKEKConfig(kekHex, env, allowInsecure string) error
//
// AC coverage:
//   AC5 (FB-29c-1) — TestKEK_ZeroDefault_FailsInProduction
//   AC5 (FB-29c-1) — TestKEK_ZeroDefault_AllowedInDev
//   AC5 (FB-29c-1) — TestKEK_ZeroDefault_AllowedWithOptIn
//   AC5 (FB-29c-1) — TestKEK_ExplicitKey_AlwaysAllowed

import (
	"testing"
)

// TestKEK_ZeroDefault_FailsInProduction — AC5
//
// Given: env="production", NEBU_KEY_ENCRYPTION_KEY="" (not set), NEBU_ALLOW_INSECURE_KEK="" (not set)
// When:  validateKEKConfig is called
// Then:  returns a non-nil error (gateway must refuse to start with zero KEK in production)
//
// RED-PHASE: FAILS because validateKEKConfig does not exist.
func TestKEK_ZeroDefault_FailsInProduction(t *testing.T) {
	err := validateKEKConfig("", "production", "")
	if err == nil {
		t.Error(
			"AC5 FAIL: validateKEKConfig(kekHex=\"\", env=\"production\", allowInsecure=\"\") " +
				"returned nil — must return an error to prevent zero-KEK startup in production. " +
				"Implement validateKEKConfig in cmd/gateway/main.go (or a new file in package main).",
		)
	}
}

// TestKEK_ZeroDefault_AllowedInDev — AC5
//
// Given: env="dev", NEBU_KEY_ENCRYPTION_KEY="" (not set)
// When:  validateKEKConfig is called
// Then:  returns nil (zero-KEK default is acceptable in dev environment)
//
// RED-PHASE: FAILS because validateKEKConfig does not exist.
func TestKEK_ZeroDefault_AllowedInDev(t *testing.T) {
	for _, env := range []string{"dev", "development", "test", "staging"} {
		err := validateKEKConfig("", env, "")
		if err != nil {
			t.Errorf(
				"AC5 FAIL: validateKEKConfig(kekHex=\"\", env=%q, allowInsecure=\"\") "+
					"returned error %v — zero-KEK should be allowed in dev-class env",
				env, err,
			)
		}
	}
}

// TestKEK_ZeroDefault_FailsOnUnsetEnv — fail-closed: an unset NEBU_ENV is treated
// as production. Without an explicit opt-in, validation must reject.
func TestKEK_ZeroDefault_FailsOnUnsetEnv(t *testing.T) {
	for _, env := range []string{"", "production", "prod", "anything-else"} {
		err := validateKEKConfig("", env, "")
		if err == nil {
			t.Errorf(
				"FAIL: validateKEKConfig(kekHex=\"\", env=%q, allowInsecure=\"\") "+
					"returned nil — fail-closed: only dev-class env may run with zero KEK",
				env,
			)
		}
	}
}

// TestKEK_ZeroDefault_AllowedWithOptIn — AC5
//
// Given: env="production", NEBU_KEY_ENCRYPTION_KEY="" (not set), NEBU_ALLOW_INSECURE_KEK="true"
// When:  validateKEKConfig is called
// Then:  returns nil (operator explicitly opted in to insecure KEK in production)
//
// Rationale: some internal test deployments use production-like config but without
// real secrets. The opt-in provides an escape hatch without disabling the guard
// for all production deployments.
//
// RED-PHASE: FAILS because validateKEKConfig does not exist.
func TestKEK_ZeroDefault_AllowedWithOptIn(t *testing.T) {
	err := validateKEKConfig("", "production", "true")
	if err != nil {
		t.Errorf(
			"AC5 FAIL: validateKEKConfig(kekHex=\"\", env=\"production\", allowInsecure=\"true\") "+
				"returned error %v — NEBU_ALLOW_INSECURE_KEK=true must bypass the production guard",
			err,
		)
	}
}

// TestKEK_ExplicitKey_AlwaysAllowed — AC5
//
// Given: NEBU_KEY_ENCRYPTION_KEY is set to a valid 64-hex-char value
// When:  validateKEKConfig is called with any env value
// Then:  returns nil (key is set; no safety concern)
//
// RED-PHASE: FAILS because validateKEKConfig does not exist.
func TestKEK_ExplicitKey_AlwaysAllowed(t *testing.T) {
	validKEK := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

	for _, env := range []string{"production", "dev", "staging", ""} {
		err := validateKEKConfig(validKEK, env, "")
		if err != nil {
			t.Errorf(
				"AC5 FAIL: validateKEKConfig(kekHex=%q, env=%q, allowInsecure=\"\") "+
					"returned error %v — an explicitly set KEK must always be accepted",
				validKEK[:8]+"...", env, err,
			)
		}
	}
}
