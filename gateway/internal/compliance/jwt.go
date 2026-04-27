package compliance

// jwt.go — Story 5.5: Compliance JWT helper
//
// IssueComplianceToken: signs a ComplianceClaims struct as an EdDSA JWT.
//   - Used internally by SessionHandler.PostSession.
//   - Algorithm is hard-wired to EdDSA (jose.EdDSA) — no other algorithm is accepted
//     for issuance (Story 5.18 lesson: algorithm pinning is mandatory).
//
// ValidateComplianceToken: parses, verifies, and validates a compliance JWT.
//   - Exported for reuse by Story 5.6 (X-Compliance-Token header validation).
//   - Pins algorithm to []jose.SignatureAlgorithm{jose.EdDSA} in ParseSigned call so
//     algorithm-confusion attacks (e.g. HS256 with public key as secret) are rejected
//     before signature verification is attempted.
//   - Checks exp > now() and sub == expectedSub.
//   - Fail-fast: never returns both non-nil claims and a non-nil error simultaneously.

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"crypto/ed25519"

	jose "github.com/go-jose/go-jose/v4"
	josejwt "github.com/go-jose/go-jose/v4/jwt"
)

// ComplianceClaims holds the custom claims for a compliance access token.
// All time fields are RFC 3339 strings in the application domain; Exp/Iat are
// POSIX seconds (standard JWT numerics).
type ComplianceClaims struct {
	Sub                 string `json:"sub"`
	ComplianceRequestID string `json:"compliance_request_id"`
	RoomID              string `json:"room_id"`
	TimeRangeStart      string `json:"time_range_start"` // RFC 3339
	TimeRangeEnd        string `json:"time_range_end"`   // RFC 3339
	Iat                 int64  `json:"iat"`
	Exp                 int64  `json:"exp"`
}

// IssueComplianceToken signs claims using the provided Ed25519 private key and returns
// a compact JWS string (three-part dot-separated JWT). Algorithm is locked to EdDSA.
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
// checks expiry (exp > now) and sub == expectedSub. Returns *ComplianceClaims on
// success. Returns (nil, error) on any validation failure — never returns both
// non-nil claims and a non-nil error.
func ValidateComplianceToken(tokenStr string, pubKey ed25519.PublicKey, expectedSub string) (*ComplianceClaims, error) {
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

	return &claims, nil
}
