# ============================================================================
# Route53 Failover Configuration
# Advanced DNS routing with latency-based and geo-based policies
# ============================================================================

# ============================================================================
# LATENCY-BASED ROUTING (for global users)
# ============================================================================

resource "aws_route53_record" "api_latency" {
  for_each = { for r in var.regions : r.name => r }
  
  zone_id = var.hosted_zone_id
  name    = "api.${var.domain}"
  type    = "A"
  
  alias {
    name                   = each.value.alb_dns_name
    zone_id                = each.value.alb_zone_id
    evaluate_target_health = true
  }
  
  set_identifier = "latency-${each.key}"
  
  latency_routing_policy {
    region = each.value.aws_region
  }
  
  health_check_id = each.value.primary ? aws_route53_health_check.primary.id : aws_route53_health_check.secondary[each.key].id
}

# ============================================================================
# GEO-BASED ROUTING (for compliance/data residency)
# ============================================================================

resource "aws_route53_record" "api_geo_eu" {
  zone_id = var.hosted_zone_id
  name    = "api.${var.domain}"
  type    = "A"
  
  alias {
    name                   = local.secondary_regions[0].alb_dns_name
    zone_id                = local.secondary_regions[0].alb_zone_id
    evaluate_target_health = true
  }
  
  set_identifier = "geo-eu"
  
  geolocation_routing_policy {
    continent = "EU"
  }
  
  health_check_id = aws_route53_health_check.secondary[local.secondary_regions[0].name].id
}

resource "aws_route53_record" "api_geo_na" {
  zone_id = var.hosted_zone_id
  name    = "api.${var.domain}"
  type    = "A"
  
  alias {
    name                   = local.primary_region.alb_dns_name
    zone_id                = local.primary_region.alb_zone_id
    evaluate_target_health = true
  }
  
  set_identifier = "geo-na"
  
  geolocation_routing_policy {
    continent = "NA"
  }
  
  health_check_id = aws_route53_health_check.primary.id
}

# Default routing (fallback)
resource "aws_route53_record" "api_geo_default" {
  zone_id = var.hosted_zone_id
  name    = "api.${var.domain}"
  type    = "A"
  
  alias {
    name                   = local.primary_region.alb_dns_name
    zone_id                = local.primary_region.alb_zone_id
    evaluate_target_health = true
  }
  
  set_identifier = "geo-default"
  
  geolocation_routing_policy {
    country = "*"
  }
  
  health_check_id = aws_route53_health_check.primary.id
}

# ============================================================================
# WEIGHTED ROUTING (for gradual failover)
# ============================================================================

resource "aws_route53_record" "api_weighted_primary" {
  count = var.failover_enabled ? 1 : 0
  
  zone_id = var.hosted_zone_id
  name    = "api.${var.domain}"
  type    = "A"
  
  alias {
    name                   = local.primary_region.alb_dns_name
    zone_id                = local.primary_region.alb_zone_id
    evaluate_target_health = true
  }
  
  set_identifier = "weighted-primary"
  
  weighted_routing_policy {
    weight = 90
  }
  
  health_check_id = aws_route53_health_check.primary.id
}

resource "aws_route53_record" "api_weighted_secondary" {
  for_each = var.failover_enabled ? { for r in local.secondary_regions : r.name => r } : {}
  
  zone_id = var.hosted_zone_id
  name    = "api.${var.domain}"
  type    = "A"
  
  alias {
    name                   = each.value.alb_dns_name
    zone_id                = each.value.alb_zone_id
    evaluate_target_health = true
  }
  
  set_identifier = "weighted-${each.key}"
  
  weighted_routing_policy {
    weight = 10
  }
  
  health_check_id = aws_route53_health_check.secondary[each.key].id
}

# ============================================================================
# FAILOVER AUTOMATION (Lambda for automatic DNS updates)
# ============================================================================

resource "aws_lambda_function" "failover_manager" {
  filename         = "${path.module}/lambda/failover_manager.zip"
  function_name    = "${var.project_name}-failover-manager-${var.environment}"
  role             = aws_iam_role.failover_manager.arn
  handler          = "index.handler"
  runtime          = "python3.11"
  timeout          = 300
  memory_size      = 256
  
  environment {
    variables = {
      HOSTED_ZONE_ID    = var.hosted_zone_id
      DOMAIN            = var.domain
      PRIMARY_REGION    = local.primary_region.aws_region
      PRIMARY_ALB_DNS   = local.primary_region.alb_dns_name
      PRIMARY_ALB_ZONE  = local.primary_region.alb_zone_id
      SNS_TOPIC_ARN     = var.sns_topic_arns[0]
    }
  }
  
  tags = {
    Name        = "${var.project_name}-failover-manager"
    Environment = var.environment
  }
}

resource "aws_cloudwatch_event_rule" "health_check_failure" {
  name        = "${var.project_name}-health-check-failure"
  description = "Trigger on Route53 health check state change"
  
  event_pattern = jsonencode({
    source      = ["aws.route53"]
    detail-type = ["Route 53 Health Check State Change"]
    detail = {
      healthCheckId = [aws_route53_health_check.primary.id]
      state         = ["UNHEALTHY"]
    }
  })
}

resource "aws_cloudwatch_event_target" "failover_lambda" {
  rule      = aws_cloudwatch_event_rule.health_check_failure.name
  target_id = "FailoverLambda"
  arn       = aws_lambda_function.failover_manager.arn
}

resource "aws_lambda_permission" "cloudwatch" {
  statement_id  = "AllowExecutionFromCloudWatch"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.failover_manager.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.health_check_failure.arn
}

# ============================================================================
# IAM ROLE FOR FAILOVER LAMBDA
# ============================================================================

resource "aws_iam_role" "failover_manager" {
  name = "${var.project_name}-failover-manager-${var.environment}"
  
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "lambda.amazonaws.com"
      }
    }]
  })
}

resource "aws_iam_role_policy" "failover_manager" {
  name = "${var.project_name}-failover-manager-policy"
  role = aws_iam_role.failover_manager.id
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "route53:ChangeResourceRecordSets",
          "route53:GetHealthCheck",
          "route53:ListResourceRecordSets"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "sns:Publish"
        ]
        Resource = var.sns_topic_arns
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:*:*:*"
      }
    ]
  })
}