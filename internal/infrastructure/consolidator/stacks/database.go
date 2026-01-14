// Package stacks provides merger implementations for different stack types.
// Each merger knows how to consolidate multiple cloud resources of a specific type
// into a unified, self-hosted stack.
package stacks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
)

// DatabaseMerger consolidates database resources into a single PostgreSQL stack.
// It combines multiple RDS, CloudSQL, Azure SQL, and other database resources
// into a unified PostgreSQL deployment with connection pooling support.
type DatabaseMerger struct {
	*consolidator.BaseMerger
}

// NewDatabaseMerger creates a new DatabaseMerger.
func NewDatabaseMerger() *DatabaseMerger {
	return &DatabaseMerger{
		BaseMerger: consolidator.NewBaseMerger(stack.StackTypeDatabase),
	}
}

// StackType returns the stack type this merger handles.
func (m *DatabaseMerger) StackType() stack.StackType {
	return stack.StackTypeDatabase
}

// CanMerge checks if this merger can handle the given results.
// Returns true if there is at least one database resource to merge.
func (m *DatabaseMerger) CanMerge(results []*mapper.MappingResult) bool {
	if len(results) == 0 {
		return false
	}

	// Check if any results are database-related
	for _, r := range results {
		if r != nil && isDatabaseResource(r.SourceResourceType) {
			return true
		}
	}

	return false
}

// Merge consolidates multiple mapping results into a single database stack.
// It creates a PostgreSQL container with multiple logical databases,
// init scripts to create all databases, and optional pgBouncer for connection pooling.
func (m *DatabaseMerger) Merge(ctx context.Context, results []*mapper.MappingResult, opts *consolidator.MergeOptions) (*stack.Stack, error) {
	if len(results) == 0 {
		return nil, fmt.Errorf("no results to merge")
	}

	// Create the stack
	name := "database"
	if opts != nil && opts.NamePrefix != "" {
		name = opts.NamePrefix + "-" + name
	}

	stk := stack.NewStack(stack.StackTypeDatabase, name)
	stk.Description = "Consolidated database stack with PostgreSQL and connection pooling"

	// Determine the database engine to use
	engine := m.selectEngine(opts)
	image := m.getImageForEngine(engine)

	// Extract database names from source resources
	dbNames := m.extractDatabaseNames(results)

	// Create primary database service
	primaryService := m.createPrimaryService(engine, image, dbNames, opts)
	stk.AddService(primaryService)

	// Add pgBouncer for connection pooling if support services are enabled
	if opts == nil || opts.IncludeSupportServices {
		pgBouncerService := m.createPgBouncerService(engine, dbNames)
		if pgBouncerService != nil {
			stk.AddService(pgBouncerService)
		}
	}

	// Generate init script to create all databases
	initScript := m.generateInitScript(engine, dbNames)
	stk.AddScript("init.sql", initScript)

	// Generate pgBouncer configuration if needed
	if opts == nil || opts.IncludeSupportServices {
		pgBouncerConfig := m.generatePgBouncerConfig(engine, dbNames)
		stk.AddConfig("pgbouncer.ini", pgBouncerConfig)

		// Generate userlist.txt for pgBouncer authentication
		userList := m.generatePgBouncerUserList()
		stk.AddConfig("userlist.txt", userList)
	}

	// Add data volume
	stk.AddVolume(stack.Volume{
		Name:   "db-data",
		Driver: "local",
		Labels: map[string]string{
			"homeport.stack": "database",
			"homeport.role":  "primary-data",
		},
	})

	// Track source resources
	for _, result := range results {
		if result != nil {
			res := &resource.Resource{
				Type: resource.Type(result.SourceResourceType),
				Name: result.SourceResourceName,
			}
			stk.AddSourceResource(res)

			// Collect warnings
			for _, warning := range result.Warnings {
				stk.Metadata["warning_"+result.SourceResourceName] = warning
			}
		}
	}

	// Add manual steps for data migration
	stk.Metadata["manual_step_1"] = "Export data from source databases using pg_dump or equivalent"
	stk.Metadata["manual_step_2"] = "Import data into the consolidated PostgreSQL instance"
	stk.Metadata["manual_step_3"] = "Update application connection strings to use the new database endpoint"
	stk.Metadata["manual_step_4"] = "Verify data integrity after migration"

	// Generate a migration documentation
	migrationDoc := m.generateMigrationDoc(results, dbNames, engine)
	stk.AddScript("MIGRATION.md", migrationDoc)

	// Merge configs and scripts from source results
	configs := consolidator.ExtractConfigs(results)
	for name, content := range configs {
		stk.AddConfig(name, content)
	}

	scripts := consolidator.ExtractScripts(results)
	for name, content := range scripts {
		stk.AddScript(name, content)
	}

	return stk, nil
}

// selectEngine determines which database engine to use based on options.
// Defaults to PostgreSQL if not specified.
func (m *DatabaseMerger) selectEngine(opts *consolidator.MergeOptions) string {
	if opts == nil || opts.DatabaseEngine == "" {
		return "postgres"
	}

	engine := strings.ToLower(opts.DatabaseEngine)
	switch engine {
	case "postgres", "postgresql":
		return "postgres"
	case "mysql":
		return "mysql"
	case "mariadb":
		return "mariadb"
	default:
		return "postgres"
	}
}

// getImageForEngine returns the Docker image for the specified engine.
func (m *DatabaseMerger) getImageForEngine(engine string) string {
	switch engine {
	case "postgres":
		return "postgres:16"
	case "mysql":
		return "mysql:8"
	case "mariadb":
		return "mariadb:11"
	default:
		return "postgres:16"
	}
}

// extractDatabaseNames extracts database names from multiple mapping results.
// It uses the source resource name or extracts from environment variables.
func (m *DatabaseMerger) extractDatabaseNames(results []*mapper.MappingResult) []string {
	nameSet := make(map[string]bool)

	for _, result := range results {
		if result == nil {
			continue
		}

		// Try to get database name from various sources
		var dbName string

		// 1. Use source resource name as database name (normalized)
		if result.SourceResourceName != "" {
			dbName = consolidator.NormalizeName(result.SourceResourceName)
		}

		// 2. Check environment variables for explicit database name
		if result.DockerService != nil && result.DockerService.Environment != nil {
			for key, value := range result.DockerService.Environment {
				keyLower := strings.ToLower(key)
				if strings.Contains(keyLower, "database") ||
					strings.Contains(keyLower, "db_name") ||
					strings.Contains(keyLower, "dbname") {
					if value != "" {
						dbName = consolidator.NormalizeName(value)
					}
				}
			}
		}

		// If we found a name, add it
		if dbName != "" {
			// Ensure valid database name (alphanumeric and underscores only)
			dbName = strings.ReplaceAll(dbName, "-", "_")
			nameSet[dbName] = true
		}
	}

	// Convert to sorted slice for consistent output
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	// If no names extracted, use a default
	if len(names) == 0 {
		names = append(names, "app_db")
	}

	return names
}

// createPrimaryService creates the main database service.
func (m *DatabaseMerger) createPrimaryService(engine, image string, dbNames []string, opts *consolidator.MergeOptions) *stack.Service {
	svc := stack.NewService(engine, image)

	// Set restart policy
	svc.Restart = "unless-stopped"

	// Configure based on engine
	switch engine {
	case "postgres":
		svc.Environment["POSTGRES_USER"] = "${DB_USER:-postgres}"
		svc.Environment["POSTGRES_PASSWORD"] = "${DB_PASSWORD:-changeme}"
		svc.Environment["POSTGRES_DB"] = dbNames[0] // Primary database
		svc.Environment["PGDATA"] = "/var/lib/postgresql/data/pgdata"
		svc.Ports = []string{"5432:5432"}
		svc.Volumes = []string{
			"db-data:/var/lib/postgresql/data",
			"./init.sql:/docker-entrypoint-initdb.d/init.sql:ro",
		}
		svc.HealthCheck = &stack.HealthCheck{
			Test:        []string{"CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-postgres}"},
			Interval:    "10s",
			Timeout:     "5s",
			Retries:     5,
			StartPeriod: "30s",
		}

	case "mysql":
		svc.Environment["MYSQL_ROOT_PASSWORD"] = "${DB_ROOT_PASSWORD:-changeme}"
		svc.Environment["MYSQL_USER"] = "${DB_USER:-appuser}"
		svc.Environment["MYSQL_PASSWORD"] = "${DB_PASSWORD:-changeme}"
		svc.Environment["MYSQL_DATABASE"] = dbNames[0]
		svc.Ports = []string{"3306:3306"}
		svc.Volumes = []string{
			"db-data:/var/lib/mysql",
			"./init.sql:/docker-entrypoint-initdb.d/init.sql:ro",
		}
		svc.HealthCheck = &stack.HealthCheck{
			Test:        []string{"CMD", "mysqladmin", "ping", "-h", "localhost"},
			Interval:    "10s",
			Timeout:     "5s",
			Retries:     5,
			StartPeriod: "30s",
		}

	case "mariadb":
		svc.Environment["MARIADB_ROOT_PASSWORD"] = "${DB_ROOT_PASSWORD:-changeme}"
		svc.Environment["MARIADB_USER"] = "${DB_USER:-appuser}"
		svc.Environment["MARIADB_PASSWORD"] = "${DB_PASSWORD:-changeme}"
		svc.Environment["MARIADB_DATABASE"] = dbNames[0]
		svc.Ports = []string{"3306:3306"}
		svc.Volumes = []string{
			"db-data:/var/lib/mysql",
			"./init.sql:/docker-entrypoint-initdb.d/init.sql:ro",
		}
		svc.HealthCheck = &stack.HealthCheck{
			Test:        []string{"CMD", "healthcheck.sh", "--connect", "--innodb_initialized"},
			Interval:    "10s",
			Timeout:     "5s",
			Retries:     5,
			StartPeriod: "30s",
		}
	}

	// Add labels
	svc.Labels["homeport.stack"] = "database"
	svc.Labels["homeport.role"] = "primary"
	svc.Labels["homeport.engine"] = engine

	return svc
}

// createPgBouncerService creates a pgBouncer service for connection pooling.
// Returns nil if the engine is not PostgreSQL.
func (m *DatabaseMerger) createPgBouncerService(engine string, dbNames []string) *stack.Service {
	// pgBouncer only works with PostgreSQL
	if engine != "postgres" {
		return nil
	}

	svc := stack.NewService("pgbouncer", "edoburu/pgbouncer:latest")
	svc.Restart = "unless-stopped"

	svc.Environment["DATABASE_URL"] = "postgres://${DB_USER:-postgres}:${DB_PASSWORD:-changeme}@postgres:5432/${DB_NAME:-" + dbNames[0] + "}"
	svc.Environment["POOL_MODE"] = "transaction"
	svc.Environment["MAX_CLIENT_CONN"] = "100"
	svc.Environment["DEFAULT_POOL_SIZE"] = "20"

	svc.Ports = []string{"6432:6432"}

	svc.Volumes = []string{
		"./pgbouncer.ini:/etc/pgbouncer/pgbouncer.ini:ro",
		"./userlist.txt:/etc/pgbouncer/userlist.txt:ro",
	}

	svc.DependsOn = []string{"postgres"}

	svc.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD-SHELL", "pg_isready -h localhost -p 6432"},
		Interval:    "10s",
		Timeout:     "5s",
		Retries:     3,
		StartPeriod: "10s",
	}

	svc.Labels["homeport.stack"] = "database"
	svc.Labels["homeport.role"] = "pooler"

	return svc
}

// generateInitScript generates a SQL initialization script that creates all databases.
func (m *DatabaseMerger) generateInitScript(engine string, dbNames []string) []byte {
	var sb strings.Builder

	sb.WriteString("-- ============================================================\n")
	sb.WriteString("-- Database Initialization Script\n")
	sb.WriteString("-- Generated by Homeport Stack Consolidation\n")
	sb.WriteString(fmt.Sprintf("-- Generated at: %s\n", time.Now().Format(time.RFC3339)))
	sb.WriteString("-- ============================================================\n")
	sb.WriteString("--\n")
	sb.WriteString("-- This script creates all logical databases that were consolidated\n")
	sb.WriteString("-- from your cloud resources. The first database is automatically\n")
	sb.WriteString("-- created by the container, additional databases are created below.\n")
	sb.WriteString("--\n")
	sb.WriteString("-- Source databases:\n")
	for _, name := range dbNames {
		sb.WriteString(fmt.Sprintf("--   - %s\n", name))
	}
	sb.WriteString("-- ============================================================\n\n")

	switch engine {
	case "postgres":
		// Skip the first database as it's created by POSTGRES_DB env var
		for i, name := range dbNames {
			if i == 0 {
				sb.WriteString(fmt.Sprintf("-- Database '%s' is created automatically via POSTGRES_DB\n\n", name))
				continue
			}
			sb.WriteString(fmt.Sprintf("-- Create database: %s\n", name))
			sb.WriteString(fmt.Sprintf("SELECT 'CREATE DATABASE %s' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '%s')\\gexec\n\n", name, name))
		}

		// Grant privileges
		sb.WriteString("-- Grant privileges to the application user\n")
		for _, name := range dbNames {
			sb.WriteString(fmt.Sprintf("GRANT ALL PRIVILEGES ON DATABASE %s TO postgres;\n", name))
		}

	case "mysql", "mariadb":
		// Skip the first database as it's created by MYSQL_DATABASE env var
		for i, name := range dbNames {
			if i == 0 {
				sb.WriteString(fmt.Sprintf("-- Database '%s' is created automatically via MYSQL_DATABASE\n\n", name))
				continue
			}
			sb.WriteString(fmt.Sprintf("-- Create database: %s\n", name))
			sb.WriteString(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;\n\n", name))
		}

		// Grant privileges
		sb.WriteString("-- Grant privileges to the application user\n")
		for _, name := range dbNames {
			sb.WriteString(fmt.Sprintf("GRANT ALL PRIVILEGES ON `%s`.* TO '${MYSQL_USER}'@'%%';\n", name))
		}
		sb.WriteString("FLUSH PRIVILEGES;\n")
	}

	return []byte(sb.String())
}

// generatePgBouncerConfig generates pgbouncer.ini configuration file.
func (m *DatabaseMerger) generatePgBouncerConfig(engine string, dbNames []string) []byte {
	if engine != "postgres" {
		return nil
	}

	var sb strings.Builder

	sb.WriteString("; ============================================================\n")
	sb.WriteString("; PgBouncer Configuration\n")
	sb.WriteString("; Generated by Homeport Stack Consolidation\n")
	sb.WriteString("; ============================================================\n")
	sb.WriteString(";\n")
	sb.WriteString("; Connection pooling configuration for consolidated databases.\n")
	sb.WriteString("; Pool mode is set to 'transaction' for best compatibility.\n")
	sb.WriteString(";\n\n")

	sb.WriteString("[databases]\n")
	sb.WriteString("; Database connection mappings\n")
	sb.WriteString("; Format: logical_name = host=hostname dbname=actual_name\n")
	for _, name := range dbNames {
		sb.WriteString(fmt.Sprintf("%s = host=postgres dbname=%s\n", name, name))
	}
	sb.WriteString("\n; Wildcard fallback - connects to default database\n")
	sb.WriteString("* = host=postgres\n\n")

	sb.WriteString("[pgbouncer]\n")
	sb.WriteString("listen_addr = 0.0.0.0\n")
	sb.WriteString("listen_port = 6432\n")
	sb.WriteString("auth_type = md5\n")
	sb.WriteString("auth_file = /etc/pgbouncer/userlist.txt\n")
	sb.WriteString("\n; Pool settings\n")
	sb.WriteString("pool_mode = transaction\n")
	sb.WriteString("max_client_conn = 100\n")
	sb.WriteString("default_pool_size = 20\n")
	sb.WriteString("min_pool_size = 5\n")
	sb.WriteString("reserve_pool_size = 5\n")
	sb.WriteString("reserve_pool_timeout = 3\n")
	sb.WriteString("\n; Connection settings\n")
	sb.WriteString("server_reset_query = DISCARD ALL\n")
	sb.WriteString("server_check_query = SELECT 1\n")
	sb.WriteString("server_check_delay = 30\n")
	sb.WriteString("\n; Logging\n")
	sb.WriteString("log_connections = 1\n")
	sb.WriteString("log_disconnections = 1\n")
	sb.WriteString("log_pooler_errors = 1\n")
	sb.WriteString("stats_period = 60\n")
	sb.WriteString("\n; Admin access\n")
	sb.WriteString("admin_users = postgres\n")
	sb.WriteString("stats_users = postgres\n")

	return []byte(sb.String())
}

// generatePgBouncerUserList generates the userlist.txt for pgBouncer authentication.
func (m *DatabaseMerger) generatePgBouncerUserList() []byte {
	var sb strings.Builder

	sb.WriteString("; PgBouncer user list\n")
	sb.WriteString("; Format: \"username\" \"password\"\n")
	sb.WriteString(";\n")
	sb.WriteString("; NOTE: Replace the password hash with the actual MD5 hash from PostgreSQL.\n")
	sb.WriteString("; Generate with: SELECT 'md5' || md5('password' || 'username');\n")
	sb.WriteString(";\n")
	sb.WriteString("; Example for user 'postgres' with password 'changeme':\n")
	sb.WriteString("\"postgres\" \"md5a6c6a17b9a7e1f8f9e0c3b2a1d4e5f6g\"\n")

	return []byte(sb.String())
}

// generateMigrationDoc generates a markdown document with migration instructions.
func (m *DatabaseMerger) generateMigrationDoc(results []*mapper.MappingResult, dbNames []string, engine string) []byte {
	var sb strings.Builder

	sb.WriteString("# Database Migration Guide\n\n")
	sb.WriteString("This document provides instructions for migrating your cloud databases to the consolidated self-hosted stack.\n\n")

	sb.WriteString("## Source Databases\n\n")
	sb.WriteString("The following databases have been consolidated:\n\n")
	sb.WriteString("| Source Resource | Database Name | Source Type |\n")
	sb.WriteString("|-----------------|---------------|-------------|\n")
	for _, result := range results {
		if result != nil {
			dbName := consolidator.NormalizeName(result.SourceResourceName)
			dbName = strings.ReplaceAll(dbName, "-", "_")
			sb.WriteString(fmt.Sprintf("| %s | %s | %s |\n",
				result.SourceResourceName,
				dbName,
				result.SourceResourceType))
		}
	}
	sb.WriteString("\n")

	sb.WriteString("## Target Configuration\n\n")
	sb.WriteString(fmt.Sprintf("- **Engine**: %s\n", engine))
	sb.WriteString(fmt.Sprintf("- **Primary Port**: %s\n", getPrimaryPort(engine)))
	if engine == "postgres" {
		sb.WriteString("- **PgBouncer Port**: 6432 (connection pooling)\n")
	}
	sb.WriteString(fmt.Sprintf("- **Databases**: %s\n", strings.Join(dbNames, ", ")))
	sb.WriteString("\n")

	sb.WriteString("## Migration Steps\n\n")

	sb.WriteString("### 1. Export Data from Source\n\n")
	switch engine {
	case "postgres":
		sb.WriteString("```bash\n")
		sb.WriteString("# For each source PostgreSQL database:\n")
		sb.WriteString("pg_dump -h <source_host> -U <user> -d <database> -F c -f backup.dump\n")
		sb.WriteString("\n")
		sb.WriteString("# For RDS:\n")
		sb.WriteString("pg_dump -h <rds_endpoint> -U <master_user> -d <database> -F c -f backup.dump\n")
		sb.WriteString("```\n\n")
	case "mysql", "mariadb":
		sb.WriteString("```bash\n")
		sb.WriteString("# For each source MySQL database:\n")
		sb.WriteString("mysqldump -h <source_host> -u <user> -p <database> > backup.sql\n")
		sb.WriteString("\n")
		sb.WriteString("# For RDS MySQL:\n")
		sb.WriteString("mysqldump -h <rds_endpoint> -u <master_user> -p <database> > backup.sql\n")
		sb.WriteString("```\n\n")
	}

	sb.WriteString("### 2. Start the Database Stack\n\n")
	sb.WriteString("```bash\n")
	sb.WriteString("docker compose up -d\n")
	sb.WriteString("```\n\n")

	sb.WriteString("### 3. Import Data to Target\n\n")
	switch engine {
	case "postgres":
		sb.WriteString("```bash\n")
		sb.WriteString("# Restore each database:\n")
		sb.WriteString("pg_restore -h localhost -U postgres -d <database> backup.dump\n")
		sb.WriteString("\n")
		sb.WriteString("# Or use Docker:\n")
		sb.WriteString("docker compose exec -T postgres pg_restore -U postgres -d <database> < backup.dump\n")
		sb.WriteString("```\n\n")
	case "mysql", "mariadb":
		sb.WriteString("```bash\n")
		sb.WriteString("# Restore each database:\n")
		sb.WriteString("mysql -h localhost -u root -p <database> < backup.sql\n")
		sb.WriteString("\n")
		sb.WriteString("# Or use Docker:\n")
		sb.WriteString("docker compose exec -T " + engine + " mysql -u root -p <database> < backup.sql\n")
		sb.WriteString("```\n\n")
	}

	sb.WriteString("### 4. Update Application Connection Strings\n\n")
	sb.WriteString("Update your applications to use the new database endpoints:\n\n")
	switch engine {
	case "postgres":
		sb.WriteString("```\n")
		sb.WriteString("# Direct connection:\n")
		sb.WriteString("postgresql://postgres:changeme@localhost:5432/<database>\n")
		sb.WriteString("\n")
		sb.WriteString("# Via PgBouncer (recommended for production):\n")
		sb.WriteString("postgresql://postgres:changeme@localhost:6432/<database>\n")
		sb.WriteString("```\n\n")
	case "mysql", "mariadb":
		sb.WriteString("```\n")
		sb.WriteString("mysql://appuser:changeme@localhost:3306/<database>\n")
		sb.WriteString("```\n\n")
	}

	sb.WriteString("### 5. Verify Data Integrity\n\n")
	sb.WriteString("After migration, verify:\n")
	sb.WriteString("- [ ] All tables exist\n")
	sb.WriteString("- [ ] Row counts match source\n")
	sb.WriteString("- [ ] Application queries work correctly\n")
	sb.WriteString("- [ ] Performance is acceptable\n\n")

	sb.WriteString("## Environment Variables\n\n")
	sb.WriteString("Configure these environment variables before starting:\n\n")
	sb.WriteString("| Variable | Description | Default |\n")
	sb.WriteString("|----------|-------------|----------|\n")
	switch engine {
	case "postgres":
		sb.WriteString("| DB_USER | Database user | postgres |\n")
		sb.WriteString("| DB_PASSWORD | Database password | changeme |\n")
		sb.WriteString("| DB_NAME | Default database | " + dbNames[0] + " |\n")
	case "mysql", "mariadb":
		sb.WriteString("| DB_ROOT_PASSWORD | Root password | changeme |\n")
		sb.WriteString("| DB_USER | Application user | appuser |\n")
		sb.WriteString("| DB_PASSWORD | User password | changeme |\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## Troubleshooting\n\n")
	sb.WriteString("### Connection Issues\n")
	sb.WriteString("- Ensure the database container is running: `docker compose ps`\n")
	sb.WriteString("- Check logs: `docker compose logs " + engine + "`\n")
	sb.WriteString("- Verify network connectivity from your application\n\n")

	sb.WriteString("### Performance Issues\n")
	sb.WriteString("- Use connection pooling (PgBouncer for PostgreSQL)\n")
	sb.WriteString("- Review slow query logs\n")
	sb.WriteString("- Consider increasing container resources\n")

	return []byte(sb.String())
}

// getPrimaryPort returns the primary port for the given database engine.
func getPrimaryPort(engine string) string {
	switch engine {
	case "postgres":
		return "5432"
	case "mysql", "mariadb":
		return "3306"
	default:
		return "5432"
	}
}

// isDatabaseResource checks if a resource type is a database resource.
func isDatabaseResource(resourceType string) bool {
	dbTypes := []string{
		// AWS
		"aws_db_instance",
		"aws_rds_cluster",
		"aws_dynamodb_table",
		"aws_docdb_cluster",
		"aws_neptune_cluster",
		// GCP
		"google_sql_database_instance",
		"google_spanner_instance",
		"google_bigtable_instance",
		"google_firestore_database",
		// Azure
		"azurerm_postgresql_server",
		"azurerm_postgresql_flexible_server",
		"azurerm_mysql_server",
		"azurerm_mysql_flexible_server",
		"azurerm_mssql_server",
		"azurerm_cosmosdb_account",
	}

	for _, t := range dbTypes {
		if strings.Contains(strings.ToLower(resourceType), strings.ToLower(t)) {
			return true
		}
	}

	// Also check for generic database indicators
	lowerType := strings.ToLower(resourceType)
	return strings.Contains(lowerType, "database") ||
		strings.Contains(lowerType, "rds") ||
		strings.Contains(lowerType, "sql") ||
		strings.Contains(lowerType, "db_instance")
}
