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
  description = "Environment name (e.g., production, staging)"
  type        = string
  default     = "production"
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.medium"
}

variable "db_instance_class" {
  description = "RDS instance class"
  type        = string
  default     = "db.t3.medium"
}

locals {
  common_tags = {
    Environment = var.environment
    ManagedBy   = "terraform"
    Project     = "simple-webapp"
  }

  app_name = "webapp"
}

resource "aws_instance" "web" {
  ami           = "ami-0c55b159cbfafe1f0"
  instance_type = var.instance_type

  subnet_id              = aws_subnet.public_a.id
  vpc_security_group_ids = [aws_security_group.web.id]

  monitoring = true

  root_block_device {
    volume_size = 20
    volume_type = "gp3"
    encrypted   = true
  }

  user_data = <<-EOF
              #!/bin/bash
              apt-get update
              apt-get install -y nginx
              EOF

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-server"
  })
}

resource "aws_db_instance" "postgres" {
  identifier     = "${local.app_name}-db"
  engine         = "postgres"
  engine_version = "15.4"
  instance_class = var.db_instance_class

  allocated_storage     = 100
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name  = "webapp"
  username = "dbadmin"
  port     = 5432

  multi_az               = true
  publicly_accessible    = false
  vpc_security_group_ids = [aws_security_group.database.id]
  db_subnet_group_name   = aws_db_subnet_group.main.name

  backup_retention_period = 7
  backup_window           = "03:00-04:00"
  maintenance_window      = "sun:04:00-sun:05:00"

  deletion_protection       = true
  skip_final_snapshot       = false
  final_snapshot_identifier = "${local.app_name}-db-final-snapshot"

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-database"
  })
}

resource "aws_s3_bucket" "assets" {
  bucket = "${local.app_name}-assets-prod"

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-assets"
  })
}

resource "aws_lb" "web" {
  name               = "${local.app_name}-alb"
  load_balancer_type = "application"
  internal           = false

  subnets         = [aws_subnet.public_a.id, aws_subnet.public_b.id]
  security_groups = [aws_security_group.alb.id]

  enable_deletion_protection = true
  enable_http2               = true

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-alb"
  })
}

resource "aws_security_group" "web" {
  name        = "${local.app_name}-web-sg"
  description = "Security group for web servers"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTP from anywhere"
  }

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTPS from anywhere"
  }

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["10.0.0.0/8"]
    description = "SSH from internal network"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-web-sg"
  })
}

resource "aws_security_group" "database" {
  name        = "${local.app_name}-db-sg"
  description = "Security group for database"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.web.id]
    description     = "PostgreSQL from web servers"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-db-sg"
  })
}

resource "aws_security_group" "alb" {
  name        = "${local.app_name}-alb-sg"
  description = "Security group for ALB"
  vpc_id      = aws_vpc.main.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTP from internet"
  }

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTPS from internet"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "Allow all outbound"
  }

  tags = merge(local.common_tags, {
    Name = "${local.app_name}-alb-sg"
  })
}

output "instance_public_ip" {
  description = "Public IP address of the web server"
  value       = aws_instance.web.public_ip
}

output "database_endpoint" {
  description = "Endpoint for the PostgreSQL database"
  value       = aws_db_instance.postgres.endpoint
}

output "s3_bucket_name" {
  description = "Name of the S3 bucket for assets"
  value       = aws_s3_bucket.assets.id
}

output "alb_dns_name" {
  description = "DNS name of the Application Load Balancer"
  value       = aws_lb.web.dns_name
}
