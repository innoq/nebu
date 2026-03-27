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
end
