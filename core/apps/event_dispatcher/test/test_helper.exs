ExUnit.start()
# Exclude :integration-tagged tests from unit test runs (mix test without NEBU_DB_URL).
# Integration tests require a live PostgreSQL instance and are only run in CI
# via make test-integration (which sets NEBU_DB_URL and starts Nebu.Repo).
ExUnit.configure(exclude: [:integration])
