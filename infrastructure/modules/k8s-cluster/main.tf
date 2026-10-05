variable "region" {
  description = "AWS region"
  type        = string
}

variable "cluster_name" {
  description = "EKS cluster name"
  type        = string
}

variable "node_count" {
  description = "Number of worker nodes"
  type        = number
  default     = 3
}

resource "aws_eks_cluster" "nidaw" {
  name     = var.cluster_name
  role_arn = aws_iam_role.cluster.arn

  vpc_config {
    subnet_ids = var.subnet_ids
  }
}

resource "aws_eks_node_group" "nidaw_nodes" {
  cluster_name    = aws_eks_cluster.nidaw.name
  node_group_name = "${var.cluster_name}-nodes"
  node_role_arn   = aws_iam_role.node.arn
  subnet_ids      = var.subnet_ids

  scaling_config {
    desired_size = var.node_count
    max_size     = var.node_count * 2
    min_size     = 1
  }

  instance_types = ["m5.large"]
}

output "cluster_endpoint" {
  value = aws_eks_cluster.nidaw.endpoint
}

output "cluster_name" {
  value = aws_eks_cluster.nidaw.name
}