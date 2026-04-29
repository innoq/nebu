package compliance

// signing_key.go — Story 5.29c: FB-55-01 — Compliance signing key encrypted at rest (AC9)
//
// EnsureComplianceSigningKey: generates or loads an Ed25519 keypair from server_config.
//   - If no row exists, generates a new keypair, encrypts the private key using encryptFn,
//     and stores the result in server_config.
//   - If a row exists, delegates to LoadComplianceSigningKey.
//   - Returns the in-memory Ed25519 key pair for immediate use.
//
// LoadComplianceSigningKey: reads the encrypted private key from server_config and
//   decrypts it via decryptFn. Rejects plaintext keys (any value that is a valid
//   hex-encoded 64-byte Ed25519 key) to enforce the encrypted-at-rest invariant
//   after migration 000025_encrypt_compliance_signing_key has run.
//
// KeyEncryptFn / KeyDecryptFn: injectable function types (production: AES-256-GCM;
// tests: simple XOR). The ciphertext format is opaque to this package — the caller
// is responsible for using a matching encrypt/decrypt pair.
//
// Storage format:
//   server_config key='compliance_signing_key_priv' → "enc:" + hex(encryptFn(privKey bytes))
//   server_config key='compliance_signing_key_pub'  → hex.EncodeToString(pubKey bytes)
//   (public key is stored as plain hex — it is not a secret)
//
// The "enc:" prefix distinguishes encrypted values from legacy plaintext hex strings.
// LoadComplianceSigningKey rejects any stored value that does not start with "enc:",
// enforcing the encrypted-at-rest invariant after migration.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
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
// does not start with the "enc:" prefix — indicating it is a legacy plaintext
// hex-encoded private key (pre-migration state). After migration, this must
// never appear — it indicates the migration was not applied or was rolled back.
var ErrPlaintextKey = errors.New("compliance/signing_key: stored key appears to be plaintext; run migration to encrypt")

// encPrefix is prepended to all encrypted private key values in server_config.
// It distinguishes encrypted values from legacy plaintext hex strings.
const encPrefix = "enc:"

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
// server_config. Returns ErrPlaintextKey if the stored value appears to be a plain
// hex-encoded key (64 bytes = 128 hex chars), enforcing the encrypted-at-rest invariant.
func LoadComplianceSigningKey(ctx context.Context, db *sql.DB, decryptFn KeyDecryptFn) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	// Load the private key ciphertext (stored as hex of ciphertext bytes).
	storedPriv, err := loadStoredValue(ctx, db, "compliance_signing_key_priv")
	if err != nil {
		return nil, nil, fmt.Errorf("LoadComplianceSigningKey: query priv: %w", err)
	}

	// Reject plaintext: encrypted values must start with the "enc:" prefix.
	// Any stored value without this prefix is assumed to be a legacy plaintext
	// hex-encoded key (pre-migration state) and is refused.
	if len(storedPriv) < len(encPrefix) || storedPriv[:len(encPrefix)] != encPrefix {
		return nil, nil, ErrPlaintextKey
	}

	// Strip the "enc:" prefix, then decode hex → ciphertext bytes.
	ciphertext, err := hex.DecodeString(storedPriv[len(encPrefix):])
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

	// Already encrypted — nothing to do.
	if len(stored) >= len(encPrefix) && stored[:len(encPrefix)] == encPrefix {
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

