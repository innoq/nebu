package compliance

// signing_key.go — Story 5.29c: FB-55-01 — Compliance signing key encrypted at rest (AC9)
// Story 5.29d: AC6 (FB-29c-2) — enc:v1: versioned envelope for KEK rotation support.
//
// EnsureComplianceSigningKey: generates or loads an Ed25519 keypair from server_config.
//   - If no row exists, generates a new keypair, encrypts the private key using encryptFn,
//     and stores the result in server_config.
//   - If a row exists, delegates to LoadComplianceSigningKey.
//   - Returns the in-memory Ed25519 key pair for immediate use.
//
// LoadComplianceSigningKey: reads the encrypted private key from server_config and
//   decrypts it via decryptFn. Rejects plaintext keys and unversioned "enc:" envelopes.
//   Only accepts "enc:v1:<hex>" format — other versions return an error containing
//   "unknown key version".
//
// KeyEncryptFn / KeyDecryptFn: injectable function types (production: AES-256-GCM;
// tests: simple XOR). The ciphertext format is opaque to this package — the caller
// is responsible for using a matching encrypt/decrypt pair.
//
// Storage format (5.29d+):
//   server_config key='compliance_signing_key_priv' → "enc:v1:" + hex(encryptFn(privKey bytes))
//   server_config key='compliance_signing_key_pub'  → hex.EncodeToString(pubKey bytes)
//   (public key is stored as plain hex — it is not a secret)
//
// The "enc:v1:" prefix distinguishes encrypted values from legacy plaintext hex strings.
// The version token ("v1") allows future KEK rotation: when rotating to a new KEK, the
// operator can update the version (e.g., "enc:v2:") so LoadComplianceSigningKey knows
// which KEK version was used for encryption.
//
// LoadComplianceSigningKey rejects:
//   - Values with no "enc:" prefix (plaintext — ErrPlaintextKey)
//   - Values with "enc:" but no version (unversioned — ErrUnknownKeyVersion)
//   - Values with an unrecognised version ("enc:v99:" — ErrUnknownKeyVersion)

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// KeyCryptoFn is a symmetric function type for key encryption and decryption.
// For encryption: input is plaintext key bytes, output is ciphertext.
// For decryption: input is ciphertext, output is plaintext key bytes.
// Using a single type allows the same function (e.g. XOR in tests) to serve both roles.
type KeyCryptoFn func(data []byte) ([]byte, error)

// KeyEncryptFn is an alias for KeyCryptoFn used when the function encrypts.
type KeyEncryptFn = KeyCryptoFn

// KeyDecryptFn is an alias for KeyCryptoFn used when the function decrypts.
type KeyDecryptFn = KeyCryptoFn

// ErrPlaintextKey is returned by LoadComplianceSigningKey when the stored value
// does not start with any "enc:" prefix — indicating it is a legacy plaintext
// hex-encoded private key (pre-migration state). After migration, this must
// never appear — it indicates the migration was not applied or was rolled back.
var ErrPlaintextKey = errors.New("compliance/signing_key: stored key appears to be plaintext; run migration to encrypt")

// ErrUnknownKeyVersion is returned by LoadComplianceSigningKey when the stored value
// starts with "enc:" but has an unrecognised or absent version token (e.g. bare "enc:"
// without a version, or "enc:v99:"). Operators must re-run EnsureComplianceSigningKey
// or the key rotation procedure to upgrade the stored value to the current format.
var ErrUnknownKeyVersion = errors.New("compliance/signing_key: unknown or unsupported key version in stored envelope")

// encBasePrefix is the minimal prefix that marks any encrypted value.
// All encrypted values start with this — used only for plaintext detection.
const encBasePrefix = "enc:"

// encV1Prefix is the versioned prefix written by EnsureComplianceSigningKey (5.29d+).
// LoadComplianceSigningKey accepts only this prefix; bare "enc:" and other versions are rejected.
const encV1Prefix = "enc:v1:"

// encPrefix is an alias for encV1Prefix used in EnsureComplianceSigningKey and
// MigrateLegacyPlaintextKey to keep write paths consistent.
// MigrateLegacyPlaintextKey always writes enc:v1: (not the legacy bare enc:).
const encPrefix = encV1Prefix

// EnsureComplianceSigningKey reads the Ed25519 keypair from server_config.
// If no private key row exists, a new keypair is generated, encrypted via encryptFn,
// and persisted. Returns the in-memory keypair regardless of whether it was
// generated or loaded.
//
// The encrypt function receives raw private key bytes (64 bytes for Ed25519) and
// returns an opaque ciphertext that will be stored in server_config (prefixed with "enc:").
// The decrypt function is the inverse — used for re-reading the stored value.
// For symmetric schemes (tests: XOR), encryptFn and decryptFn may be the same function.
//
// Race handling: ON CONFLICT DO NOTHING is used so concurrent instances cannot
// overwrite each other's keys. After insert, the function re-reads to return
// whichever key actually won.
func EnsureComplianceSigningKey(ctx context.Context, db *sql.DB, encryptFn KeyEncryptFn, decryptFn ...KeyDecryptFn) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	// Determine decrypt function: use decryptFn[0] if provided, otherwise use encryptFn
	// (for symmetric test ciphers like XOR).
	decFn := KeyDecryptFn(encryptFn)
	if len(decryptFn) > 0 && decryptFn[0] != nil {
		decFn = decryptFn[0]
	}

	// Try to load an existing encrypted key first.
	stored, err := loadStoredValue(ctx, db, "compliance_signing_key_priv")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, fmt.Errorf("EnsureComplianceSigningKey: query: %w", err)
	}

	if stored != "" {
		// Key exists — delegate to LoadComplianceSigningKey.
		return LoadComplianceSigningKey(ctx, db, decFn)
	}

	// No key yet: generate a fresh Ed25519 keypair.
	pubKey, privKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("EnsureComplianceSigningKey: generate key: %w", err)
	}

	// Encrypt the private key bytes.
	ciphertext, err := encryptFn([]byte(privKey))
	if err != nil {
		return nil, nil, fmt.Errorf("EnsureComplianceSigningKey: encrypt private key: %w", err)
	}

	// Store ciphertext with "enc:" prefix + hex encoding; public key as plain hex.
	// The "enc:" prefix distinguishes encrypted values from legacy plaintext hex strings.
	// Using hex for the ciphertext bytes ensures safe storage in TEXT columns.
	ciphertextHex := encPrefix + hex.EncodeToString(ciphertext)
	pubHex := hex.EncodeToString(pubKey)
	setAt := time.Now().UnixMilli()

	_, err = db.ExecContext(ctx,
		`INSERT INTO server_config (key, value, set_at) VALUES
		   ('compliance_signing_key_priv', $1, $3),
		   ('compliance_signing_key_pub',  $2, $3)
		 ON CONFLICT (key) DO NOTHING`,
		ciphertextHex, pubHex, setAt,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("EnsureComplianceSigningKey: insert: %w", err)
	}

	// Re-read to get whichever key actually won the race.
	return LoadComplianceSigningKey(ctx, db, decFn)
}

// LoadComplianceSigningKey reads and decrypts the compliance Ed25519 private key from
// server_config.
//
// Accepted formats:
//   - "enc:v1:<hex(ciphertext)>" — the current format (5.29d+); decrypts via decryptFn.
//
// Rejected formats (return error):
//   - Any value without an "enc:" prefix → ErrPlaintextKey (pre-migration plaintext)
//   - "enc:" without a recognised version (bare "enc:") → ErrUnknownKeyVersion
//   - "enc:v<N>:" where N is not a recognised version → ErrUnknownKeyVersion
func LoadComplianceSigningKey(ctx context.Context, db *sql.DB, decryptFn KeyDecryptFn) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	// Load the private key ciphertext (stored as hex of ciphertext bytes).
	storedPriv, err := loadStoredValue(ctx, db, "compliance_signing_key_priv")
	if err != nil {
		return nil, nil, fmt.Errorf("LoadComplianceSigningKey: query priv: %w", err)
	}

	// Step 1: reject plaintext — encrypted values must start with "enc:".
	if !strings.HasPrefix(storedPriv, encBasePrefix) {
		return nil, nil, ErrPlaintextKey
	}

	// Step 2: require a recognised version token. Only "enc:v1:" is accepted.
	// Bare "enc:" (no version) and unknown versions (enc:v99:, etc.) are rejected.
	if !strings.HasPrefix(storedPriv, encV1Prefix) {
		// The value starts with "enc:" but is not "enc:v1:..." — unknown version.
		return nil, nil, fmt.Errorf("%w: stored value starts with %q but only %q is supported",
			ErrUnknownKeyVersion, storedPriv[:min(len(storedPriv), 12)], encV1Prefix)
	}

	// Strip the "enc:v1:" prefix, then decode hex → ciphertext bytes.
	ciphertext, err := hex.DecodeString(storedPriv[len(encV1Prefix):])
	if err != nil {
		return nil, nil, fmt.Errorf("LoadComplianceSigningKey: decode ciphertext hex: %w", err)
	}

	// Decrypt via the provided key.
	privBytes, err := decryptFn(ciphertext)
	if err != nil {
		return nil, nil, fmt.Errorf("LoadComplianceSigningKey: decrypt: %w", err)
	}

	privKey := ed25519.PrivateKey(privBytes)
	pubKey := privKey.Public().(ed25519.PublicKey)

	return privKey, pubKey, nil
}

// loadStoredValue fetches a single value from server_config by key.
// Returns (value, nil) on success, ("", sql.ErrNoRows) when no row exists.
func loadStoredValue(ctx context.Context, db *sql.DB, key string) (string, error) {
	var val string
	err := db.QueryRowContext(ctx,
		`SELECT value FROM server_config WHERE key = $1`, key,
	).Scan(&val)
	if errors.Is(err, sql.ErrNoRows) {
		return "", sql.ErrNoRows
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// MigrateLegacyPlaintextKey upgrades a plaintext compliance_signing_key_priv
// stored prior to Story 5.29c (= 128-char hex of the raw 64-byte Ed25519
// private key) into the new "enc:<hex(ciphertext)>" format.
//
// This MUST run with a connection that bypasses RLS — the server_config
// table has only INSERT and SELECT policies, so UPDATE is denied for the
// runtime nebu_app role. Pass the migration DB handle (post Story 5.29a:
// cfg.DBURLMigrate / nebu_migrate, which has BYPASSRLS).
//
// Idempotent: if no row exists, or if the row already starts with "enc:",
// or if the row is some other unexpected format, the function logs and
// returns nil without touching the DB. Only the precise legacy shape
// (128 lower-hex chars decoding to 64 bytes) is rewritten.
//
// New deployments hit the no-row branch and this function is a cheap no-op.
func MigrateLegacyPlaintextKey(ctx context.Context, migrateDB *sql.DB, encryptFn KeyEncryptFn) error {
	stored, err := loadStoredValue(ctx, migrateDB, "compliance_signing_key_priv")
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // fresh deployment — nothing to migrate
		}
		return fmt.Errorf("MigrateLegacyPlaintextKey: query: %w", err)
	}

	// Already encrypted — nothing to do. Check for both old bare "enc:" rows and
	// new "enc:v1:" rows so MigrateLegacyPlaintextKey is idempotent across all
	// post-5.29c deployments regardless of whether they have been upgraded to v1 format.
	if strings.HasPrefix(stored, encBasePrefix) {
		return nil
	}

	// Legacy plaintext is exactly hex(privKey) — 128 chars, valid hex, decoding to 64 bytes.
	// Anything else is unexpected; refuse to touch it so we don't corrupt unrelated values.
	if len(stored) != ed25519.PrivateKeySize*2 {
		return fmt.Errorf("MigrateLegacyPlaintextKey: stored value has unexpected length %d (want %d for plaintext or %q-prefix for encrypted)",
			len(stored), ed25519.PrivateKeySize*2, encPrefix)
	}
	privBytes, err := hex.DecodeString(stored)
	if err != nil {
		return fmt.Errorf("MigrateLegacyPlaintextKey: stored value is not valid hex: %w", err)
	}

	// Re-encrypt with the production encrypt fn and UPDATE the row.
	ciphertext, err := encryptFn(privBytes)
	if err != nil {
		return fmt.Errorf("MigrateLegacyPlaintextKey: encrypt: %w", err)
	}
	encoded := encPrefix + hex.EncodeToString(ciphertext)
	setAt := time.Now().UnixMilli()
	if _, err := migrateDB.ExecContext(ctx,
		`UPDATE server_config SET value = $1, set_at = $2 WHERE key = 'compliance_signing_key_priv'`,
		encoded, setAt,
	); err != nil {
		return fmt.Errorf("MigrateLegacyPlaintextKey: update: %w", err)
	}
	return nil
}

