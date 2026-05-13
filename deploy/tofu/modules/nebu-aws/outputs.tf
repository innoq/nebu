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
