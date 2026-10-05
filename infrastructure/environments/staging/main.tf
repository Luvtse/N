# ============================================================================
# Staging Environment
# Production-like with smaller scale
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
      Environment = "staging"
      ManagedBy   = "terraform"
    }
  }
}

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# ============================================================================
# VPC
# ============================================================================

module "vpc" {
  source = "../../modules/vpc"
  
  environment = "staging"
  region      = var.region
  vpc_cidr    = "10.1.0.0/16"
  
  public_subnet_cidrs  = ["10.1.1.0/24", "10.1.2.0/24", "10.1.3.0/24"]
  private_subnet_cidrs = ["10.1.10.0/24", "10.1.11.0/24", "10.1.12.0/24"]
  
  enable_nat_gateway = true
  single_nat_gateway = true # Cost savings for staging
}

# ============================================================================
# KUBERNETES CLUSTER
# ============================================================================

module "k8s_cluster" {
  source = "../../modules/k8s-cluster"
  
  cluster_name         = "${var.project_name}-staging"
  environment          = "staging"
  region               = var.region
  vpc_id               = module.vpc.vpc_id
  subnet_ids           = module.vpc.private_subnet_ids
  kubernetes_version   = "1.28"
  
  node_groups = {
    general = {
      instance_types = ["m5.large"]
      min_size       = 2
      max_size       = 6
      desired_size   = 3
      disk_size      = 100
      labels = {
        "node-type" = "general"
      }
      taints = []
    }
  }
  
  enable_cluster_autoscaler = true
  enable_metrics_server     = true
  enable_logging            = true
}

# ============================================================================
# DATABASE
# ============================================================================

module "database" {
  source = "../../modules/database"
  
  environment         = "staging"
  region              = var.region
  vpc_id              = module.vpc.vpc_id
  subnet_ids          = module.vpc.private_subnet_ids
  
  db_name             = "nidaw_staging"
  instance_class      = "db.r6g.large"
  allocated_storage   = 100
  max_allocated_storage = 500
  
  multi_az            = true
  backup_retention_period = 14
  deletion_protection = true
  skip_final_snapshot = false
  
  enable_performance_insights = true
  enable_enhanced_monitoring  = true
  
  allowed_security_group_ids = [module.k8s_cluster.cluster_security_group_id]
}

# ============================================================================
# KAFKA
# ============================================================================

module "kafka" {
  source = "../../modules/kafka"
  
  environment            = "staging"
  region                 = var.region
  cluster_name           = "${var.project_name}-staging"
  kafka_version          = "3.5.1"
  number_of_broker_nodes = 3
  instance_type          = "kafka.m5.large"
  ebs_volume_size        = 500
  
  subnet_ids = module.vpc.private_subnet_ids
  vpc_id     = module.vpc.vpc_id
  
  allowed_security_group_ids = [module.k8s_cluster.cluster_security_group_id]
  
  enable_cloudwatch_logs = true
  log_retention_days     = 14
  auto_scaling_enabled   = true
  auto_scaling_max_storage = 1000
}

# ============================================================================
# REDIS
# ============================================================================

module "redis" {
  source = "../../modules/redis"
  
  environment    = "staging"
  region         = var.region
  cluster_name   = "${var.project_name}-staging"
  node_type      = "cache.r6g.large"
  num_cache_nodes = 2
  
  subnet_ids = module.vpc.private_subnet_ids
  vpc_id     = module.vpc.vpc_id
  
  allowed_security_group_ids = [module.k8s_cluster.cluster_security_group_id]
  
  automatic_failover_enabled = true
  multi_az_enabled           = true
}

# ============================================================================
# SECURITY
# ============================================================================

module "security" {
  source = "../../modules/security"
  
  environment = "staging"
  region      = var.region
  account_id  = data.aws_caller_identity.current.account_id
  
  waf_enabled         = true
  waf_rate_limit      = 1000
  guardduty_enabled   = true
  securityhub_enabled = true
  config_enabled      = true
  cloudtrail_enabled  = true
  
  cloudtrail_log_retention_days = 90
}

# ============================================================================
# SECRETS MANAGER
# ============================================================================

module "secrets" {
  source = "../../modules/secrets_manager"
  
  project_name = var.project_name
  environment  = "staging"
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
  
  enable_database_rotation = true
  rotation_days            = 30
  
  allowed_role_arns = [module.k8s_cluster.node_group_role_arns["general"]]
}

# ============================================================================
# MONITORING
# ============================================================================

module "monitoring" {
  source = "../../modules/monitoring"
  
  environment  = "staging"
  region       = var.region
  project_name = var.project_name
  
  enable_grafana    = true
  enable_prometheus = true
  enable_loki       = true
  enable_jaeger     = true
  
  grafana_instance_type = "t3.medium"
  retention_days        = 14
}