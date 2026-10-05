# SOC 2 Compliance Infrastructure

# KMS Master Key for envelope encryption
resource "aws_kms_key" "nidaw_master" {
  description             = "NIDAW Master Encryption Key"
  deletion_window_in_days = 30
  enable_key_rotation     = true  # SOC 2 CC6.1
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "Enable IAM User Permissions"
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
        }
        Action   = "kms:*"
        Resource = "*"
      },
      {
        Sid    = "Allow use of the key"
        Effect = "Allow"
        Principal = {
          AWS = [
            aws_iam_role.backend.arn,
            aws_iam_role.data_pipeline.arn,
          ]
        }
        Action = [
          "kms:Encrypt",
          "kms:Decrypt",
          "kms:ReEncrypt*",
          "kms:GenerateDataKey*",
          "kms:DescribeKey"
        ]
        Resource = "*"
      }
    ]
  })
  
  tags = {
    Compliance = "SOC2"
    Control    = "CC6.1"
  }
}

# KMS Alias
resource "aws_kms_alias" "nidaw_master" {
  name          = "alias/nidaw-master-key"
  target_key_id = aws_kms_key.nidaw_master.key_id
}

# RDS Encryption (at rest)
resource "aws_db_instance" "nidaw_encrypted" {
  identifier = "nidaw-postgres-encrypted"
  
  storage_encrypted = true  # SOC 2 CC6.1
  kms_key_id        = aws_kms_key.nidaw_master.arn
  
  # Additional security
  public_accessible    = false
  storage_encrypted    = true
  deletion_protection  = true
  multi_az             = true
  
  # Audit logging
  enabled_cloudwatch_logs_exports = ["postgresql"]
  
  backup_retention_period = 35  # SOC 2 CC7.4
  
  tags = {
    Compliance = "SOC2"
    Control    = "CC6.1"
  }
}

# S3 Bucket Encryption
resource "aws_s3_bucket" "nidaw_data" {
  bucket = "nidaw-data-${var.environment}"
}

resource "aws_s3_bucket_server_side_encryption_configuration" "nidaw_data" {
  bucket = aws_s3_bucket.nidaw_data.id
  
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm     = "aws:kms"
      kms_master_key_id = aws_kms_key.nidaw_master.arn
    }
    
    bucket_key_enabled = true
  }
}

# Block public access
resource "aws_s3_bucket_public_access_block" "nidaw_data" {
  bucket = aws_s3_bucket.nidaw_data.id
  
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Versioning for audit trail
resource "aws_s3_bucket_versioning" "nidaw_data" {
  bucket = aws_s3_bucket.nidaw_data.id
  versioning_configuration {
    status = "Enabled"
  }
}

# Audit log bucket (immutable)
resource "aws_s3_bucket" "nidaw_audit" {
  bucket = "nidaw-audit-${var.environment}"
}

resource "aws_s3_bucket_object_lock_configuration" "nidaw_audit" {
  bucket = aws_s3_bucket.nidaw_audit.id
  
  rule {
    default_retention {
      mode = "COMPLIANCE"
      days = 2555  # 7 years retention for SOC 2
    }
  }
}

# CloudTrail for audit logging (SOC 2 CC7.1, CC7.2)
resource "aws_cloudtrail" "nidaw" {
  name                          = "nidaw-audit-trail"
  s3_bucket_name                = aws_s3_bucket.nidaw_audit.id
  include_global_service_events = true
  is_multi_region_trail         = true
  enable_logging                = true
  
  # Encryption
  kms_key_id = aws_kms_key.nidaw_master.arn
  
  # Advanced features
  enable_log_file_validation = true
  
  # Event selectors for detailed logging
  event_selector {
    name = "ManagementEvents"
    read_write_type = "All"
    
    include_management_events = true
  }
  
  event_selector {
    name = "DataEvents"
    
    data_resource {
      type = "AWS::S3::Object"
      values = ["arn:aws:s3"]
    }
    
    data_resource {
      type = "AWS::Lambda::Function"
      values = ["arn:aws:lambda"]
    }
  }
  
  tags = {
    Compliance = "SOC2"
    Control    = "CC7.1,CC7.2"
  }
}

# AWS Config for continuous compliance monitoring
resource "aws_config_config_rule" "encrypted_volumes" {
  name = "encrypted-volumes"
  
  source {
    owner             = "AWS"
    source_identifier = "ENCRYPTED_VOLUMES"
  }
  
  tags = {
    Compliance = "SOC2"
    Control    = "CC6.1"
  }
}

resource "aws_config_config_rule" "rds_encrypted" {
  name = "rds-storage-encrypted"
  
  source {
    owner             = "AWS"
    source_identifier = "RDS_STORAGE_ENCRYPTED"
  }
}

resource "aws_config_config_rule" "s3_bucket_ssl_requests_only" {
  name = "s3-bucket-ssl-requests-only"
  
  source {
    owner             = "AWS"
    source_identifier = "S3_BUCKET_SSL_REQUESTS_ONLY"
  }
}

resource "aws_config_config_rule" "iam_password_policy" {
  name = "iam-password-policy"
  
  source {
    owner             = "AWS"
    source_identifier = "IAM_PASSWORD_POLICY"
  }
}

resource "aws_config_config_rule" "root_account_mfa" {
  name = "root-account-mfa-enabled"
  
  source {
    owner             = "AWS"
    source_identifier = "ROOT_ACCOUNT_MFA_ENABLED"
  }
}

# GuardDuty for threat detection (SOC 2 CC7.3)
resource "aws_guardduty_detector" "nidaw" {
  enable = true
  
  finding_publishing_frequency = "FIFTEEN_MINUTES"
  
  datasources {
    s3 {
      enable = true
    }
    
    kubernetes {
      audit_logs {
        enable = true
      }
    }
  }
  
  tags = {
    Compliance = "SOC2"
    Control    = "CC7.3"
  }
}

# Security Hub for centralized security findings
resource "aws_securityhub_account" "nidaw" {}

resource "aws_securityhub_standards_subscription" "cis" {
  standards_arn = "arn:aws:securityhub:::ruleset/cis-aws-foundations-benchmark/v/1.2.0"
}

resource "aws_securityhub_standards_subscription" "pci" {
  standards_arn = "arn:aws:securityhub:::standards/pci-dss/v/3.2.1"
}

# Vault for secrets management (SOC 2 CC6.1)
resource "aws_iam_role" "vault" {
  name = "nidaw-vault-role"
  
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRoleWithWebIdentity"
        Effect = "Allow"
        Principal = {
          Service = "eks.amazonaws.com"
        }
      }
    ]
  })
}

# WAF for application protection (SOC 2 CC6.6)
resource "aws_wafv2_web_acl" "nidaw" {
  name        = "nidaw-waf"
  description = "WAF for NIDAW API"
  scope       = "REGIONAL"
  
  default_action {
    allow {}
  }
  
  # AWS Managed Rules
  rule {
    name     = "AWSManagedRulesCommonRuleSet"
    priority = 1
    
    override_action {
      none {}
    }
    
    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesCommonRuleSet"
        vendor_name = "AWS"
      }
    }
    
    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "AWSManagedRulesCommonRuleSet"
      sampled_requests_enabled   = true
    }
  }
  
  # Rate limiting (DDoS protection)
  rule {
    name     = "RateLimit"
    priority = 2
    
    action {
      block {}
    }
    
    statement {
      rate_based_statement {
        limit              = 1000
        aggregate_key_type = "IP"
      }
    }
    
    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "RateLimit"
      sampled_requests_enabled   = true
    }
  }
  
  # SQL injection protection
  rule {
    name     = "SQLInjection"
    priority = 3
    
    override_action {
      none {}
    }
    
    statement {
      managed_rule_group_statement {
        name        = "AWSManagedRulesSQLiRuleSet"
        vendor_name = "AWS"
      }
    }
    
    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "SQLInjection"
      sampled_requests_enabled   = true
    }
  }
  
  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = "nidaw-waf"
    sampled_requests_enabled   = true
  }
  
  tags = {
    Compliance = "SOC2"
    Control    = "CC6.6"
  }
}