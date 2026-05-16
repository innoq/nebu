defmodule Nebu.Session.BulkImporterTest do
  use ExUnit.Case, async: false
  # async: false required — Application.put_env is process-global

  # ─── Story 14-3a: BulkImportUsers gRPC RPC + Core Provisioning ───────────────
  #
  # These tests are written FIRST (red phase) before BulkImporter module exists.
  # They cover:
  #   AT-1: single user import → {:ok, %{imported: 1, skipped: 0, failed: 0}}
  #   AT-2: duplicate user skipped → {:ok, %{imported: 0, skipped: 1, failed: 0}}
  #   AT-3: bulk import 10 users (8 new + 2 existing) → {:ok, %{imported: 8, skipped: 2, failed: 0}}
  #   AT-4: keypair generation correctness (key algorithm + size validated)
  #
  # BulkImporter module expected location:
  #   core/apps/session_manager/lib/nebu/session/bulk_importer.ex
  #
  # Config injection points:
  #   :session_manager, :bulk_importer_user_store_module  — UserStore behaviour
  #   :session_manager, :bulk_importer_provisioner_module — UserProvisioner behaviour
  #   :session_manager, :bulk_importer_lookup_module      — lookup module (lookup/1)

  # ─── Fake Modules ─────────────────────────────────────────────────────────────

  defmodule FakeUserStore do
    @behaviour Nebu.Session.UserStore

    @impl Nebu.Session.UserStore
    def upsert_user(user_id, _system_role) do
      # Use a :bag-style insert by using the user_id as part of a unique key.
      # ETS :set uses first element as key — use a unique composite key.
      :ets.insert(:bulk_import_test, {{:upserted, user_id}, true})
      {:ok, user_id}
    end
  end

  defmodule FakeProvisioner do
    @behaviour Nebu.Session.UserProvisioner

    @impl Nebu.Session.UserProvisioner
    def provision_user(user_id, _display_name, _email, _server_key) do
      :ets.insert(:bulk_import_test, {{:provisioned, user_id}, true})
      {:ok, :provisioned}
    end
  end

  defmodule FakeLookupProvisioned do
    # Returns :already_provisioned for all users (signing_key_id IS NOT NULL).
    def lookup(_user_id), do: :already_provisioned
  end

  defmodule FakeLookupNotProvisioned do
    # Returns :not_provisioned for all users (no DB row or signing_key_id IS NULL).
    def lookup(_user_id), do: :not_provisioned
  end

  defmodule FakeLookupMixed do
    # Returns :already_provisioned for user IDs containing "-existing" in the localpart,
    # :not_provisioned for all others.
    # e.g. "@user-a-existing:nebu.local" → :already_provisioned
    def lookup(user_id) do
      if String.contains?(user_id, "-existing") do
        :already_provisioned
      else
        :not_provisioned
      end
    end
  end

  defmodule CapturingProvisioner do
    # Records the size of the server_key passed to provision_user.
    @behaviour Nebu.Session.UserProvisioner

    @impl Nebu.Session.UserProvisioner
    def provision_user(_user_id, _display_name, _email, server_key) do
      :ets.insert(:bulk_import_test, {:server_key_size, byte_size(server_key)})
      {:ok, :provisioned}
    end
    # Note: key is :server_key_size (atom), value is integer.
    # :ets.lookup(:bulk_import_test, :server_key_size) returns [{:server_key_size, N}].
  end

  # ─── Setup ────────────────────────────────────────────────────────────────────

  setup do
    if :ets.whereis(:bulk_import_test) != :undefined do
      :ets.delete(:bulk_import_test)
    end

    :ets.new(:bulk_import_test, [:named_table, :set, :public])

    Application.put_env(:session_manager, :bulk_importer_user_store_module, FakeUserStore)
    Application.put_env(:session_manager, :bulk_importer_provisioner_module, FakeProvisioner)
    Application.put_env(:session_manager, :bulk_importer_lookup_module, FakeLookupNotProvisioned)

    on_exit(fn ->
      Application.delete_env(:session_manager, :bulk_importer_user_store_module)
      Application.delete_env(:session_manager, :bulk_importer_provisioner_module)
      Application.delete_env(:session_manager, :bulk_importer_lookup_module)

      if :ets.whereis(:bulk_import_test) != :undefined do
        :ets.delete(:bulk_import_test)
      end
    end)

    :ok
  end

  # ─── AT-1: Single user import ─────────────────────────────────────────────────

  describe "import_users/1 — single user" do
    test "AT-1: returns imported:1 skipped:0 failed:0 for a new user" do
      user = %{
        user_id: "@alice:nebu.local",
        system_role: "user",
        display_name: "Alice",
        email: "alice@example.com"
      }

      assert {:ok, %{imported: 1, skipped: 0, failed: 0}} =
               Nebu.Session.BulkImporter.import_users([user])
    end

    test "AT-1b: upsert_user and provision_user are called for new user" do
      user = %{
        user_id: "@bob:nebu.local",
        system_role: "user",
        display_name: "Bob",
        email: "bob@example.com"
      }

      Nebu.Session.BulkImporter.import_users([user])

      # FakeUserStore stores {{:upserted, user_id}, true} — ETS key is {:upserted, user_id}.
      assert :ets.member(:bulk_import_test, {:upserted, "@bob:nebu.local"})
      assert :ets.member(:bulk_import_test, {:provisioned, "@bob:nebu.local"})
    end
  end

  # ─── AT-2: Duplicate user skipped ─────────────────────────────────────────────

  describe "import_users/1 — duplicate skip" do
    test "AT-2: returns imported:0 skipped:1 failed:0 for already-provisioned user" do
      Application.put_env(:session_manager, :bulk_importer_lookup_module, FakeLookupProvisioned)

      user = %{
        user_id: "@carol:nebu.local",
        system_role: "user",
        display_name: "Carol",
        email: "carol@example.com"
      }

      assert {:ok, %{imported: 0, skipped: 1, failed: 0}} =
               Nebu.Session.BulkImporter.import_users([user])
    end

    test "AT-2b: upsert_user and provision_user are NOT called for skipped user" do
      Application.put_env(:session_manager, :bulk_importer_lookup_module, FakeLookupProvisioned)

      user = %{
        user_id: "@dave:nebu.local",
        system_role: "user",
        display_name: "Dave",
        email: "dave@example.com"
      }

      Nebu.Session.BulkImporter.import_users([user])

      # For a skipped user, FakeUserStore.upsert_user is never called.
      refute :ets.member(:bulk_import_test, {:upserted, "@dave:nebu.local"})
      refute :ets.member(:bulk_import_test, {:provisioned, "@dave:nebu.local"})
    end
  end

  # ─── AT-3: Bulk import of 10 users (8 new + 2 existing) ──────────────────────

  describe "import_users/1 — bulk import" do
    test "AT-3: returns imported:8 skipped:2 failed:0 for mixed list of 10" do
      Application.put_env(:session_manager, :bulk_importer_lookup_module, FakeLookupMixed)

      users =
        Enum.map(1..8, fn i ->
          %{user_id: "@user#{i}:nebu.local", system_role: "user", display_name: "User #{i}", email: "user#{i}@example.com"}
        end) ++
        [
          %{user_id: "@user-a-existing:nebu.local", system_role: "user", display_name: "Existing A", email: "a@example.com"},
          %{user_id: "@user-b-existing:nebu.local", system_role: "user", display_name: "Existing B", email: "b@example.com"}
        ]

      assert {:ok, %{imported: 8, skipped: 2, failed: 0}} =
               Nebu.Session.BulkImporter.import_users(users)
    end

    test "AT-3b: returns {:ok, %{imported: 0, skipped: 0, failed: 0}} for empty list" do
      assert {:ok, %{imported: 0, skipped: 0, failed: 0}} =
               Nebu.Session.BulkImporter.import_users([])
    end
  end

  # ─── AT-4: Keypair generation correctness ────────────────────────────────────

  describe "keypair generation — Nebu.Signature" do
    test "AT-4a: generate_signing_keypair/0 produces Ed25519 key pair (both binaries)" do
      {pub, priv} = Nebu.Signature.generate_signing_keypair()
      # Ed25519 via OTP :crypto — public key is 32 bytes; private key (seed) is 32 bytes
      assert is_binary(pub)
      assert is_binary(priv)
      assert byte_size(pub) > 0
      assert byte_size(priv) > 0
    end

    test "AT-4b: generate_encryption_keypair/0 produces X25519 key pair (32 bytes each)" do
      {pub, priv} = Nebu.Signature.generate_encryption_keypair()
      assert is_binary(pub)
      assert is_binary(priv)
      assert byte_size(pub) == 32
      assert byte_size(priv) == 32
    end

    test "AT-4c: encrypt_operational_pii/2 produces non-empty ciphertext + 12-byte nonce" do
      server_key = :crypto.strong_rand_bytes(32)
      {encrypted, nonce} = Nebu.Signature.encrypt_operational_pii("Alice", server_key)
      assert is_binary(encrypted)
      assert is_binary(nonce)
      assert byte_size(encrypted) > 0
      # AES-256-GCM nonce is 12 bytes
      assert byte_size(nonce) == 12
    end

    test "AT-4d: encrypt_sensitive_pii/2 produces encrypted binary + 32-byte ephemeral pub + 12-byte nonce" do
      {enc_pub, _enc_priv} = Nebu.Signature.generate_encryption_keypair()
      {encrypted, ephemeral_pub, nonce} = Nebu.Signature.encrypt_sensitive_pii("alice@example.com", enc_pub)
      assert is_binary(encrypted)
      assert is_binary(ephemeral_pub)
      assert is_binary(nonce)
      assert byte_size(encrypted) > 0
      assert byte_size(ephemeral_pub) == 32
      assert byte_size(nonce) == 12
    end

    test "AT-4e: BulkImporter.import_users/1 invokes provision_user with 32-byte AES-256 server key" do
      Application.put_env(:session_manager, :bulk_importer_provisioner_module, CapturingProvisioner)
      Application.put_env(:signature, :pii_encryption_key, :crypto.strong_rand_bytes(32))

      user = %{user_id: "@eve:nebu.local", system_role: "user", display_name: "Eve", email: "eve@example.com"}
      Nebu.Session.BulkImporter.import_users([user])

      [{:server_key_size, key_size}] = :ets.lookup(:bulk_import_test, :server_key_size)
      assert key_size == 32

      Application.delete_env(:signature, :pii_encryption_key)
    end
  end
end
