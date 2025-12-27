terraform {
  required_version = ">= 1.6.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

variable "aws_region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "production"
}

locals {
  common_tags = {
    Environment = var.environment
    ManagedBy   = "terraform"
    Project     = "serverless-api"
  }
  app_name = "serverless-api"
}

# Lambda Function
resource "aws_lambda_function" "api" {
  function_name = "${local.app_name}-handler"
  role          = aws_iam_role.lambda.arn
  handler       = "index.handler"
  runtime       = "nodejs20.x"

  memory_size = 256
  timeout     = 30
  architectures = ["arm64"]

  environment {
    variables = {
      TABLE_NAME = aws_dynamodb_table.main.name
      NODE_ENV   = "production"
      LOG_LEVEL  = "info"
    }
  }

  tracing_config {
    mode = "Active"
  }

  dead_letter_config {
    target_arn = aws_sqs_queue.dlq.arn
  }

  logging_config {
    log_format = "JSON"
    application_log_level = "INFO"
    system_log_level = "INFO"
  }

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-handler"
  })
}

# API Gateway HTTP API
resource "aws_apigatewayv2_api" "main" {
  name          = local.app_name
  protocol_type = "HTTP"
  description   = "Serverless API Gateway"

  cors_configuration {
    allow_credentials = false
    allow_headers     = ["content-type", "authorization"]
    allow_methods     = ["GET", "POST", "PUT", "DELETE", "OPTIONS"]
    allow_origins     = ["https://app.example.com"]
    max_age           = 3600
  }

  tags = merge(local.common_tags, {
    Name = local.app_name
  })
}

resource "aws_apigatewayv2_stage" "prod" {
  api_id      = aws_apigatewayv2_api.main.id
  name        = "prod"
  auto_deploy = true

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api_gateway.arn
    format = jsonencode({
      requestId      = "$context.requestId"
      ip             = "$context.identity.sourceIp"
      requestTime    = "$context.requestTime"
      httpMethod     = "$context.httpMethod"
      routeKey       = "$context.routeKey"
      status         = "$context.status"
      responseLength = "$context.responseLength"
    })
  }

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-prod"
  })
}

# DynamoDB Table
resource "aws_dynamodb_table" "main" {
  name         = "${local.app_name}-data"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "pk"
  range_key    = "sk"

  attribute {
    name = "pk"
    type = "S"
  }

  attribute {
    name = "sk"
    type = "S"
  }

  attribute {
    name = "gsi1pk"
    type = "S"
  }

  attribute {
    name = "gsi1sk"
    type = "S"
  }

  global_secondary_index {
    name            = "gsi1"
    hash_key        = "gsi1pk"
    range_key       = "gsi1sk"
    projection_type = "ALL"
  }

  point_in_time_recovery {
    enabled = true
  }

  ttl {
    enabled        = true
    attribute_name = "ttl"
  }

  server_side_encryption {
    enabled = true
  }

  stream_enabled   = true
  stream_view_type = "NEW_AND_OLD_IMAGES"

  deletion_protection_enabled = true

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-data"
  })
}

# SQS Queues
resource "aws_sqs_queue" "events" {
  name = "${local.app_name}-events"

  delay_seconds              = 0
  max_message_size           = 262144
  message_retention_seconds  = 345600
  receive_wait_time_seconds  = 20
  visibility_timeout_seconds = 60

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-events"
  })
}

resource "aws_sqs_queue" "dlq" {
  name = "${local.app_name}-dlq"

  message_retention_seconds = 1209600  # 14 days

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-dlq"
  })
}

# S3 Bucket for uploads
resource "aws_s3_bucket" "uploads" {
  bucket = "${local.app_name}-uploads"

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-uploads"
  })
}

resource "aws_s3_bucket_versioning" "uploads" {
  bucket = aws_s3_bucket.uploads.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_lifecycle_configuration" "uploads" {
  bucket = aws_s3_bucket.uploads.id

  rule {
    id     = "expire-temp-uploads"
    status = "Enabled"

    filter {
      prefix = "temp/"
    }

    expiration {
      days = 1
    }
  }
}

# CloudWatch Events Rule
resource "aws_cloudwatch_event_rule" "scheduled" {
  name                = "${local.app_name}-scheduled-task"
  description         = "Scheduled task for serverless API"
  schedule_expression = "rate(1 hour)"

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-scheduled"
  })
}

# Cognito User Pool
resource "aws_cognito_user_pool" "main" {
  name = "${local.app_name}-users"

  mfa_configuration = "OPTIONAL"

  password_policy {
    minimum_length                   = 12
    require_lowercase                = true
    require_numbers                  = true
    require_symbols                  = true
    require_uppercase                = true
    temporary_password_validity_days = 7
  }

  auto_verified_attributes = ["email"]
  username_attributes      = ["email"]

  email_configuration {
    email_sending_account = "DEVELOPER"
    from_email_address    = "no-reply@example.com"
    source_arn            = aws_ses_email_identity.main.arn
  }

  account_recovery_setting {
    recovery_mechanism {
      name     = "verified_email"
      priority = 1
    }
  }

  deletion_protection = "ACTIVE"

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-users"
  })
}

# Secrets Manager
resource "aws_secretsmanager_secret" "api_keys" {
  name                    = "${local.app_name}/api-keys"
  description             = "API keys for serverless API"
  recovery_window_in_days = 30

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-keys"
  })
}

# CloudWatch Log Groups
resource "aws_cloudwatch_log_group" "api_gateway" {
  name              = "/aws/apigateway/${local.app_name}"
  retention_in_days = 30

  tags = local.common_tags
}

# Outputs
output "api_endpoint" {
  description = "API Gateway endpoint URL"
  value       = aws_apigatewayv2_stage.prod.invoke_url
}

output "lambda_function_arn" {
  description = "Lambda function ARN"
  value       = aws_lambda_function.api.arn
}

output "dynamodb_table_name" {
  description = "DynamoDB table name"
  value       = aws_dynamodb_table.main.name
}
