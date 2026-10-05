# ============================================================================
# Terraform State Backend - Development
# ============================================================================

terraform {
  backend "s3" {
    bucket         = "nidaw-terraform-state"
    key            = "dev/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "nidaw-terraform-locks"
    
    # Enable versioning for state history
    # State is automatically versioned by S3
  }
}