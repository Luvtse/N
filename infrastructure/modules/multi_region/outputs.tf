# ============================================================================
# Multi-Region Module - Outputs
# ============================================================================

output "primary_region" {
  description = "Primary region configuration"
  value       = local.primary_region
}

output "secondary_regions" {
  description = "Secondary region configurations"
  value       = local.secondary_regions
}

output "primary_health_check_id" {
  description = "Primary region health check ID"
  value       = aws_route53_health_check.primary.id
}

output "secondary_health_check_ids" {
  description = "Secondary region health check IDs"
  value       = { for k, v in aws_route53_health_check.secondary : k => v.id }
}

output "api_dns_name" {
  description = "API DNS name"
  value       = "api.${var.domain}"
}

output "primary_record_id" {
  description = "Primary Route53 record ID"
  value       = aws_route53_record.api_primary.id
}

output "secondary_record_ids" {
  description = "Secondary Route53 record IDs"
  value       = { for k, v in aws_route53_record.api_secondary : k => v.id }
}

output "read_replica_endpoints" {
  description = "Read replica endpoints"
  value       = { for k, v in aws_db_instance.read_replica : k => v.endpoint }
}

output "s3_replication_role_arn" {
  description = "S3 replication IAM role ARN"
  value       = aws_iam_role.s3_replication.arn
}