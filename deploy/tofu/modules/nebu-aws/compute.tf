# nebu-aws: ECS Fargate cluster, IAM task execution role, task definitions, and ECS services.

# ── ECS Cluster ──────────────────────────────────────────────────────────────

resource "aws_ecs_cluster" "this" {
  name = "nebu-${var.environment}"

  setting {
    name  = "containerInsights"
    value = "enabled"
  }

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-ecs-cluster"
  })
}

# ── IAM: ECS Task Execution Role ─────────────────────────────────────────────

resource "aws_iam_role" "ecs_task_execution" {
  name        = "nebu-${var.environment}-ecs-task-execution-role"
  description = "IAM role assumed by ECS to pull container images and read Secrets Manager secrets."

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Principal = {
          Service = "ecs-tasks.amazonaws.com"
        }
        Action = "sts:AssumeRole"
      }
    ]
  })

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-ecs-task-execution-role"
  })
}

resource "aws_iam_role_policy_attachment" "ecs_task_execution_managed" {
  role       = aws_iam_role.ecs_task_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

# Inline policy: allow reading Secrets Manager secrets scoped to nebu/{environment}/*.
# Least-privilege: only the secrets this module provisions under nebu/{environment}/ are accessible.
# NOTE: kms:Decrypt is omitted here. If your secrets use a customer-managed KMS key (CMK),
# add kms:Decrypt with the CMK ARN to this policy — without it ECS cannot decrypt CMK-encrypted secrets.
resource "aws_iam_role_policy" "ecs_task_execution_secrets" {
  name = "nebu-${var.environment}-ecs-secrets-read"
  role = aws_iam_role.ecs_task_execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "ReadNebuSecrets"
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue"
        ]
        Resource = "arn:aws:secretsmanager:*:*:secret:nebu/${var.environment}/*"
      }
    ]
  })
}

# ── CloudWatch Log Groups for ECS tasks ─────────────────────────────────────

resource "aws_cloudwatch_log_group" "gateway" {
  name              = "/ecs/nebu-${var.environment}-gateway"
  retention_in_days = 30

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-gateway-logs"
  })
}

resource "aws_cloudwatch_log_group" "core" {
  name              = "/ecs/nebu-${var.environment}-core"
  retention_in_days = 30

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-core-logs"
  })
}

# ── ECS Task Definition: gateway ─────────────────────────────────────────────

resource "aws_ecs_task_definition" "gateway" {
  family                   = "nebu-${var.environment}-gateway"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.ecs_task_execution.arn

  depends_on = [aws_cloudwatch_log_group.gateway]

  container_definitions = jsonencode([
    {
      name      = "gateway"
      image     = "${var.image_registry}/nebu-gateway:${var.nebu_version}"
      essential = true

      portMappings = [
        {
          containerPort = 8008
          protocol      = "tcp"
        }
      ]

      healthCheck = {
        command     = ["CMD-SHELL", "wget -qO- http://localhost:8008/_matrix/client/v3/versions || exit 1"]
        interval    = 30
        timeout     = 5
        retries     = 3
        startPeriod = 60
      }

      # All NEBU_* environment variables sourced from Secrets Manager — never plaintext.
      secrets = [
        {
          name      = "NEBU_DB_URL"
          valueFrom = aws_secretsmanager_secret.db_url.arn
        },
        {
          name      = "NEBU_OIDC_ISSUER"
          valueFrom = aws_secretsmanager_secret.oidc_issuer.arn
        },
        {
          name      = "NEBU_OIDC_CLIENT_SECRET"
          valueFrom = aws_secretsmanager_secret.oidc_client_secret.arn
        },
        {
          name      = "NEBU_INTERNAL_SECRET"
          valueFrom = aws_secretsmanager_secret.nebu_internal_secret.arn
        }
      ]

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = "/ecs/nebu-${var.environment}-gateway"
          awslogs-region        = var.aws_region
          awslogs-stream-prefix = "gateway"
        }
      }
    }
  ])

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-gateway-task"
  })
}

# ── ECS Task Definition: core ────────────────────────────────────────────────

resource "aws_ecs_task_definition" "core" {
  family                   = "nebu-${var.environment}-core"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = 256
  memory                   = 512
  execution_role_arn       = aws_iam_role.ecs_task_execution.arn

  depends_on = [aws_cloudwatch_log_group.core]

  container_definitions = jsonencode([
    {
      name      = "core"
      image     = "${var.image_registry}/nebu-core:${var.nebu_version}"
      essential = true

      portMappings = [
        {
          containerPort = 9000
          protocol      = "tcp"
        }
      ]

      healthCheck = {
        command     = ["CMD-SHELL", "wget -qO- http://localhost:4000/health || exit 1"]
        interval    = 30
        timeout     = 5
        retries     = 3
        startPeriod = 90
      }

      # All runtime credentials sourced from Secrets Manager — never plaintext.
      secrets = [
        {
          name      = "DATABASE_URL"
          valueFrom = aws_secretsmanager_secret.db_url.arn
        },
        {
          name      = "RELEASE_COOKIE"
          valueFrom = aws_secretsmanager_secret.release_cookie.arn
        },
        {
          name      = "NEBU_INTERNAL_SECRET"
          valueFrom = aws_secretsmanager_secret.nebu_internal_secret.arn
        }
      ]

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = "/ecs/nebu-${var.environment}-core"
          awslogs-region        = var.aws_region
          awslogs-stream-prefix = "core"
        }
      }
    }
  ])

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-core-task"
  })
}

# ── ECS Service: gateway ──────────────────────────────────────────────────────

resource "aws_ecs_service" "gateway" {
  name            = "nebu-${var.environment}-gateway"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.gateway.arn
  desired_count   = var.ecs_desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.ecs.id]
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.gateway.arn
    container_name   = "gateway"
    container_port   = 8008
  }

  # Allow ECS to update the service without tofu treating it as a drift.
  lifecycle {
    ignore_changes = [task_definition, desired_count]
  }

  depends_on = [
    aws_lb_listener.https,
    aws_iam_role_policy_attachment.ecs_task_execution_managed,
  ]

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-gateway-service"
  })
}

# ── ECS Service: core ─────────────────────────────────────────────────────────

resource "aws_ecs_service" "core" {
  name            = "nebu-${var.environment}-core"
  cluster         = aws_ecs_cluster.this.id
  task_definition = aws_ecs_task_definition.core.arn
  desired_count   = var.ecs_desired_count
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.ecs.id]
    assign_public_ip = false
  }

  # Allow ECS to update the service without tofu treating it as a drift.
  lifecycle {
    ignore_changes = [task_definition, desired_count]
  }

  depends_on = [
    aws_iam_role_policy_attachment.ecs_task_execution_managed,
  ]

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-core-service"
  })
}
