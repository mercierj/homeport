// Package database provides mappers for Azure database services.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
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
	svc.Labels = map[string]string{
		"cloudexit.source":   "azurerm_mssql_database",
		"cloudexit.engine":   "mssql",
		"cloudexit.database": dbName,
	}

	initScript := m.generateInitScript(dbName)
	result.AddScript("init_database.sh", []byte(initScript))

	migrationScript := m.generateMigrationScript(dbName)
	result.AddScript("migrate_azuresql.sh", []byte(migrationScript))

	result.AddWarning("SQL Server Developer Edition is for development only. Use Express or get a license for production.")
	result.AddWarning("Update the SA_PASSWORD - never use default passwords in production!")

	result.AddManualStep("Update SQL Server SA password in docker-compose.yml")
	result.AddManualStep("Export database from Azure SQL using BACPAC or SqlPackage")
	result.AddManualStep("Import data using SqlPackage or SSMS")

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
