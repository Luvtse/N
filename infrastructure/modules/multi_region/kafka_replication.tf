# ============================================================================
# Kafka Cross-Region Replication
# Uses MirrorMaker 2 for real-time topic replication
# ============================================================================

# ============================================================================
# MIRRORMAKER 2 CLUSTER (Secondary Region)
# ============================================================================

resource "aws_msk_cluster" "mirror_maker" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  cluster_name           = "${var.project_name}-mirror-${each.key}"
  kafka_version          = var.kafka_version
  number_of_broker_nodes = 3
  
  broker_node_group_info {
    instance_type   = "kafka.m5.large"
    
    client_subnets  = each.value.subnet_ids
    security_groups = [aws_security_group.mirror_maker[each.key].id]
    
    storage_info {
      ebs_storage_info {
        volume_size = 500
      }
    }
  }
  
  encryption_info {
    encryption_in_transit {
      client_broker = "TLS"
      in_cluster    = true
    }
  }
  
  logging_info {
    broker_logs {
      cloudwatch {
        enabled       = true
        log_group     = aws_cloudwatch_log_group.mirror_maker[each.key].name
      }
    }
  }
  
  tags = {
    Name        = "${var.project_name}-mirror-${each.key}"
    Environment = var.environment
    Role        = "mirror-maker"
  }
}

# ============================================================================
# SECURITY GROUP FOR MIRRORMAKER
# ============================================================================

resource "aws_security_group" "mirror_maker" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  name_prefix = "${var.project_name}-mirror-${each.key}-"
  vpc_id      = each.value.vpc_id
  
  ingress {
    from_port   = 9092
    to_port     = 9094
    protocol    = "tcp"
    cidr_blocks = [each.value.vpc_cidr]
    description = "Kafka broker"
  }
  
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  
  tags = {
    Name = "${var.project_name}-mirror-${each.key}"
  }
}

# ============================================================================
# CLOUDWATCH LOG GROUP
# ============================================================================

resource "aws_cloudwatch_log_group" "mirror_maker" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  name              = "/aws/msk/${var.project_name}-mirror-${each.key}"
  retention_in_days = 30
  
  tags = {
    Name        = "${var.project_name}-mirror-${each.key}"
    Environment = var.environment
  }
}

# ============================================================================
# MIRRORMAKER 2 CONFIGURATION
# ============================================================================

resource "aws_msk_configuration" "mirror_maker_config" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  kafka_versions    = [var.kafka_version]
  name              = "${var.project_name}-mirror-config-${each.key}"
  description       = "MirrorMaker 2 configuration"
  
  server_properties = <<-PROPERTIES
    # MirrorMaker 2 Settings
    mirrors = primary-cluster
    primary-cluster.bootstrap.servers = ${local.primary_region.msk_bootstrap_brokers}
    primary-cluster.security.protocol = SSL
    primary-cluster.ssl.truststore.location = /tmp/kafka.client.truststore.jks
    
    # Topic replication
    primary-cluster.topics = .*
    primary-cluster.groups = .*
    
    # Performance tuning
    primary-cluster.tasks.max = 10
    primary-cluster.emit.checkpoints.interval.seconds = 60
    primary-cluster.sync.topic.configs.interval.seconds = 30
    primary-cluster.refresh.topics.interval.seconds = 30
    primary-cluster.refresh.groups.interval.seconds = 30
    
    # Replication settings
    primary-cluster.replication.policy.class = org.apache.kafka.connect.mirror.DefaultReplicationPolicy
    primary-cluster.replication.factor = 3
    primary-cluster.checkpoints.topic.replication.factor = 3
    primary-cluster.heartbeats.topic.replication.factor = 3
    primary-cluster.offset-syncs.topic.replication.factor = 3
    
    # Consumer settings
    primary-cluster.consumer.auto.offset.reset = earliest
  PROPERTIES
}

# ============================================================================
# ECS SERVICE FOR MIRRORMAKER 2
# ============================================================================

resource "aws_ecs_cluster" "mirror_maker" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  name = "${var.project_name}-mirror-${each.key}"
  
  setting {
    name  = "containerInsights"
    value = "enabled"
  }
  
  tags = {
    Name        = "${var.project_name}-mirror-${each.key}"
    Environment = var.environment
  }
}

resource "aws_ecs_task_definition" "mirror_maker" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  family                   = "${var.project_name}-mirror-${each.key}"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "1024"
  memory                   = "2048"
  execution_role_arn       = aws_iam_role.mirror_maker_execution[each.key].arn
  task_role_arn            = aws_iam_role.mirror_maker_task[each.key].arn
  
  container_definitions = jsonencode([
    {
      name  = "mirror-maker"
      image = "confluentinc/cp-kafka:7.5.0"
      
      command = [
        "/etc/confluent/docker/run"
      ]
      
      environment = [
        {
          name  = "KAFKA_HEAP_OPTS"
          value = "-Xmx1G -Xms1G"
        }
      ]
      
      secrets = [
        {
          name      = "KAFKA_SSL_KEYSTORE_PASSWORD"
          valueFrom = var.ssl_keystore_secret_arn
        },
        {
          name      = "KAFKA_SSL_TRUSTSTORE_PASSWORD"
          valueFrom = var.ssl_truststore_secret_arn
        }
      ]
      
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.mirror_maker[each.key].name
          "awslogs-region"        = each.value.aws_region
          "awslogs-stream-prefix" = "mirror-maker"
        }
      }
      
      essential = true
    }
  ])
  
  tags = {
    Name        = "${var.project_name}-mirror-${each.key}"
    Environment = var.environment
  }
}

resource "aws_ecs_service" "mirror_maker" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  name            = "${var.project_name}-mirror-${each.key}"
  cluster         = aws_ecs_cluster.mirror_maker[each.key].id
  task_definition = aws_ecs_task_definition.mirror_maker[each.key].arn
  desired_count   = 2
  launch_type     = "FARGATE"
  
  network_configuration {
    subnets          = each.value.subnet_ids
    security_groups  = [aws_security_group.mirror_maker[each.key].id]
    assign_public_ip = false
  }
  
  deployment_circuit_breaker {
    enable   = true
    rollback = true
  }
  
  tags = {
    Name        = "${var.project_name}-mirror-${each.key}"
    Environment = var.environment
  }
}

# ============================================================================
# IAM ROLES FOR MIRRORMAKER
# ============================================================================

resource "aws_iam_role" "mirror_maker_execution" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  name = "${var.project_name}-mirror-execution-${each.key}"
  
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ecs-tasks.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "mirror_maker_execution" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  role       = aws_iam_role.mirror_maker_execution[each.key].name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role" "mirror_maker_task" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  name = "${var.project_name}-mirror-task-${each.key}"
  
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ecs-tasks.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy" "mirror_maker_task" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  name = "${var.project_name}-mirror-task-policy-${each.key}"
  role = aws_iam_role.mirror_maker_task[each.key].id
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue"
        ]
        Resource = [
          var.ssl_keystore_secret_arn,
          var.ssl_truststore_secret_arn
        ]
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "${aws_cloudwatch_log_group.mirror_maker[each.key].arn}:*"
      }
    ]
  })
}

# ============================================================================
# MONITORING & ALERTS
# ============================================================================

resource "aws_cloudwatch_metric_alarm" "mirror_maker_lag" {
  for_each = { for r in local.secondary_regions : r.name => r }
  
  provider = aws.secondary
  
  alarm_name          = "${var.project_name}-mirror-lag-${each.key}"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "ReplicationLatencyMs"
  namespace           = "AWS/Kafka"
  period              = 60
  statistic           = "Average"
  threshold           = 60000 # 1 minute lag
  alarm_description   = "MirrorMaker replication lag is too high"
  
  dimensions = {
    "Cluster Name" = aws_msk_cluster.mirror_maker[each.key].cluster_name
  }
  
  alarm_actions = var.sns_topic_arns
  ok_actions    = var.sns_topic_arns
  
  tags = {
    Name        = "${var.project_name}-mirror-lag-${each.key}"
    Environment = var.environment
  }
}