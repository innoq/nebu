# ── PostgresFlex Managed Database ────────────────────────────────────────────
# Replaces the self-hosted postgres:16-alpine container in cloud-init.
# Provides automated daily backups, PITR, and HA replication via the Stackit platform.

resource "stackit_postgresflex_instance" "nebu" {
  project_id      = var.stackit_project_id
  name            = "nebu-${var.environment}-postgres"
  version         = "16"
  replicas        = var.postgres_replicas
  backup_schedule = "0 2 * * *" # Daily at 02:00 UTC

  # ACL: VMs in the same Stackit project connect via the internal 10.250.0.0/16 network.
  # The PostgresFlex hostname resolves to a public IP externally, but Stackit routes
  # VM traffic internally — source IP seen by PostgresFlex is 10.250.x.x, not the floating IP.
  acl = ["10.250.0.0/16"]

  flavor = {
    cpu = var.postgres_cpu
    ram = var.postgres_ram
  }

  storage = {
    class = "premium-perf6-stackit"
    size  = var.postgres_storage_size
  }
}

resource "stackit_postgresflex_user" "nebu" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  username    = "nebu_app"
  roles       = ["login"]
}

# nebu_migrate: table owner + migration runner (DDL privileges).
# Migrations 24+ transfer table/sequence/function ownership to this role and
# subsequently run as this role so that ALTER TABLE, CREATE TRIGGER, etc. work.
# NEBU_DB_URL_MIGRATE in .env points to this user.
resource "stackit_postgresflex_user" "nebu_migrate" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  username    = "nebu_migrate"
  roles       = ["login"]
}

# nebu (legacy role): placeholder required by migrations 18, 25, 28 which issue
# `GRANT EXECUTE ON FUNCTION audit_log_purge(INT) TO nebu` for backward compat.
# This user does not connect to the database at runtime.
resource "stackit_postgresflex_user" "nebu_legacy" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  username    = "nebu"
  roles       = ["login"]
}

resource "stackit_postgresflex_database" "nebu" {
  project_id  = var.stackit_project_id
  instance_id = stackit_postgresflex_instance.nebu.instance_id
  name        = "nebu"
  owner       = stackit_postgresflex_user.nebu.username
}
