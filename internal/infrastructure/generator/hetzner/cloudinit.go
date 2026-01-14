// Package hetzner provides cloud-init generation for Hetzner Cloud.
package hetzner

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/domain/mapper"
)

// GenerateCloudInitTF generates the cloud-init Terraform configuration for Hetzner.
func GenerateCloudInitTF(categorized *CategorizedResults, config *generator.TargetConfig) string {
	var buf bytes.Buffer

	buf.WriteString("# Cloud-Init Configuration\n")
	buf.WriteString("# User data for server initialization\n\n")

	// Generate cloud-init for app servers
	buf.WriteString(generateAppCloudInit(categorized, config))

	// Generate cloud-init for database servers
	if len(categorized.Database) > 0 {
		buf.WriteString(generateDatabaseCloudInit(categorized, config))
	}

	return buf.String()
}

func generateAppCloudInit(categorized *CategorizedResults, config *generator.TargetConfig) string {
	var buf bytes.Buffer

	projectName := "homeport"
	if config.ProjectName != "" {
		projectName = SanitizeName(config.ProjectName)
	}

	// Build Docker Compose content
	composeContent := generateDockerComposeContent(categorized, config)

	buf.WriteString(`# App Server Cloud-Init
data "cloudinit_config" "app" {
  gzip          = true
  base64_encode = true

  part {
    content_type = "text/cloud-config"
    content      = <<-EOF
#cloud-config
package_update: true
package_upgrade: true

packages:
  - docker.io
  - docker-compose
  - htop
  - vim
  - curl
  - wget

write_files:
  - path: /opt/app/docker-compose.yml
    permissions: "0644"
    content: |
`)

	// Indent the compose content
	for _, line := range strings.Split(composeContent, "\n") {
		buf.WriteString("      " + line + "\n")
	}

	buf.WriteString(fmt.Sprintf(`
  - path: /opt/app/.env.example
    permissions: "0644"
    content: |
      # Environment variables for %s
      # Copy to .env and fill in values

      # Database
      DB_HOST=10.0.2.10
      DB_PORT=5432
      DB_NAME=app
      DB_USER=app
      DB_PASSWORD=changeme

      # Redis
      REDIS_HOST=10.0.2.10
      REDIS_PORT=6379

runcmd:
  - systemctl enable docker
  - systemctl start docker
  - usermod -aG docker root
  - cd /opt/app && docker-compose pull
  - cd /opt/app && docker-compose up -d
EOF
  }
}

`, projectName))

	return buf.String()
}

func generateDatabaseCloudInit(categorized *CategorizedResults, config *generator.TargetConfig) string {
	var buf bytes.Buffer

	// Determine which databases are needed
	hasPostgres := false
	hasMySQL := false
	hasRedis := false

	for _, result := range categorized.Database {
		if result == nil || result.DockerService == nil {
			continue
		}

		image := strings.ToLower(result.DockerService.Image)
		if strings.Contains(image, "postgres") {
			hasPostgres = true
		}
		if strings.Contains(image, "mysql") || strings.Contains(image, "mariadb") {
			hasMySQL = true
		}
		if strings.Contains(image, "redis") || strings.Contains(image, "valkey") {
			hasRedis = true
		}
	}

	buf.WriteString(`# Database Server Cloud-Init
data "cloudinit_config" "db" {
  gzip          = true
  base64_encode = true

  part {
    content_type = "text/cloud-config"
    content      = <<-EOF
#cloud-config
package_update: true
package_upgrade: true

packages:
  - docker.io
  - docker-compose
  - htop

write_files:
  - path: /opt/db/docker-compose.yml
    permissions: "0644"
    content: |
      version: "3.8"

      services:
`)

	if hasPostgres {
		buf.WriteString(`        postgres:
          image: postgres:16-alpine
          restart: unless-stopped
          environment:
            POSTGRES_USER: app
            POSTGRES_PASSWORD: \${POSTGRES_PASSWORD:-changeme}
            POSTGRES_DB: app
          volumes:
            - /mnt/postgres:/var/lib/postgresql/data
          ports:
            - "5432:5432"
          healthcheck:
            test: ["CMD-SHELL", "pg_isready -U app"]
            interval: 10s
            timeout: 5s
            retries: 5

`)
	}

	if hasMySQL {
		buf.WriteString(`        mysql:
          image: mariadb:10.11
          restart: unless-stopped
          environment:
            MYSQL_ROOT_PASSWORD: \${MYSQL_ROOT_PASSWORD:-changeme}
            MYSQL_DATABASE: app
            MYSQL_USER: app
            MYSQL_PASSWORD: \${MYSQL_PASSWORD:-changeme}
          volumes:
            - /mnt/mysql:/var/lib/mysql
          ports:
            - "3306:3306"
          healthcheck:
            test: ["CMD", "healthcheck.sh", "--connect", "--innodb_initialized"]
            interval: 10s
            timeout: 5s
            retries: 5

`)
	}

	if hasRedis {
		buf.WriteString(`        redis:
          image: valkey/valkey:8-alpine
          restart: unless-stopped
          command: valkey-server --appendonly yes
          volumes:
            - /mnt/redis:/data
          ports:
            - "6379:6379"
          healthcheck:
            test: ["CMD", "valkey-cli", "ping"]
            interval: 10s
            timeout: 5s
            retries: 5

`)
	}

	buf.WriteString(`      networks:
        default:
          driver: bridge

runcmd:
  - systemctl enable docker
  - systemctl start docker
  - usermod -aG docker root
  # Wait for volumes to be mounted
  - sleep 10
  - cd /opt/db && docker-compose pull
  - cd /opt/db && docker-compose up -d
EOF
  }
}

`)

	return buf.String()
}

func generateDockerComposeContent(categorized *CategorizedResults, config *generator.TargetConfig) string {
	var buf bytes.Buffer

	buf.WriteString("version: \"3.8\"\n\n")
	buf.WriteString("services:\n")

	// Add Traefik reverse proxy
	buf.WriteString(`  traefik:
    image: traefik:v3.0
    restart: unless-stopped
    command:
      - "--api.dashboard=true"
      - "--providers.docker=true"
      - "--providers.docker.exposedbydefault=false"
      - "--entrypoints.web.address=:80"
      - "--entrypoints.websecure.address=:443"
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock:ro
      - ./traefik:/etc/traefik
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.dashboard.rule=Host(` + "`" + `traefik.localhost` + "`" + `)"
      - "traefik.http.routers.dashboard.service=api@internal"

`)

	// Add compute services
	for _, result := range categorized.Compute {
		if result == nil || result.DockerService == nil {
			continue
		}

		svc := result.DockerService
		buf.WriteString(generateServiceBlock(svc))
	}

	// Add messaging services
	for _, result := range categorized.Messaging {
		if result == nil || result.DockerService == nil {
			continue
		}

		svc := result.DockerService
		buf.WriteString(generateServiceBlock(svc))
	}

	buf.WriteString("\nnetworks:\n")
	buf.WriteString("  default:\n")
	buf.WriteString("    driver: bridge\n")

	return buf.String()
}

func generateServiceBlock(svc *mapper.DockerService) string {
	var buf bytes.Buffer

	serviceName := SanitizeName(svc.Name)
	buf.WriteString(fmt.Sprintf("  %s:\n", serviceName))
	buf.WriteString(fmt.Sprintf("    image: %s\n", svc.Image))
	buf.WriteString("    restart: unless-stopped\n")

	if len(svc.Environment) > 0 {
		buf.WriteString("    environment:\n")
		for k, v := range svc.Environment {
			buf.WriteString(fmt.Sprintf("      %s: %s\n", k, v))
		}
	}

	if len(svc.Ports) > 0 {
		buf.WriteString("    ports:\n")
		for _, p := range svc.Ports {
			buf.WriteString(fmt.Sprintf("      - \"%s\"\n", p))
		}
	}

	if len(svc.Volumes) > 0 {
		buf.WriteString("    volumes:\n")
		for _, v := range svc.Volumes {
			buf.WriteString(fmt.Sprintf("      - %s\n", v))
		}
	}

	if len(svc.Labels) > 0 {
		buf.WriteString("    labels:\n")
		for k, v := range svc.Labels {
			buf.WriteString(fmt.Sprintf("      - \"%s=%s\"\n", k, v))
		}
	}

	buf.WriteString("\n")
	return buf.String()
}

