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
    Project     = "fullstack"
  }
  app_name = "fullstack"
}

# EKS Cluster
resource "aws_eks_cluster" "main" {
  name     = "${local.app_name}-cluster"
  role_arn = aws_iam_role.eks_cluster.arn
  version  = "1.28"

  vpc_config {
    subnet_ids              = aws_subnet.private[*].id
    endpoint_private_access = true
    endpoint_public_access  = true
    security_group_ids      = [aws_security_group.eks.id]
  }

  enabled_cluster_log_types = ["api", "audit", "authenticator"]

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-cluster"
  })
}

# Aurora PostgreSQL Cluster
resource "aws_rds_cluster" "main" {
  cluster_identifier = "${local.app_name}-cluster"
  engine            = "aurora-postgresql"
  engine_version    = "15.4"
  engine_mode       = "provisioned"

  database_name   = local.app_name
  master_username = "admin"

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  storage_encrypted = true
  kms_key_id        = aws_kms_key.main.arn

  backup_retention_period      = 7
  preferred_backup_window      = "03:00-04:00"
  preferred_maintenance_window = "sun:04:00-sun:05:00"

  deletion_protection       = true
  skip_final_snapshot       = false
  final_snapshot_identifier = "${local.app_name}-cluster-final"

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-database"
  })
}

# ElastiCache Redis
resource "aws_elasticache_cluster" "redis" {
  cluster_id           = "${local.app_name}-redis"
  engine               = "redis"
  engine_version       = "7.0"
  node_type            = "cache.t3.medium"
  num_cache_nodes      = 1
  port                 = 6379
  parameter_group_name = "default.redis7"

  subnet_group_name  = aws_elasticache_subnet_group.main.name
  security_group_ids = [aws_security_group.cache.id]

  snapshot_retention_limit = 5
  snapshot_window          = "05:00-06:00"
  maintenance_window       = "sun:06:00-sun:07:00"

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-redis"
  })
}

# SQS Queue
resource "aws_sqs_queue" "main" {
  name = "${local.app_name}-queue"

  delay_seconds              = 0
  max_message_size           = 262144
  message_retention_seconds  = 345600
  receive_wait_time_seconds  = 10
  visibility_timeout_seconds = 30

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-queue"
  })
}

resource "aws_sqs_queue" "dlq" {
  name = "${local.app_name}-dlq"

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-dlq"
  })
}

# SNS Topic
resource "aws_sns_topic" "notifications" {
  name         = "${local.app_name}-notifications"
  display_name = "Fullstack Notifications"

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-notifications"
  })
}

# S3 Bucket for Data
resource "aws_s3_bucket" "data" {
  bucket = "${local.app_name}-data-prod"

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-data"
  })
}

# CloudFront Distribution
resource "aws_cloudfront_distribution" "cdn" {
  enabled             = true
  is_ipv6_enabled     = true
  comment             = "Fullstack CDN"
  default_root_object = "index.html"
  price_class         = "PriceClass_100"
  http_version        = "http2and3"

  aliases = ["cdn.fullstack.example.com"]

  origin {
    domain_name              = aws_s3_bucket.data.bucket_regional_domain_name
    origin_id                = "S3-${local.app_name}-data"
    origin_access_control_id = aws_cloudfront_origin_access_control.main.id
  }

  default_cache_behavior {
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]
    target_origin_id       = "S3-${local.app_name}-data"
    viewer_protocol_policy = "redirect-to-https"
    compress               = true
    cache_policy_id        = "658327ea-f89d-4fab-a63d-7e88639e58f6"
  }

  viewer_certificate {
    acm_certificate_arn      = aws_acm_certificate.cdn.arn
    ssl_support_method       = "sni-only"
    minimum_protocol_version = "TLSv1.2_2021"
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-cdn"
  })
}

# Application Load Balancer
resource "aws_lb" "api" {
  name               = "${local.app_name}-api-alb"
  load_balancer_type = "application"
  internal           = false

  subnets         = aws_subnet.public[*].id
  security_groups = [aws_security_group.alb.id]

  enable_deletion_protection = true
  enable_http2               = true

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-api-alb"
  })
}

output "eks_cluster_endpoint" {
  description = "EKS cluster endpoint"
  value       = aws_eks_cluster.main.endpoint
}

output "rds_cluster_endpoint" {
  description = "Aurora cluster endpoint"
  value       = aws_rds_cluster.main.endpoint
}

output "elasticache_endpoint" {
  description = "ElastiCache endpoint"
  value       = aws_elasticache_cluster.redis.cache_nodes[0].address
}
