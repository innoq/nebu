package compliance_test

// jwt_test.go — Story 5.5: ComplianceJWT helper — RED-phase tests
//
// ALL tests in this file are expected to FAIL until Story 5.5 is implemented.
// Failing reason: ValidateComplianceToken, IssueComplianceToken, ComplianceClaims
// do not exist in the compliance package yet.
//
// Test strategy:
//   - Each test generates or reuses the package-level Ed25519 test keypair.
//   - IssueComplianceToken is used to produce tokens for validation tests
//     (black-box: call the real issuance helper instead of hand-crafting JWS).
//   - Algorithm-confusion test (AC8 / Story 5.18 lesson) uses go-jose/v4 directly
//     to sign with HS256 and confirms ValidateComplianceToken rejects it.
//
// AC coverage:
//   AC8 — ValidateComplianceToken (all six tests below)

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
	"github.com/nebu/nebu/internal/compliance"
)

// ─── Test 11: ValidateComplianceToken_Valid ───────────────────────────────────
//
// AC8 — fresh token, correct sub → returns *ComplianceClaims with nil error

func TestValidateComplianceToken_Valid(t *testing.T) {
	initTestSessionKey()

	// Issue a token via the compliance package helper
	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-001",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	got, err := compliance.ValidateComplianceToken(tokenStr, testSessionPub, "@alice:server.example")
	if err != nil {
		t.Fatalf("ValidateComplianceToken returned error for valid token: %v", err)
	}
	if got == nil {
		t.Fatal("ValidateComplianceToken returned nil claims for valid token")
	}
	if got.Sub != "@alice:server.example" {
		t.Errorf("claims.Sub: expected @alice:server.example, got %q", got.Sub)
	}
	if got.RoomID != "!room:server.example" {
		t.Errorf("claims.RoomID: expected !room:server.example, got %q", got.RoomID)
	}
	if got.TimeRangeStart != "2026-01-01T00:00:00Z" {
		t.Errorf("claims.TimeRangeStart: expected 2026-01-01T00:00:00Z, got %q", got.TimeRangeStart)
	}
	if got.TimeRangeEnd != "2026-03-31T23:59:59Z" {
		t.Errorf("claims.TimeRangeEnd: expected 2026-03-31T23:59:59Z, got %q", got.TimeRangeEnd)
	}
}

// ─── Test 12: ValidateComplianceToken_Expired ─────────────────────────────────
//
// AC8 — token with exp in the past → error "token expired"

func TestValidateComplianceToken_Expired(t *testing.T) {
	initTestSessionKey()

	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-expired",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Add(-7200 * time.Second).Unix(),
		Exp:                 time.Now().Add(-1 * time.Second).Unix(), // 1 second in the past
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	got, err := compliance.ValidateComplianceToken(tokenStr, testSessionPub, "@alice:server.example")
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error for expired token, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for expired token")
	}
}

// ─── Kassandra MEDIUM (2026-04-23): iat-future-Check ────────────────────────
//
// Token issued with iat > now+30s must be rejected — RFC 7519 §4.1.6.
// Defense-in-depth against forged claims with future timestamps.

func TestValidateComplianceToken_IatInFuture(t *testing.T) {
	initTestSessionKey()

	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-future-iat",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Add(2 * time.Hour).Unix(), // far in the future
		Exp:                 time.Now().Add(26 * time.Hour).Unix(),
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	got, err := compliance.ValidateComplianceToken(tokenStr, testSessionPub, "@alice:server.example")
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error for future-iat token, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for future-iat token")
	}
}

// ─── Test 13: ValidateComplianceToken_SubMismatch ────────────────────────────
//
// AC8 — token sub=@alice:server, expectedSub=@bob:server → error

func TestValidateComplianceToken_SubMismatch(t *testing.T) {
	initTestSessionKey()

	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-mismatch",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	got, err := compliance.ValidateComplianceToken(tokenStr, testSessionPub, "@bob:server.example") // different sub
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error when sub mismatches, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims when sub mismatches")
	}
}

// ─── Test 14: ValidateComplianceToken_InvalidSignature ───────────────────────
//
// AC8 — token signed with a different Ed25519 key → error

func TestValidateComplianceToken_InvalidSignature(t *testing.T) {
	initTestSessionKey()

	// Generate a different key pair for signing
	otherPub, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate other key pair: %v", err)
	}
	_ = otherPub // unused; we sign with otherPriv but verify with testSessionPub

	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-badsig",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
	}

	// Sign with the OTHER private key
	tokenStr, err := compliance.IssueComplianceToken(otherPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken (other key) failed: %v", err)
	}

	// Verify with the TEST public key — must fail
	got, err := compliance.ValidateComplianceToken(tokenStr, testSessionPub, "@alice:server.example")
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error for mismatched signature, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for invalid signature")
	}
}

// ─── Test 15: ValidateComplianceToken_AlgConfusion ───────────────────────────
//
// AC8 / Story 5.18 lesson — algorithm-confusion attack (HMAC-SHA256 with public key as secret)
//
// An attacker takes the Ed25519 public key bytes and uses them as an HMAC-SHA256
// secret to forge a token with alg=HS256. ValidateComplianceToken must reject this
// because it pins to EdDSA only.
//
// Implementation note: go-jose/v4 ParseSigned with allowed algorithms []{"EdDSA"}
// rejects any token where the header alg is not EdDSA before signature verification.

func TestValidateComplianceToken_AlgConfusion(t *testing.T) {
	initTestSessionKey()

	// Build a JWT payload
	payload := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-algconfusion",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
	}

	// Sign with HS256 using the Ed25519 public key bytes as HMAC secret
	// (this is the classic "algorithm confusion" attack pattern)
	hmacKey := []byte(testSessionPub) // use pubkey bytes as HMAC secret

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: hmacKey},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("jose.NewSigner (HS256): %v", err)
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal claims: %v", err)
	}

	// Use raw jose signing (not josejwt.Signed) to avoid josejwt type constraints
	jws, err := signer.Sign(payloadBytes)
	if err != nil {
		t.Fatalf("signer.Sign: %v", err)
	}
	tokenStr := jws.FullSerialize()

	// ValidateComplianceToken must reject this — alg is HS256, not EdDSA
	got, err := compliance.ValidateComplianceToken(tokenStr, testSessionPub, "@alice:server.example")
	if err == nil {
		t.Fatal("ValidateComplianceToken must reject HS256-signed token (algorithm confusion), got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for algorithm-confused token")
	}

	// Explicit check: error must indicate rejection (any non-nil error is accepted;
	// the key requirement is that the function does NOT return valid claims)
	_ = err // documented: any error satisfies the alg-pinning requirement

	// Ensure HMAC is not accidentally included — this ensures the test key
	// is actually being used in the confusion attempt
	mac := hmac.New(sha256.New, hmacKey)
	mac.Write([]byte("test"))
	_ = mac.Sum(nil) // confirm hmac package is referenced (import guard)
}

// ─── Test 16: ValidateComplianceToken_MalformedToken ─────────────────────────
//
// AC8 — random string input → error

func TestValidateComplianceToken_MalformedToken(t *testing.T) {
	initTestSessionKey()

	got, err := compliance.ValidateComplianceToken("not.a.valid.jwt.string", testSessionPub, "@alice:server.example")
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error for malformed token, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for malformed token")
	}
}

// ─── compile-time import guard ────────────────────────────────────────────────
// josejwt.Claims is referenced to ensure the library is available.
var _ josejwt.Claims
