// Package database provides mappers for Azure database services.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/infrastructure/mapper/shared/datarunbook"
)

// AzureSQLMapper converts Azure SQL Database to SQL Server containers.
type AzureSQLMapper struct {
	*mapper.BaseMapper
}

// NewAzureSQLMapper creates a new Azure SQL mapper.
func NewAzureSQLMapper() *AzureSQLMapper {
	return &AzureSQLMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeAzureSQL, nil),
	}
}

// Map converts an Azure SQL Database to a SQL Server service.
func (m *AzureSQLMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	dbName := res.GetConfigString("name")
	if dbName == "" {
		dbName = res.Name
	}

	result := mapper.NewMappingResult("mssql")
	svc := result.DockerService

	svc.Image = "mcr.microsoft.com/mssql/server:2022-latest"
	svc.Environment = map[string]string{
		"ACCEPT_EULA":           "Y",
		"SA_PASSWORD":           "YourStrong@Passw0rd",
		"MSSQL_PID":             "Developer",
		"MSSQL_COLLATION":       "SQL_Latin1_General_CP1_CI_AS",
		"MSSQL_MEMORY_LIMIT_MB": "2048",
	}
	svc.Ports = []string{"1433:1433"}
	svc.Volumes = []string{
		"./data/mssql:/var/opt/mssql/data",
		"./backups/mssql:/var/opt/mssql/backups",
	}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:        []string{"CMD-SHELL", "/opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P 'YourStrong@Passw0rd' -Q 'SELECT 1' || exit 1"},
		Interval:    15 * time.Second,
		Timeout:     10 * time.Second,
		Retries:     5,
		StartPeriod: 30 * time.Second,
	}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":   "azurerm_mssql_database",
		"homeport.engine":   "mssql",
		"homeport.database": dbName,
	}

	initScript := m.generateInitScript(dbName)
	result.AddScript("init_database.sh", []byte(initScript))
	result.AddConfig("config/sql/credentials.env", []byte(m.generateCredentials(dbName)))
	result.AddConfig("config/sql/app-change.env", []byte(m.generateAppChange(dbName)))
	result.AddConfig("config/sql/replication.env", []byte(m.generateReplicationConfig(dbName)))
	result.AddConfig("config/sql/generated-client.patch", []byte(m.generateClientPatch(dbName)))

	migrationScript := m.generateMigrationScript(dbName)
	result.AddScript("migrate_azuresql.sh", []byte(migrationScript))
	result.AddScript("validate_database.sh", []byte(m.generateValidateScript(dbName)))
	result.AddScript("backup_database.sh", []byte(m.generateBackupScript(dbName)))
	result.AddScript("cutover_database.sh", []byte(m.generateCutoverScript(dbName)))
	for _, step := range datarunbook.SQL("mssql", dbName, "migrate_azuresql.sh") {
		result.AddRunbookStep(step)
	}

	result.AddWarning("SQL Server Developer Edition is for development only. Use Express or get a license for production.")
	result.AddWarning("Update the SA_PASSWORD - never use default passwords in production!")

	return result, nil
}

func (m *AzureSQLMapper) generateInitScript(dbName string) string {
	return fmt.Sprintf(`#!/bin/bash
# SQL Server Database Initialization
set -e

echo "Waiting for SQL Server to start..."
sleep 30

echo "Creating database: %s"
/opt/mssql-tools/bin/sqlcmd -S localhost -U sa -P "YourStrong@Passw0rd" -Q "
IF NOT EXISTS (SELECT name FROM sys.databases WHERE name = '%s')
BEGIN
    CREATE DATABASE [%s];
    PRINT 'Database created successfully.';
END
"
`, dbName, dbName, dbName)
}

func (m *AzureSQLMapper) generateMigrationScript(dbName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Azure SQL Database Migration Script
set -e

echo "Azure SQL Database Migration"
echo "============================"
echo "Database: %s"

echo "Option 1: Using SqlPackage (BACPAC export/import)"
echo "  # Export from Azure"
echo "  SqlPackage /Action:Export /SourceServerName:server.database.windows.net \\"
echo "    /SourceDatabaseName:%s /SourceUser:admin /SourcePassword:*** \\"
echo "    /TargetFile:%s.bacpac"
echo ""
echo "  # Import to local SQL Server"
echo "  SqlPackage /Action:Import /SourceFile:%s.bacpac \\"
echo "    /TargetServerName:localhost,1433 /TargetDatabaseName:%s \\"
echo "    /TargetUser:sa /TargetPassword:'YourStrong@Passw0rd'"

echo "Option 2: Using Azure Data Studio or SSMS"
echo "  1. Connect to Azure SQL Database"
echo "  2. Right-click database -> Tasks -> Export Data-tier Application"
echo "  3. Save as BACPAC file"
echo "  4. Connect to local SQL Server"
echo "  5. Right-click Databases -> Import Data-tier Application"
`, dbName, dbName, dbName, dbName, dbName)
}

func (m *AzureSQLMapper) generateCredentials(dbName string) string {
	return fmt.Sprintf("SOURCE_AZURE_SQL=%s\nSQLSERVER_HOST=mssql\nSQLSERVER_PORT=1433\nSQLSERVER_USER=sa\nSQLSERVER_PASSWORD=YourStrong@Passw0rd\n", dbName)
}

func (m *AzureSQLMapper) generateAppChange(dbName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_AZURE_SQL=%s\nDATABASE_URL=sqlserver://sa:YourStrong@Passw0rd@mssql:1433?database=%s\nGENERATED_PATCH=config/sql/generated-client.patch\n", dbName, dbName)
}

func (m *AzureSQLMapper) generateReplicationConfig(dbName string) string {
	return fmt.Sprintf("SOURCE_AZURE_SQL=%s\nREPLICATION_MODE=bacpac_export_import\nLIVE_REPLICATION_SUPPORTED=false\n", dbName)
}

func (m *AzureSQLMapper) generateClientPatch(dbName string) string {
	return fmt.Sprintf("--- a/app/database.env\n+++ b/app/database.env\n@@\n-AZURE_SQL_DATABASE=%s\n+DATABASE_URL=sqlserver://sa:YourStrong@Passw0rd@mssql:1433?database=%s\n", dbName, dbName)
}

func (m *AzureSQLMapper) generateValidateScript(dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/sql/credentials.env\ntest -s config/sql/app-change.env\ngrep -q %q config/sql/app-change.env\n", dbName)
}

func (m *AzureSQLMapper) generateBackupScript(dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/azuresql-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/sql backups/mssql 2>/dev/null || tar -czf \"$archive\" config/sql\necho \"$archive\"\n", dbName)
}

func (m *AzureSQLMapper) generateCutoverScript(dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/sql/app-change.env\ntest \"$SOURCE_AZURE_SQL\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and use $DATABASE_URL\"\n", dbName)
}
