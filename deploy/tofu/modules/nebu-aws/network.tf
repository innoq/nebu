# nebu-aws: AWS networking resources — VPC, subnets, gateways, route tables, security groups.

# ── Availability zones ───────────────────────────────────────────────────────

data "aws_availability_zones" "available" {
  state = "available"

  lifecycle {
    postcondition {
      condition     = length(self.names) >= 2
      error_message = "At least 2 availability zones must be available in the selected region. Got: ${length(self.names)}."
    }
  }
}

locals {
  azs = length(var.availability_zones) >= 2 ? var.availability_zones : slice(data.aws_availability_zones.available.names, 0, 2)
}

# ── VPC ─────────────────────────────────────────────────────────────────────

resource "aws_vpc" "this" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-vpc"
  })
}

# ── Public subnets ───────────────────────────────────────────────────────────

resource "aws_subnet" "public" {
  count = 2

  vpc_id                  = aws_vpc.this.id
  cidr_block              = cidrsubnet(var.vpc_cidr, 8, count.index)
  availability_zone       = local.azs[count.index]
  map_public_ip_on_launch = true

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-public-${local.azs[count.index]}"
    Tier = "public"
  })
}

# ── Private subnets ──────────────────────────────────────────────────────────

resource "aws_subnet" "private" {
  count = 2

  vpc_id            = aws_vpc.this.id
  cidr_block        = cidrsubnet(var.vpc_cidr, 8, count.index + 10)
  availability_zone = local.azs[count.index]

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-private-${local.azs[count.index]}"
    Tier = "private"
  })
}

# ── Internet Gateway ─────────────────────────────────────────────────────────

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-igw"
  })
}

# ── Elastic IP for NAT Gateway ───────────────────────────────────────────────

resource "aws_eip" "nat" {
  domain = "vpc"

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-nat-eip"
  })
}

# ── NAT Gateway (in first public subnet) ────────────────────────────────────
# Single NAT Gateway (AZ0 only) — cost-optimized for dev/staging.
# For HA production use, provision one NAT GW per AZ.

resource "aws_nat_gateway" "this" {
  allocation_id = aws_eip.nat.id
  subnet_id     = aws_subnet.public[0].id

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-nat"
  })

  depends_on = [aws_internet_gateway.this]
}

# ── Public route table ───────────────────────────────────────────────────────

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-rt-public"
  })
}

resource "aws_route_table_association" "public" {
  count = length(aws_subnet.public)

  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}

# ── Private route table ──────────────────────────────────────────────────────

resource "aws_route_table" "private" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block     = "0.0.0.0/0"
    nat_gateway_id = aws_nat_gateway.this.id
  }

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-rt-private"
  })
}

resource "aws_route_table_association" "private" {
  count = length(aws_subnet.private)

  subnet_id      = aws_subnet.private[count.index].id
  route_table_id = aws_route_table.private.id
}

# ── Security Group: ALB ──────────────────────────────────────────────────────

resource "aws_security_group" "alb" {
  name        = "nebu-${var.environment}-alb-sg"
  description = "Security group for the Application Load Balancer. Allows inbound HTTP/HTTPS from the internet."
  vpc_id      = aws_vpc.this.id

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-alb-sg"
  })
}

resource "aws_security_group_rule" "alb_ingress_http" {
  type              = "ingress"
  security_group_id = aws_security_group.alb.id
  from_port         = 80
  to_port           = 80
  protocol          = "tcp"
  cidr_blocks       = ["0.0.0.0/0"]
  description       = "Allow HTTP from internet"
}

resource "aws_security_group_rule" "alb_ingress_https" {
  type              = "ingress"
  security_group_id = aws_security_group.alb.id
  from_port         = 443
  to_port           = 443
  protocol          = "tcp"
  cidr_blocks       = ["0.0.0.0/0"]
  description       = "Allow HTTPS from internet"
}

resource "aws_security_group_rule" "alb_egress_all" {
  type              = "egress"
  security_group_id = aws_security_group.alb.id
  from_port         = 0
  to_port           = 0
  protocol          = "-1"
  cidr_blocks       = ["0.0.0.0/0"]
  description       = "Allow all outbound traffic"
}

# ── Security Group: ECS ──────────────────────────────────────────────────────

resource "aws_security_group" "ecs" {
  name        = "nebu-${var.environment}-ecs-sg"
  description = "Security group for ECS tasks. Allows inbound traffic from ALB security group only."
  vpc_id      = aws_vpc.this.id

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-ecs-sg"
  })
}

resource "aws_security_group_rule" "ecs_ingress_matrix_from_alb" {
  type                     = "ingress"
  security_group_id        = aws_security_group.ecs.id
  from_port                = 8008
  to_port                  = 8008
  protocol                 = "tcp"
  source_security_group_id = aws_security_group.alb.id
  description              = "Allow Matrix API (port 8008) from ALB security group"
}

resource "aws_security_group_rule" "ecs_ingress_grpc_from_alb" {
  type                     = "ingress"
  security_group_id        = aws_security_group.ecs.id
  from_port                = 9000
  to_port                  = 9000
  protocol                 = "tcp"
  source_security_group_id = aws_security_group.alb.id
  description              = "Allow gRPC Core service (port 9000) from ALB security group"
}

resource "aws_security_group_rule" "ecs_egress_all" {
  type              = "egress"
  security_group_id = aws_security_group.ecs.id
  from_port         = 0
  to_port           = 0
  protocol          = "-1"
  cidr_blocks       = ["0.0.0.0/0"]
  description       = "Allow all outbound traffic"
}

# ── Security Group: RDS ──────────────────────────────────────────────────────

resource "aws_security_group" "rds" {
  name        = "nebu-${var.environment}-rds-sg"
  description = "Security group for RDS PostgreSQL. Allows inbound on port 5432 from ECS security group only."
  vpc_id      = aws_vpc.this.id

  tags = merge(var.common_tags, {
    Name = "nebu-${var.environment}-rds-sg"
  })
}

resource "aws_security_group_rule" "rds_ingress_from_ecs" {
  type                     = "ingress"
  security_group_id        = aws_security_group.rds.id
  from_port                = 5432
  to_port                  = 5432
  protocol                 = "tcp"
  source_security_group_id = aws_security_group.ecs.id
  description              = "Allow PostgreSQL from ECS security group"
}

resource "aws_security_group_rule" "rds_egress_vpc" {
  type              = "egress"
  security_group_id = aws_security_group.rds.id
  from_port         = 5432
  to_port           = 5432
  protocol          = "tcp"
  cidr_blocks       = [var.vpc_cidr]
  description       = "Allow outbound PostgreSQL within VPC only"
}
