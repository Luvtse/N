# ============================================================================
# Secrets Manager - Rotation Lambda Function
# Purpose: Automatically rotate database passwords
# ============================================================================

# ============================================================================
# LAMBDA FUNCTION
# ============================================================================

data "archive_file" "rotation_lambda" {
  type        = "zip"
  source_file = "${path.module}/lambda/rotation.py"
  output_path = "${path.module}/lambda/rotation.zip"
}

resource "aws_lambda_function" "rotation" {
  filename         = data.archive_file.rotation_lambda.output_path
  function_name    = "${var.project_name}-secret-rotation-${var.environment}"
  role             = aws_iam_role.rotation_lambda.arn
  handler          = "rotation.lambda_handler"
  runtime          = "python3.11"
  timeout          = 300
  memory_size      = 256
  
  environment {
    variables = {
      SECRETS_MANAGER_ENDPOINT = "https://secretsmanager.${data.aws_region.current.name}.amazonaws.com"
    }
  }
  
  vpc_config {
    subnet_ids         = var.lambda_subnet_ids
    security_group_ids = [aws_security_group.rotation_lambda.id]
  }
  
  tags = {
    Name        = "${var.project_name}-secret-rotation"
    Environment = var.environment
  }
}

data "aws_region" "current" {}

# ============================================================================
# SECURITY GROUP
# ============================================================================

resource "aws_security_group" "rotation_lambda" {
  name_prefix = "${var.project_name}-rotation-lambda-"
  vpc_id      = var.vpc_id
  
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
  
  tags = {
    Name = "${var.project_name}-rotation-lambda"
  }
}

# ============================================================================
# IAM ROLE
# ============================================================================

resource "aws_iam_role" "rotation_lambda" {
  name = "${var.project_name}-rotation-lambda-${var.environment}"
  
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

resource "aws_iam_role_policy" "rotation_lambda" {
  name = "${var.project_name}-rotation-lambda-policy"
  role = aws_iam_role.rotation_lambda.id
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:DescribeSecret",
          "secretsmanager:GetSecretValue",
          "secretsmanager:PutSecretValue",
          "secretsmanager:UpdateSecretVersionStage"
        ]
        Resource = aws_secretsmanager_secret.database.arn
      },
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetRandomPassword"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "kms:GenerateDataKey",
          "kms:Decrypt"
        ]
        Resource = aws_kms_key.secrets.arn
        Condition = {
          StringEquals = {
            "kms:ViaService" = "secretsmanager.${data.aws_region.current.name}.amazonaws.com"
          }
        }
      },
      {
        Effect = "Allow"
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ]
        Resource = "arn:aws:logs:*:*:*"
      },
      {
        Effect = "Allow"
        Action = [
          "ec2:CreateNetworkInterface",
          "ec2:DeleteNetworkInterface",
          "ec2:DescribeNetworkInterfaces",
          "ec2:DetachNetworkInterface"
        ]
        Resource = "*"
      }
    ]
  })
}

# ============================================================================
# PERMISSION FOR SECRETS MANAGER TO INVOKE LAMBDA
# ============================================================================

resource "aws_lambda_permission" "secrets_manager" {
  statement_id  = "AllowSecretsManagerInvocation"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.rotation.function_name
  principal     = "secretsmanager.amazonaws.com"
  source_arn    = aws_secretsmanager_secret.database.arn
}

# ============================================================================
# CLOUDWATCH LOG GROUP
# ============================================================================

resource "aws_cloudwatch_log_group" "rotation_lambda" {
  name              = "/aws/lambda/${aws_lambda_function.rotation.function_name}"
  retention_in_days = 30
  
  tags = {
    Name        = "${var.project_name}-rotation-lambda"
    Environment = var.environment
  }
}

# ============================================================================
# MONITORING ALARMS
# ============================================================================

resource "aws_cloudwatch_metric_alarm" "rotation_errors" {
  alarm_name          = "${var.project_name}-rotation-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Sum"
  threshold           = 0
  alarm_description   = "Secret rotation Lambda has errors"
  
  dimensions = {
    FunctionName = aws_lambda_function.rotation.function_name
  }
  
  alarm_actions = var.sns_topic_arns
  
  tags = {
    Name        = "${var.project_name}-rotation-errors"
    Environment = var.environment
  }
}

resource "aws_cloudwatch_metric_alarm" "rotation_duration" {
  alarm_name          = "${var.project_name}-rotation-duration"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Duration"
  namespace           = "AWS/Lambda"
  period              = 300
  statistic           = "Average"
  threshold           = 60000 # 60 seconds
  alarm_description   = "Secret rotation Lambda is taking too long"
  
  dimensions = {
    FunctionName = aws_lambda_function.rotation.function_name
  }
  
  alarm_actions = var.sns_topic_arns
  
  tags = {
    Name        = "${var.project_name}-rotation-duration"
    Environment = var.environment
  }
}