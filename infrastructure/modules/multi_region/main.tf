# ============================================================================
# Multi-Region Module - Main Configuration
# Purpose: Active-Passive failover with Route53 health checks
# ============================================================================

terraform {
  required_version = ">= 1.5.0"
  
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

# ============================================================================
# LOCALS
# ============================================================================

locals {
  primary_region   = [for r in var.regions : r if r.primary][0]
  secondary_regions = [for r in var.regions : r if !r.primary]
  
  health_check_path = "/health"
  health_check_port = 443
}

# ============================================================================
# ROUTE53 HEALTH CHECKS
# ============================================================================

resource "aws_route53_health_check" "primary" {
  fqdn              = local.primary_region.alb_dns_name
  port              = local.health_check_port
  type              = "HTTPS"
  resource_path     = local.health_check_path
  failure_threshold = 3
  request_interval  = 30
  
  tags = {
    Name        = "${var.project_name}-primary-health-check"
    Environment = var.environment
    Region      = local.primary_region.name
  }
}

resource "aws_route53_health_check" "secondary" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  fqdn              = each.value.alb_dns_name
  port              = local.health_check_port
  type              = "HTTPS"
  resource_path     = local.health_check_path
  failure_threshold = 3
  request_interval  = 30
  
  tags = {
    Name        = "${var.project_name}-${each.key}-health-check"
    Environment = var.environment
    Region      = each.key
  }
}

# ============================================================================
# ROUTE53 FAILOVER RECORDS
# ============================================================================

resource "aws_route53_record" "api_primary" {
  zone_id = var.hosted_zone_id
  name    = "api.${var.domain}"
  type    = "A"
  
  alias {
    name                   = local.primary_region.alb_dns_name
    zone_id                = local.primary_region.alb_zone_id
    evaluate_target_health = true
  }
  
  set_identifier = "primary"
  
  failover_routing_policy {
    type = "PRIMARY"
  }
  
  health_check_id = aws_route53_health_check.primary.id
}

resource "aws_route53_record" "api_secondary" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  zone_id = var.hosted_zone_id
  name    = "api.${var.domain}"
  type    = "A"
  
  alias {
    name                   = each.value.alb_dns_name
    zone_id                = each.value.alb_zone_id
    evaluate_target_health = true
  }
  
  set_identifier = "secondary-${each.key}"
  
  failover_routing_policy {
    type = "SECONDARY"
  }
  
  health_check_id = aws_route53_health_check.secondary[each.key].id
}

# ============================================================================
# CLOUDWATCH ALARMS FOR FAILOVER
# ============================================================================

resource "aws_cloudwatch_metric_alarm" "primary_health" {
  alarm_name          = "${var.project_name}-primary-unhealthy"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 2
  metric_name         = "HealthCheckStatus"
  namespace           = "AWS/Route53"
  period              = 60
  statistic           = "Minimum"
  threshold           = 1.0
  alarm_description   = "Primary region health check is failing"
  
  dimensions = {
    HealthCheckId = aws_route53_health_check.primary.id
  }
  
  alarm_actions = var.sns_topic_arns
  ok_actions    = var.sns_topic_arns
}

# ============================================================================
# DATABASE CROSS-REGION REPLICATION (Read Replicas)
# ============================================================================

resource "aws_db_instance" "read_replica" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  identifier          = "${var.project_name}-replica-${each.key}"
  replicate_source_db = "arn:aws:rds:${local.primary_region.aws_region}:${var.account_id}:db:${var.project_name}-primary"
  
  instance_class    = each.value.db_instance_class
  storage_encrypted = true
  kms_key_id        = var.kms_key_arn
  
  multi_az            = false
  publicly_accessible = false
  
  backup_retention_period = 7
  maintenance_window      = "Sun:04:00-Sun:05:00"
  
  tags = {
    Name        = "${var.project_name}-replica-${each.key}"
    Environment = var.environment
    Region      = each.key
    Role        = "read-replica"
  }
}

# ============================================================================
# S3 CROSS-REGION REPLICATION
# ============================================================================

resource "aws_s3_bucket_replication_configuration" "audit_logs" {
  provider = aws.primary
  
  bucket = var.audit_bucket_name
  role   = aws_iam_role.s3_replication.arn
  
  rule {
    id     = "replicate-to-secondary"
    status = "Enabled"
    
    destination {
      bucket        = var.secondary_audit_bucket_arn
      storage_class = "STANDARD_IA"
      
      encryption_configuration {
        replica_kms_key_id = var.secondary_kms_key_arn
      }
    }
    
    delete_marker_replication {
      status = "Enabled"
    }
  }
}

# ============================================================================
# IAM ROLE FOR S3 REPLICATION
# ============================================================================

resource "aws_iam_role" "s3_replication" {
  name = "${var.project_name}-s3-replication-${var.environment}"
  
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "s3.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy" "s3_replication" {
  name = "${var.project_name}-s3-replication-policy"
  role = aws_iam_role.s3_replication.id
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = [
          "s3:GetObjectVersionForReplication",
          "s3:GetObjectVersionAcl",
          "s3:GetObjectVersionTagging"
        ]
        Effect   = "Allow"
        Resource = "${var.primary_audit_bucket_arn}/*"
      },
      {
        Action = [
          "s3:ReplicateObject",
          "s3:ReplicateDelete",
          "s3:ReplicateTags"
        ]
        Effect   = "Allow"
        Resource = "${var.secondary_audit_bucket_arn}/*"
      }
    ]
  })
}