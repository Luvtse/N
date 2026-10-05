# ============================================================================
# Global Resources
# Resources that span multiple regions/environments
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

provider "aws" {
  region = "us-east-1" # Route53 is global
  
  default_tags {
    tags = {
      Project   = "nidaw"
      ManagedBy = "terraform"
      Scope     = "global"
    }
  }
}

# ============================================================================
# ROUTE53 HOSTED ZONE
# ============================================================================

resource "aws_route53_zone" "main" {
  name = var.domain
  
  tags = {
    Name = var.domain
  }
}

# ============================================================================
# S3 BUCKET FOR TERRAFORM STATE
# ============================================================================

resource "aws_s3_bucket" "terraform_state" {
  bucket = "${var.project_name}-terraform-state"
  
  tags = {
    Name = "${var.project_name}-terraform-state"
  }
}

resource "aws_s3_bucket_versioning" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id
  
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id
  
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "aws:kms"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "terraform_state" {
  bucket = aws_s3_bucket.terraform_state.id
  
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ============================================================================
# DYNAMODB TABLE FOR TERRAFORM LOCKS
# ============================================================================

resource "aws_dynamodb_table" "terraform_locks" {
  name         = "${var.project_name}-terraform-locks"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "LockID"
  
  attribute {
    name = "LockID"
    type = "S"
  }
  
  point_in_time_recovery {
    enabled = true
  }
  
  tags = {
    Name = "${var.project_name}-terraform-locks"
  }
}

# ============================================================================
# AWS ORGANIZATION (Optional)
# ============================================================================

resource "aws_organizations_organization" "main" {
  count = var.enable_organizations ? 1 : 0
  
  aws_service_access_principals = [
    "cloudtrail.amazonaws.com",
    "config.amazonaws.com",
    "sso.amazonaws.com",
  ]
  
  enabled_policy_types = [
    "SERVICE_CONTROL_POLICY",
    "TAG_POLICY",
  ]
  
  feature_set = "ALL"
}

# ============================================================================
# IAM IDENTITY CENTER (SSO)
# ============================================================================

resource "aws_ssoadmin_instance" "main" {
  count = var.enable_sso ? 1 : 0
}

# ============================================================================
# CLOUDFRONT DISTRIBUTION (for static assets)
# ============================================================================

resource "aws_cloudfront_distribution" "static_assets" {
  enabled             = true
  is_ipv6_enabled     = true
  default_root_object = "index.html"
  price_class         = "PriceClass_100"
  
  origin {
    domain_name = aws_s3_bucket.static_assets.bucket_regional_domain_name
    origin_id   = "S3-static-assets"
    
    s3_origin_config {
      origin_access_identity = aws_cloudfront_origin_access_identity.static_assets.cloudfront_access_identity_path
    }
  }
  
  default_cache_behavior {
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]
    target_origin_id       = "S3-static-assets"
    viewer_protocol_policy = "redirect-to-https"
    
    forwarded_values {
      query_string = false
      cookies {
        forward = "none"
      }
    }
    
    min_ttl     = 0
    default_ttl = 3600
    max_ttl     = 86400
  }
  
  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }
  
  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate.static_assets.arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }
  
  tags = {
    Name = "${var.project_name}-static-assets"
  }
}

# ============================================================================
# S3 BUCKET FOR STATIC ASSETS
# ============================================================================

resource "aws_s3_bucket" "static_assets" {
  bucket = "${var.project_name}-static-assets"
}

resource "aws_s3_bucket_public_access_block" "static_assets" {
  bucket = aws_s3_bucket.static_assets.id
  
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_cloudfront_origin_access_identity" "static_assets" {
  comment = "OAI for ${var.project_name} static assets"
}

# ============================================================================
# ACM CERTIFICATE
# ============================================================================

resource "aws_acm_certificate" "static_assets" {
  domain_name       = "assets.${var.domain}"
  validation_method = "DNS"
  
  subject_alternative_names = [
    "*.${var.domain}"
  ]
  
  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_route53_record" "cert_validation" {
  for_each = {
    for dvo in aws_acm_certificate.static_assets.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  }
  
  allow_overwrite = true
  name            = each.value.name
  records         = [each.value.record]
  ttl             = 60
  type            = each.value.type
  zone_id         = aws_route53_zone.main.zone_id
}

resource "aws_acm_certificate_validation" "static_assets" {
  certificate_arn         = aws_acm_certificate.static_assets.arn
  validation_record_fqdns = [for record in aws_route53_record.cert_validation : record.fqdn]
}

# ============================================================================
# SNS TOPIC FOR GLOBAL ALERTS
# ============================================================================

resource "aws_sns_topic" "global_alerts" {
  name = "${var.project_name}-global-alerts"
  
  delivery_policy = jsonencode({
    http = {
      defaultHealthyRetryPolicy = {
        minDelayTarget     = 20
        maxDelayTarget     = 20
        numRetries         = 3
        numMaxDelayRetries = 0
        numNoDelayRetries  = 0
        numMinDelayRetries = 0
        backoffFunction    = "linear"
      }
    }
  })
}

resource "aws_sns_topic_subscription" "email" {
  for_each = toset(var.alert_emails)
  
  topic_arn = aws_sns_topic.global_alerts.arn
  protocol  = "email"
  endpoint  = each.value
}

# ============================================================================
# COST EXPLORER BUDGET
# ============================================================================

resource "aws_budgets_budget" "monthly" {
  name         = "${var.project_name}-monthly-budget"
  budget_type  = "COST"
  limit_amount = var.monthly_budget_limit
  limit_unit   = "USD"
  time_unit    = "MONTHLY"
  
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = var.alert_emails
  }
  
  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = var.alert_emails
  }
}