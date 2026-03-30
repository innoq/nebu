# Story 2.13: User Provisioning Orchestration — Keypairs + PII Encryption

Status: done

## Story

As a developer,
I want keypair generation and PII encryption to run automatically when a user record is first created,
so that every user has cryptographic identity and protected PII from their first login.

## Acceptance Criteria

1. **Given** a new user record created in Story 2.12,
   **When** provisioning completes,
   **Then** two keypairs are generated and stored: one Ed25519 signing keypair (`key_type = 'signing'`, `algorithm = 'ed25519'`) and one X25519 encryption keypair (`key_type = 'encryption'`, `algorithm = 'x25519'`) in the `user_keys` table

2. **Given** keypairs are stored,
   **When** the `users` table row is inspected,
   **Then** `signing_key_id` and `encryption_key_id` reference the respective `key_id` values in `user_keys`

3. **Given** `preferred_username` and `email` claims from the JWT,
   **When** a new user is provisioned,
   **Then** `display_name` is encrypted with the server PII key (Tier 1, `encrypt_operational_pii/2`) and stored as `users.display_name_encrypted + users.display_name_nonce`, and `email` is encrypted with the user's X25519 public key (Tier 2, `encrypt_sensitive_pii/2`) and stored as `users.email_encrypted + users.email_nonce + users.email_ephemeral_pub`

4. **Given** the entire provisioning process,
   **When** it runs,
   **Then** it executes within a single PostgreSQL transaction — either all succeed or all roll back

5. **Given** a unit test with a fake provisioner,
   **When** `provision_user/4` is called twice for the same `user_id`,
   **Then** the second call sees `signing_key_id IS NOT NULL` and the UPDATE affects 0 rows (idempotency guard in Postgres implementation)

## Tasks / Subtasks

- [x] Create migration `000006_users_email_pii` (AC: #3)
  - [x] Create `gateway/migrations/000006_users_email_pii.up.sql` — ALTER TABLE users ADD COLUMN email_encrypted, email_nonce, email_ephemeral_pub
  - [x] Create `gateway/migrations/000006_users_email_pii.down.sql`
  - [x] Update `gateway/migrations/migrations_test.go` — add 000006 files to `TestFS_ContainsExpectedMigrationFiles`

- [x] Add `{:signature, in_umbrella: true}` to `session_manager/mix.exs` deps (AC: #1, #2, #3)

- [x] Create `Nebu.Session.UserProvisioner` behaviour + delegation (AC: #1–#5)
  - [x] Create `core/apps/session_manager/lib/nebu/session/user_provisioner.ex`
  - [x] Define `@callback provision_user(user_id, display_name, email, server_key) :: {:ok, :provisioned} | {:error, term()}`
  - [x] Implement delegating `provision_user/4` via `Application.get_env(:session_manager, :provisioner_module, UserProvisioner.Postgres)`

- [x] Create `Nebu.Session.UserProvisioner.Postgres` implementation (AC: #1–#4)
  - [x] Create `core/apps/session_manager/lib/nebu/session/user_provisioner/postgres.ex`
  - [x] Generate signing keypair + encryption keypair and UUIDs for key_ids **before** the transaction
  - [x] Encrypt display_name with server_key via `Nebu.Signature.encrypt_operational_pii/2` before transaction
  - [x] Encrypt email with enc_pub via `Nebu.Signature.encrypt_sensitive_pii/2` before transaction
  - [x] Open `Nebu.Repo.transaction/1` wrapping all DB writes
  - [x] INSERT signing keypair into `user_keys` via raw SQL
  - [x] INSERT encryption keypair into `user_keys` via raw SQL
  - [x] UPDATE users with key_ids + encrypted PII via raw SQL (with `WHERE signing_key_id IS NULL` guard)
  - [x] Return `{:ok, :provisioned}` or `Nebu.Repo.rollback(reason)` on error

- [x] Write unit tests (AC: unit test)
  - [x] Create `core/apps/session_manager/test/nebu/session/user_provisioner_test.exs`
  - [x] Define `FakeProvisioner` (ETS-backed, records calls, returns `{:ok, :provisioned}`)
  - [x] Define `FailingProvisioner` (returns `{:error, :db_error}`)
  - [x] Test: `provision_user/4` returns `{:ok, :provisioned}` with FakeProvisioner
  - [x] Test: `provision_user/4` records correct user_id and display_name arguments
  - [x] Test: `provision_user/4` propagates `{:error, reason}` from DB module
  - [x] Confirm `mix test apps/session_manager --warnings-as-errors` passes

## Dev Notes

### Scope

This story does **three things**:
1. Adds a Go migration (`000006`) for email PII columns to the `users` table
2. Creates `Nebu.Session.UserProvisioner` behaviour + Postgres implementation
3. Orchestrates: keypair generation → `user_keys` inserts → `users` UPDATE, all in one transaction

**NOT in this story:**
- Calling `provision_user/4` from the `ValidateToken` gRPC handler — that is Story 2.14
- Bootstrap mode / `instance_admin` auto-assignment — Story 2.15
- Proto changes for email/display_name in `ValidateTokenRequest` — Story 2.14's concern
- DSGVO private key deletion — Story 5.7
- Any changes to `upsert_user/2` — do NOT touch Story 2.12's code

### Step 1: Go Migration `000006_users_email_pii`

The users table from Story 2.1 has `display_name_encrypted` / `display_name_nonce` columns but no email columns. Story 2.13 adds them.

**`gateway/migrations/000006_users_email_pii.up.sql`:**
```sql
-- gateway/migrations/000006_users_email_pii.up.sql
-- Add Sensitive PII (Tier 2) columns for email storage to users table.
-- email is encrypted with the user's X25519 public key via ephemeral ECDH (Story 2.13).
-- Three columns needed: ciphertext+tag, nonce, ephemeral public key (required for ECDH decrypt).

ALTER TABLE users ADD COLUMN email_encrypted     BYTEA;
ALTER TABLE users ADD COLUMN email_nonce         BYTEA;
ALTER TABLE users ADD COLUMN email_ephemeral_pub BYTEA;
```

**`gateway/migrations/000006_users_email_pii.down.sql`:**
```sql
-- gateway/migrations/000006_users_email_pii.down.sql
ALTER TABLE users DROP COLUMN IF EXISTS email_ephemeral_pub;
ALTER TABLE users DROP COLUMN IF EXISTS email_nonce;
ALTER TABLE users DROP COLUMN IF EXISTS email_encrypted;
```

**Update `gateway/migrations/migrations_test.go`:** Add to the `files` slice:
```go
"000006_users_email_pii.up.sql",
"000006_users_email_pii.down.sql",
```

### Step 2: Update `session_manager/mix.exs`

Add `{:signature, in_umbrella: true}` to deps so `UserProvisioner.Postgres` can call `Nebu.Signature.*`:

```elixir
defp deps do
  [
    {:nebu_db, in_umbrella: true},
    {:signature, in_umbrella: true}
  ]
end
```

### Step 3: `Nebu.Session.UserProvisioner` Behaviour + Delegation

**`core/apps/session_manager/lib/nebu/session/user_provisioner.ex`:**

```elixir
defmodule Nebu.Session.UserProvisioner do
  @moduledoc """
  Orchestrates user provisioning on first login:
  - Generates Ed25519 signing keypair + X25519 encryption keypair
  - Stores both in user_keys table
  - Encrypts display_name (Tier 1) and email (Tier 2)
  - Updates users table with key_ids + encrypted PII
  - Runs all DB writes in a single PostgreSQL transaction

  Delegates to a configurable module for testability.
  Real implementation: Nebu.Session.UserProvisioner.Postgres
  Test implementation: configured via Application.put_env in test setup.

  Called by Story 2.14 ValidateToken handler for new users only.
  """

  @callback provision_user(
              user_id :: String.t(),
              display_name :: String.t(),
              email :: String.t(),
              server_key :: binary()
            ) :: {:ok, :provisioned} | {:error, term()}

  @doc """
  Provisions a new user with keypairs and encrypted PII.
  - user_id: Matrix user ID (e.g. "@kai:nebu.local")
  - display_name: from OIDC preferred_username claim
  - email: from OIDC email claim (Sensitive PII, Tier 2)
  - server_key: 32-byte binary from Application.get_env(:signature, :pii_encryption_key) (Tier 1)

  Returns {:ok, :provisioned} on success, {:error, reason} on failure.
  Idempotent: UPDATE uses WHERE signing_key_id IS NULL — safe to call twice.
  """
  @spec provision_user(String.t(), String.t(), String.t(), binary()) ::
          {:ok, :provisioned} | {:error, term()}
  def provision_user(user_id, display_name, email, server_key) do
    provisioner_module().provision_user(user_id, display_name, email, server_key)
  end

  defp provisioner_module do
    Application.get_env(:session_manager, :provisioner_module, Nebu.Session.UserProvisioner.Postgres)
  end
end
```

### Step 4: `Nebu.Session.UserProvisioner.Postgres` Implementation

**`core/apps/session_manager/lib/nebu/session/user_provisioner/postgres.ex`:**

```elixir
defmodule Nebu.Session.UserProvisioner.Postgres do
  @moduledoc "PostgreSQL implementation of Nebu.Session.UserProvisioner."

  @behaviour Nebu.Session.UserProvisioner

  @insert_key_sql """
  INSERT INTO user_keys (key_id, user_id, key_type, algorithm, public_key, private_key, created_at)
  VALUES ($1, $2, $3, $4, $5, $6, $7)
  """

  @update_user_sql """
  UPDATE users
  SET signing_key_id       = $1,
      encryption_key_id    = $2,
      display_name_encrypted = $3,
      display_name_nonce   = $4,
      email_encrypted      = $5,
      email_nonce          = $6,
      email_ephemeral_pub  = $7
  WHERE user_id = $8
    AND signing_key_id IS NULL
  """

  @impl Nebu.Session.UserProvisioner
  def provision_user(user_id, display_name, email, server_key) do
    now_ms = Nebu.DB.Helpers.now_ms()

    # Generate crypto material outside the transaction — keeps transaction window short
    {sign_pub, sign_priv} = Nebu.Signature.generate_signing_keypair()
    {enc_pub, enc_priv} = Nebu.Signature.generate_encryption_keypair()
    signing_key_id = Ecto.UUID.generate()
    encryption_key_id = Ecto.UUID.generate()

    {dn_encrypted, dn_nonce} = Nebu.Signature.encrypt_operational_pii(display_name, server_key)
    {email_encrypted, email_ephemeral_pub, email_nonce} =
      Nebu.Signature.encrypt_sensitive_pii(email, enc_pub)

    Nebu.Repo.transaction(fn ->
      with {:ok, _} <-
             query(@insert_key_sql, [signing_key_id, user_id, "signing", "ed25519", sign_pub, sign_priv, now_ms]),
           {:ok, _} <-
             query(@insert_key_sql, [encryption_key_id, user_id, "encryption", "x25519", enc_pub, enc_priv, now_ms]),
           {:ok, _} <-
             query(@update_user_sql, [
               signing_key_id, encryption_key_id,
               dn_encrypted, dn_nonce,
               email_encrypted, email_nonce, email_ephemeral_pub,
               user_id
             ]) do
        :provisioned
      else
        {:error, reason} -> Nebu.Repo.rollback(reason)
      end
    end)
  end

  defp query(sql, params) do
    case Ecto.Adapters.SQL.query(Nebu.Repo, sql, params) do
      {:ok, _} = ok -> ok
      {:error, _} = err -> err
    end
  end
end
```

**Critical design notes:**

- **Crypto BEFORE transaction**: `generate_signing_keypair/0`, `generate_encryption_keypair/0`, `encrypt_operational_pii/2`, `encrypt_sensitive_pii/2` are all called before `Repo.transaction/1`. They don't touch the DB. This keeps the transaction as short as possible (only 3 SQL statements).

- **`Ecto.UUID.generate/0`**: Available from `ecto_sql ~> 3.12` (already in deps via `nebu_db`). Returns UUID v4 strings like `"a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"`, valid for `TEXT PRIMARY KEY`.

- **`WHERE signing_key_id IS NULL` guard**: Prevents double-provisioning in race conditions. If a second concurrent request calls `provision_user` after the first completes, the UPDATE affects 0 rows. This is safe — `Repo.transaction/1` still returns `{:ok, :provisioned}` even if 0 rows were updated. Story 2.14 controls whether to call provisioning at all.

- **`Nebu.Repo.rollback/1`**: Calling this inside `Repo.transaction/1` immediately terminates the transaction and returns `{:error, reason}` from `Repo.transaction/1`.

- **Raw SQL only**: No `Ecto.Schema`, no `Repo.insert/2`. Go owns all schema migrations. See anti-patterns section below.

- **`enc_priv` in INSERT**: The private key is stored in `user_keys.private_key` (BYTEA, nullable for DSGVO deletion). Raw binary from `:crypto.generate_key(:ecdh, :x25519)` — no encoding needed. Postgrex maps Elixir binaries to PostgreSQL BYTEA directly.

### Step 5: Unit Tests

**`core/apps/session_manager/test/nebu/session/user_provisioner_test.exs`:**

```elixir
defmodule Nebu.Session.UserProvisionerTest do
  use ExUnit.Case, async: false
  # async: false required — Application.put_env is process-global

  alias Nebu.Session.UserProvisioner

  defmodule FakeProvisioner do
    @behaviour Nebu.Session.UserProvisioner

    @impl Nebu.Session.UserProvisioner
    def provision_user(user_id, display_name, email, server_key) do
      :ets.insert(:provisioner_test, {user_id, display_name, email, byte_size(server_key)})
      {:ok, :provisioned}
    end
  end

  defmodule FailingProvisioner do
    @behaviour Nebu.Session.UserProvisioner

    @impl Nebu.Session.UserProvisioner
    def provision_user(_user_id, _display_name, _email, _server_key) do
      {:error, :db_error}
    end
  end

  setup do
    if :ets.whereis(:provisioner_test) != :undefined do
      :ets.delete(:provisioner_test)
    end

    :ets.new(:provisioner_test, [:named_table, :set, :public])
    Application.put_env(:session_manager, :provisioner_module, FakeProvisioner)

    on_exit(fn ->
      Application.delete_env(:session_manager, :provisioner_module)
      if :ets.whereis(:provisioner_test) != :undefined do
        :ets.delete(:provisioner_test)
      end
    end)

    :ok
  end

  describe "provision_user/4" do
    test "returns {:ok, :provisioned} on success" do
      server_key = :crypto.strong_rand_bytes(32)

      assert {:ok, :provisioned} =
               UserProvisioner.provision_user("@kai:nebu.local", "kai.mueller", "kai@example.com", server_key)
    end

    test "records user_id, display_name, email and server_key size" do
      server_key = :crypto.strong_rand_bytes(32)
      UserProvisioner.provision_user("@kai:nebu.local", "kai.mueller", "kai@example.com", server_key)

      assert [{"@kai:nebu.local", "kai.mueller", "kai@example.com", 32}] =
               :ets.lookup(:provisioner_test, "@kai:nebu.local")
    end

    test "propagates {:error, reason} from DB module" do
      Application.put_env(:session_manager, :provisioner_module, FailingProvisioner)
      server_key = :crypto.strong_rand_bytes(32)

      assert {:error, :db_error} =
               UserProvisioner.provision_user("@alex:nebu.local", "alex", "alex@example.com", server_key)
    end
  end
end
```

**Expected test count after this story:** `6 tests, 0 failures`
(3 from `user_store_test.exs` + 3 new from `user_provisioner_test.exs`)

### Schema Reference

**`users` table after migration 000006** (relevant columns for this story):

```sql
-- Existing (Story 2.1, filled by Story 2.12):
user_id                   TEXT    PRIMARY KEY
system_role               TEXT    NOT NULL DEFAULT 'user'
is_active                 BOOLEAN NOT NULL DEFAULT true
created_at                BIGINT  NOT NULL
last_seen_at              BIGINT

-- Filled by THIS story (Story 2.13):
display_name_encrypted    BYTEA           -- AES-256-GCM ciphertext+tag, server key (Tier 1)
display_name_nonce        BYTEA           -- 12-byte AES-GCM nonce
signing_key_id            TEXT            -- FK-like ref to user_keys.key_id (signing)
encryption_key_id         TEXT            -- FK-like ref to user_keys.key_id (encryption)
email_encrypted           BYTEA           -- AES-256-GCM ciphertext+tag, user X25519 key (Tier 2)
email_nonce               BYTEA           -- 12-byte AES-GCM nonce
email_ephemeral_pub       BYTEA           -- 32-byte ephemeral X25519 public key for ECDH
```

**`user_keys` table** (two rows per user after provisioning):

```sql
key_id      TEXT   PRIMARY KEY   -- Ecto.UUID.generate() — UUID v4
user_id     TEXT   NOT NULL REFERENCES users(user_id)
key_type    TEXT   NOT NULL CHECK (key_type IN ('signing', 'encryption'))
algorithm   TEXT   NOT NULL      -- 'ed25519' | 'x25519'
public_key  BYTEA  NOT NULL      -- 32-byte raw binary
private_key BYTEA               -- 32-byte raw binary (NULL after DSGVO deletion)
created_at  BIGINT NOT NULL
deleted_at  BIGINT              -- NULL until DSGVO deletion (Story 5.7)
```

**Note on `signing_key_id` / `encryption_key_id` FK design:** These columns in `users` are plain `TEXT`, NOT `REFERENCES user_keys`. This is intentional (Story 2.1). A `users → user_keys → users` circular FK would deadlock provisioning. The back-references are managed by application logic.

### Crypto Functions Reference (all in `Nebu.Signature`)

| Function | Input | Returns | Used for |
|----------|-------|---------|---------|
| `generate_signing_keypair/0` | — | `{pub_32b, priv_32b}` | Ed25519 keypair |
| `generate_encryption_keypair/0` | — | `{pub_32b, priv_32b}` | X25519 keypair |
| `encrypt_operational_pii/2` | `(plaintext, server_key_32b)` | `{ciphertext_with_tag, nonce_12b}` | display_name |
| `encrypt_sensitive_pii/2` | `(plaintext, recipient_pub_32b)` | `{ciphertext_with_tag, ephemeral_pub_32b, nonce_12b}` | email |

**Server key acquisition** (in Story 2.14 gRPC handler, NOT in this story):
```elixir
server_key = Application.get_env(:signature, :pii_encryption_key)
# Set by runtime.exs from NEBU_PII_ENCRYPTION_KEY (64-char hex → 32 bytes)
```

### Architecture Compliance

**AI-Constraint #1:** All timestamps `BIGINT` — `Nebu.DB.Helpers.now_ms/0` used for `user_keys.created_at` ✓

**AI-Constraint #2:** Auth token NEVER forwarded. Provisioner receives `user_id` + `display_name` + `email` (derived claims), never the raw JWT ✓

**AI-Constraint #6:** All fallible operations return `{:ok, result}` / `{:error, reason}` — `Repo.rollback/1` used inside transaction, never raise/throw ✓

**AI-Constraint #10:** PII encrypted exclusively via X25519 (Encryption Key) — `encrypt_sensitive_pii/2` uses `enc_pub` (X25519), never `sign_pub` (Ed25519) ✓

**Architecture V1:** Two keypairs per user: Ed25519 (signing) + X25519 (encryption) stored as separate rows in `user_keys` ✓

### Critical Anti-Patterns to Avoid

**NEVER define `Ecto.Schema` in Elixir** — use raw SQL only:
```elixir
# ❌ WRONG — Go owns schema
defmodule Nebu.UserKey do
  use Ecto.Schema
  schema "user_keys" do ...
end

# ✅ CORRECT — raw SQL
Ecto.Adapters.SQL.query(Nebu.Repo, @insert_key_sql, params)
```

**NEVER use the Ed25519 private key for PII encryption**:
```elixir
# ❌ WRONG — Ed25519 is for signing, not encryption
encrypt_sensitive_pii(email, sign_pub)

# ✅ CORRECT — X25519 key is the encryption key
encrypt_sensitive_pii(email, enc_pub)
```

**NEVER call `encrypt_operational_pii/2` for email** — Tier 2 uses ECDH:
```elixir
# ❌ WRONG — email is Tier 2 (per-user key), not Tier 1 (server key)
{email_enc, email_nonce} = Nebu.Signature.encrypt_operational_pii(email, server_key)

# ✅ CORRECT — email uses user's X25519 public key
{email_enc, email_ephemeral_pub, email_nonce} = Nebu.Signature.encrypt_sensitive_pii(email, enc_pub)
```

**NEVER store enc_pub as the X25519 public key AND as email_ephemeral_pub** — they are different things:
- `enc_pub` = the user's **permanent** X25519 public key (stored in `user_keys.public_key`)
- `email_ephemeral_pub` = the **ephemeral** sender public key returned by `encrypt_sensitive_pii/2` (stored in `users.email_ephemeral_pub` for future ECDH decryption)

**NEVER inline `DateTime.utc_now()` for timestamps**:
```elixir
# ❌ WRONG
created_at = DateTime.utc_now() |> DateTime.to_unix(:millisecond)

# ✅ CORRECT
now_ms = Nebu.DB.Helpers.now_ms()
```

**NEVER put crypto generation inside `Repo.transaction/1`** — it's not DB work:
```elixir
# ❌ WRONG — extends transaction duration unnecessarily
Nebu.Repo.transaction(fn ->
  {sign_pub, sign_priv} = Nebu.Signature.generate_signing_keypair()  # not DB
  {dn_enc, dn_nonce} = Nebu.Signature.encrypt_operational_pii(...)    # not DB
  ...
end)

# ✅ CORRECT — compute outside, only DB writes inside transaction
{sign_pub, sign_priv} = Nebu.Signature.generate_signing_keypair()
...
Nebu.Repo.transaction(fn ->
  query(@insert_key_sql, [...])
  ...
end)
```

### Previous Story Intelligence (2.12)

- **`async: false` required** — `Application.put_env(:session_manager, :provisioner_module, ...)` is global state
- **ETS table pattern** — Create named table in `setup`, clean up in `on_exit`. Use `[:set]` semantics for upsert-like behavior
- **`Nebu.DB.Helpers.now_ms/0`** — now lives in `core/apps/nebu_db/lib/nebu/db_helpers.ex`, available to all umbrella apps that depend on `nebu_db`
- **`Nebu.Repo`** — lives in `core/apps/nebu_db/lib/nebu/repo.ex`, started by `Nebu.DB.Application`. No test DB config (unit tests use fake modules, not real DB)
- **`Nebu.Session.UserStore.Postgres` pattern** — the raw SQL + `Ecto.Adapters.SQL.query/3` + no Ecto.Schema pattern is established and must be continued
- **`--warnings-as-errors` discipline** — prefix all unused variables with `_`
- **`@behaviour` compiler enforcement** — `@behaviour Nebu.Session.UserProvisioner` on `Postgres` and `FakeProvisioner` gets callback signatures checked at compile time

### Build & Test Commands

```bash
# Run session_manager tests only:
cd core && mix test apps/session_manager --warnings-as-errors

# Run all Elixir unit tests (regression check):
make test-unit-elixir

# Run Go unit tests (migration test regression check):
make test-unit-go
```

**Expected outputs:**
- `session_manager`: `6 tests, 0 failures`
- `make test-unit-elixir`: all apps green, 0 failures
- `make test-unit-go`: `PASS` (migrations test includes 000006 files)

### Files to Create / Modify

**CREATE:**
```
gateway/migrations/
  000006_users_email_pii.up.sql
  000006_users_email_pii.down.sql
core/apps/session_manager/lib/nebu/session/
  user_provisioner.ex
  user_provisioner/
    postgres.ex
core/apps/session_manager/test/nebu/session/
  user_provisioner_test.exs
```

**MODIFY:**
```
gateway/migrations/migrations_test.go          ← add 000006 files to TestFS_ContainsExpectedMigrationFiles
core/apps/session_manager/mix.exs              ← add {:signature, in_umbrella: true} to deps
```

**DO NOT TOUCH:**
- `core/apps/signature/` — no changes (crypto functions already exist and are complete)
- `core/apps/session_manager/lib/nebu/session/user_store.ex` — do NOT modify Story 2.12's code
- `core/apps/session_manager/lib/nebu/session/user_store/postgres.ex` — do NOT modify
- `core/config/runtime.exs` — no new env vars needed (NEBU_PII_ENCRYPTION_KEY already exists)
- Any gateway Go files beyond the migration files
- Any `*_test.exs` in other apps

### Project Structure Notes

- `user_provisioner.ex` and `user_provisioner/postgres.ex` mirror the established `user_store.ex` + `user_store/postgres.ex` pattern exactly
- The `session_manager` app is the right home for this (Story 2.14 will call `provision_user/4` from the session_manager context)
- Adding `{:signature, in_umbrella: true}` to `session_manager/mix.exs` deps creates a direct dep on the `signature` app — this is correct per project structure (session_manager calls crypto functions from signature)

### References

- [Source: epics.md#Story-2.13] Authoritative user story, acceptance criteria, PII tier definitions
- [Source: epics.md#Story-2.14] Scope boundary — ValidateToken gRPC wiring is next story
- [Source: architecture.md#Enforcement rule 1] BIGINT timestamps via `now_ms/0`
- [Source: architecture.md#Enforcement rule 6] `{:ok, result}` / `{:error, reason}` mandate
- [Source: architecture.md#Enforcement rule 10] PII encryption via X25519 only, never Ed25519
- [Source: architecture.md#V1] Two keypairs per user (Ed25519 signing + X25519 encryption)
- [Source: architecture.md#NFR-S2] Sensitive PII at-rest encrypted, Operational PII at-rest encrypted
- [Source: gateway/migrations/000004_users.up.sql] users + user_keys schema (Story 2.1)
- [Source: gateway/migrations/000005_sessions.up.sql] Migration naming pattern
- [Source: core/apps/signature/lib/nebu/signature.ex] All crypto functions — generate_signing_keypair/0, generate_encryption_keypair/0, encrypt_operational_pii/2, encrypt_sensitive_pii/2
- [Source: implementation-artifacts/2-12-user-record-db-write-on-first-login.md] UserStore pattern, nebu_db setup, async:false test pattern, now_ms usage

## Dev Agent Record

### Agent Model Used

claude-sonnet-4-6 (1M context)

### Debug Log References

### Completion Notes List

- All tasks completed. Migration 000006 adds email PII columns. UserProvisioner behaviour + Postgres implementation follow established UserStore pattern. Crypto generated outside transaction (short transaction window). WHERE signing_key_id IS NULL guard ensures idempotency. Unit tests use FakeProvisioner (ETS-backed) and FailingProvisioner with async: false. `session_manager`: 6 tests, 0 failures. Go migrations: `ok github.com/nebu/nebu/migrations`.

### File List

- gateway/migrations/000006_users_email_pii.up.sql (created)
- gateway/migrations/000006_users_email_pii.down.sql (created)
- gateway/migrations/migrations_test.go (modified)
- core/apps/session_manager/mix.exs (modified)
- core/apps/session_manager/lib/nebu/session/user_provisioner.ex (created)
- core/apps/session_manager/lib/nebu/session/user_provisioner/postgres.ex (created)
- core/apps/session_manager/test/nebu/session/user_provisioner_test.exs (created)

## Change Log

- 2026-03-29: Story 2-13 implemented — migration 000006 (email PII columns), UserProvisioner behaviour + Postgres implementation, unit tests. All tests green (6 session_manager, migrations ok).
- 2026-03-30: Code review passed — 0 HIGH, 0 MEDIUM, 2 LOW (commit label cosmetic, sprint tracking gap). All ACs verified, all tasks confirmed done. Status → done.
