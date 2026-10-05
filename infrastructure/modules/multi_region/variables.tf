# ============================================================================
# Multi-Region Module - Variables
# ============================================================================

variable "project_name" {
  description = "Project name"
  type        = string
  default     = "nidaw"
}

variable "environment" {
  description = "Environment name"
  type        = string
}

variable "domain" {
  description = "Root domain name"
  type        = string
}

variable "hosted_zone_id" {
  description = "Route53 hosted zone ID"
  type        = string
}

variable "account_id" {
  description = "AWS account ID"
  type        = string
}

variable "regions" {
  description = "List of regions with their configuration"
  type = list(object({
    name               = string
    aws_region         = string
    primary            = bool
    alb_dns_name       = string
    alb_zone_id        = string
    db_instance_class  = string
  }))
  
  validation {
    condition     = length([for r in var.regions : r if r.primary]) == 1
    error_message = "Exactly one region must be marked as primary."
  }
}

variable "kms_key_arn" {
  description = "Primary region KMS key ARN"
  type        = string
}

variable "secondary_kms_key_arn" {
  description = "Secondary region KMS key ARN"
  type        = string
}

variable "audit_bucket_name" {
  description = "Primary audit bucket name"
  type        = string
}

variable "primary_audit_bucket_arn" {
  description = "Primary audit bucket ARN"
  type        = string
}

variable "secondary_audit_bucket_arn" {
  description = "Secondary audit bucket ARN"
  type        = string
}

variable "sns_topic_arns" {
  description = "SNS topic ARNs for alerts"
  type        = list(string)
  default     = []
}

variable "failover_enabled" {
  description = "Enable automatic failover"
  type        = bool
  default     = true
}

variable "health_check_threshold" {
  description = "Number of failed health checks before failover"
  type        = number
  default     = 3
}

variable "tags" {
  description = "Additional tags"
  type        = map(string)
  default     = {}
}