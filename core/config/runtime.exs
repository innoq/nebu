import Config

# Placeholder — NEBU_* env vars added as each story requires them
# e.g., Story 1.3 adds database URL:
# config :room_manager, Nebu.Repo,
#   url: System.get_env("NEBU_DB_URL") || raise "NEBU_DB_URL not set"

# Room IDs use this server name — must match the gateway's NEBU_SERVER_NAME.
config :event_dispatcher, server_name: System.get_env("NEBU_SERVER_NAME", "localhost")

if config_env() in [:prod, :dev] do
  config :nebu_db, Nebu.Repo,
    url: System.get_env("NEBU_DB_URL") || raise("NEBU_DB_URL is not set"),
    pool_size: 10
end

# ─── Story 13-6: libcluster — Core node clustering ──────────────────────────
# Only active in prod/dev environments (Docker Compose + Kubernetes).
# In test, clustering is not started to avoid EPMD noise in unit tests.
#
# Strategy selection via CLUSTER_STRATEGY env var:
#   "gossip"     — Cluster.Strategy.Gossip (UDP multicast, Docker Compose default)
#   "kubernetes" — Cluster.Strategy.Kubernetes.DNS (Helm / K8s default)
#   anything else / unset — no libcluster started
#
# CLUSTER_NODES (comma-separated) is used by the Epmd strategy for explicit
# peer lists when gossip multicast is unavailable. In Docker Compose the
# RELEASE_NODE env var sets the Erlang node name (e.g. "nebu@core").
if config_env() in [:prod, :dev] do
  cluster_strategy = System.get_env("CLUSTER_STRATEGY", "")

  libcluster_topologies =
    case cluster_strategy do
      "gossip" ->
        [
          nebu: [
            strategy: Cluster.Strategy.Gossip,
            config: [
              port: 45892,
              if_addr: "0.0.0.0",
              multicast_addr: "255.255.255.255",
              broadcast_only: true
            ]
          ]
        ]

      "kubernetes" ->
        k8s_namespace = System.get_env("KUBERNETES_NAMESPACE", "default")
        k8s_selector = System.get_env("KUBERNETES_SELECTOR", "app.kubernetes.io/name=nebu-core")
        [
          nebu: [
            strategy: Cluster.Strategy.Kubernetes.DNS,
            config: [
              service: System.get_env("KUBERNETES_SERVICE_NAME", "nebu-core-headless"),
              application_name: "nebu",
              namespace: k8s_namespace,
              polling_interval: 10_000,
              kubernetes_selector: k8s_selector
            ]
          ]
        ]

      _ ->
        # No clustering — single-node mode (default when CLUSTER_STRATEGY is not set)
        []
    end

  if libcluster_topologies != [] do
    config :libcluster, topologies: libcluster_topologies
  end
end

if config_env() in [:prod, :dev] do
  pii_key_hex =
    System.get_env("NEBU_PII_ENCRYPTION_KEY") ||
      raise "NEBU_PII_ENCRYPTION_KEY is not set. Must be a 64-char hex string (32 bytes)."

  pii_key =
    case Base.decode16(pii_key_hex, case: :mixed) do
      {:ok, decoded} -> decoded
      :error -> raise "NEBU_PII_ENCRYPTION_KEY is not valid hex. Must be a 64-char hex string."
    end

  unless byte_size(pii_key) == 32 do
    raise "NEBU_PII_ENCRYPTION_KEY must decode to exactly 32 bytes, got #{byte_size(pii_key)}"
  end

  config :signature, pii_encryption_key: pii_key
end
