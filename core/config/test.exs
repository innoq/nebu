import Config

config :logger, level: :warning
config :signature, pii_encryption_key: :crypto.strong_rand_bytes(32)
