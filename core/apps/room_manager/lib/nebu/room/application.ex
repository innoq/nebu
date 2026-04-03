defmodule Nebu.Room.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    # ETS table for txn_id idempotency deduplication.
    # Created here (owned by Application process) so it survives individual
    # Room GenServer crashes/restarts. Type :set, :public so GenServers can
    # read and write without going through the owner process.
    # AC #2: must be created BEFORE any Room GenServer starts.
    :ets.new(:NebuTxnDedup, [:named_table, :set, :public])

    # Start the :pg scope for room process group broadcast (ADR-005).
    # :pg is an OTP built-in (OTP 23+) — no external dependency.
    # Handle {:error, {:already_started, _}} gracefully in case another
    # umbrella app starts :pg first.
    case :pg.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _pid}} -> :ok
    end

    # Generate a server-level Ed25519 signing key for MVP event signing.
    # Stored in persistent_term so all Room GenServers share one key without
    # process communication overhead. Phase 2 will use per-user keys from DB.
    #
    # WARNING (MVP limitation): This key is regenerated on every Application
    # restart. Signatures on events created before a restart will NOT be
    # verifiable with the new key. Phase 2 must persist the key to DB/disk.
    {pub_key, priv_key} = :crypto.generate_key(:eddsa, :ed25519)
    :persistent_term.put(:nebu_signing_key, {pub_key, priv_key})

    children = [
      {Horde.Registry,
       [name: Nebu.Room.Registry, keys: :unique, members: :auto]},
      {Horde.DynamicSupervisor,
       [name: Nebu.Room.HordeSupervisor, strategy: :one_for_one, members: :auto]}
    ]

    opts = [strategy: :one_for_one, name: Nebu.Room.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
