# ============================================================================
# Kubernetes Cluster Module - Variables
# ============================================================================

variable "cluster_name" {
  description = "Name of the EKS cluster"
  type        = string
  
  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{1,98}[a-z0-9]$", var.cluster_name))
    error_message = "Cluster name must start with a letter, contain only lowercase letters, numbers, and hyphens, and be 3-100 characters long."
  }
}

variable "environment" {
  description = "Environment name (dev, staging, production)"
  type        = string
  
  validation {
    condition     = contains(["dev", "staging", "production"], var.environment)
    error_message = "Environment must be one of: dev, staging, production."
  }
}

variable "region" {
  description = "AWS region for the cluster"
  type        = string
}

variable "vpc_id" {
  description = "VPC ID where the cluster will be deployed"
  type        = string
}

variable "subnet_ids" {
  description = "List of subnet IDs for the cluster"
  type        = list(string)
}

variable "kubernetes_version" {
  description = "Kubernetes version for the EKS cluster"
  type        = string
  default     = "1.28"
}

variable "node_groups" {
  description = "Configuration for EKS node groups"
  type = map(object({
    instance_types = list(string)
    min_size       = number
    max_size       = number
    desired_size   = number
    disk_size      = number
    labels         = map(string)
    taints = list(object({
      key    = string
      value  = string
      effect = string
    }))
  }))
  
  default = {
    general = {
      instance_types = ["m5.large"]
      min_size       = 2
      max_size       = 10
      desired_size   = 3
      disk_size      = 100
      labels = {
        "node-type" = "general"
      }
      taints = []
    }
  }
}

variable "enable_cluster_autoscaler" {
  description = "Enable cluster autoscaler"
  type        = bool
  default     = true
}

variable "enable_metrics_server" {
  description = "Enable metrics server for HPA"
  type        = bool
  default     = true
}

variable "enable_logging" {
  description = "Enable EKS control plane logging"
  type        = bool
  default     = true
}

variable "enabled_log_types" {
  description = "List of control plane log types to enable"
  type        = list(string)
  default     = ["api", "audit", "authenticator", "controllerManager", "scheduler"]
}

variable "tags" {
  description = "Additional tags for all resources"
  type        = map(string)
  default     = {}
}

variable "access_entries" {
  description = "List of IAM entities to grant access to the cluster"
  type = list(object({
    principal_arn = string
    type          = string
    policy_arns   = list(string)
  }))
  default = []
}