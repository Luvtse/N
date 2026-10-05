# ============================================================================
# Terraform State Backend - Global Resources
# ============================================================================

terraform {
  backend "s3" {
    bucket         = "nidaw-terraform-state"
    key            = "global/terraform.tfstate"
    region         = "us-east-1"
    encrypt        = true
    dynamodb_table = "nidaw-terraform-locks"
  }
}