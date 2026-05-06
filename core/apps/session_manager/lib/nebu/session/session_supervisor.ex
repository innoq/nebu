defmodule Nebu.Session.SessionSupervisor do
  @moduledoc """
  Orchestration layer for session lifecycle management.

  This is a plain module (NOT a GenServer or OTP Supervisor) — it is a thin
  coordination layer that orchestrates ETS and PostgreSQL operations.

  ## create_session/2

  Writes the session to ETS (hot-path cache) for immediate `/sync` access.
  The session record in the `sessions` PostgreSQL table is written by the
  Go gateway during login (Story 2-18); the Elixir side only manages ETS
  and `sync_tokens`.

  ## destroy_session/1

  Delegates to `Nebu.Session.PgStore.invalidate_session/1` which handles:
  - Deleting from `sync_tokens` and `sessions` in a single PostgreSQL transaction
  - Evicting from ETS *only after* the DB transaction commits (atomicity guarantee)
  """

  @doc """
  Writes `session_map` to the ETS session store for `user_id`.

  Returns `:ok`. The write is synchronous and immediately visible to
  `Nebu.Session.EtsStore.get_session/1`.
  """
  @spec create_session(String.t(), map()) :: :ok
  def create_session(user_id, session_map) do
    Nebu.Session.EtsStore.put_session(user_id, session_map)
  end

  @doc """
  Invalidates all sessions for `user_id` by delegating to
  `Nebu.Session.PgStore.invalidate_session/1`.

  Returns `:ok` on success, `{:error, reason}` if the DB transaction fails.
  On DB failure the ETS entry is NOT removed (atomicity guarantee).
  """
  @spec destroy_session(String.t()) :: :ok | {:error, term()}
  def destroy_session(user_id) do
    Nebu.Session.PgStore.invalidate_session(user_id)
  end

  @doc """
  Invalidates the `(user_id, device_id)` session by delegating to
  `Nebu.Session.PgStore.invalidate_session/2`.

  Deletes the `sync_tokens` row and the `sessions` row for this specific device
  in a single PostgreSQL transaction (AC4, Story 9-22). Does NOT evict from ETS —
  other devices for the same user may still be active.

  Returns `:ok` on success, `{:error, reason}` if the DB transaction fails.
  """
  @spec destroy_session(String.t(), String.t()) :: :ok | {:error, term()}
  def destroy_session(user_id, device_id) do
    Nebu.Session.PgStore.invalidate_session(user_id, device_id)
  end
end
