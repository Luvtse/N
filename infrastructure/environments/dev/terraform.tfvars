# ============================================================================
# Development Environment Variables
# ============================================================================

project_name = "nidaw"
region       = "us-east-1"

# ============================================================================
# Secrets (Use environment variables in CI/CD, not hardcoded)
# ============================================================================

# db_password          = ""  # Set via TF_VAR_db_password
# jwt_secret           = ""  # Set via TF_VAR_jwt_secret
# stripe_api_key       = ""  # Set via TF_VAR_stripe_api_key
# stripe_webhook_secret = "" # Set via TF_VAR_stripe_webhook_secret
# stripe_publishable_key = "" # Set via TF_VAR_stripe_publishable_key
# google_maps_api_key  = ""  # Set via TF_VAR_google_maps_api_key
# twilio_account_sid   = ""  # Set via TF_VAR_twilio_account_sid
# twilio_auth_token    = ""  # Set via TF_VAR_twilio_auth_token
# sendgrid_api_key     = ""  # Set via TF_VAR_sendgrid_api_key
# firebase_project_id  = ""  # Set via TF_VAR_firebase_project_id

# ============================================================================
# Feature Flags
# ============================================================================

enable_autonomous_vehicles = false
enable_drone_delivery      = false
