variable "region" {
  type = string
}

# PostgreSQL (Citus for sharding)
resource "aws_db_instance" "nidaw_postgres" {
  identifier     = "nidaw-postgres-${var.region}"
  engine         = "postgres"
  engine_version = "15.4"
  instance_class = "db.r5.xlarge"
  
  allocated_storage     = 1000
  max_allocated_storage = 5000
  storage_type          = "io1"
  iops                  = 10000
  
  db_name  = "nidaw"
  username = "nidaw_admin"
  password = var.db_password
  
  multi_az            = true
  storage_encrypted   = true
  deletion_protection = true
  
  backup_retention_period = 30
  backup_window           = "03:00-04:00"
  
  tags = {
    Environment = "production"
    Service     = "nidaw"
  }
}

# TimescaleDB for time-series
resource "aws_db_instance" "nidaw_timescale" {
  identifier     = "nidaw-timescale-${var.region}"
  engine         = "postgres"
  engine_version = "15.4"
  instance_class = "db.r5.large"
  
  allocated_storage = 500
  storage_type      = "io1"
  iops              = 5000
  
  db_name  = "nidaw_timeseries"
  username = "timescale_admin"
  password = var.db_password
  
  multi_az          = true
  storage_encrypted = true
}

# Redis for caching
resource "aws_elasticache_cluster" "nidaw_redis" {
  cluster_id         = "nidaw-redis-${var.region}"
  engine             = "redis"
  node_type          = "cache.r6g.large"
  num_cache_nodes    = 3
  parameter_group_name = "default.redis7"
  port               = 6379
}