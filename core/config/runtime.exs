import Config

# Placeholder — NEBU_* env vars added as each story requires them
# e.g., Story 1.3 adds database URL:
# config :room_manager, Nebu.Repo,
#   url: System.get_env("NEBU_DB_URL") || raise "NEBU_DB_URL not set"

if config_env() in [:prod, :dev] do
  pii_key_hex =
    System.get_env("NEBU_PII_ENCRYPTION_KEY") ||
      raise "NEBU_PII_ENCRYPTION_KEY is not set. Must be a 64-char hex string (32 bytes)."

  pii_key =
    case Base.decode16(pii_key_hex, case: :mixed) do
      {:ok, decoded} -> decoded
      :error -> raise "NEBU_PII_ENCRYPTION_KEY is not valid hex. Must be a 64-char hex string."
    end

  unless byte_size(pii_key) == 32 do
    raise "NEBU_PII_ENCRYPTION_KEY must decode to exactly 32 bytes, got #{byte_size(pii_key)}"
  end

  config :signature, pii_encryption_key: pii_key
end
