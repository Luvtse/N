# ============================================================================
# Development Environment
# Smaller resources, cost-optimized
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

provider "aws" {
  region = var.region
  
  default_tags {
    tags = {
      Project     = var.project_name
      Environment = "dev"
      ManagedBy   = "terraform"
    }
  }
}

# ============================================================================
# DATA SOURCES
# ============================================================================

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# ============================================================================
# VPC (Using existing module)
# ============================================================================

module "vpc" {
  source = "../../modules/vpc"
  
  environment = "dev"
  region      = var.region
  vpc_cidr    = "10.0.0.0/16"
  
  public_subnet_cidrs  = ["10.0.1.0/24", "10.0.2.0/24"]
  private_subnet_cidrs = ["10.0.10.0/24", "10.0.11.0/24"]
  
  enable_nat_gateway = false # Cost savings for dev
  single_nat_gateway = true
}

# ============================================================================
# KUBERNETES CLUSTER
# ============================================================================

module "k8s_cluster" {
  source = "../../modules/k8s-cluster"
  
  cluster_name         = "${var.project_name}-dev"
  environment          = "dev"
  region               = var.region
  vpc_id               = module.vpc.vpc_id
  subnet_ids           = module.vpc.private_subnet_ids
  kubernetes_version   = "1.28"
  
  node_groups = {
    general = {
      instance_types = ["t3.medium"]
      min_size       = 1
      max_size       = 3
      desired_size   = 2
      disk_size      = 50
      labels = {
        "node-type" = "general"
      }
      taints = []
    }
  }
  
  enable_cluster_autoscaler = false
  enable_logging            = false # Cost savings
}

# ============================================================================
# DATABASE
# ============================================================================

module "database" {
  source = "../../modules/database"
  
  environment         = "dev"
  region              = var.region
  vpc_id              = module.vpc.vpc_id
  subnet_ids          = module.vpc.private_subnet_ids
  
  db_name             = "nidaw_dev"
  instance_class      = "db.t3.medium"
  allocated_storage   = 20
  max_allocated_storage = 50
  
  multi_az            = false # Cost savings
  backup_retention_period = 7
  deletion_protection = false
  skip_final_snapshot = true
  
  enable_performance_insights = false
  enable_enhanced_monitoring  = false
  
  allowed_security_group_ids = [module.k8s_cluster.cluster_security_group_id]
}

# ============================================================================
# KAFKA
# ============================================================================

module "kafka" {
  source = "../../modules/kafka"
  
  environment            = "dev"
  region                 = var.region
  cluster_name           = "${var.project_name}-dev"
  kafka_version          = "3.5.1"
  number_of_broker_nodes = 3
  instance_type          = "kafka.t3.small"
  ebs_volume_size        = 100
  
  subnet_ids = module.vpc.private_subnet_ids
  vpc_id     = module.vpc.vpc_id
  
  allowed_security_group_ids = [module.k8s_cluster.cluster_security_group_id]
  
  enable_cloudwatch_logs = true
  log_retention_days     = 7
  auto_scaling_enabled   = false
}

# ============================================================================
# REDIS
# ============================================================================

module "redis" {
  source = "../../modules/redis"
  
  environment    = "dev"
  region         = var.region
  cluster_name   = "${var.project_name}-dev"
  node_type      = "cache.t3.medium"
  num_cache_nodes = 1
  
  subnet_ids = module.vpc.private_subnet_ids
  vpc_id     = module.vpc.vpc_id
  
  allowed_security_group_ids = [module.k8s_cluster.cluster_security_group_id]
  
  automatic_failover_enabled = false
  multi_az_enabled           = false
}

# ============================================================================
# SECURITY (Minimal for dev)
# ============================================================================

module "security" {
  source = "../../modules/security"
  
  environment = "dev"
  region      = var.region
  account_id  = data.aws_caller_identity.current.account_id
  
  waf_enabled          = false # Cost savings
  guardduty_enabled    = true
  securityhub_enabled  = false # Cost savings
  config_enabled       = false # Cost savings
  cloudtrail_enabled   = true
  
  cloudtrail_log_retention_days = 30
}

# ============================================================================
# SECRETS MANAGER
# ============================================================================

module "secrets" {
  source = "../../modules/secrets_manager"
  
  project_name = var.project_name
  environment  = "dev"
  account_id   = data.aws_caller_identity.current.account_id
  
  db_host     = module.database.endpoint
  db_username = module.database.master_username
  db_password = var.db_password
  
  jwt_secret = var.jwt_secret
  
  stripe_api_key         = var.stripe_api_key
  stripe_webhook_secret  = var.stripe_webhook_secret
  stripe_publishable_key = var.stripe_publishable_key
  
  kafka_bootstrap_servers = module.kafka.bootstrap_brokers
  kafka_schema_registry_url = module.kafka.schema_registry_url
  
  redis_url = module.redis.primary_endpoint
  
  google_maps_api_key = var.google_maps_api_key
  twilio_account_sid  = var.twilio_account_sid
  twilio_auth_token   = var.twilio_auth_token
  sendgrid_api_key    = var.sendgrid_api_key
  firebase_project_id = var.firebase_project_id
  
  enable_database_rotation = false # Dev doesn't need rotation
  
  allowed_role_arns = [module.k8s_cluster.node_group_role_arns["general"]]
}

# ============================================================================
# MONITORING (Lightweight)
# ============================================================================

module "monitoring" {
  source = "../../modules/monitoring"
  
  environment  = "dev"
  region       = var.region
  project_name = var.project_name
  
  enable_grafana   = true
  enable_prometheus = true
  enable_loki      = true
  enable_jaeger    = false # Cost savings
  
  grafana_instance_type = "t3.small"
  retention_days        = 7
}