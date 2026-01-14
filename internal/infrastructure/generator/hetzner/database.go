// Package hetzner provides database resource generation for Hetzner Cloud.
package hetzner

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/generator"
)

// GenerateDatabaseTF generates the database Terraform configuration for Hetzner.
// Databases run on dedicated servers with Docker and cloud-init.
func GenerateDatabaseTF(categorized *CategorizedResults, config *generator.TargetConfig) string {
	var buf bytes.Buffer

	if len(categorized.Database) == 0 {
		buf.WriteString("# No database resources to generate\n")
		return buf.String()
	}

	buf.WriteString("# Database Server Configuration\n")
	buf.WriteString("# Databases are deployed as Docker containers on dedicated servers\n\n")

	projectName := "homeport"
	if config.ProjectName != "" {
		projectName = SanitizeName(config.ProjectName)
	}

	// Generate database server
	buf.WriteString(generateDatabaseServer(categorized, config, projectName))

	// Generate database-specific configurations
	for _, result := range categorized.Database {
		if result == nil || result.DockerService == nil {
			continue
		}

		serviceName := result.DockerService.Name
		buf.WriteString(fmt.Sprintf("# Database: %s\n", serviceName))
	}

	return buf.String()
}

func generateDatabaseServer(categorized *CategorizedResults, config *generator.TargetConfig, projectName string) string {
	var buf bytes.Buffer

	serverType := "cx21" // Default server type for database workloads

	// Determine database types
	hasPostgres := false
	hasMySQL := false
	hasRedis := false
	hasMongoDB := false

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
		if strings.Contains(image, "mongo") {
			hasMongoDB = true
		}
	}

	buf.WriteString(fmt.Sprintf(`# Database Server
resource "hcloud_server" "db" {
  name        = "%s-db"
  server_type = "%s"
  image       = "docker-ce"
  location    = var.location

  ssh_keys = [hcloud_ssh_key.admin.id]

  firewall_ids = [hcloud_firewall.db.id]

  user_data = data.cloudinit_config.db.rendered

  labels = {
    project = "%s"
    role    = "database"
  }

  lifecycle {
    ignore_changes = [user_data]
  }
}

# Attach database server to network
resource "hcloud_server_network" "db" {
  server_id  = hcloud_server.db.id
  network_id = hcloud_network.main.id
  ip         = "10.0.2.10"
}

`, projectName, serverType, projectName))

	// Generate volumes for each database type
	if hasPostgres {
		buf.WriteString(generateDatabaseVolumeResource("postgres", 20))
	}
	if hasMySQL {
		buf.WriteString(generateDatabaseVolumeResource("mysql", 20))
	}
	if hasRedis {
		buf.WriteString(generateDatabaseVolumeResource("redis", 10))
	}
	if hasMongoDB {
		buf.WriteString(generateDatabaseVolumeResource("mongodb", 20))
	}

	return buf.String()
}
