# ============================================================================
# Secrets Manager Module - Main Configuration
# Purpose: Secure secret storage with automatic rotation
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
# KMS KEY FOR SECRET ENCRYPTION
# ============================================================================

resource "aws_kms_key" "secrets" {
  description             = "KMS key for encrypting secrets"
  deletion_window_in_days = 10
  enable_key_rotation     = true
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid    = "Enable IAM User Permissions"
        Effect = "Allow"
        Principal = {
          AWS = "arn:aws:iam::${var.account_id}:root"
        }
        Action   = "kms:*"
        Resource = "*"
      },
      {
        Sid    = "Allow use of the key"
        Effect = "Allow"
        Principal = {
          AWS = var.allowed_role_arns
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
    Name        = "${var.project_name}-secrets-key-${var.environment}"
    Environment = var.environment
  }
}

resource "aws_kms_alias" "secrets" {
  name          = "alias/${var.project_name}/secrets/${var.environment}"
  target_key_id = aws_kms_key.secrets.key_id
}

# ============================================================================
# DATABASE CREDENTIALS
# ============================================================================

resource "aws_secretsmanager_secret" "database" {
  name                    = "${var.project_name}/database/${var.environment}"
  description             = "Database credentials for ${var.environment}"
  kms_key_id              = aws_kms_key.secrets.arn
  recovery_window_in_days = var.recovery_window_days
  
  tags = {
    Environment = var.environment
    Service     = "database"
  }
}

resource "aws_secretsmanager_secret_version" "database" {
  secret_id = aws_secretsmanager_secret.database.id
  secret_string = jsonencode({
    username = var.db_username
    password = var.db_password != "" ? var.db_password : random_password.db_password.result
    engine   = "postgres"
    host     = var.db_host
    port     = var.db_port
    dbname   = var.db_name
  })
}

resource "random_password" "db_password" {
  length           = 32
  special          = true
  override_special = "!#$%&*()-_=+[]{}<>:?"
}

# ============================================================================
# JWT SECRET
# ============================================================================

resource "aws_secretsmanager_secret" "jwt" {
  name                    = "${var.project_name}/auth/jwt/${var.environment}"
  description             = "JWT signing secret"
  kms_key_id              = aws_kms_key.secrets.arn
  recovery_window_in_days = var.recovery_window_days
  
  tags = {
    Environment = var.environment
    Service     = "auth"
  }
}

resource "aws_secretsmanager_secret_version" "jwt" {
  secret_id = aws_secretsmanager_secret.jwt.id
  secret_string = jsonencode({
    secret         = var.jwt_secret != "" ? var.jwt_secret : random_password.jwt_secret.result
    access_ttl     = var.jwt_access_ttl
    refresh_ttl    = var.jwt_refresh_ttl
    issuer         = var.jwt_issuer
    audience       = var.jwt_audience
  })
}

resource "random_password" "jwt_secret" {
  length           = 64
  special          = true
  override_special = "!#$%&*()-_=+[]{}<>:?"
}

# ============================================================================
# STRIPE API KEYS
# ============================================================================

resource "aws_secretsmanager_secret" "stripe" {
  name                    = "${var.project_name}/payments/stripe/${var.environment}"
  description             = "Stripe API credentials"
  kms_key_id              = aws_kms_key.secrets.arn
  recovery_window_in_days = var.recovery_window_days
  
  tags = {
    Environment = var.environment
    Service     = "payments"
  }
}

resource "aws_secretsmanager_secret_version" "stripe" {
  secret_id = aws_secretsmanager_secret.stripe.id
  secret_string = jsonencode({
    api_key         = var.stripe_api_key
    webhook_secret  = var.stripe_webhook_secret
    publishable_key = var.stripe_publishable_key
  })
}

# ============================================================================
# KAFKA CREDENTIALS
# ============================================================================

resource "aws_secretsmanager_secret" "kafka" {
  name                    = "${var.project_name}/kafka/${var.environment}"
  description             = "Kafka connection credentials"
  kms_key_id              = aws_kms_key.secrets.arn
  recovery_window_in_days = var.recovery_window_days
  
  tags = {
    Environment = var.environment
    Service     = "kafka"
  }
}

resource "aws_secretsmanager_secret_version" "kafka" {
  secret_id = aws_secretsmanager_secret.kafka.id
  secret_string = jsonencode({
    bootstrap_servers = var.kafka_bootstrap_servers
    schema_registry   = var.kafka_schema_registry_url
    sasl_username     = var.kafka_sasl_username
    sasl_password     = var.kafka_sasl_password
  })
}

# ============================================================================
# REDIS CREDENTIALS
# ============================================================================

resource "aws_secretsmanager_secret" "redis" {
  name                    = "${var.project_name}/cache/redis/${var.environment}"
  description             = "Redis connection credentials"
  kms_key_id              = aws_kms_key.secrets.arn
  recovery_window_in_days = var.recovery_window_days
  
  tags = {
    Environment = var.environment
    Service     = "cache"
  }
}

resource "aws_secretsmanager_secret_version" "redis" {
  secret_id = aws_secretsmanager_secret.redis.id
  secret_string = jsonencode({
    url      = var.redis_url
    password = var.redis_password
  })
}

# ============================================================================
# THIRD-PARTY API KEYS
# ============================================================================

resource "aws_secretsmanager_secret" "external_apis" {
  name                    = "${var.project_name}/external-apis/${var.environment}"
  description             = "External API credentials"
  kms_key_id              = aws_kms_key.secrets.arn
  recovery_window_in_days = var.recovery_window_days
  
  tags = {
    Environment = var.environment
    Service     = "integrations"
  }
}

resource "aws_secretsmanager_secret_version" "external_apis" {
  secret_id = aws_secretsmanager_secret.external_apis.id
  secret_string = jsonencode({
    google_maps_api_key = var.google_maps_api_key
    twilio_account_sid  = var.twilio_account_sid
    twilio_auth_token   = var.twilio_auth_token
    sendgrid_api_key    = var.sendgrid_api_key
    firebase_project_id = var.firebase_project_id
  })
}

# ============================================================================
# ROTATION CONFIGURATION
# ============================================================================

resource "aws_secretsmanager_secret_rotation" "database" {
  count = var.enable_database_rotation ? 1 : 0
  
  secret_id           = aws_secretsmanager_secret.database.id
  rotation_lambda_arn = aws_lambda_function.rotation.arn
  
  rotation_rules {
    automatically_after_days = var.rotation_days
    duration                 = "2h"
    schedule_expression      = var.rotation_schedule
  }
}

# ============================================================================
# IAM POLICY FOR SECRET ACCESS
# ============================================================================

resource "aws_iam_policy" "secret_access" {
  name        = "${var.project_name}-secret-access-${var.environment}"
  description = "Policy for accessing secrets"
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret"
        ]
        Resource = [
          aws_secretsmanager_secret.database.arn,
          aws_secretsmanager_secret.jwt.arn,
          aws_secretsmanager_secret.stripe.arn,
          aws_secretsmanager_secret.kafka.arn,
          aws_secretsmanager_secret.redis.arn,
          aws_secretsmanager_secret.external_apis.arn
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "kms:Decrypt"
        ]
        Resource = aws_kms_key.secrets.arn
      }
    ]
  })
}