# ============================================================================
# Production Environment
# High availability, maximum performance
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
      Environment = "production"
      ManagedBy   = "terraform"
      CostCenter  = "engineering"
    }
  }
}

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# ============================================================================
# VPC (Multi-AZ)
# ============================================================================

module "vpc" {
  source = "../../modules/vpc"
  
  environment = "production"
  region      = var.region
  vpc_cidr    = "10.2.0.0/16"
  
  public_subnet_cidrs  = ["10.2.1.0/24", "10.2.2.0/24", "10.2.3.0/24"]
  private_subnet_cidrs = ["10.2.10.0/24", "10.2.11.0/24", "10.2.12.0/24"]
  database_subnet_cidrs = ["10.2.20.0/24", "10.2.21.0/24", "10.2.22.0/24"]
  
  enable_nat_gateway = true
  single_nat_gateway = false # One per AZ for HA
}

# ============================================================================
# KUBERNETES CLUSTER (HA)
# ============================================================================

module "k8s_cluster" {
  source = "../../modules/k8s-cluster"
  
  cluster_name         = "${var.project_name}-prod"
  environment          = "production"
  region               = var.region
  vpc_id               = module.vpc.vpc_id
  subnet_ids           = module.vpc.private_subnet_ids
  kubernetes_version   = "1.28"
  
  node_groups = {
    general = {
      instance_types = ["m5.xlarge"]
      min_size       = 3
      max_size       = 20
      desired_size   = 5
      disk_size      = 200
      labels = {
        "node-type" = "general"
      }
      taints = []
    }
    compute = {
      instance_types = ["c5.2xlarge"]
      min_size       = 2
      max_size       = 10
      desired_size   = 3
      disk_size      = 200
      labels = {
        "node-type" = "compute"
      }
      taints = [
        {
          key    = "workload-type"
          value  = "compute"
          effect = "NO_SCHEDULE"
        }
      ]
    }
  }
  
  enable_cluster_autoscaler = true
  enable_metrics_server     = true
  enable_logging            = true
}

# ============================================================================
# DATABASE (HA with Read Replicas)
# ============================================================================

module "database" {
  source = "../../modules/database"
  
  environment         = "production"
  region              = var.region
  vpc_id              = module.vpc.vpc_id
  subnet_ids          = module.vpc.database_subnet_ids
  
  db_name             = "nidaw_prod"
  instance_class      = "db.r6g.2xlarge"
  allocated_storage   = 500
  max_allocated_storage = 2000
  storage_type        = "io2"
  iops                = 10000
  
  multi_az            = true
  backup_retention_period = 35
  deletion_protection = true
  skip_final_snapshot = false
  
  enable_performance_insights = true
  enable_enhanced_monitoring  = true
  monitoring_interval         = 15
  
  allowed_security_group_ids = [module.k8s_cluster.cluster_security_group_id]
}

# ============================================================================
# KAFKA (Production-grade)
# ============================================================================

module "kafka" {
  source = "../../modules/kafka"
  
  environment            = "production"
  region                 = var.region
  cluster_name           = "${var.project_name}-prod"
  kafka_version          = "3.5.1"
  number_of_broker_nodes = 5 # Odd number for quorum
  instance_type          = "kafka.m5.2xlarge"
  ebs_volume_size        = 2000
  
  subnet_ids = module.vpc.private_subnet_ids
  vpc_id     = module.vpc.vpc_id
  
  allowed_security_group_ids = [module.k8s_cluster.cluster_security_group_id]
  
  enable_cloudwatch_logs = true
  log_retention_days     = 30
  auto_scaling_enabled   = true
  auto_scaling_max_storage = 5000
  
  monitoring_level = "PER_TOPIC_PER_PARTITION"
  enable_open_monitoring = true
}

# ============================================================================
# REDIS (Cluster mode)
# ============================================================================

module "redis" {
  source = "../../modules/redis"
  
  environment    = "production"
  region         = var.region
  cluster_name   = "${var.project_name}-prod"
  node_type      = "cache.r6g.xlarge"
  num_cache_nodes = 6 # 3 shards x 2 replicas
  
  subnet_ids = module.vpc.private_subnet_ids
  vpc_id     = module.vpc.vpc_id
  
  allowed_security_group_ids = [module.k8s_cluster.cluster_security_group_id]
  
  automatic_failover_enabled = true
  multi_az_enabled           = true
  cluster_mode_enabled       = true
  num_shards                 = 3
  replicas_per_shard         = 1
}

# ============================================================================
# SECURITY (Full stack)
# ============================================================================

module "security" {
  source = "../../modules/security"
  
  environment = "production"
  region      = var.region
  account_id  = data.aws_caller_identity.current.account_id
  
  waf_enabled         = true
  waf_rate_limit      = 2000
  waf_blocked_countries = ["KP", "IR", "SY"]
  
  guardduty_enabled   = true
  guardduty_enable_s3_protection = true
  guardduty_enable_eks_protection = true
  
  securityhub_enabled = true
  config_enabled      = true
  cloudtrail_enabled  = true
  
  cloudtrail_is_multi_region      = true
  cloudtrail_log_retention_days   = 365
  
  audit_bucket_versioning   = true
  audit_bucket_object_lock  = true
  audit_bucket_retention_years = 7
}

# ============================================================================
# SECRETS MANAGER
# ============================================================================

module "secrets" {
  source = "../../modules/secrets_manager"
  
  project_name = var.project_name
  environment  = "production"
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
  
  recovery_window_days = 30
  
  allowed_role_arns = [module.k8s_cluster.node_group_role_arns["general"]]
}

# ============================================================================
# MULTI-REGION (Active-Passive)
# ============================================================================

module "multi_region" {
  source = "../../modules/multi_region"
  
  project_name   = var.project_name
  environment    = "production"
  domain         = var.domain
  hosted_zone_id = var.hosted_zone_id
  account_id     = data.aws_caller_identity.current.account_id
  
  regions = [
    {
      name              = "us-east-1"
      aws_region        = "us-east-1"
      primary           = true
      alb_dns_name      = module.alb.dns_name
      alb_zone_id       = module.alb.zone_id
      db_instance_class = "db.r6g.xlarge"
    },
    {
      name              = "eu-west-1"
      aws_region        = "eu-west-1"
      primary           = false
      alb_dns_name      = module.alb_eu.dns_name
      alb_zone_id       = module.alb_eu.zone_id
      db_instance_class = "db.r6g.large"
    }
  ]
  
  kms_key_arn            = module.security.kms_key_arn
  secondary_kms_key_arn  = module.security_eu.kms_key_arn
  
  audit_bucket_name            = module.security.audit_bucket_name
  primary_audit_bucket_arn     = module.security.audit_bucket_arn
  secondary_audit_bucket_arn   = module.security_eu.audit_bucket_arn
  
  sns_topic_arns = [module.monitoring.sns_topic_arn]
  
  failover_enabled = true
}

# ============================================================================
# MONITORING (Full stack)
# ============================================================================

module "monitoring" {
  source = "../../modules/monitoring"
  
  environment  = "production"
  region       = var.region
  project_name = var.project_name
  
  enable_grafana    = true
  enable_prometheus = true
  enable_loki       = true
  enable_jaeger     = true
  
  grafana_instance_type = "m5.large"
  retention_days        = 90
  
  alert_email = var.alert_email
}

# ============================================================================
# BACKUP & DISASTER RECOVERY
# ============================================================================

module "backup" {
  source = "../../modules/backup"
  
  environment  = "production"
  region       = var.region
  project_name = var.project_name
  
  # Database backups
  db_backup_frequency    = "daily"
  db_backup_retention    = 35
  db_pitr_enabled        = true
  
  # S3 backups
  s3_backup_frequency    = "hourly"
  s3_backup_retention    = 90
  s3_cross_region_copy   = true
  s3_dr_region           = "eu-west-1"
  
  # Kafka topic backups
  kafka_backup_enabled   = true
  kafka_backup_retention = 7
}