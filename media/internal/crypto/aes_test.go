package crypto

// ─── Story 4-19: AES-256-GCM crypto unit tests ───────────────────────────────
//
// These tests are written FIRST (ATDD gate), before any implementation exists.
// ALL tests in this file are expected to FAIL until Story 4-19 is implemented.
//
// Test strategy:
//   - Tests call Encrypt, GenerateKey, GenerateNonce — none of which exist yet.
//   - Compilation will fail: "undefined: Encrypt", "undefined: GenerateKey", etc.
//   - Once aes.go is written with the correct signatures, tests must pass.
//
// Functions under test (from story spec):
//   func Encrypt(plaintext, key, nonce []byte) ([]byte, error)
//   func GenerateKey() ([]byte, error)
//   func GenerateNonce() ([]byte, error)

import (
	"bytes"
	"testing"
)

// ─── Test 1: Encrypt produces output that is NOT identical to plaintext ───────
//
// AC #4 — encrypted bytes must differ from the raw plaintext.
// Also validates GCM output length = len(plaintext) + 16 (auth tag).

func TestEncrypt_ProducesNonIdenticalOutput(t *testing.T) {
	key := make([]byte, 32)  // 32 zero bytes — valid AES-256 key
	nonce := make([]byte, 12) // 12 zero bytes — valid GCM nonce

	plaintext := make([]byte, 1024)
	for i := range plaintext {
		plaintext[i] = byte(i % 256)
	}

	ciphertext, err := Encrypt(plaintext, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt returned unexpected error: %v", err)
	}

	// Encrypted output must NOT be equal to plaintext.
	if bytes.Equal(ciphertext, plaintext) {
		t.Error("Encrypt returned output identical to plaintext — encryption did not occur")
	}

	// GCM Seal appends a 16-byte authentication tag; ciphertext len = len(plaintext) + 16.
	expectedLen := len(plaintext) + 16
	if len(ciphertext) != expectedLen {
		t.Errorf("expected ciphertext length %d (plaintext + 16-byte auth tag), got %d", expectedLen, len(ciphertext))
	}
}

// ─── Test 2: Two calls with same key but different nonce produce different output ─
//
// Verifies nonce uniqueness matters — same plaintext + same key but two distinct
// nonces must yield two distinct ciphertexts (GCM is nonce-dependent).

func TestEncrypt_SameKeyDifferentNonce(t *testing.T) {
	key := make([]byte, 32)

	nonce1 := make([]byte, 12)
	nonce1[0] = 0x01

	nonce2 := make([]byte, 12)
	nonce2[0] = 0x02

	plaintext := []byte("hello nebu media gateway")

	ct1, err := Encrypt(plaintext, key, nonce1)
	if err != nil {
		t.Fatalf("Encrypt (nonce1) returned error: %v", err)
	}

	ct2, err := Encrypt(plaintext, key, nonce2)
	if err != nil {
		t.Fatalf("Encrypt (nonce2) returned error: %v", err)
	}

	if bytes.Equal(ct1, ct2) {
		t.Error("Encrypt with different nonces produced identical ciphertext — nonce is being ignored")
	}
}

// ─── Test 3: GenerateKey returns exactly 32 bytes ─────────────────────────────
//
// AC #4 — AES-256 requires a 256-bit (32-byte) key.

func TestEncrypt_KeyLength(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	if len(key) != 32 {
		t.Errorf("GenerateKey returned %d bytes, expected 32", len(key))
	}
}

// ─── Test 4: GenerateNonce returns exactly 12 bytes ──────────────────────────
//
// GCM nonces are 96-bit (12 bytes). Any other size will be rejected by
// crypto/cipher.NewGCM's Seal/Open methods.

func TestGenerateNonce_Length(t *testing.T) {
	nonce, err := GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce() unexpected error: %v", err)
	}
	if len(nonce) != 12 {
		t.Errorf("GenerateNonce() returned %d bytes, expected 12 (96-bit GCM nonce)", len(nonce))
	}
}

// ─── Test 5: Decrypt(Encrypt(plaintext)) == plaintext (round-trip) ───────────
//
// Validates that encryption is reversible — the decrypted output must be
// byte-for-byte identical to the original plaintext.
//
// NOTE: This test also exercises GenerateKey and GenerateNonce for realistic key
// material (not zero bytes), covering both happy-path functions in one test.

func TestDecrypt_RoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}

	nonce, err := GenerateNonce()
	if err != nil {
		t.Fatalf("GenerateNonce returned error: %v", err)
	}

	plaintext := []byte("round-trip test: nebu AES-256-GCM media upload")

	ciphertext, err := Encrypt(plaintext, key, nonce)
	if err != nil {
		t.Fatalf("Encrypt returned error: %v", err)
	}

	decrypted, err := Decrypt(ciphertext, key, nonce)
	if err != nil {
		t.Fatalf("Decrypt returned error: %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Decrypt(Encrypt(plaintext)) != plaintext\nwant: %q\ngot:  %q", plaintext, decrypted)
	}
}
