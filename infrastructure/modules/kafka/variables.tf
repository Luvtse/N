# ============================================================================
# Kafka Module - Variables
# ============================================================================

variable "environment" {
  description = "Environment name"
  type        = string
}

variable "region" {
  description = "AWS region"
  type        = string
}

variable "cluster_name" {
  description = "MSK cluster name"
  type        = string
}

variable "kafka_version" {
  description = "Apache Kafka version"
  type        = string
  default     = "3.5.1"
}

variable "number_of_broker_nodes" {
  description = "Number of broker nodes"
  type        = number
  default     = 3
  
  validation {
    condition     = var.number_of_broker_nodes >= 3 && var.number_of_broker_nodes % 2 == 1
    error_message = "Number of broker nodes must be at least 3 and odd."
  }
}

variable "instance_type" {
  description = "MSK broker instance type"
  type        = string
  default     = "kafka.m5.large"
}

variable "ebs_volume_size" {
  description = "EBS volume size per broker in GB"
  type        = number
  default     = 1000
}

variable "subnet_ids" {
  description = "Subnet IDs for broker nodes"
  type        = list(string)
}

variable "vpc_id" {
  description = "VPC ID"
  type        = string
}

variable "allowed_security_group_ids" {
  description = "Security group IDs allowed to access Kafka"
  type        = list(string)
  default     = []
}

variable "allowed_cidr_blocks" {
  description = "CIDR blocks allowed to access Kafka"
  type        = list(string)
  default     = []
}

variable "encryption_in_transit_client_broker" {
  description = "Encryption in transit between client and broker"
  type        = string
  default     = "TLS"
  
  validation {
    condition     = contains(["TLS", "TLS_PLAINTEXT", "PLAINTEXT"], var.encryption_in_transit_client_broker)
    error_message = "Must be TLS, TLS_PLAINTEXT, or PLAINTEXT."
  }
}

variable "encryption_in_transit_in_cluster" {
  description = "Encryption in transit within cluster"
  type        = bool
  default     = true
}

variable "enable_cloudwatch_logs" {
  description = "Enable CloudWatch logging"
  type        = bool
  default     = true
}

variable "log_retention_days" {
  description = "CloudWatch log retention in days"
  type        = number
  default     = 30
}

variable "monitoring_level" {
  description = "Monitoring level (DEFAULT, PER_BROKER, PER_TOPIC_PER_BROKER, PER_TOPIC_PER_PARTITION)"
  type        = string
  default     = "PER_TOPIC_PER_BROKER"
}

variable "enable_open_monitoring" {
  description = "Enable open monitoring with Prometheus"
  type        = bool
  default     = true
}

variable "auto_scaling_enabled" {
  description = "Enable storage auto-scaling"
  type        = bool
  default     = true
}

variable "auto_scaling_max_storage" {
  description = "Maximum storage for auto-scaling in GB"
  type        = number
  default     = 2000
}

variable "server_properties" {
  description = "Kafka server properties"
  type        = string
  default     = <<-PROPERTIES
    auto.create.topics.enable = false
    delete.topic.enable = true
    log.retention.hours = 168
    log.segment.bytes = 1073741824
    num.io.threads = 8
    num.network.threads = 5
    num.partitions = 6
    num.replica.fetchers = 2
    replica.lag.time.max.ms = 30000
    socket.receive.buffer.bytes = 102400
    socket.request.max.bytes = 104857600
    socket.send.buffer.bytes = 102400
    unclean.leader.election.enable = false
    zookeeper.session.timeout.ms = 18000
  PROPERTIES
}

variable "tags" {
  description = "Additional tags"
  type        = map(string)
  default     = {}
}