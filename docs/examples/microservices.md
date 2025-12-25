# Example: Microservices Architecture

This example demonstrates migrating a microservices-based application from AWS to a self-hosted Docker stack.

## Architecture

```
                         AWS Architecture
┌──────────────────────────────────────────────────────────────────────┐
│                                                                       │
│   ┌─────────────┐                                                    │
│   │ API Gateway │                                                    │
│   └──────┬──────┘                                                    │
│          │                                                           │
│   ┌──────▼──────┐                                                    │
│   │     ALB     │                                                    │
│   └──────┬──────┘                                                    │
│          │                                                           │
│   ┌──────┴─────────────────────────────────┐                        │
│   │                                         │                        │
│   ▼                    ▼                    ▼                        │
│ ┌────────────┐  ┌────────────┐  ┌────────────┐                      │
│ │  ECS Svc   │  │  ECS Svc   │  │  ECS Svc   │                      │
│ │   (API)    │  │  (Orders)  │  │  (Users)   │                      │
│ └─────┬──────┘  └─────┬──────┘  └─────┬──────┘                      │
│       │               │               │                              │
│       │               ▼               │                              │
│       │         ┌──────────┐          │                              │
│       │         │   SQS    │          │                              │
│       │         │ (Orders) │          │                              │
│       │         └────┬─────┘          │                              │
│       │              │                │                              │
│       ▼              ▼                ▼                              │
│ ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐             │
│ │   RDS    │  │   RDS    │  │   RDS    │  │ElastiCache│             │
│ │ (API DB) │  │(OrdersDB)│  │(UsersDB) │  │ (Redis)   │             │
│ └──────────┘  └──────────┘  └──────────┘  └──────────┘             │
│                                                                       │
└──────────────────────────────────────────────────────────────────────┘

                            ▼ CloudExit ▼

                     Self-Hosted Architecture
┌──────────────────────────────────────────────────────────────────────┐
│                                                                       │
│   ┌─────────────────────────────────────────────────────────────┐    │
│   │                        Traefik                               │    │
│   │            (API Gateway + Load Balancer + SSL)               │    │
│   └──────────────────────────┬──────────────────────────────────┘    │
│                              │                                        │
│   ┌──────────────────────────┴───────────────────────┐               │
│   │                          │                        │               │
│   ▼                          ▼                        ▼               │
│ ┌────────────┐  ┌────────────┐  ┌────────────┐                       │
│ │  API Svc   │  │ Orders Svc │  │ Users Svc  │                       │
│ │  (Docker)  │  │  (Docker)  │  │  (Docker)  │                       │
│ └─────┬──────┘  └─────┬──────┘  └─────┬──────┘                       │
│       │               │               │                               │
│       │               ▼               │                               │
│       │         ┌──────────┐          │                               │
│       │         │ RabbitMQ │          │                               │
│       │         └────┬─────┘          │                               │
│       │              │                │                               │
│       ▼              ▼                ▼                               │
│ ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐              │
│ │PostgreSQL│  │PostgreSQL│  │PostgreSQL│  │  Redis   │              │
│ │ (API DB) │  │(OrdersDB)│  │(UsersDB) │  │          │              │
│ └──────────┘  └──────────┘  └──────────┘  └──────────┘              │
│                                                                       │
└──────────────────────────────────────────────────────────────────────┘
```

## Terraform Configuration (Input)

### `main.tf`

```hcl
# ECS Cluster
resource "aws_ecs_cluster" "main" {
  name = "microservices-cluster"
}

# API Service
resource "aws_ecs_service" "api" {
  name            = "api-service"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.api.arn
  desired_count   = 2
  launch_type     = "FARGATE"

  network_configuration {
    subnets         = aws_subnet.private[*].id
    security_groups = [aws_security_group.ecs.id]
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.api.arn
    container_name   = "api"
    container_port   = 8080
  }
}

resource "aws_ecs_task_definition" "api" {
  family                   = "api"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = 256
  memory                   = 512

  container_definitions = jsonencode([{
    name  = "api"
    image = "myorg/api-service:latest"
    portMappings = [{
      containerPort = 8080
    }]
    environment = [
      { name = "DATABASE_URL", value = "postgres://..." },
      { name = "REDIS_URL", value = "redis://..." }
    ]
  }])
}

# Orders Service
resource "aws_ecs_service" "orders" {
  name            = "orders-service"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.orders.arn
  desired_count   = 2
  launch_type     = "FARGATE"

  network_configuration {
    subnets         = aws_subnet.private[*].id
    security_groups = [aws_security_group.ecs.id]
  }
}

resource "aws_ecs_task_definition" "orders" {
  family                   = "orders"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = 512
  memory                   = 1024

  container_definitions = jsonencode([{
    name  = "orders"
    image = "myorg/orders-service:latest"
    portMappings = [{
      containerPort = 8080
    }]
  }])
}

# Users Service
resource "aws_ecs_service" "users" {
  name            = "users-service"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.users.arn
  desired_count   = 2
  launch_type     = "FARGATE"

  network_configuration {
    subnets         = aws_subnet.private[*].id
    security_groups = [aws_security_group.ecs.id]
  }
}

resource "aws_ecs_task_definition" "users" {
  family                   = "users"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = 256
  memory                   = 512

  container_definitions = jsonencode([{
    name  = "users"
    image = "myorg/users-service:latest"
    portMappings = [{
      containerPort = 8080
    }]
  }])
}

# Databases
resource "aws_db_instance" "api_db" {
  identifier     = "api-db"
  engine         = "postgres"
  engine_version = "15"
  instance_class = "db.t3.micro"
  db_name        = "api"
  username       = "api"
  password       = "changeme"
}

resource "aws_db_instance" "orders_db" {
  identifier     = "orders-db"
  engine         = "postgres"
  engine_version = "15"
  instance_class = "db.t3.micro"
  db_name        = "orders"
  username       = "orders"
  password       = "changeme"
}

resource "aws_db_instance" "users_db" {
  identifier     = "users-db"
  engine         = "postgres"
  engine_version = "15"
  instance_class = "db.t3.micro"
  db_name        = "users"
  username       = "users"
  password       = "changeme"
}

# SQS Queue
resource "aws_sqs_queue" "orders" {
  name                       = "orders-queue"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 86400
}

# ElastiCache
resource "aws_elasticache_cluster" "redis" {
  cluster_id           = "microservices-cache"
  engine               = "redis"
  node_type            = "cache.t3.micro"
  num_cache_nodes      = 1
  parameter_group_name = "default.redis7"
}

# API Gateway
resource "aws_api_gateway_rest_api" "main" {
  name = "microservices-api"
}

# ALB
resource "aws_lb" "main" {
  name               = "microservices-alb"
  internal           = false
  load_balancer_type = "application"
  subnets            = aws_subnet.public[*].id
}
```

## CloudExit Migration

```bash
cloudexit migrate ./terraform \
  --output ./microservices-stack \
  --domain api.example.com \
  --include-monitoring
```

## Generated Output

### `docker-compose.yml`

```yaml
version: "3.8"

services:
  # Reverse Proxy & API Gateway
  traefik:
    image: traefik:v3.0
    restart: unless-stopped
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./traefik:/etc/traefik
    networks:
      - frontend
      - backend
    labels:
      - "traefik.enable=true"

  # API Service
  api-service:
    image: myorg/api-service:latest
    restart: unless-stopped
    deploy:
      replicas: 2
      resources:
        limits:
          cpus: "0.25"
          memory: 512M
    environment:
      DATABASE_URL: postgres://api:${API_DB_PASSWORD}@api-db:5432/api
      REDIS_URL: redis://redis:6379
      ORDERS_SERVICE_URL: http://orders-service:8080
      USERS_SERVICE_URL: http://users-service:8080
    depends_on:
      - api-db
      - redis
    networks:
      - frontend
      - backend
    labels:
      - "cloudexit.source=aws_ecs_service"
      - "traefik.enable=true"
      - "traefik.http.routers.api.rule=Host(`api.example.com`) && PathPrefix(`/api`)"
      - "traefik.http.routers.api.entrypoints=websecure"
      - "traefik.http.routers.api.tls.certresolver=letsencrypt"
      - "traefik.http.services.api.loadbalancer.server.port=8080"

  # Orders Service
  orders-service:
    image: myorg/orders-service:latest
    restart: unless-stopped
    deploy:
      replicas: 2
      resources:
        limits:
          cpus: "0.5"
          memory: 1024M
    environment:
      DATABASE_URL: postgres://orders:${ORDERS_DB_PASSWORD}@orders-db:5432/orders
      RABBITMQ_URL: amqp://admin:${RABBITMQ_PASSWORD}@rabbitmq:5672
      QUEUE_NAME: orders-queue
    depends_on:
      - orders-db
      - rabbitmq
    networks:
      - backend
    labels:
      - "cloudexit.source=aws_ecs_service"
      - "traefik.enable=true"
      - "traefik.http.routers.orders.rule=Host(`api.example.com`) && PathPrefix(`/orders`)"
      - "traefik.http.services.orders.loadbalancer.server.port=8080"

  # Users Service
  users-service:
    image: myorg/users-service:latest
    restart: unless-stopped
    deploy:
      replicas: 2
      resources:
        limits:
          cpus: "0.25"
          memory: 512M
    environment:
      DATABASE_URL: postgres://users:${USERS_DB_PASSWORD}@users-db:5432/users
      REDIS_URL: redis://redis:6379
    depends_on:
      - users-db
      - redis
    networks:
      - backend
    labels:
      - "cloudexit.source=aws_ecs_service"
      - "traefik.enable=true"
      - "traefik.http.routers.users.rule=Host(`api.example.com`) && PathPrefix(`/users`)"
      - "traefik.http.services.users.loadbalancer.server.port=8080"

  # Databases
  api-db:
    image: postgres:15-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: api
      POSTGRES_USER: api
      POSTGRES_PASSWORD: ${API_DB_PASSWORD}
    volumes:
      - ./data/api-db:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U api"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - backend

  orders-db:
    image: postgres:15-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: orders
      POSTGRES_USER: orders
      POSTGRES_PASSWORD: ${ORDERS_DB_PASSWORD}
    volumes:
      - ./data/orders-db:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U orders"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - backend

  users-db:
    image: postgres:15-alpine
    restart: unless-stopped
    environment:
      POSTGRES_DB: users
      POSTGRES_USER: users
      POSTGRES_PASSWORD: ${USERS_DB_PASSWORD}
    volumes:
      - ./data/users-db:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U users"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - backend

  # Message Queue (SQS replacement)
  rabbitmq:
    image: rabbitmq:3-management-alpine
    restart: unless-stopped
    ports:
      - "15672:15672"
    environment:
      RABBITMQ_DEFAULT_USER: admin
      RABBITMQ_DEFAULT_PASS: ${RABBITMQ_PASSWORD}
    volumes:
      - ./data/rabbitmq:/var/lib/rabbitmq
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "check_running"]
      interval: 30s
      timeout: 10s
      retries: 5
    networks:
      - backend
    labels:
      - "cloudexit.source=aws_sqs_queue"

  # Cache (ElastiCache replacement)
  redis:
    image: redis:7-alpine
    restart: unless-stopped
    command: redis-server --appendonly yes
    volumes:
      - ./data/redis:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
    networks:
      - backend
    labels:
      - "cloudexit.source=aws_elasticache_cluster"

networks:
  frontend:
    driver: bridge
  backend:
    driver: bridge
    internal: true
```

### Traefik Routing Configuration

`traefik/dynamic/microservices.yml`:

```yaml
http:
  routers:
    api-router:
      rule: "Host(`api.example.com`) && PathPrefix(`/api`)"
      service: api-service
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt
      middlewares:
        - api-ratelimit
        - api-headers

    orders-router:
      rule: "Host(`api.example.com`) && PathPrefix(`/orders`)"
      service: orders-service
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt

    users-router:
      rule: "Host(`api.example.com`) && PathPrefix(`/users`)"
      service: users-service
      entryPoints:
        - websecure
      tls:
        certResolver: letsencrypt

  middlewares:
    api-ratelimit:
      rateLimit:
        average: 100
        burst: 50

    api-headers:
      headers:
        customRequestHeaders:
          X-Forwarded-Proto: "https"
        accessControlAllowMethods:
          - GET
          - POST
          - PUT
          - DELETE
        accessControlAllowOriginList:
          - "https://app.example.com"
```

## Deployment

### 1. Configure Environment

```bash
cd microservices-stack
cp .env.example .env

# Generate secure passwords
API_DB_PASSWORD=$(openssl rand -base64 24)
ORDERS_DB_PASSWORD=$(openssl rand -base64 24)
USERS_DB_PASSWORD=$(openssl rand -base64 24)
RABBITMQ_PASSWORD=$(openssl rand -base64 24)

cat >> .env << EOF
API_DB_PASSWORD=$API_DB_PASSWORD
ORDERS_DB_PASSWORD=$ORDERS_DB_PASSWORD
USERS_DB_PASSWORD=$USERS_DB_PASSWORD
RABBITMQ_PASSWORD=$RABBITMQ_PASSWORD
EOF
```

### 2. Start Infrastructure First

```bash
# Start databases and messaging first
docker compose up -d api-db orders-db users-db rabbitmq redis

# Wait for health checks
docker compose ps
```

### 3. Start Services

```bash
docker compose up -d
```

### 4. Verify Routing

```bash
# Test API endpoint
curl https://api.example.com/api/health

# Test Orders endpoint
curl https://api.example.com/orders/health

# Test Users endpoint
curl https://api.example.com/users/health
```

## Scaling

### Horizontal Scaling

```bash
# Scale orders service
docker compose up -d --scale orders-service=4

# Scale all services
docker compose up -d --scale api-service=3 --scale orders-service=4 --scale users-service=3
```

### Resource Limits

Adjust in `docker-compose.yml`:

```yaml
deploy:
  replicas: 4
  resources:
    limits:
      cpus: "1.0"
      memory: 2048M
    reservations:
      cpus: "0.5"
      memory: 1024M
```

## Monitoring

The generated stack includes Prometheus and Grafana. Access:

- Grafana: `http://grafana.example.com`
- Prometheus: `http://prometheus.example.com`

Pre-configured dashboards include:
- Service health
- Request latency
- Error rates
- Database connections
- Queue depth

## Service Discovery

Services communicate using Docker DNS:
- `api-service:8080`
- `orders-service:8080`
- `users-service:8080`

Update your service code to use these hostnames instead of AWS service discovery.
