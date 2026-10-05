# ============================================================================
# Terraform State Backend - Production
# ============================================================================

terraform {
  backend "s3" {
    bucket         = "nidaw-terraform-state"
    key            = "production/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "nidaw-terraform-locks"
    
    # Enable state locking and consistency checks
    skip_metadata_api_check = false
  }
}