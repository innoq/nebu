defmodule Nebu.Signature do
  @moduledoc """
  Cryptographic key generation and signing operations for Nebu.

  Two separate keypairs per user (Architecture V1):
  - Signing: Ed25519 — message signing, non-repudiation
  - Encryption: X25519 — PII encryption via ECDH (Story 2.9)

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
end
