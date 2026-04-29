package compliance

// jwt.go — Story 5.5: Compliance JWT helper
//           Story 5.29c: AC1 (revocation DB check), AC4 (iss/aud claims)
//
// IssueComplianceToken: signs a ComplianceClaims struct as an EdDSA JWT.
//   - Used internally by SessionHandler.PostSession.
//   - Algorithm is hard-wired to EdDSA (jose.EdDSA) — no other algorithm is accepted
//     for issuance (Story 5.18 lesson: algorithm pinning is mandatory).
//   - The caller must set Iss and Aud; IssueComplianceToken does not auto-fill them.
//
// ValidateComplianceToken: parses, verifies, and validates a compliance JWT.
//   - Exported for reuse by Story 5.6 (X-Compliance-Token header validation).
//   - Pins algorithm to []jose.SignatureAlgorithm{jose.EdDSA} in ParseSigned call so
//     algorithm-confusion attacks (e.g. HS256 with public key as secret) are rejected
//     before signature verification is attempted.
//   - Checks exp > now() and sub == expectedSub.
//   - Checks iss == JWTIssuer and aud == JWTAudience (AC4, Story 5.29c).
//   - Checks DB revocation via SessionLookupDB (AC1, Story 5.29c).
//   - Fail-fast: never returns both non-nil claims and a non-nil error simultaneously.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"crypto/ed25519"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
)

// JWTIssuer is the expected value of the "iss" claim in compliance JWTs (AC4, Story 5.29c).
const JWTIssuer = "nebu-gateway"

// JWTAudience is the expected value of the "aud" claim in compliance JWTs (AC4, Story 5.29c).
const JWTAudience = "compliance-export"

// SessionLookupDB is the consumer-defined interface for compliance session DB lookups.
// Implementations check whether a given token_hash is present in compliance_sessions
// with revoked_at IS NULL. Returns (true, nil) for active sessions, (false, nil) for
// missing or revoked tokens (AC1, Story 5.29c).
type SessionLookupDB interface {
	IsTokenActive(ctx context.Context, tokenHash []byte) (bool, error)
}

// ComplianceClaims holds the custom claims for a compliance access token.
// All time fields are RFC 3339 strings in the application domain; Exp/Iat are
// POSIX seconds (standard JWT numerics).
// Iss and Aud are the standard JWT issuer and audience claims (AC4, Story 5.29c).
type ComplianceClaims struct {
	Sub                 string `json:"sub"`
	ComplianceRequestID string `json:"compliance_request_id"`
	RoomID              string `json:"room_id"`
	TimeRangeStart      string `json:"time_range_start"` // RFC 3339
	TimeRangeEnd        string `json:"time_range_end"`   // RFC 3339
	Iat                 int64  `json:"iat"`
	Exp                 int64  `json:"exp"`
	Iss                 string `json:"iss"` // AC4: issuer claim
	Aud                 string `json:"aud"` // AC4: audience claim
}

// IssueComplianceToken signs claims using the provided Ed25519 private key and returns
// a compact JWS string (three-part dot-separated JWT). Algorithm is locked to EdDSA.
// The caller is responsible for setting Iss and Aud — IssueComplianceToken does not
// auto-fill them, so tests can create "bad" tokens with missing iss/aud to verify that
// ValidateComplianceToken rejects them (AC4).
func IssueComplianceToken(privKey ed25519.PrivateKey, claims ComplianceClaims) (string, error) {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.EdDSA, Key: privKey},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return "", fmt.Errorf("compliance/jwt: create EdDSA signer: %w", err)
	}

	raw, err := josejwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", fmt.Errorf("compliance/jwt: serialize JWT: %w", err)
	}
	return raw, nil
}

// ValidateComplianceToken parses tokenStr, verifies the EdDSA signature with pubKey,
// checks expiry (exp > now), sub == expectedSub, iss == JWTIssuer, aud == JWTAudience,
// and performs a DB revocation check via db.IsTokenActive (AC1, AC4, Story 5.29c).
// The provided ctx is used for the DB lookup so HTTP-request cancellations
// propagate (TEA Gate 2 MINOR-2 fix; was using context.Background()).
// Returns *ComplianceClaims on success. Returns (nil, error) on any validation failure
// — never returns both non-nil claims and a non-nil error.
func ValidateComplianceToken(ctx context.Context, tokenStr string, pubKey ed25519.PublicKey, expectedSub string, db SessionLookupDB) (*ComplianceClaims, error) {
	// Step 1: Parse with strict algorithm allow-list. Any token whose header
	// contains an algorithm other than EdDSA is rejected here — before signature
	// verification — which prevents algorithm-confusion attacks (Story 5.18).
	tok, err := jose.ParseSigned(tokenStr, []jose.SignatureAlgorithm{jose.EdDSA})
	if err != nil {
		return nil, fmt.Errorf("compliance/jwt: parse JWS: %w", err)
	}

	// Step 2: Verify signature. Verify returns the raw payload bytes on success.
	payload, err := tok.Verify(pubKey)
	if err != nil {
		return nil, fmt.Errorf("compliance/jwt: signature verification failed: %w", err)
	}

	// Step 3: Unmarshal claims from verified payload.
	var claims ComplianceClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("compliance/jwt: unmarshal claims: %w", err)
	}

	// Step 4: Check expiry. Token is expired when now > exp (standard JWT check).
	now := time.Now().Unix()
	if now > claims.Exp {
		return nil, errors.New("compliance/jwt: token expired")
	}

	// Step 5: Reject tokens with iat in the future beyond a small clock-skew
	// tolerance — a token timestamp ahead of "now" indicates either a malformed
	// payload or a forged claim. Kassandra MEDIUM (2026-04-23): RFC 7519 §4.1.6.
	const iatSkewToleranceSec = 30
	if claims.Iat > now+iatSkewToleranceSec {
		return nil, fmt.Errorf("compliance/jwt: iat in future (iat=%d, now=%d)", claims.Iat, now)
	}

	// Step 6: Check sub claim matches expected subject.
	if claims.Sub != expectedSub {
		return nil, fmt.Errorf("compliance/jwt: sub mismatch: expected %q, got %q", expectedSub, claims.Sub)
	}

	// Step 7: Check iss and aud claims (AC4, Story 5.29c).
	if claims.Iss != JWTIssuer {
		return nil, fmt.Errorf("compliance/jwt: iss mismatch: expected %q, got %q", JWTIssuer, claims.Iss)
	}
	if claims.Aud != JWTAudience {
		return nil, fmt.Errorf("compliance/jwt: aud mismatch: expected %q, got %q", JWTAudience, claims.Aud)
	}

	// Step 8: DB revocation check (AC1, Story 5.29c).
	// Compute SHA-256 of the raw token string and query compliance_sessions.
	tokenHash := sha256.Sum256([]byte(tokenStr))
	active, err := db.IsTokenActive(ctx, tokenHash[:])
	if err != nil {
		return nil, fmt.Errorf("compliance/jwt: revocation check failed: %w", err)
	}
	if !active {
		return nil, errors.New("compliance/jwt: token revoked")
	}

	return &claims, nil
}
