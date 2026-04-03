defmodule Nebu.Session.EtsStore do
  @moduledoc """
  ETS-backed session store for active user sessions.

  The ETS table `:NebuSessions` is created in `Nebu.Session.Application.start/2`
  (owned by the Application process), so it survives `Nebu.Session.EtsStore`
  GenServer crashes and supervisor restarts.

  This GenServer is a supervised worker that holds no internal state.
  All public operations are direct ETS reads/writes against the `:public` table,
  keeping the hot path for `/sync` session lookups at O(1) without GenServer
  message-passing overhead.

  ## ETS entry format

      {user_id :: String.t(), %{
        access_token_hash: String.t(),   # Base16-encoded SHA-256, never raw token
        device_id: String.t(),
        created_at_ms: integer(),        # BIGINT milliseconds since epoch
        last_seen_at_ms: integer()       # BIGINT milliseconds since epoch
      }}
  """

  use GenServer

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @doc """
  Starts the EtsStore worker and registers it under its module name so that
  `Process.whereis(Nebu.Session.EtsStore)` works in tests and supervision trees.
  """
  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, :ok, Keyword.put_new(opts, :name, __MODULE__))
  end

  @impl true
  def init(:ok), do: {:ok, :no_state}

  # ---------------------------------------------------------------------------
  # Public API — direct ETS operations (no GenServer.call overhead)
  # ---------------------------------------------------------------------------

  @doc """
  Upserts a session entry for `user_id`.

  `session_map` must contain at least the fields:
  - `:access_token_hash` — Base16-encoded SHA-256 of the raw access token
  - `:device_id`         — Matrix device identifier string
  - `:created_at_ms`     — BIGINT milliseconds since epoch
  - `:last_seen_at_ms`   — BIGINT milliseconds since epoch

  The ETS table type is `:set`, so calling `put_session/2` twice with the
  same `user_id` is an upsert — the previous entry is replaced.
  """
  @spec put_session(String.t(), map()) :: :ok
  def put_session(user_id, session_map) do
    :ets.insert(:NebuSessions, {user_id, session_map})
    :ok
  end

  @doc """
  Returns `{:ok, session_map}` for the given `user_id`, or
  `{:error, :not_found}` if no session exists.
  """
  @spec get_session(String.t()) :: {:ok, map()} | {:error, :not_found}
  def get_session(user_id) do
    case :ets.lookup(:NebuSessions, user_id) do
      [{^user_id, session_map}] -> {:ok, session_map}
      [] -> {:error, :not_found}
    end
  end

  @doc """
  Removes the session entry for `user_id`. Always returns `:ok`, even if no
  entry existed (idempotent).
  """
  @spec delete_session(String.t()) :: :ok
  def delete_session(user_id) do
    :ets.delete(:NebuSessions, user_id)
    :ok
  end

  @doc """
  Returns all session maps currently stored in the ETS table.
  Intended for Admin metrics; not on the hot path.
  """
  @spec list_sessions() :: [map()]
  def list_sessions do
    :ets.tab2list(:NebuSessions)
    |> Enum.map(fn {_user_id, session_map} -> session_map end)
  end
end
