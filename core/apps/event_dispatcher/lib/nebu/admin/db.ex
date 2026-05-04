defmodule Nebu.Admin.DB do
  @moduledoc """
  PostgreSQL queries for Admin gRPC RPCs (Story 9.1).

  All queries support the DB dependency-injection pattern used throughout the
  event_dispatcher app — tests override this module via:

      Application.put_env(:event_dispatcher, :admin_db_module, FakeAdminDB)

  This module owns the queries for:
    - User management (list, get, set_is_active, set_system_role)
    - Room management (list, get, archive_room_atomic)
    - Server config (get, upsert)

  PII note: display_name_encrypted (Tier 1) and email_encrypted (Tier 2) are
  AES-256-GCM encrypted in the DB. This module returns the raw encrypted bytes;
  decryption and masking happen in the gRPC server handler.
  """

  # ─── User operations ─────────────────────────────────────────────────────────

  @doc """
  Returns {users_list, next_cursor} for paginated user listing.

  Cursor is the last user_id seen (opaque, lexicographic).
  search is applied as ILIKE on display_name_encrypted (plaintext not available here;
  the server layer must handle search after decryption if needed — at MVP, search is
  passed through as-is and filtering is done post-decryption).

  Returns {list_of_user_maps, next_cursor_string}.
  """
  @spec list_users(pos_integer(), String.t(), String.t()) :: {list(map()), String.t()}
  def list_users(limit, cursor, _search) do
    # Fetch limit+1 rows to detect if there is a next page.
    fetch_limit = limit + 1

    rows =
      if cursor == "" do
        Ecto.Adapters.SQL.query!(
          Nebu.Repo,
          """
          SELECT user_id,
                 display_name_encrypted,
                 display_name_nonce,
                 email_encrypted,
                 email_nonce,
                 email_ephemeral_pub,
                 is_active,
                 system_role,
                 created_at
          FROM users
          ORDER BY user_id
          LIMIT $1
          """,
          [fetch_limit]
        )
      else
        Ecto.Adapters.SQL.query!(
          Nebu.Repo,
          """
          SELECT user_id,
                 display_name_encrypted,
                 display_name_nonce,
                 email_encrypted,
                 email_nonce,
                 email_ephemeral_pub,
                 is_active,
                 system_role,
                 created_at
          FROM users
          WHERE user_id > $1
          ORDER BY user_id
          LIMIT $2
          """,
          [cursor, fetch_limit]
        )
      end

    users = Enum.map(rows.rows, &row_to_user_map/1)
    has_more = length(users) > limit
    page = Enum.take(users, limit)
    next = if has_more, do: List.last(page).user_id, else: ""
    {page, next}
  end

  @doc """
  Returns {:ok, user_map} or {:error, :not_found}.
  """
  @spec get_user(String.t()) :: {:ok, map()} | {:error, :not_found | term()}
  def get_user(user_id) do
    result =
      Ecto.Adapters.SQL.query!(
        Nebu.Repo,
        """
        SELECT user_id,
               display_name_encrypted,
               display_name_nonce,
               email_encrypted,
               email_nonce,
               email_ephemeral_pub,
               is_active,
               system_role,
               created_at
        FROM users
        WHERE user_id = $1
        LIMIT 1
        """,
        [user_id]
      )

    case result.rows do
      [] -> {:error, :not_found}
      [row] -> {:ok, row_to_user_map(row)}
    end
  end

  @doc """
  Sets is_active for user_id. Returns :ok or {:error, :not_found}.
  """
  @spec set_is_active(String.t(), boolean()) :: :ok | {:error, :not_found | term()}
  def set_is_active(user_id, is_active) do
    result =
      Ecto.Adapters.SQL.query!(
        Nebu.Repo,
        "UPDATE users SET is_active = $1 WHERE user_id = $2",
        [is_active, user_id]
      )

    case result.num_rows do
      0 -> {:error, :not_found}
      _ -> :ok
    end
  end

  @doc """
  Updates system_role for user_id. Returns :ok or {:error, :not_found}.
  """
  @spec set_system_role(String.t(), String.t()) :: :ok | {:error, :not_found | term()}
  def set_system_role(user_id, role) do
    result =
      Ecto.Adapters.SQL.query!(
        Nebu.Repo,
        "UPDATE users SET system_role = $1 WHERE user_id = $2",
        [role, user_id]
      )

    case result.num_rows do
      0 -> {:error, :not_found}
      _ -> :ok
    end
  end

  # ─── Room operations ──────────────────────────────────────────────────────────

  @doc """
  Returns {rooms_list, next_cursor} for paginated room listing.
  status_filter: "active" | "archived" | "" (all).
  """
  @spec list_rooms(pos_integer(), String.t(), String.t(), String.t()) :: {list(map()), String.t()}
  def list_rooms(limit, cursor, status_filter, _search) do
    fetch_limit = limit + 1

    {sql, params} =
      cond do
        cursor == "" && status_filter == "" ->
          {"""
           SELECT room_id, name, status,
                  (SELECT COUNT(*) FROM room_members WHERE room_id = r.room_id AND left_at IS NULL) AS member_count,
                  created_at
           FROM rooms r
           ORDER BY room_id
           LIMIT $1
           """, [fetch_limit]}

        cursor == "" && status_filter != "" ->
          {"""
           SELECT room_id, name, status,
                  (SELECT COUNT(*) FROM room_members WHERE room_id = r.room_id AND left_at IS NULL) AS member_count,
                  created_at
           FROM rooms r
           WHERE status = $1
           ORDER BY room_id
           LIMIT $2
           """, [status_filter, fetch_limit]}

        cursor != "" && status_filter == "" ->
          {"""
           SELECT room_id, name, status,
                  (SELECT COUNT(*) FROM room_members WHERE room_id = r.room_id AND left_at IS NULL) AS member_count,
                  created_at
           FROM rooms r
           WHERE room_id > $1
           ORDER BY room_id
           LIMIT $2
           """, [cursor, fetch_limit]}

        true ->
          {"""
           SELECT room_id, name, status,
                  (SELECT COUNT(*) FROM room_members WHERE room_id = r.room_id AND left_at IS NULL) AS member_count,
                  created_at
           FROM rooms r
           WHERE room_id > $1 AND status = $2
           ORDER BY room_id
           LIMIT $3
           """, [cursor, status_filter, fetch_limit]}
      end

    rows = Ecto.Adapters.SQL.query!(Nebu.Repo, sql, params)
    rooms = Enum.map(rows.rows, &row_to_room_map/1)
    has_more = length(rooms) > limit
    page = Enum.take(rooms, limit)
    next = if has_more, do: List.last(page).room_id, else: ""
    {page, next}
  end

  @doc """
  Returns {:ok, room_map} or {:error, :not_found}.
  """
  @spec get_room(String.t()) :: {:ok, map()} | {:error, :not_found | term()}
  def get_room(room_id) do
    result =
      Ecto.Adapters.SQL.query!(
        Nebu.Repo,
        """
        SELECT r.room_id, r.name, r.status,
               (SELECT COUNT(*) FROM room_members m WHERE m.room_id = r.room_id AND m.left_at IS NULL) AS member_count,
               COALESCE(r.max_members, 0),
               COALESCE(r.visibility, 'private'),
               r.created_at
        FROM rooms r
        WHERE r.room_id = $1
        LIMIT 1
        """,
        [room_id]
      )

    case result.rows do
      [] -> {:error, :not_found}
      [row] -> {:ok, row_to_room_detail_map(row)}
    end
  end

  @doc """
  Atomically sets rooms.status='archived' using SELECT FOR UPDATE inside an Ecto transaction.
  Returns :ok | {:error, :not_found} | {:error, :already_archived}.

  This is the Story 9.1 AC:4 implementation — Core owns the DB write atomically.
  The previous archive_room/2 handler only terminated the GenServer (pre-9.1 contract).
  """
  @spec archive_room_atomic(String.t()) :: :ok | {:error, :not_found | term()}
  def archive_room_atomic(room_id) do
    result =
      Nebu.Repo.transaction(fn ->
        # SELECT FOR UPDATE to prevent concurrent archive operations
        status_result =
          Ecto.Adapters.SQL.query!(
            Nebu.Repo,
            "SELECT status FROM rooms WHERE room_id = $1 FOR UPDATE",
            [room_id]
          )

        case status_result.rows do
          [] ->
            Nebu.Repo.rollback(:not_found)

          [["archived"]] ->
            # Idempotent — already archived, return ok
            :ok

          [[_other_status]] ->
            Ecto.Adapters.SQL.query!(
              Nebu.Repo,
              "UPDATE rooms SET status = 'archived' WHERE room_id = $1",
              [room_id]
            )

            :ok
        end
      end)

    case result do
      {:ok, :ok} -> :ok
      {:error, :not_found} -> {:error, :not_found}
      {:error, reason} -> {:error, reason}
    end
  end

  # ─── Server config operations ─────────────────────────────────────────────────

  @doc """
  Returns {:ok, config_map} with server config keys.
  MUST NOT include oidc_client_secret (security invariant — it is AES-256-GCM
  encrypted in the DB and only the Go Gateway has the decryption key).
  """
  @spec get_server_config() :: {:ok, map()} | {:error, term()}
  def get_server_config do
    result =
      Ecto.Adapters.SQL.query!(
        Nebu.Repo,
        """
        SELECT key, value FROM server_config
        WHERE key != 'oidc_client_secret'
        """,
        []
      )

    config =
      result.rows
      |> Enum.map(fn [k, v] -> {k, v} end)
      |> Map.new()

    {:ok, config}
  end

  @doc """
  Upserts one or more server_config key-value pairs.
  Returns :ok.
  """
  @spec upsert_server_config(map()) :: :ok | {:error, term()}
  def upsert_server_config(changes) do
    now = System.system_time(:millisecond)

    Enum.each(changes, fn {key, value} ->
      Ecto.Adapters.SQL.query!(
        Nebu.Repo,
        """
        INSERT INTO server_config (key, value, set_at)
        VALUES ($1, $2, $3)
        ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, set_at = EXCLUDED.set_at
        """,
        [key, to_string(value), now]
      )
    end)

    :ok
  end

  # ─── Private helpers ──────────────────────────────────────────────────────────

  defp row_to_user_map([user_id, display_name_encrypted, display_name_nonce, email_encrypted, email_nonce, email_ephemeral_pub, is_active, system_role, created_at]) do
    %{
      user_id: user_id,
      display_name_encrypted: display_name_encrypted,
      display_name_nonce: display_name_nonce,
      email_encrypted: email_encrypted,
      email_nonce: email_nonce,
      email_ephemeral_pub: email_ephemeral_pub,
      is_active: is_active,
      system_role: system_role || "user",
      created_at: created_at || 0
    }
  end

  defp row_to_room_map([room_id, name, status, member_count, created_at]) do
    %{
      room_id: room_id,
      name: name || "",
      status: status || "active",
      member_count: member_count || 0,
      created_at: created_at || 0
    }
  end

  defp row_to_room_detail_map([room_id, name, status, member_count, max_members, visibility, created_at]) do
    %{
      room_id: room_id,
      name: name || "",
      status: status || "active",
      member_count: member_count || 0,
      max_members: max_members || 0,
      visibility: visibility || "private",
      created_at: created_at || 0
    }
  end
end
