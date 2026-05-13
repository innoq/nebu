# nebu-aws: ECS Fargate cluster, IAM task execution role, and task definition skeletons.

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

# Inline policy: allow reading Secrets Manager secrets referenced in task definitions.
resource "aws_iam_role_policy" "ecs_task_execution_secrets" {
  name = "nebu-${var.environment}-ecs-secrets-read"
  role = aws_iam_role.ecs_task_execution.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "kms:Decrypt"
        ]
        Resource = var.nebu_secrets_arn != "" ? [var.nebu_secrets_arn] : ["arn:aws:secretsmanager:*:*:secret:placeholder"]
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

      secrets = var.nebu_secrets_arn != "" ? [
        {
          name      = "NEBU_DB_URL"
          valueFrom = "${var.nebu_secrets_arn}:NEBU_DB_URL::"
        },
        {
          name      = "NEBU_OIDC_ISSUER"
          valueFrom = "${var.nebu_secrets_arn}:NEBU_OIDC_ISSUER::"
        },
        {
          name      = "NEBU_INTERNAL_SECRET_FILE"
          valueFrom = "${var.nebu_secrets_arn}:NEBU_INTERNAL_SECRET_FILE::"
        },
        {
          name      = "NEBU_CORE_GRPC_ADDR"
          valueFrom = "${var.nebu_secrets_arn}:NEBU_CORE_GRPC_ADDR::"
        }
      ] : []

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

      secrets = var.nebu_secrets_arn != "" ? [
        {
          name      = "DATABASE_URL"
          valueFrom = "${var.nebu_secrets_arn}:DATABASE_URL::"
        },
        {
          name      = "RELEASE_COOKIE"
          valueFrom = "${var.nebu_secrets_arn}:RELEASE_COOKIE::"
        },
        {
          name      = "NEBU_INTERNAL_SECRET"
          valueFrom = "${var.nebu_secrets_arn}:NEBU_INTERNAL_SECRET::"
        }
      ] : []

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
