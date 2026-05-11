defmodule Nebu.Health.Server do
  @moduledoc """
  Minimal HTTP server for the health endpoint on port 4000.

  Uses OTP's built-in :gen_tcp with HTTP packet parsing — no external
  web framework needed for a single-endpoint server.

  Routes:
    GET /health → 200 (UP/DEGRADED) or 503 (DOWN) with JSON body
    GET /info   → 200 with build metadata JSON (Story 11-9)
    *           → 404 Not Found
  """

  use GenServer
  require Logger

  @port 4000

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    port = Application.get_env(:event_dispatcher, :health_port, @port)

    case :gen_tcp.listen(port, [:binary, packet: :http_bin, active: false, reuseaddr: true]) do
      {:ok, listen_socket} ->
        Task.start_link(fn -> accept(listen_socket) end)
        {:ok, listen_socket}

      {:error, reason} ->
        Logger.error("Health server failed to listen on port #{port}: #{inspect(reason)}")
        {:stop, reason}
    end
  end

  defp accept(listen_socket) do
    case :gen_tcp.accept(listen_socket) do
      {:ok, socket} ->
        Task.start(fn -> handle_connection(socket) end)
        accept(listen_socket)

      {:error, :closed} ->
        :ok
    end
  end

  defp handle_connection(socket) do
    case :gen_tcp.recv(socket, 0, 5_000) do
      {:ok, {:http_request, :GET, {:abs_path, "/health"}, _}} ->
        drain_headers(socket)
        health = Nebu.Health.check()
        {status_code, status_text} =
          if health.status == "DOWN", do: {503, "Service Unavailable"}, else: {200, "OK"}
        body = Jason.encode!(health)

        response =
          "HTTP/1.1 #{status_code} #{status_text}\r\n" <>
            "Content-Type: application/json\r\n" <>
            "Content-Length: #{byte_size(body)}\r\n" <>
            "Connection: close\r\n\r\n" <>
            body

        :gen_tcp.send(socket, response)

      {:ok, {:http_request, :GET, {:abs_path, "/info"}, _}} ->
        drain_headers(socket)
        body = Jason.encode!(Nebu.BuildInfo.get())
        response =
          "HTTP/1.1 200 OK\r\n" <>
            "Content-Type: application/json\r\n" <>
            "Content-Length: #{byte_size(body)}\r\n" <>
            "Connection: close\r\n\r\n" <>
            body
        :gen_tcp.send(socket, response)

      {:ok, {:http_request, _, _, _}} ->
        drain_headers(socket)
        :gen_tcp.send(
          socket,
          "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
        )

      _ ->
        :ok
    end

    :gen_tcp.close(socket)
  end

  # Consume all HTTP headers before sending response
  defp drain_headers(socket) do
    case :gen_tcp.recv(socket, 0, 2_000) do
      {:ok, :http_eoh} -> :ok
      {:ok, {:http_header, _, _, _, _}} -> drain_headers(socket)
      _ -> :ok
    end
  end
end
