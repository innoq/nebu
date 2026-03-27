defmodule Nebu.SignatureTest do
  use ExUnit.Case, async: true

  alias Nebu.Signature

  describe "generate_signing_keypair/0" do
    test "returns binary keypair of correct length" do
      {pub, priv} = Signature.generate_signing_keypair()
      assert is_binary(pub)
      assert is_binary(priv)
      assert byte_size(pub) == 32
      # OTP :crypto.generate_key(:eddsa, :ed25519) returns 32-byte private key seed
      # (Story spec said 64 bytes — that's the libsodium convention; OTP uses seed-only format)
      assert byte_size(priv) == 32
    end

    test "sign_and_verify: signature over message verifies correctly" do
      {pub, priv} = Signature.generate_signing_keypair()
      message = "hello nebu"
      signature = :crypto.sign(:eddsa, :none, message, [priv, :ed25519])
      assert :crypto.verify(:eddsa, :none, message, signature, [pub, :ed25519])
    end

    test "tampered_message: modified message fails verification" do
      {pub, priv} = Signature.generate_signing_keypair()
      message = "hello nebu"
      signature = :crypto.sign(:eddsa, :none, message, [priv, :ed25519])
      tampered = "hello nebu TAMPERED"
      refute :crypto.verify(:eddsa, :none, tampered, signature, [pub, :ed25519])
    end
  end

  describe "generate_encryption_keypair/0" do
    test "returns binary keypair of correct length" do
      {pub, priv} = Signature.generate_encryption_keypair()
      assert is_binary(pub)
      assert is_binary(priv)
      assert byte_size(pub) == 32
      assert byte_size(priv) == 32
    end

    test "ecdh_shared_secret: ECDH exchange produces identical shared secrets on both sides" do
      {alice_pub, alice_priv} = Signature.generate_encryption_keypair()
      {bob_pub, bob_priv} = Signature.generate_encryption_keypair()

      alice_shared = :crypto.compute_key(:ecdh, bob_pub, alice_priv, :x25519)
      bob_shared = :crypto.compute_key(:ecdh, alice_pub, bob_priv, :x25519)

      assert alice_shared == bob_shared
      assert byte_size(alice_shared) == 32
    end
  end

  describe "derive_aes_key/1" do
    test "returns a 32-byte AES-256 key from shared secret" do
      {_alice_pub, alice_priv} = Signature.generate_encryption_keypair()
      {bob_pub, _bob_priv} = Signature.generate_encryption_keypair()

      shared = :crypto.compute_key(:ecdh, bob_pub, alice_priv, :x25519)
      aes_key = Signature.derive_aes_key(shared)

      assert is_binary(aes_key)
      assert byte_size(aes_key) == 32
    end

    test "derive_aes_key is deterministic: same input yields same key" do
      shared_secret = :crypto.strong_rand_bytes(32)
      assert Signature.derive_aes_key(shared_secret) == Signature.derive_aes_key(shared_secret)
    end
  end
end
