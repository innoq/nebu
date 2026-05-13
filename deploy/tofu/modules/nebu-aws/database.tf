# nebu-aws: RDS PostgreSQL 16 — subnet group and Multi-AZ DB instance.

# ── DB Subnet Group ──────────────────────────────────────────────────────────

resource "aws_db_subnet_group" "this" {
  name        = "nebu-${var.environment}-db-subnet-group"
  description = "Subnet group for Nebu RDS PostgreSQL — private subnets only."
  subnet_ids  = aws_subnet.private[*].id

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-db-subnet-group"
  })
}

# ── RDS PostgreSQL 16 Instance ───────────────────────────────────────────────

resource "aws_db_instance" "this" {
  identifier = "nebu-${var.environment}-postgres"

  engine               = "postgres"
  engine_version       = "16"
  instance_class       = var.db_instance_class
  db_name              = "nebu"
  username             = "nebu"
  password             = var.db_password
  parameter_group_name = "default.postgres16"

  # Storage
  allocated_storage     = 20
  max_allocated_storage = 100
  storage_type          = "gp3"
  storage_encrypted     = true

  # High availability
  multi_az = true

  # Network
  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.rds.id]
  publicly_accessible    = false

  # Backup
  backup_retention_period = 7
  backup_window           = "03:00-04:00"
  maintenance_window      = "sun:04:00-sun:05:00"

  # Lifecycle
  # final_snapshot_identifier is used only when skip_final_snapshot = false (production).
  skip_final_snapshot       = var.skip_final_snapshot
  final_snapshot_identifier = "nebu-${var.environment}-final-snapshot"
  deletion_protection       = false

  # Performance Insights (not supported on all instance classes — disable for db.t3.micro)
  performance_insights_enabled = var.enable_performance_insights

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-postgres"
  })
}
