# ============================================================================
# Security Module - Variables
# ============================================================================

variable "environment" {
  description = "Environment name"
  type        = string
}

variable "region" {
  description = "AWS region"
  type        = string
}

variable "account_id" {
  description = "AWS account ID"
  type        = string
}

variable "project_name" {
  description = "Project name"
  type        = string
  default     = "nidaw"
}

# ============================================================================
# KMS Configuration
# ============================================================================

variable "kms_key_deletion_window" {
  description = "KMS key deletion window in days"
  type        = number
  default     = 30
  
  validation {
    condition     = var.kms_key_deletion_window >= 7 && var.kms_key_deletion_window <= 30
    error_message = "Deletion window must be between 7 and 30 days."
  }
}

variable "kms_enable_key_rotation" {
  description = "Enable automatic KMS key rotation"
  type        = bool
  default     = true
}

variable "kms_admin_role_arns" {
  description = "IAM role ARNs that can administer the KMS key"
  type        = list(string)
  default     = []
}

variable "kms_user_role_arns" {
  description = "IAM role ARNs that can use the KMS key"
  type        = list(string)
  default     = []
}

# ============================================================================
# WAF Configuration
# ============================================================================

variable "waf_enabled" {
  description = "Enable WAF"
  type        = bool
  default     = true
}

variable "waf_rate_limit" {
  description = "WAF rate limit per 5 minutes per IP"
  type        = number
  default     = 2000
}

variable "waf_blocked_countries" {
  description = "List of country codes to block"
  type        = list(string)
  default     = []
}

variable "waf_allowed_countries" {
  description = "List of country codes to allow (if using allowlist mode)"
  type        = list(string)
  default     = []
}

variable "waf_ip_allowlist" {
  description = "List of IP addresses to allow"
  type        = list(string)
  default     = []
}

variable "waf_ip_blocklist" {
  description = "List of IP addresses to block"
  type        = list(string)
  default     = []
}

# ============================================================================
# GuardDuty Configuration
# ============================================================================

variable "guardduty_enabled" {
  description = "Enable GuardDuty"
  type        = bool
  default     = true
}

variable "guardduty_finding_publishing_frequency" {
  description = "GuardDuty finding publishing frequency"
  type        = string
  default     = "FIFTEEN_MINUTES"
  
  validation {
    condition     = contains(["FIFTEEN_MINUTES", "ONE_HOUR", "SIX_HOURS"], var.guardduty_finding_publishing_frequency)
    error_message = "Must be FIFTEEN_MINUTES, ONE_HOUR, or SIX_HOURS."
  }
}

variable "guardduty_enable_s3_protection" {
  description = "Enable GuardDuty S3 protection"
  type        = bool
  default     = true
}

variable "guardduty_enable_eks_protection" {
  description = "Enable GuardDuty EKS protection"
  type        = bool
  default     = true
}

# ============================================================================
# Security Hub Configuration
# ============================================================================

variable "securityhub_enabled" {
  description = "Enable Security Hub"
  type        = bool
  default     = true
}

variable "securityhub_standards" {
  description = "Security Hub standards to enable"
  type = list(object({
    name = string
    arn  = string
  }))
  default = [
    {
      name = "cis"
      arn  = "arn:aws:securityhub:::ruleset/cis-aws-foundations-benchmark/v/1.4.0"
    },
    {
      name = "pci"
      arn  = "arn:aws:securityhub:::standards/pci-dss/v/3.2.1"
    },
    {
      name = "aws-foundational"
      arn  = "arn:aws:securityhub:us-east-1::standards/aws-foundational-security-best-practices/v/1.0.0"
    }
  ]
}

# ============================================================================
# Config Rules
# ============================================================================

variable "config_enabled" {
  description = "Enable AWS Config"
  type        = bool
  default     = true
}

variable "config_rules" {
  description = "List of AWS Config rules to enable"
  type        = list(string)
  default = [
    "ENCRYPTED_VOLUMES",
    "RDS_STORAGE_ENCRYPTED",
    "S3_BUCKET_SSL_REQUESTS_ONLY",
    "IAM_PASSWORD_POLICY",
    "ROOT_ACCOUNT_MFA_ENABLED",
    "MFA_ENABLED_FOR_IAM_CONSOLE_ACCESS",
    "RESTRICTED_SSH",
    "RESTRICTED_INCOMING_TRAFFIC"
  ]
}

# ============================================================================
# CloudTrail Configuration
# ============================================================================

variable "cloudtrail_enabled" {
  description = "Enable CloudTrail"
  type        = bool
  default     = true
}

variable "cloudtrail_is_multi_region" {
  description = "Enable multi-region CloudTrail"
  type        = bool
  default     = true
}

variable "cloudtrail_log_retention_days" {
  description = "CloudTrail log retention in days"
  type        = number
  default     = 365
}

# ============================================================================
# Audit Logging
# ============================================================================

variable "audit_bucket_versioning" {
  description = "Enable versioning on audit bucket"
  type        = bool
  default     = true
}

variable "audit_bucket_object_lock" {
  description = "Enable object lock on audit bucket (WORM)"
  type        = bool
  default     = true
}

variable "audit_bucket_retention_years" {
  description = "Audit log retention in years"
  type        = number
  default     = 7
}

variable "tags" {
  description = "Additional tags"
  type        = map(string)
  default     = {}
}