import Config

config :logger, level: :warning
config :signature, pii_encryption_key: :crypto.strong_rand_bytes(32)

# Story 5.5: suppress Compliance.SessionExpiryWorker from Application supervisor in test env
# so existing application_test.exs (Story 5.2 AC2) continues to assert children=[].
# Tests that require the worker start it directly via SessionExpiryWorker.start_link([]).
config :compliance, :workers, []

# Epic 9 HIGH-1 fix: default to NoOpAuditWriter in test env so gRPC handler tests
# that add audit calls (deactivate_user, reactivate_user, update_user_role) do not
# require a live Nebu.Repo connection. Tests that need to verify audit emission
# override this via Application.put_env(:compliance, :audit_writer, FakeAuditWriter).
config :compliance, :audit_writer, Compliance.NoOpAuditWriter
