# ============================================================================
# Secrets Manager Module - Variables
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

variable "account_id" {
  description = "AWS account ID"
  type        = string
}

variable "allowed_role_arns" {
  description = "IAM role ARNs allowed to use the KMS key"
  type        = list(string)
  default     = []
}

variable "recovery_window_days" {
  description = "Number of days to recover a deleted secret"
  type        = number
  default     = 30
  
  validation {
    condition     = var.recovery_window_days == 0 || (var.recovery_window_days >= 7 && var.recovery_window_days <= 30)
    error_message = "Recovery window must be 0 (immediate) or between 7 and 30 days."
  }
}

# ============================================================================
# Database Credentials
# ============================================================================

variable "db_username" {
  description = "Database username"
  type        = string
  default     = "nidaw_admin"
}

variable "db_password" {
  description = "Database password (leave empty to auto-generate)"
  type        = string
  default     = ""
  sensitive   = true
}

variable "db_host" {
  description = "Database host"
  type        = string
}

variable "db_port" {
  description = "Database port"
  type        = number
  default     = 5432
}

variable "db_name" {
  description = "Database name"
  type        = string
  default     = "nidaw"
}

# ============================================================================
# JWT Configuration
# ============================================================================

variable "jwt_secret" {
  description = "JWT secret (leave empty to auto-generate)"
  type        = string
  default     = ""
  sensitive   = true
}

variable "jwt_access_ttl" {
  description = "JWT access token TTL"
  type        = string
  default     = "15m"
}

variable "jwt_refresh_ttl" {
  description = "JWT refresh token TTL"
  type        = string
  default     = "168h"
}

variable "jwt_issuer" {
  description = "JWT issuer"
  type        = string
  default     = "nidaw"
}

variable "jwt_audience" {
  description = "JWT audience"
  type        = string
  default     = "nidaw-client"
}

# ============================================================================
# Stripe Configuration
# ============================================================================

variable "stripe_api_key" {
  description = "Stripe API key"
  type        = string
  sensitive   = true
}

variable "stripe_webhook_secret" {
  description = "Stripe webhook secret"
  type        = string
  sensitive   = true
}

variable "stripe_publishable_key" {
  description = "Stripe publishable key"
  type        = string
}

# ============================================================================
# Kafka Configuration
# ============================================================================

variable "kafka_bootstrap_servers" {
  description = "Kafka bootstrap servers"
  type        = string
}

variable "kafka_schema_registry_url" {
  description = "Kafka schema registry URL"
  type        = string
}

variable "kafka_sasl_username" {
  description = "Kafka SASL username"
  type        = string
  default     = ""
}

variable "kafka_sasl_password" {
  description = "Kafka SASL password"
  type        = string
  default     = ""
  sensitive   = true
}

# ============================================================================
# Redis Configuration
# ============================================================================

variable "redis_url" {
  description = "Redis URL"
  type        = string
}

variable "redis_password" {
  description = "Redis password"
  type        = string
  default     = ""
  sensitive   = true
}

# ============================================================================
# External APIs
# ============================================================================

variable "google_maps_api_key" {
  description = "Google Maps API key"
  type        = string
  sensitive   = true
}

variable "twilio_account_sid" {
  description = "Twilio account SID"
  type        = string
  sensitive   = true
}

variable "twilio_auth_token" {
  description = "Twilio auth token"
  type        = string
  sensitive   = true
}

variable "sendgrid_api_key" {
  description = "SendGrid API key"
  type        = string
  sensitive   = true
}

variable "firebase_project_id" {
  description = "Firebase project ID"
  type        = string
}

# ============================================================================
# Rotation Configuration
# ============================================================================

variable "enable_database_rotation" {
  description = "Enable automatic database password rotation"
  type        = bool
  default     = true
}

variable "rotation_days" {
  description = "Number of days between rotations"
  type        = number
  default     = 30
  
  validation {
    condition     = var.rotation_days >= 1 && var.rotation_days <= 365
    error_message = "Rotation days must be between 1 and 365."
  }
}

variable "rotation_schedule" {
  description = "CloudWatch Events schedule expression for rotation"
  type        = string
  default     = "rate(30 days)"
}

variable "tags" {
  description = "Additional tags"
  type        = map(string)
  default     = {}
}