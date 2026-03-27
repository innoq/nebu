defmodule Nebu.Signature do
  @moduledoc """
  Cryptographic key generation and signing operations for Nebu.

  Two separate keypairs per user (Architecture V1):
  - Signing: Ed25519 — message signing, non-repudiation
  - Encryption: X25519 — PII encryption via ECDH key agreement

  Both use OTP native :crypto — no external dependencies.
  """

  @doc """
  Generates an Ed25519 signing keypair.

  Returns `{public_key, private_key}` as binaries.
  - public_key: 32 bytes
  - private_key: 32 bytes (OTP seed format; libsodium convention uses 64 bytes)

  Uses OTP native `:crypto.generate_key(:eddsa, :ed25519)` (available since OTP 24+).
  """
  @spec generate_signing_keypair() :: {binary(), binary()}
  def generate_signing_keypair do
    :crypto.generate_key(:eddsa, :ed25519)
  end

  @doc """
  Generates an X25519 encryption keypair.

  Returns `{public_key, private_key}` as binaries.
  - public_key: 32 bytes
  - private_key: 32 bytes

  Used for PII encryption via ECDH key agreement (Architecture V1).
  Uses OTP native `:crypto.generate_key(:ecdh, :x25519)` (available since OTP 24+).
  """
  @spec generate_encryption_keypair() :: {binary(), binary()}
  def generate_encryption_keypair do
    :crypto.generate_key(:ecdh, :x25519)
  end

  @doc """
  Derives a 32-byte AES-256 key from an ECDH shared secret.

  Uses SHA-256 (`:crypto.hash(:sha256, shared_secret)`) — deterministic, no external deps.
  Input must be a 32-byte binary (X25519 ECDH shared secret).

  Used as the symmetric key for AES-256-GCM encryption in Stories 2.10 and 2.11.
  """
  @spec derive_aes_key(binary()) :: binary()
  def derive_aes_key(shared_secret) do
    :crypto.hash(:sha256, shared_secret)
  end

  @doc """
  Encrypts Operational PII (Tier 1: display_name, avatar_url) with the server-side AES-256 key.

  Returns `{ciphertext, nonce}` where:
  - `ciphertext` is the encrypted data with appended 16-byte GCM auth tag (`encrypted_bytes <> tag`)
  - `nonce` is a freshly generated 12-byte random nonce (must be stored alongside ciphertext)

  `server_key` must be a 32-byte binary (decoded from `NEBU_PII_ENCRYPTION_KEY`).
  """
  @spec encrypt_operational_pii(binary(), binary()) :: {binary(), binary()}
  def encrypt_operational_pii(plaintext, server_key) do
    nonce = :crypto.strong_rand_bytes(12)
    {ciphertext, tag} = :crypto.crypto_one_time_aead(:aes_256_gcm, server_key, nonce, plaintext, <<>>, 16, true)
    {ciphertext <> tag, nonce}
  end

  @doc """
  Decrypts Operational PII (Tier 1) encrypted by `encrypt_operational_pii/2`.

  Returns `{:ok, plaintext}` on success.
  Returns `{:error, :decryption_failed}` if authentication fails (wrong key, tampered data).

  `ciphertext` must include the 16-byte GCM auth tag appended by `encrypt_operational_pii/2`.
  `nonce` must be the 12-byte nonce returned during encryption.
  `server_key` must match the key used during encryption.
  """
  @spec decrypt_operational_pii(binary(), binary(), binary()) :: {:ok, binary()} | {:error, :decryption_failed}
  def decrypt_operational_pii(ciphertext_with_tag, nonce, server_key) do
    ct_size = byte_size(ciphertext_with_tag) - 16
    <<ciphertext::binary-size(ct_size), tag::binary-size(16)>> = ciphertext_with_tag

    try do
      case :crypto.crypto_one_time_aead(:aes_256_gcm, server_key, nonce, ciphertext, <<>>, tag, false) do
        :error -> {:error, :decryption_failed}
        plaintext -> {:ok, plaintext}
      end
    rescue
      _ -> {:error, :decryption_failed}
    end
  end

  @doc """
  Encrypts Sensitive PII (Tier 2: email, IdP subject) with the recipient's X25519 public key.

  Uses ephemeral ECDH: a fresh X25519 keypair is generated per call, shared secret derived,
  then AES-256-GCM encryption applied. The ephemeral public key must be stored alongside
  the ciphertext so decryption can reconstruct the shared secret.

  Returns `{ciphertext, ephemeral_public_key, nonce}` where:
  - `ciphertext` = encrypted data with appended 16-byte GCM auth tag
  - `ephemeral_public_key` = 32-byte public key of the ephemeral sender (store in DB)
  - `nonce` = 12-byte random nonce (store in DB)

  DSGVO: deleting `recipient_private_key` renders this data permanently irrecoverable.
  """
  @spec encrypt_sensitive_pii(binary(), binary()) :: {binary(), binary(), binary()}
  def encrypt_sensitive_pii(plaintext, recipient_public_key) do
    {ephemeral_pub, ephemeral_priv} = :crypto.generate_key(:ecdh, :x25519)
    shared = :crypto.compute_key(:ecdh, recipient_public_key, ephemeral_priv, :x25519)
    aes_key = derive_aes_key(shared)
    nonce = :crypto.strong_rand_bytes(12)
    {ciphertext, tag} = :crypto.crypto_one_time_aead(:aes_256_gcm, aes_key, nonce, plaintext, <<>>, 16, true)
    {ciphertext <> tag, ephemeral_pub, nonce}
  end

  @doc """
  Decrypts Sensitive PII (Tier 2) encrypted by `encrypt_sensitive_pii/2`.

  Returns `{:ok, plaintext}` on success.
  Returns `{:error, :no_private_key}` when `recipient_private_key` is `nil` — DSGVO deletion case.
  Returns `{:error, :decryption_failed}` if AES-GCM authentication fails.

  `recipient_private_key` must match the public key used during encryption.
  `ephemeral_public_key` must be the 32-byte value returned by `encrypt_sensitive_pii/2`.
  `nonce` must be the 12-byte value returned by `encrypt_sensitive_pii/2`.
  """
  @spec decrypt_sensitive_pii(binary(), binary(), binary(), binary() | nil) ::
          {:ok, binary()} | {:error, :no_private_key} | {:error, :decryption_failed}
  def decrypt_sensitive_pii(_, _, _, nil), do: {:error, :no_private_key}

  def decrypt_sensitive_pii(ciphertext_with_tag, ephemeral_public_key, nonce, recipient_private_key) do
    shared = :crypto.compute_key(:ecdh, ephemeral_public_key, recipient_private_key, :x25519)
    aes_key = derive_aes_key(shared)
    ct_size = byte_size(ciphertext_with_tag) - 16
    <<ciphertext::binary-size(ct_size), tag::binary-size(16)>> = ciphertext_with_tag

    try do
      case :crypto.crypto_one_time_aead(:aes_256_gcm, aes_key, nonce, ciphertext, <<>>, tag, false) do
        :error -> {:error, :decryption_failed}
        plaintext -> {:ok, plaintext}
      end
    rescue
      _ -> {:error, :decryption_failed}
    end
  end
end
