# nebu-aws: AWS Secrets Manager secrets for Nebu runtime credentials.
# Initial versions contain placeholder values — operators MUST rotate before go-live.
#
# Secret value format notes:
#   db_url          — full PostgreSQL DSN, e.g. "postgres://nebu:PASSWORD@host:5432/nebu"
#   db_password     — RDS master password (plain string; also used by RDS parameter store)
#   internal_secret — random 32+ byte PSK (see `make setup` for local generation)
#   oidc_client_secret — OIDC client secret from the identity provider registration
#   oidc_issuer     — OIDC issuer URL, e.g. "https://auth.example.com"
#   release_cookie  — Erlang distribution cookie for OTP cluster auth (random string, keep secret)

# ── DB URL (full DSN for gateway + core) ─────────────────────────────────────

resource "aws_secretsmanager_secret" "db_url" {
  name        = "nebu/${var.environment}/db_url"
  description = "Full PostgreSQL DSN for Nebu gateway and core."

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-db-url"
  })
}

resource "aws_secretsmanager_secret_version" "db_url" {
  secret_id     = aws_secretsmanager_secret.db_url.id
  secret_string = "PLACEHOLDER_postgres://nebu:ROTATE_ME@hostname:5432/nebu"

  lifecycle {
    # Prevent tofu from overwriting the value once an operator has set the real DSN.
    ignore_changes = [secret_string]
  }
}

# ── DB Password (RDS master — kept separate for RDS rotation support) ─────────

resource "aws_secretsmanager_secret" "db_password" {
  name        = "nebu/${var.environment}/db_password"
  description = "Nebu RDS PostgreSQL master password (plain string). Rotate via Secrets Manager before go-live."

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-db-password"
  })
}

resource "aws_secretsmanager_secret_version" "db_password" {
  secret_id     = aws_secretsmanager_secret.db_password.id
  secret_string = "PLACEHOLDER_rotate_before_go_live"

  lifecycle {
    # Prevent tofu from overwriting the value once an operator has rotated it.
    ignore_changes = [secret_string]
  }
}

# ── Internal Node Secret ──────────────────────────────────────────────────────

resource "aws_secretsmanager_secret" "nebu_internal_secret" {
  name        = "nebu/${var.environment}/internal_secret"
  description = "Shared PSK for Nebu gateway ↔ core node registration (ADR-008)."

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-internal-secret"
  })
}

resource "aws_secretsmanager_secret_version" "nebu_internal_secret" {
  secret_id     = aws_secretsmanager_secret.nebu_internal_secret.id
  secret_string = "PLACEHOLDER_rotate_before_go_live"

  lifecycle {
    ignore_changes = [secret_string]
  }
}

# ── OIDC Client Secret ────────────────────────────────────────────────────────

resource "aws_secretsmanager_secret" "oidc_client_secret" {
  name        = "nebu/${var.environment}/oidc_client_secret"
  description = "OIDC client secret for the Nebu application registered with the identity provider."

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-oidc-client-secret"
  })
}

resource "aws_secretsmanager_secret_version" "oidc_client_secret" {
  secret_id     = aws_secretsmanager_secret.oidc_client_secret.id
  secret_string = "PLACEHOLDER_rotate_before_go_live"

  lifecycle {
    ignore_changes = [secret_string]
  }
}

# ── OIDC Issuer URL ───────────────────────────────────────────────────────────

resource "aws_secretsmanager_secret" "oidc_issuer" {
  name        = "nebu/${var.environment}/oidc_issuer"
  description = "OIDC issuer URL for the identity provider (e.g. https://auth.example.com)."

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-oidc-issuer"
  })
}

resource "aws_secretsmanager_secret_version" "oidc_issuer" {
  secret_id     = aws_secretsmanager_secret.oidc_issuer.id
  secret_string = "PLACEHOLDER_https://auth.example.com"

  lifecycle {
    ignore_changes = [secret_string]
  }
}

# ── Erlang Release Cookie (OTP cluster auth) ──────────────────────────────────

resource "aws_secretsmanager_secret" "release_cookie" {
  name        = "nebu/${var.environment}/release_cookie"
  description = "Erlang distribution cookie for Nebu OTP cluster authentication."

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-release-cookie"
  })
}

resource "aws_secretsmanager_secret_version" "release_cookie" {
  secret_id     = aws_secretsmanager_secret.release_cookie.id
  secret_string = "PLACEHOLDER_rotate_before_go_live"

  lifecycle {
    ignore_changes = [secret_string]
  }
}
