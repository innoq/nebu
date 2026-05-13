# nebu-aws: Module outputs — networking identifiers for use by other modules.

output "vpc_id" {
  description = "ID of the VPC."
  value       = aws_vpc.this.id
}

output "public_subnet_ids" {
  description = "IDs of the two public subnets (one per availability zone)."
  value       = aws_subnet.public[*].id
}

output "private_subnet_ids" {
  description = "IDs of the two private subnets (one per availability zone)."
  value       = aws_subnet.private[*].id
}

output "alb_sg_id" {
  description = "ID of the ALB security group."
  value       = aws_security_group.alb.id
}

output "ecs_sg_id" {
  description = "ID of the ECS security group."
  value       = aws_security_group.ecs.id
}

output "rds_sg_id" {
  description = "ID of the RDS security group."
  value       = aws_security_group.rds.id
}

output "ecs_cluster_arn" {
  description = "ARN of the ECS Fargate cluster."
  value       = aws_ecs_cluster.this.arn
}

output "db_endpoint" {
  description = "Connection endpoint for the RDS PostgreSQL instance (host:port)."
  value       = aws_db_instance.this.endpoint
}

output "task_execution_role_arn" {
  description = "ARN of the IAM role used by ECS task definitions for execution (image pull + Secrets Manager)."
  value       = aws_iam_role.ecs_task_execution.arn
}

output "alb_dns_name" {
  description = "DNS name of the Application Load Balancer. Point your domain's CNAME or ALIAS record here."
  value       = aws_lb.this.dns_name
}

output "alb_zone_id" {
  description = "Route 53 hosted zone ID of the ALB (used for ALIAS records in Route 53)."
  value       = aws_lb.this.zone_id
}
