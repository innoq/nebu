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
end
