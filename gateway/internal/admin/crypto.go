package admin

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// encryptAES256GCM encrypts plaintext using AES-256-GCM.
// The key is derived from secret via SHA-256. Returns hex-encoded nonce||ciphertext.
func encryptAES256GCM(secret []byte, plaintext string) (string, error) {
	keyHash := sha256.Sum256(secret)
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// DecryptAES256GCM is the exported variant of decryptAES256GCM.
// Used by main.go to decrypt stored secrets (e.g. scim_bearer_token) using the
// gateway internal secret. Prefer the unexported variant within the admin package.
func DecryptAES256GCM(secret []byte, hexCiphertext string) (string, error) {
	return decryptAES256GCM(secret, hexCiphertext)
}

// decryptAES256GCM decrypts a hex-encoded nonce||ciphertext produced by encryptAES256GCM.
// The key is derived from secret via SHA-256.
func decryptAES256GCM(secret []byte, hexCiphertext string) (string, error) {
	keyHash := sha256.Sum256(secret)
	data, err := hex.DecodeString(hexCiphertext)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(keyHash[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
