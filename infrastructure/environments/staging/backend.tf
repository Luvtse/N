# ============================================================================
# Terraform State Backend - Staging
# ============================================================================

terraform {
  backend "s3" {
    bucket         = "nidaw-terraform-state"
    key            = "staging/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "nidaw-terraform-locks"
  }
}