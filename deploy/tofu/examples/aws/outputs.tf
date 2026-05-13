# AWS deployment example outputs.

output "dns_name" {
  description = "ALB DNS name to register in your external DNS server when dns_mode = 'external'. Create a CNAME record pointing your domain to this value. Note: CNAME is not supported at the zone apex — use an ALIAS/ANAME record for apex domains. When dns_mode = 'default', Route 53 manages this automatically."
  value       = module.nebu_aws.alb_dns_name
}
