# Sample AWS infrastructure for testing the architecture diagram

resource "aws_vpc" "main" {
  cidr_block = "10.0.0.0/16"
  tags = {
    Name = "main-vpc"
  }
}

resource "aws_lb" "web" {
  name               = "web-alb"
  internal           = false
  load_balancer_type = "application"

  tags = {
    Name = "web-load-balancer"
  }
}

resource "aws_ecs_service" "api" {
  name            = "api-service"
  cluster         = "main-cluster"
  task_definition = "api-task"
  desired_count   = 2

  tags = {
    Name = "api-service"
  }
}

resource "aws_lambda_function" "processor" {
  function_name = "data-processor"
  runtime       = "nodejs18.x"
  handler       = "index.handler"
  role          = aws_iam_role.lambda.arn

  tags = {
    Name = "data-processor"
  }
}

resource "aws_db_instance" "postgres" {
  identifier     = "main-db"
  engine         = "postgres"
  engine_version = "15.4"
  instance_class = "db.t3.medium"

  tags = {
    Name = "main-database"
  }
}

resource "aws_dynamodb_table" "sessions" {
  name         = "user-sessions"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "session_id"

  attribute {
    name = "session_id"
    type = "S"
  }

  tags = {
    Name = "sessions-table"
  }
}

resource "aws_s3_bucket" "assets" {
  bucket = "app-assets-bucket"

  tags = {
    Name = "assets-storage"
  }
}

resource "aws_sqs_queue" "tasks" {
  name = "background-tasks"

  tags = {
    Name = "task-queue"
  }
}

resource "aws_elasticache_cluster" "redis" {
  cluster_id           = "app-cache"
  engine               = "redis"
  node_type            = "cache.t3.micro"
  num_cache_nodes      = 1

  tags = {
    Name = "redis-cache"
  }
}

resource "aws_cognito_user_pool" "users" {
  name = "app-users"

  tags = {
    Name = "user-pool"
  }
}

resource "aws_iam_role" "lambda" {
  name = "lambda-execution-role"

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

  tags = {
    Name = "lambda-role"
  }
}
