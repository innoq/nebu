# nebu-aws: Aurora Serverless v2 PostgreSQL — subnet group, cluster, and cluster instance.

# ── DB Subnet Group ──────────────────────────────────────────────────────────

resource "aws_db_subnet_group" "this" {
  name        = "nebu-${var.environment}-db-subnet-group"
  description = "Subnet group for Nebu Aurora PostgreSQL — private subnets only."
  subnet_ids  = aws_subnet.private[*].id

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-db-subnet-group"
  })
}

# ── Aurora Serverless v2 Cluster ─────────────────────────────────────────────
# NOTE: Aurora Serverless v2 uses engine_mode = "provisioned" — NOT "serverless".
# Serverless v2 scaling is controlled by the serverlessv2_scaling_configuration block below.

resource "aws_rds_cluster" "this" {
  cluster_identifier = "nebu-${var.environment}-aurora"

  engine         = "aurora-postgresql"
  engine_mode    = "provisioned" # Serverless v2 uses "provisioned" — NOT "serverless"
  engine_version = "16.6"

  database_name   = "nebu"
  master_username = "nebu"
  master_password = var.db_password

  storage_encrypted = true

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  # Serverless v2 scaling — min=0 allows scale-to-zero (auto-pause) for dev.
  # For production, set min_capacity = 0.5 to avoid cold-start latency.
  serverlessv2_scaling_configuration {
    min_capacity             = var.aurora_min_capacity
    max_capacity             = var.aurora_max_capacity
    seconds_until_auto_pause = 3600
  }

  backup_retention_period      = 7
  preferred_backup_window      = "03:00-04:00"
  preferred_maintenance_window = "sun:04:00-sun:05:00"

  # Lifecycle
  # final_snapshot_identifier is used only when skip_final_snapshot = false (production).
  # Timestamp suffix ensures uniqueness across destroy+recreate cycles.
  skip_final_snapshot       = var.skip_final_snapshot
  final_snapshot_identifier = "nebu-${var.environment}-final-${formatdate("YYYYMMDDhhmmss", timestamp())}"
  deletion_protection       = false

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-aurora-cluster"
  })

  # final_snapshot_identifier uses timestamp() which re-evaluates every plan,
  # causing a perpetual diff. The value only matters at destroy time, so we
  # suppress the diff with ignore_changes.
  lifecycle {
    ignore_changes = [final_snapshot_identifier]
  }
}

# ── Aurora Serverless v2 Cluster Instance ────────────────────────────────────
# instance_class = "db.serverless" is required for Serverless v2 instances.

resource "aws_rds_cluster_instance" "this" {
  identifier         = "nebu-${var.environment}-aurora-instance-1"
  cluster_identifier = aws_rds_cluster.this.id
  instance_class     = "db.serverless" # Required for Serverless v2
  engine             = aws_rds_cluster.this.engine
  engine_version     = aws_rds_cluster.this.engine_version

  db_subnet_group_name = aws_db_subnet_group.this.name

  # Aurora Serverless v2 does NOT enable Performance Insights by default — enable explicitly.
  performance_insights_enabled          = true
  performance_insights_retention_period = 7

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-aurora-instance-1"
  })
}
