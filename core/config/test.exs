import Config

config :logger, level: :warning
config :signature, pii_encryption_key: :crypto.strong_rand_bytes(32)

# Story 5.5: suppress Compliance.SessionExpiryWorker from Application supervisor in test env
# so existing application_test.exs (Story 5.2 AC2) continues to assert children=[].
# Tests that require the worker start it directly via SessionExpiryWorker.start_link([]).
config :compliance, :workers, []
