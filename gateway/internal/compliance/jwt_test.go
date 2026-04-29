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
	"context"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"strings"
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
		Iss:                 compliance.JWTIssuer,   // AC4: required
		Aud:                 compliance.JWTAudience, // AC4: required
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	activeDB := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", activeDB)
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
		Iss:                 compliance.JWTIssuer,
		Aud:                 compliance.JWTAudience,
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	activeDB := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", activeDB)
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
		Iss:                 compliance.JWTIssuer,
		Aud:                 compliance.JWTAudience,
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	activeDB := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", activeDB)
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
		Iss:                 compliance.JWTIssuer,
		Aud:                 compliance.JWTAudience,
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	activeDB := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@bob:server.example", activeDB) // different sub
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
		Iss:                 compliance.JWTIssuer,
		Aud:                 compliance.JWTAudience,
	}

	// Sign with the OTHER private key
	tokenStr, err := compliance.IssueComplianceToken(otherPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken (other key) failed: %v", err)
	}

	// Verify with the TEST public key — must fail
	activeDB := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", activeDB)
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
	activeDB := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", activeDB)
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

	activeDB := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), "not.a.valid.jwt.string", testSessionPub, "@alice:server.example", activeDB)
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error for malformed token, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for malformed token")
	}
}

// ─── Story 5.29c: FB-E5-04 — JWT revocation DB checks (AC1) ─────────────────
//
// These tests are RED-phase: they FAIL until ValidateComplianceToken is extended
// to perform a SHA-256 token_hash lookup in compliance_sessions.
//
// ValidateComplianceToken's current signature (tokenStr, pubKey, expectedSub)
// does not accept a DB handle. The new signature must be:
//   ValidateComplianceToken(tokenStr string, pubKey ed25519.PublicKey, expectedSub string, db SessionLookupDB) (*ComplianceClaims, error)
// where SessionLookupDB is a new interface defined in compliance package.
// Until the signature changes, these tests fail to compile — the intended red state.
//
// The mockSessionLookupDB below implements the expected interface contract.

// mockSessionLookupDB implements compliance.SessionLookupDB (interface to be defined in jwt.go).
// TokenHashFound: false → 0 rows (token never issued or forged)
// TokenHashFound: true, Revoked: true → revoked_at IS NOT NULL
// TokenHashFound: true, Revoked: false → active session (revoked_at IS NULL)
type mockSessionLookupDB struct {
	tokenHashFound bool
	revoked        bool
}

// IsTokenActive checks whether token_hash exists and revoked_at IS NULL.
// This is the interface method that compliance.ValidateComplianceToken will call.
// Returns (true, nil) for active sessions, (false, nil) for missing/revoked tokens.
func (m *mockSessionLookupDB) IsTokenActive(_ context.Context, _ []byte) (bool, error) {
	if !m.tokenHashFound {
		return false, nil // 0 rows
	}
	if m.revoked {
		return false, nil // revoked_at IS NOT NULL
	}
	return true, nil // active
}

// TestValidateComplianceToken_Revoked_Rejected — AC1
// token_hash exists in compliance_sessions with revoked_at IS NOT NULL → reject
// with error containing "token revoked".
func TestValidateComplianceToken_Revoked_Rejected(t *testing.T) {
	initTestSessionKey()

	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-revoked",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
		Iss:                 compliance.JWTIssuer,
		Aud:                 compliance.JWTAudience,
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	db := &mockSessionLookupDB{tokenHashFound: true, revoked: true}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", db)
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error for revoked token, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for revoked token")
	}
	if !strings.Contains(err.Error(), "token revoked") {
		t.Errorf("error must mention 'token revoked', got: %v", err)
	}
}

// TestValidateComplianceToken_NoSessionRow_Rejected — AC1
// token_hash absent from compliance_sessions (token never issued or forged) → reject.
func TestValidateComplianceToken_NoSessionRow_Rejected(t *testing.T) {
	initTestSessionKey()

	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-phantom",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
		Iss:                 compliance.JWTIssuer,
		Aud:                 compliance.JWTAudience,
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	db := &mockSessionLookupDB{tokenHashFound: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", db)
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error for unknown token hash, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for unknown token hash")
	}
	if !strings.Contains(err.Error(), "token revoked") {
		t.Errorf("error must mention 'token revoked', got: %v", err)
	}
}

// TestValidateComplianceToken_ActiveSession_Accepted — AC1 (happy path with DB)
// token_hash found + revoked_at IS NULL → accept.
func TestValidateComplianceToken_ActiveSession_Accepted(t *testing.T) {
	initTestSessionKey()

	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-active",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
		Iss:                 compliance.JWTIssuer,
		Aud:                 compliance.JWTAudience,
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	db := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", db)
	if err != nil {
		t.Fatalf("ValidateComplianceToken must accept active session token, got error: %v", err)
	}
	if got == nil {
		t.Fatal("ValidateComplianceToken must return claims for active session token")
	}
	if got.Sub != "@alice:server.example" {
		t.Errorf("claims.Sub: expected @alice:server.example, got %q", got.Sub)
	}
}

// ─── Story 5.29c: FB-E5-06 — iss/aud claims validation (AC4) ────────────────
//
// These tests are RED-phase: they FAIL until:
//   1. ComplianceClaims struct gains Iss string and Aud string fields.
//   2. IssueComplianceToken embeds iss/aud in the JWT when set.
//   3. ValidateComplianceToken checks iss == "nebu-gateway" and aud == "compliance-export".
//
// Until ComplianceClaims.Iss and ComplianceClaims.Aud fields exist, these tests
// fail to compile — the intended red state.

// TestComplianceJWT_MissingIss_Rejected — AC4
// Token with iss="" (zero value, not set) → reject.
func TestComplianceJWT_MissingIss_Rejected(t *testing.T) {
	initTestSessionKey()

	// ComplianceClaims.Iss does not exist yet → compile error = red phase.
	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-no-iss",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
		Iss:                 "", // missing — zero value
		Aud:                 "compliance-export",
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	db := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", db)
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error for token missing iss, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for missing iss")
	}
}

// TestComplianceJWT_WrongIss_Rejected — AC4
// Token with iss="other-issuer" → reject.
func TestComplianceJWT_WrongIss_Rejected(t *testing.T) {
	initTestSessionKey()

	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-wrong-iss",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
		Iss:                 "other-issuer",
		Aud:                 "compliance-export",
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	db := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", db)
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error for wrong iss, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for wrong iss")
	}
}

// TestComplianceJWT_MismatchedAud_Rejected — AC4
// Token with aud="wrong-audience" → reject.
func TestComplianceJWT_MismatchedAud_Rejected(t *testing.T) {
	initTestSessionKey()

	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-wrong-aud",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
		Iss:                 "nebu-gateway",
		Aud:                 "wrong-audience",
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	db := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", db)
	if err == nil {
		t.Fatal("ValidateComplianceToken must return error for wrong aud, got nil error")
	}
	if got != nil {
		t.Error("ValidateComplianceToken must return nil claims for wrong aud")
	}
}

// TestComplianceJWT_ValidIssAud_Accepted — AC4 (happy path with iss+aud)
// Token with iss="nebu-gateway" and aud="compliance-export" → accept.
func TestComplianceJWT_ValidIssAud_Accepted(t *testing.T) {
	initTestSessionKey()

	claims := compliance.ComplianceClaims{
		Sub:                 "@alice:server.example",
		ComplianceRequestID: "req-valid-issaud",
		RoomID:              "!room:server.example",
		TimeRangeStart:      "2026-01-01T00:00:00Z",
		TimeRangeEnd:        "2026-03-31T23:59:59Z",
		Iat:                 time.Now().Unix(),
		Exp:                 time.Now().Add(3600 * time.Second).Unix(),
		Iss:                 "nebu-gateway",
		Aud:                 "compliance-export",
	}

	tokenStr, err := compliance.IssueComplianceToken(testSessionPriv, claims)
	if err != nil {
		t.Fatalf("IssueComplianceToken failed: %v", err)
	}

	db := &mockSessionLookupDB{tokenHashFound: true, revoked: false}
	got, err := compliance.ValidateComplianceToken(context.Background(), tokenStr, testSessionPub, "@alice:server.example", db)
	if err != nil {
		t.Fatalf("ValidateComplianceToken must accept token with valid iss+aud, got error: %v", err)
	}
	if got == nil {
		t.Fatal("ValidateComplianceToken must return claims for valid iss+aud token")
	}
	if got.Iss != "nebu-gateway" {
		t.Errorf("claims.Iss: expected nebu-gateway, got %q", got.Iss)
	}
	if got.Aud != "compliance-export" {
		t.Errorf("claims.Aud: expected compliance-export, got %q", got.Aud)
	}
}

// ─── compile-time import guard ────────────────────────────────────────────────
// josejwt.Claims is referenced to ensure the library is available.
var _ josejwt.Claims
