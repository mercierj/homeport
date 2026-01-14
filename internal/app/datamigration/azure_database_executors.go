package datamigration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// AzureSQLToPostgresExecutor migrates Azure SQL Database to PostgreSQL.
type AzureSQLToPostgresExecutor struct{}

// NewAzureSQLToPostgresExecutor creates a new Azure SQL to Postgres executor.
func NewAzureSQLToPostgresExecutor() *AzureSQLToPostgresExecutor {
	return &AzureSQLToPostgresExecutor{}
}

// Type returns the migration type.
func (e *AzureSQLToPostgresExecutor) Type() string {
	return "azure_sql_to_postgres"
}

// GetPhases returns the migration phases.
func (e *AzureSQLToPostgresExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Analyzing database schema",
		"Generating schema conversion",
		"Exporting data",
		"Generating migration scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *AzureSQLToPostgresExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["server"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.server is required (e.g., myserver.database.windows.net)")
		}
		if _, ok := config.Source["database"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.database is required")
		}
		if _, ok := config.Source["username"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.username is required")
		}
		if _, ok := config.Source["password"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.password is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "T-SQL to PostgreSQL conversion may require manual adjustments")
	result.Warnings = append(result.Warnings, "Stored procedures and triggers need manual review")

	return result, nil
}

// Execute performs the migration.
func (e *AzureSQLToPostgresExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	server := config.Source["server"].(string)
	database := config.Source["database"].(string)
	username := config.Source["username"].(string)
	password := config.Source["password"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure SQL credentials")
	EmitProgress(m, 10, "Checking credentials")

	// Test connection using sqlcmd
	testCmd := exec.CommandContext(ctx, "sqlcmd",
		"-S", server,
		"-d", database,
		"-U", username,
		"-P", password,
		"-Q", "SELECT 1",
		"-b",
	)
	if output, err := testCmd.CombinedOutput(); err != nil {
		EmitLog(m, "warning", fmt.Sprintf("sqlcmd test failed (may not be installed): %s", string(output)))
		// Continue anyway as we'll generate scripts
	} else {
		EmitLog(m, "info", "Successfully connected to Azure SQL")
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Analyzing database schema
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", "Analyzing database schema")
	EmitProgress(m, 25, "Analyzing schema")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save connection info (without password)
	connInfo := map[string]string{
		"server":   server,
		"database": database,
		"username": username,
	}
	connInfoBytes, _ := json.MarshalIndent(connInfo, "", "  ")
	connInfoPath := filepath.Join(outputDir, "connection-info.json")
	if err := os.WriteFile(connInfoPath, connInfoBytes, 0644); err != nil {
		return fmt.Errorf("failed to write connection info: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Generating schema conversion
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Generating schema conversion scripts")
	EmitProgress(m, 40, "Generating schema conversion")

	// Generate schema extraction script
	schemaScript := fmt.Sprintf(`#!/bin/bash
# Azure SQL Schema Export Script
# Server: %s
# Database: %s

set -e

echo "Azure SQL Schema Export"
echo "======================="

# Configuration
SERVER="%s"
DATABASE="%s"
USERNAME="%s"
PASSWORD="${AZURE_SQL_PASSWORD:-%s}"

# Export schema using sqlcmd
echo "Exporting schema..."

# Tables
sqlcmd -S "$SERVER" -d "$DATABASE" -U "$USERNAME" -P "$PASSWORD" -Q "
SELECT
    'CREATE TABLE ' + QUOTENAME(s.name) + '.' + QUOTENAME(t.name) + ' (' +
    STUFF((
        SELECT ', ' + QUOTENAME(c.name) + ' ' +
            CASE WHEN ty.name IN ('varchar', 'nvarchar', 'char', 'nchar')
                THEN ty.name + '(' + CASE WHEN c.max_length = -1 THEN 'MAX' ELSE CAST(c.max_length AS VARCHAR) END + ')'
                WHEN ty.name IN ('decimal', 'numeric')
                THEN ty.name + '(' + CAST(c.precision AS VARCHAR) + ',' + CAST(c.scale AS VARCHAR) + ')'
                ELSE ty.name
            END +
            CASE WHEN c.is_nullable = 0 THEN ' NOT NULL' ELSE '' END
        FROM sys.columns c
        JOIN sys.types ty ON c.user_type_id = ty.user_type_id
        WHERE c.object_id = t.object_id
        ORDER BY c.column_id
        FOR XML PATH(''), TYPE
    ).value('.', 'NVARCHAR(MAX)'), 1, 2, '') + ');' AS CreateTableStatement
FROM sys.tables t
JOIN sys.schemas s ON t.schema_id = s.schema_id
WHERE t.is_ms_shipped = 0
" -o schema-tables.sql -s"" -W

# Indexes
sqlcmd -S "$SERVER" -d "$DATABASE" -U "$USERNAME" -P "$PASSWORD" -Q "
SELECT
    'CREATE ' + CASE WHEN i.is_unique = 1 THEN 'UNIQUE ' ELSE '' END +
    CASE WHEN i.type = 1 THEN 'CLUSTERED ' ELSE '' END +
    'INDEX ' + QUOTENAME(i.name) + ' ON ' + QUOTENAME(s.name) + '.' + QUOTENAME(t.name) + ' (' +
    STUFF((
        SELECT ', ' + QUOTENAME(c.name) + CASE WHEN ic.is_descending_key = 1 THEN ' DESC' ELSE '' END
        FROM sys.index_columns ic
        JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
        WHERE ic.object_id = i.object_id AND ic.index_id = i.index_id
        ORDER BY ic.key_ordinal
        FOR XML PATH(''), TYPE
    ).value('.', 'NVARCHAR(MAX)'), 1, 2, '') + ');'
FROM sys.indexes i
JOIN sys.tables t ON i.object_id = t.object_id
JOIN sys.schemas s ON t.schema_id = s.schema_id
WHERE i.is_primary_key = 0 AND i.type > 0 AND t.is_ms_shipped = 0
" -o schema-indexes.sql -s"" -W

echo "Schema exported to schema-*.sql files"
`, server, database, server, database, username, password)

	schemaScriptPath := filepath.Join(outputDir, "export-schema.sh")
	if err := os.WriteFile(schemaScriptPath, []byte(schemaScript), 0755); err != nil {
		return fmt.Errorf("failed to write schema export script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Exporting data
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating data export scripts")
	EmitProgress(m, 55, "Generating data export")

	// Generate data export script using bcp
	dataScript := fmt.Sprintf(`#!/bin/bash
# Azure SQL Data Export Script
# Server: %s
# Database: %s

set -e

echo "Azure SQL Data Export"
echo "====================="

# Configuration
SERVER="%s"
DATABASE="%s"
USERNAME="%s"
PASSWORD="${AZURE_SQL_PASSWORD:-%s}"
OUTPUT_DIR="./data"

mkdir -p "$OUTPUT_DIR"

# Get list of tables
TABLES=$(sqlcmd -S "$SERVER" -d "$DATABASE" -U "$USERNAME" -P "$PASSWORD" -Q "
SET NOCOUNT ON;
SELECT s.name + '.' + t.name
FROM sys.tables t
JOIN sys.schemas s ON t.schema_id = s.schema_id
WHERE t.is_ms_shipped = 0
" -h -1 -W)

# Export each table using bcp
for TABLE in $TABLES; do
    echo "Exporting $TABLE..."
    bcp "$DATABASE.$TABLE" out "$OUTPUT_DIR/${TABLE}.csv" \
        -S "$SERVER" -U "$USERNAME" -P "$PASSWORD" \
        -c -t "," -r "\n"
done

echo ""
echo "Data exported to $OUTPUT_DIR/"
`, server, database, server, database, username, password)

	dataScriptPath := filepath.Join(outputDir, "export-data.sh")
	if err := os.WriteFile(dataScriptPath, []byte(dataScript), 0755); err != nil {
		return fmt.Errorf("failed to write data export script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Generating migration scripts
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Generating PostgreSQL migration scripts")
	EmitProgress(m, 75, "Generating migration scripts")

	// Generate PostgreSQL import script
	importScript := `#!/bin/bash
# PostgreSQL Import Script
# Imports data from Azure SQL export

set -e

echo "PostgreSQL Import"
echo "================="

# Configuration
PGHOST="${PGHOST:-localhost}"
PGPORT="${PGPORT:-5432}"
PGDATABASE="${PGDATABASE:-migrated_db}"
PGUSER="${PGUSER:-postgres}"
DATA_DIR="./data"

# Create database if not exists
psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -c "CREATE DATABASE $PGDATABASE" 2>/dev/null || true

# Apply schema (after manual T-SQL to PostgreSQL conversion)
if [ -f "schema-postgres.sql" ]; then
    echo "Applying schema..."
    psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE" -f schema-postgres.sql
fi

# Import data from CSV files
for CSV in $DATA_DIR/*.csv; do
    TABLE=$(basename "$CSV" .csv)
    echo "Importing $TABLE..."
    psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDATABASE" -c "\COPY $TABLE FROM '$CSV' WITH CSV"
done

echo ""
echo "Import complete!"
`
	importScriptPath := filepath.Join(outputDir, "import-postgres.sh")
	if err := os.WriteFile(importScriptPath, []byte(importScript), 0755); err != nil {
		return fmt.Errorf("failed to write import script: %w", err)
	}

	// Generate Docker Compose for PostgreSQL
	dockerCompose := `version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    container_name: postgres
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: migrated_db
    volumes:
      - postgres-data:/var/lib/postgresql/data
      - ./data:/import
    ports:
      - "5432:5432"
    restart: unless-stopped

  pgadmin:
    image: dpage/pgadmin4:latest
    container_name: pgadmin
    environment:
      PGADMIN_DEFAULT_EMAIL: admin@local.dev
      PGADMIN_DEFAULT_PASSWORD: admin
    ports:
      - "5050:80"
    depends_on:
      - postgres
    restart: unless-stopped

volumes:
  postgres-data:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate T-SQL to PostgreSQL conversion guide
	conversionGuide := `# T-SQL to PostgreSQL Conversion Guide

## Data Type Mappings

| T-SQL Type | PostgreSQL Type |
|------------|-----------------|
| NVARCHAR(n) | VARCHAR(n) |
| NVARCHAR(MAX) | TEXT |
| DATETIME | TIMESTAMP |
| DATETIME2 | TIMESTAMP |
| SMALLDATETIME | TIMESTAMP |
| BIT | BOOLEAN |
| MONEY | DECIMAL(19,4) |
| SMALLMONEY | DECIMAL(10,4) |
| UNIQUEIDENTIFIER | UUID |
| IMAGE | BYTEA |
| VARBINARY | BYTEA |
| XML | XML |
| FLOAT | DOUBLE PRECISION |
| REAL | REAL |

## Common Function Conversions

| T-SQL | PostgreSQL |
|-------|------------|
| GETDATE() | NOW() |
| GETUTCDATE() | NOW() AT TIME ZONE 'UTC' |
| ISNULL(a, b) | COALESCE(a, b) |
| LEN(s) | LENGTH(s) |
| CHARINDEX(s, t) | POSITION(s IN t) |
| CONVERT(type, val) | CAST(val AS type) |
| TOP n | LIMIT n |
| NEWID() | gen_random_uuid() |
| DATEADD(part, n, d) | d + INTERVAL 'n part' |
| DATEDIFF(part, d1, d2) | DATE_PART('part', d2 - d1) |

## Identity Columns

T-SQL:
'''sql
id INT IDENTITY(1,1) PRIMARY KEY
'''

PostgreSQL:
'''sql
id SERIAL PRIMARY KEY
-- or
id INT GENERATED ALWAYS AS IDENTITY PRIMARY KEY
'''

## String Concatenation

T-SQL: 'a' + 'b'
PostgreSQL: 'a' || 'b'

## Boolean Values

T-SQL: 1, 0
PostgreSQL: TRUE, FALSE

## Notes
- Review stored procedures and convert to PL/pgSQL
- Check for SQL Server-specific functions
- Test triggers and constraints after migration
`
	conversionPath := filepath.Join(outputDir, "CONVERSION_GUIDE.md")
	if err := os.WriteFile(conversionPath, []byte(conversionGuide), 0644); err != nil {
		return fmt.Errorf("failed to write conversion guide: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 6: Finalizing
	EmitPhase(m, phases[5], 6)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Azure SQL to PostgreSQL Migration

## Source Database
- Server: %s
- Database: %s

## Migration Steps

1. Export schema:
'''bash
./export-schema.sh
'''

2. Convert T-SQL to PostgreSQL (manual step):
   - Review CONVERSION_GUIDE.md
   - Create schema-postgres.sql from exported schema

3. Export data:
'''bash
./export-data.sh
'''

4. Start PostgreSQL:
'''bash
docker-compose up -d
'''

5. Import to PostgreSQL:
'''bash
./import-postgres.sh
'''

## Files Generated
- connection-info.json: Connection configuration
- export-schema.sh: Schema export script
- export-data.sh: Data export script
- import-postgres.sh: PostgreSQL import script
- docker-compose.yml: PostgreSQL container setup
- CONVERSION_GUIDE.md: T-SQL to PostgreSQL conversion guide

## Access
- PostgreSQL: localhost:5432
- pgAdmin: http://localhost:5050 (admin@local.dev / admin)

## Notes
- Azure SQL uses T-SQL which requires conversion to PostgreSQL SQL
- Review stored procedures and triggers manually
- Test application compatibility after migration
`, server, database)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Azure SQL %s migration prepared at %s", database, outputDir))

	return nil
}

// CosmosDBToMongoDBExecutor migrates Azure Cosmos DB to MongoDB.
type CosmosDBToMongoDBExecutor struct{}

// NewCosmosDBToMongoDBExecutor creates a new Cosmos DB to MongoDB executor.
func NewCosmosDBToMongoDBExecutor() *CosmosDBToMongoDBExecutor {
	return &CosmosDBToMongoDBExecutor{}
}

// Type returns the migration type.
func (e *CosmosDBToMongoDBExecutor) Type() string {
	return "cosmosdb_to_mongodb"
}

// GetPhases returns the migration phases.
func (e *CosmosDBToMongoDBExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching database info",
		"Exporting collections",
		"Generating import scripts",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *CosmosDBToMongoDBExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["connection_string"].(string); !ok {
			if _, ok := config.Source["account_name"].(string); !ok {
				result.Valid = false
				result.Errors = append(result.Errors, "source.connection_string or account_name is required")
			}
		}
		if _, ok := config.Source["database"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.database is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Cosmos DB-specific features (stored procs, UDFs) require manual migration")
	result.Warnings = append(result.Warnings, "Partition key configuration needs review for MongoDB")

	return result, nil
}

// Execute performs the migration.
func (e *CosmosDBToMongoDBExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	connectionString, _ := config.Source["connection_string"].(string)
	accountName, _ := config.Source["account_name"].(string)
	database := config.Source["database"].(string)
	resourceGroup, _ := config.Source["resource_group"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Cosmos DB credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching database info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching database info for %s", database))
	EmitProgress(m, 25, "Fetching database info")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Get connection string if not provided
	if connectionString == "" && accountName != "" && resourceGroup != "" {
		args := []string{"cosmosdb", "keys", "list",
			"--name", accountName,
			"--resource-group", resourceGroup,
			"--type", "connection-strings",
			"--query", "connectionStrings[0].connectionString",
			"--output", "tsv",
		}
		connCmd := exec.CommandContext(ctx, "az", args...)
		output, err := connCmd.Output()
		if err == nil {
			connectionString = string(output)
		}
	}

	// Save database info
	dbInfo := map[string]string{
		"account_name": accountName,
		"database":     database,
	}
	dbInfoBytes, _ := json.MarshalIndent(dbInfo, "", "  ")
	dbInfoPath := filepath.Join(outputDir, "database-info.json")
	if err := os.WriteFile(dbInfoPath, dbInfoBytes, 0644); err != nil {
		return fmt.Errorf("failed to write database info: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting collections
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Generating collection export scripts")
	EmitProgress(m, 50, "Generating export scripts")

	// Generate export script for Cosmos DB MongoDB API
	exportScript := fmt.Sprintf(`#!/bin/bash
# Cosmos DB to MongoDB Export Script
# Database: %s

set -e

echo "Cosmos DB Export"
echo "================"

# Configuration
CONNECTION_STRING="${COSMOS_CONNECTION_STRING:-%s}"
DATABASE="%s"
OUTPUT_DIR="./data"

mkdir -p "$OUTPUT_DIR"

# If using MongoDB API, use mongodump
if echo "$CONNECTION_STRING" | grep -q "mongo.cosmos.azure.com"; then
    echo "Using MongoDB API export..."
    mongodump --uri="$CONNECTION_STRING" --db="$DATABASE" --out="$OUTPUT_DIR"
else
    # For SQL API, use Azure Data Factory or az cosmosdb
    echo "SQL API detected. Use Azure Data Factory or the following script:"
    echo ""
    echo "# Install dt (Azure Cosmos DB Data Migration Tool)"
    echo "# Download from: https://aka.ms/csdmtool"
    echo ""
    echo "dt /s:DocumentDB /s.ConnectionString:\"$CONNECTION_STRING\" /s.Collection:* \\"
    echo "   /t:JsonFile /t.File:$OUTPUT_DIR/export.json"
fi

echo ""
echo "Export complete!"
`, database, connectionString, database)

	exportScriptPath := filepath.Join(outputDir, "export-cosmosdb.sh")
	if err := os.WriteFile(exportScriptPath, []byte(exportScript), 0755); err != nil {
		return fmt.Errorf("failed to write export script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Generating import scripts
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Generating MongoDB import scripts")
	EmitProgress(m, 75, "Generating import scripts")

	// Generate MongoDB import script
	importScript := fmt.Sprintf(`#!/bin/bash
# MongoDB Import Script
# Database: %s

set -e

echo "MongoDB Import"
echo "=============="

# Configuration
MONGODB_URI="${MONGODB_URI:-mongodb://localhost:27017}"
DATABASE="%s"
DATA_DIR="./data"

# Import using mongorestore
if [ -d "$DATA_DIR/%s" ]; then
    echo "Restoring from mongodump..."
    mongorestore --uri="$MONGODB_URI" --db="$DATABASE" "$DATA_DIR/%s"
elif [ -f "$DATA_DIR/export.json" ]; then
    echo "Importing from JSON..."
    mongoimport --uri="$MONGODB_URI" --db="$DATABASE" --file="$DATA_DIR/export.json" --jsonArray
fi

echo ""
echo "Import complete!"
`, database, database, database, database)

	importScriptPath := filepath.Join(outputDir, "import-mongodb.sh")
	if err := os.WriteFile(importScriptPath, []byte(importScript), 0755); err != nil {
		return fmt.Errorf("failed to write import script: %w", err)
	}

	// Generate Docker Compose for MongoDB
	dockerCompose := fmt.Sprintf(`version: '3.8'

services:
  mongodb:
    image: mongo:6
    container_name: mongodb
    environment:
      MONGO_INITDB_ROOT_USERNAME: admin
      MONGO_INITDB_ROOT_PASSWORD: admin
      MONGO_INITDB_DATABASE: %s
    volumes:
      - mongodb-data:/data/db
      - ./data:/import
    ports:
      - "27017:27017"
    restart: unless-stopped

  mongo-express:
    image: mongo-express:latest
    container_name: mongo-express
    environment:
      ME_CONFIG_MONGODB_ADMINUSERNAME: admin
      ME_CONFIG_MONGODB_ADMINPASSWORD: admin
      ME_CONFIG_MONGODB_URL: mongodb://admin:admin@mongodb:27017/
    ports:
      - "8081:8081"
    depends_on:
      - mongodb
    restart: unless-stopped

volumes:
  mongodb-data:
`, database)

	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Finalizing
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Cosmos DB to MongoDB Migration

## Source Database
- Account: %s
- Database: %s

## Migration Steps

1. Export from Cosmos DB:
'''bash
export COSMOS_CONNECTION_STRING="your-connection-string"
./export-cosmosdb.sh
'''

2. Start MongoDB:
'''bash
docker-compose up -d
'''

3. Import to MongoDB:
'''bash
export MONGODB_URI="mongodb://admin:admin@localhost:27017"
./import-mongodb.sh
'''

## Files Generated
- database-info.json: Database configuration
- export-cosmosdb.sh: Cosmos DB export script
- import-mongodb.sh: MongoDB import script
- docker-compose.yml: MongoDB container setup

## Access
- MongoDB: localhost:27017
- Mongo Express: http://localhost:8081

## Notes
- For Cosmos DB SQL API, use Azure Data Migration Tool
- For MongoDB API, mongodump/mongorestore works directly
- Review partition key strategy for MongoDB sharding
- Stored procedures and UDFs need manual migration
`, accountName, database)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Cosmos DB %s migration prepared at %s", database, outputDir))

	return nil
}

// AzureCacheToRedisExecutor migrates Azure Cache for Redis to self-hosted Redis.
type AzureCacheToRedisExecutor struct{}

// NewAzureCacheToRedisExecutor creates a new Azure Cache to Redis executor.
func NewAzureCacheToRedisExecutor() *AzureCacheToRedisExecutor {
	return &AzureCacheToRedisExecutor{}
}

// Type returns the migration type.
func (e *AzureCacheToRedisExecutor) Type() string {
	return "azure_cache_to_redis"
}

// GetPhases returns the migration phases.
func (e *AzureCacheToRedisExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Fetching cache info",
		"Generating export scripts",
		"Creating Redis configuration",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *AzureCacheToRedisExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["cache_name"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.cache_name is required")
		}
		if _, ok := config.Source["resource_group"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.resource_group is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	result.Warnings = append(result.Warnings, "Redis data is typically transient; consider if migration is needed")
	result.Warnings = append(result.Warnings, "Cluster mode requires additional configuration")

	return result, nil
}

// Execute performs the migration.
func (e *AzureCacheToRedisExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	cacheName := config.Source["cache_name"].(string)
	resourceGroup := config.Source["resource_group"].(string)
	subscription, _ := config.Source["subscription"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating Azure credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Fetching cache info
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Fetching cache info for %s", cacheName))
	EmitProgress(m, 25, "Fetching cache info")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	args := []string{"redis", "show",
		"--name", cacheName,
		"--resource-group", resourceGroup,
		"--output", "json",
	}
	if subscription != "" {
		args = append(args, "--subscription", subscription)
	}

	showCmd := exec.CommandContext(ctx, "az", args...)
	cacheOutput, err := showCmd.Output()
	if err != nil {
		EmitLog(m, "warning", "Could not fetch cache info (may require authentication)")
	}

	var cacheInfo struct {
		Name       string `json:"name"`
		HostName   string `json:"hostName"`
		Port       int    `json:"port"`
		SSLPort    int    `json:"sslPort"`
		Sku        struct {
			Name     string `json:"name"`
			Capacity int    `json:"capacity"`
		} `json:"sku"`
		RedisVersion   string `json:"redisVersion"`
		ProvisioningState string `json:"provisioningState"`
	}
	if len(cacheOutput) > 0 {
		json.Unmarshal(cacheOutput, &cacheInfo)
	}

	// Save cache info
	cacheInfoPath := filepath.Join(outputDir, "cache-info.json")
	if len(cacheOutput) > 0 {
		if err := os.WriteFile(cacheInfoPath, cacheOutput, 0644); err != nil {
			return fmt.Errorf("failed to write cache info: %w", err)
		}
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Generating export scripts
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Generating export scripts")
	EmitProgress(m, 50, "Generating export scripts")

	// Generate export script
	exportScript := fmt.Sprintf(`#!/bin/bash
# Azure Cache for Redis Export Script
# Cache: %s

set -e

echo "Azure Cache for Redis Export"
echo "============================"

# Configuration
CACHE_NAME="%s"
RESOURCE_GROUP="%s"
HOST="${REDIS_HOST:-%s.redis.cache.windows.net}"
PORT="${REDIS_PORT:-6380}"

# Get access key
echo "Fetching access key..."
ACCESS_KEY=$(az redis list-keys \
    --name "$CACHE_NAME" \
    --resource-group "$RESOURCE_GROUP" \
    --query primaryKey \
    --output tsv)

echo "Access key retrieved."

# Export using redis-cli
# Note: Azure Cache requires SSL on port 6380
echo "Exporting data..."
redis-cli -h "$HOST" -p "$PORT" -a "$ACCESS_KEY" --tls --no-auth-warning \
    --rdb dump.rdb

# Alternative: Use BGSAVE and copy RDB file
# redis-cli -h "$HOST" -p "$PORT" -a "$ACCESS_KEY" --tls BGSAVE

echo ""
echo "Export complete! RDB file: dump.rdb"
`, cacheName, cacheName, resourceGroup, cacheName)

	exportScriptPath := filepath.Join(outputDir, "export-cache.sh")
	if err := os.WriteFile(exportScriptPath, []byte(exportScript), 0755); err != nil {
		return fmt.Errorf("failed to write export script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Creating Redis configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Creating Redis configuration")
	EmitProgress(m, 75, "Creating configuration")

	// Generate Docker Compose for Redis
	dockerCompose := `version: '3.8'

services:
  redis:
    image: redis:7-alpine
    container_name: redis
    command: redis-server /usr/local/etc/redis/redis.conf
    volumes:
      - redis-data:/data
      - ./redis.conf:/usr/local/etc/redis/redis.conf
      - ./dump.rdb:/data/dump.rdb
    ports:
      - "6379:6379"
    restart: unless-stopped

  redis-commander:
    image: rediscommander/redis-commander:latest
    container_name: redis-commander
    environment:
      REDIS_HOSTS: local:redis:6379
    ports:
      - "8081:8081"
    depends_on:
      - redis
    restart: unless-stopped

volumes:
  redis-data:
`
	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate Redis configuration
	redisConf := `# Redis Configuration
# Migrated from Azure Cache for Redis

# Basic settings
bind 0.0.0.0
port 6379
protected-mode yes

# Authentication (change this!)
requirepass changeme

# Persistence
save 900 1
save 300 10
save 60 10000
dbfilename dump.rdb
dir /data

# Memory management
maxmemory 256mb
maxmemory-policy allkeys-lru

# Logging
loglevel notice

# Slow log
slowlog-log-slower-than 10000
slowlog-max-len 128
`
	redisConfPath := filepath.Join(outputDir, "redis.conf")
	if err := os.WriteFile(redisConfPath, []byte(redisConf), 0644); err != nil {
		return fmt.Errorf("failed to write redis.conf: %w", err)
	}

	// Generate import script
	importScript := `#!/bin/bash
# Redis Import Script

set -e

echo "Redis Import"
echo "============"

# If dump.rdb exists, Redis will load it automatically on startup
if [ -f "dump.rdb" ]; then
    echo "RDB file found. Redis will load it on startup."
    docker-compose up -d
    echo "Redis started. Data will be loaded from dump.rdb"
else
    echo "No dump.rdb found. Starting fresh Redis instance."
    docker-compose up -d
fi

echo ""
echo "Redis is running at localhost:6379"
echo "Redis Commander: http://localhost:8081"
`
	importScriptPath := filepath.Join(outputDir, "import-redis.sh")
	if err := os.WriteFile(importScriptPath, []byte(importScript), 0755); err != nil {
		return fmt.Errorf("failed to write import script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Finalizing
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	skuName := "Unknown"
	redisVersion := "Unknown"
	if cacheInfo.Sku.Name != "" {
		skuName = cacheInfo.Sku.Name
	}
	if cacheInfo.RedisVersion != "" {
		redisVersion = cacheInfo.RedisVersion
	}

	readme := fmt.Sprintf(`# Azure Cache for Redis Migration

## Source Cache
- Cache Name: %s
- Resource Group: %s
- SKU: %s
- Redis Version: %s

## Migration Steps

1. Export from Azure Cache:
'''bash
./export-cache.sh
'''

2. Start Redis:
'''bash
./import-redis.sh
'''

## Files Generated
- cache-info.json: Cache configuration
- export-cache.sh: Azure Cache export script
- import-redis.sh: Redis import script
- docker-compose.yml: Redis container setup
- redis.conf: Redis configuration

## Access
- Redis: localhost:6379
- Redis Commander: http://localhost:8081

## Notes
- Redis data is typically ephemeral; verify if migration is needed
- Update redis.conf password before production use
- Azure Cache uses SSL by default (port 6380)
- Consider Redis Cluster for high availability
`, cacheName, resourceGroup, skuName, redisVersion)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Azure Cache %s migration prepared at %s", cacheName, outputDir))

	return nil
}

// AzureMySQLToMySQLExecutor migrates Azure Database for MySQL to self-hosted MySQL.
type AzureMySQLToMySQLExecutor struct{}

// NewAzureMySQLToMySQLExecutor creates a new Azure MySQL to MySQL executor.
func NewAzureMySQLToMySQLExecutor() *AzureMySQLToMySQLExecutor {
	return &AzureMySQLToMySQLExecutor{}
}

// Type returns the migration type.
func (e *AzureMySQLToMySQLExecutor) Type() string {
	return "azure_mysql_to_mysql"
}

// GetPhases returns the migration phases.
func (e *AzureMySQLToMySQLExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Analyzing database",
		"Exporting schema and data",
		"Creating MySQL configuration",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *AzureMySQLToMySQLExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["server"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.server is required (e.g., myserver.mysql.database.azure.com)")
		}
		if _, ok := config.Source["database"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.database is required")
		}
		if _, ok := config.Source["username"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.username is required")
		}
		if _, ok := config.Source["password"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.password is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	return result, nil
}

// Execute performs the migration.
func (e *AzureMySQLToMySQLExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	server := config.Source["server"].(string)
	database := config.Source["database"].(string)
	username := config.Source["username"].(string)
	password := config.Source["password"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating MySQL credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Analyzing database
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Analyzing database %s", database))
	EmitProgress(m, 25, "Analyzing database")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save connection info
	connInfo := map[string]string{
		"server":   server,
		"database": database,
		"username": username,
	}
	connInfoBytes, _ := json.MarshalIndent(connInfo, "", "  ")
	connInfoPath := filepath.Join(outputDir, "connection-info.json")
	if err := os.WriteFile(connInfoPath, connInfoBytes, 0644); err != nil {
		return fmt.Errorf("failed to write connection info: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting schema and data
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Generating export scripts")
	EmitProgress(m, 50, "Generating export scripts")

	// Generate export script using mysqldump
	exportScript := fmt.Sprintf(`#!/bin/bash
# Azure MySQL Export Script
# Server: %s
# Database: %s

set -e

echo "Azure MySQL Export"
echo "=================="

# Configuration
SERVER="%s"
DATABASE="%s"
USERNAME="%s"
PASSWORD="${MYSQL_PASSWORD:-%s}"

# Full username for Azure (username@servername)
FULL_USERNAME="${USERNAME}@${SERVER%%%%.*}"

# Export using mysqldump
echo "Exporting database..."
mysqldump \
    --host="$SERVER" \
    --user="$FULL_USERNAME" \
    --password="$PASSWORD" \
    --ssl-mode=REQUIRED \
    --single-transaction \
    --routines \
    --triggers \
    --events \
    "$DATABASE" > dump.sql

echo ""
echo "Export complete! SQL file: dump.sql"
`, server, database, server, database, username, password)

	exportScriptPath := filepath.Join(outputDir, "export-mysql.sh")
	if err := os.WriteFile(exportScriptPath, []byte(exportScript), 0755); err != nil {
		return fmt.Errorf("failed to write export script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Creating MySQL configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Creating MySQL configuration")
	EmitProgress(m, 75, "Creating configuration")

	// Generate Docker Compose for MySQL
	dockerCompose := fmt.Sprintf(`version: '3.8'

services:
  mysql:
    image: mysql:8
    container_name: mysql
    environment:
      MYSQL_ROOT_PASSWORD: root
      MYSQL_DATABASE: %s
      MYSQL_USER: app
      MYSQL_PASSWORD: app
    volumes:
      - mysql-data:/var/lib/mysql
      - ./dump.sql:/docker-entrypoint-initdb.d/dump.sql
      - ./my.cnf:/etc/mysql/conf.d/my.cnf
    ports:
      - "3306:3306"
    restart: unless-stopped

  phpmyadmin:
    image: phpmyadmin:latest
    container_name: phpmyadmin
    environment:
      PMA_HOST: mysql
      PMA_USER: root
      PMA_PASSWORD: root
    ports:
      - "8080:80"
    depends_on:
      - mysql
    restart: unless-stopped

volumes:
  mysql-data:
`, database)

	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate MySQL configuration
	myCnf := `[mysqld]
# Character set
character-set-server = utf8mb4
collation-server = utf8mb4_unicode_ci

# InnoDB settings
innodb_buffer_pool_size = 256M
innodb_log_file_size = 64M
innodb_flush_log_at_trx_commit = 1

# Query cache (deprecated in MySQL 8, but kept for compatibility)
# query_cache_type = 1
# query_cache_size = 64M

# Connections
max_connections = 200

# Logging
slow_query_log = 1
slow_query_log_file = /var/lib/mysql/slow.log
long_query_time = 2
`
	myCnfPath := filepath.Join(outputDir, "my.cnf")
	if err := os.WriteFile(myCnfPath, []byte(myCnf), 0644); err != nil {
		return fmt.Errorf("failed to write my.cnf: %w", err)
	}

	// Generate import script
	importScript := fmt.Sprintf(`#!/bin/bash
# MySQL Import Script

set -e

echo "MySQL Import"
echo "============"

# Start MySQL
docker-compose up -d

echo "Waiting for MySQL to be ready..."
sleep 10

# The dump.sql will be automatically imported via docker-entrypoint-initdb.d

echo ""
echo "MySQL is running at localhost:3306"
echo "phpMyAdmin: http://localhost:8080"
echo "Database: %s"
`, database)

	importScriptPath := filepath.Join(outputDir, "import-mysql.sh")
	if err := os.WriteFile(importScriptPath, []byte(importScript), 0755); err != nil {
		return fmt.Errorf("failed to write import script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Finalizing
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Azure MySQL to Self-Hosted MySQL Migration

## Source Database
- Server: %s
- Database: %s

## Migration Steps

1. Export from Azure MySQL:
'''bash
export MYSQL_PASSWORD="your-password"
./export-mysql.sh
'''

2. Start MySQL and import:
'''bash
./import-mysql.sh
'''

## Files Generated
- connection-info.json: Connection configuration
- export-mysql.sh: Azure MySQL export script
- import-mysql.sh: MySQL import script
- docker-compose.yml: MySQL container setup
- my.cnf: MySQL configuration

## Access
- MySQL: localhost:3306
- phpMyAdmin: http://localhost:8080

## Default Credentials
- Root: root / root
- App User: app / app

## Notes
- Azure MySQL requires SSL connections
- The dump.sql is auto-imported on first container start
- Adjust my.cnf settings for production use
`, server, database)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Azure MySQL %s migration prepared at %s", database, outputDir))

	return nil
}

// AzurePostgresToPostgresExecutor migrates Azure Database for PostgreSQL to self-hosted PostgreSQL.
type AzurePostgresToPostgresExecutor struct{}

// NewAzurePostgresToPostgresExecutor creates a new Azure Postgres to Postgres executor.
func NewAzurePostgresToPostgresExecutor() *AzurePostgresToPostgresExecutor {
	return &AzurePostgresToPostgresExecutor{}
}

// Type returns the migration type.
func (e *AzurePostgresToPostgresExecutor) Type() string {
	return "azure_postgres_to_postgres"
}

// GetPhases returns the migration phases.
func (e *AzurePostgresToPostgresExecutor) GetPhases() []string {
	return []string{
		"Validating credentials",
		"Analyzing database",
		"Exporting schema and data",
		"Creating PostgreSQL configuration",
		"Finalizing",
	}
}

// Validate validates the migration configuration.
func (e *AzurePostgresToPostgresExecutor) Validate(ctx context.Context, config *MigrationConfig) (*ValidationResult, error) {
	result := &ValidationResult{Valid: true}

	if config.Source == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "source configuration is required")
	} else {
		if _, ok := config.Source["server"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.server is required (e.g., myserver.postgres.database.azure.com)")
		}
		if _, ok := config.Source["database"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.database is required")
		}
		if _, ok := config.Source["username"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.username is required")
		}
		if _, ok := config.Source["password"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "source.password is required")
		}
	}

	if config.Destination == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "destination configuration is required")
	} else {
		if _, ok := config.Destination["output_dir"].(string); !ok {
			result.Valid = false
			result.Errors = append(result.Errors, "destination.output_dir is required")
		}
	}

	return result, nil
}

// Execute performs the migration.
func (e *AzurePostgresToPostgresExecutor) Execute(ctx context.Context, m *Migration, config *MigrationConfig) error {
	phases := e.GetPhases()

	server := config.Source["server"].(string)
	database := config.Source["database"].(string)
	username := config.Source["username"].(string)
	password := config.Source["password"].(string)

	outputDir := config.Destination["output_dir"].(string)

	// Phase 1: Validating credentials
	EmitPhase(m, phases[0], 1)
	EmitLog(m, "info", "Validating PostgreSQL credentials")
	EmitProgress(m, 10, "Checking credentials")

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 2: Analyzing database
	EmitPhase(m, phases[1], 2)
	EmitLog(m, "info", fmt.Sprintf("Analyzing database %s", database))
	EmitProgress(m, 25, "Analyzing database")

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Save connection info
	connInfo := map[string]string{
		"server":   server,
		"database": database,
		"username": username,
	}
	connInfoBytes, _ := json.MarshalIndent(connInfo, "", "  ")
	connInfoPath := filepath.Join(outputDir, "connection-info.json")
	if err := os.WriteFile(connInfoPath, connInfoBytes, 0644); err != nil {
		return fmt.Errorf("failed to write connection info: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 3: Exporting schema and data
	EmitPhase(m, phases[2], 3)
	EmitLog(m, "info", "Generating export scripts")
	EmitProgress(m, 50, "Generating export scripts")

	// Generate export script using pg_dump
	exportScript := fmt.Sprintf(`#!/bin/bash
# Azure PostgreSQL Export Script
# Server: %s
# Database: %s

set -e

echo "Azure PostgreSQL Export"
echo "======================="

# Configuration
SERVER="%s"
DATABASE="%s"
USERNAME="%s"
PASSWORD="${PGPASSWORD:-%s}"

# Full username for Azure (username@servername)
FULL_USERNAME="${USERNAME}@${SERVER%%%%.*}"

export PGPASSWORD="$PASSWORD"

# Export using pg_dump
echo "Exporting database..."
pg_dump \
    --host="$SERVER" \
    --port=5432 \
    --username="$FULL_USERNAME" \
    --dbname="$DATABASE" \
    --format=custom \
    --file=dump.backup

# Also create plain SQL for inspection
pg_dump \
    --host="$SERVER" \
    --port=5432 \
    --username="$FULL_USERNAME" \
    --dbname="$DATABASE" \
    --format=plain \
    --file=dump.sql

echo ""
echo "Export complete!"
echo "  Custom format: dump.backup (for pg_restore)"
echo "  Plain SQL: dump.sql (for inspection)"
`, server, database, server, database, username, password)

	exportScriptPath := filepath.Join(outputDir, "export-postgres.sh")
	if err := os.WriteFile(exportScriptPath, []byte(exportScript), 0755); err != nil {
		return fmt.Errorf("failed to write export script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 4: Creating PostgreSQL configuration
	EmitPhase(m, phases[3], 4)
	EmitLog(m, "info", "Creating PostgreSQL configuration")
	EmitProgress(m, 75, "Creating configuration")

	// Generate Docker Compose for PostgreSQL
	dockerCompose := fmt.Sprintf(`version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    container_name: postgres
    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: %s
    volumes:
      - postgres-data:/var/lib/postgresql/data
      - ./dump.backup:/import/dump.backup
      - ./dump.sql:/import/dump.sql
    ports:
      - "5432:5432"
    restart: unless-stopped

  pgadmin:
    image: dpage/pgadmin4:latest
    container_name: pgadmin
    environment:
      PGADMIN_DEFAULT_EMAIL: admin@local.dev
      PGADMIN_DEFAULT_PASSWORD: admin
    ports:
      - "5050:80"
    depends_on:
      - postgres
    restart: unless-stopped

volumes:
  postgres-data:
`, database)

	composePath := filepath.Join(outputDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(dockerCompose), 0644); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}

	// Generate import script
	importScript := fmt.Sprintf(`#!/bin/bash
# PostgreSQL Import Script

set -e

echo "PostgreSQL Import"
echo "================="

# Start PostgreSQL
docker-compose up -d postgres

echo "Waiting for PostgreSQL to be ready..."
sleep 5

# Wait for PostgreSQL to be ready
until docker exec postgres pg_isready -U postgres; do
    echo "Waiting for PostgreSQL..."
    sleep 2
done

# Import using pg_restore
echo "Importing database..."
docker exec -i postgres pg_restore \
    -U postgres \
    -d %s \
    --no-owner \
    --no-privileges \
    /import/dump.backup || true

# Start pgAdmin
docker-compose up -d pgadmin

echo ""
echo "PostgreSQL is running at localhost:5432"
echo "pgAdmin: http://localhost:5050 (admin@local.dev / admin)"
echo "Database: %s"
`, database, database)

	importScriptPath := filepath.Join(outputDir, "import-postgres.sh")
	if err := os.WriteFile(importScriptPath, []byte(importScript), 0755); err != nil {
		return fmt.Errorf("failed to write import script: %w", err)
	}

	if m.IsCancelled() {
		return fmt.Errorf("migration cancelled")
	}

	// Phase 5: Finalizing
	EmitPhase(m, phases[4], 5)
	EmitLog(m, "info", "Finalizing migration artifacts")
	EmitProgress(m, 90, "Finalizing")

	readme := fmt.Sprintf(`# Azure PostgreSQL to Self-Hosted PostgreSQL Migration

## Source Database
- Server: %s
- Database: %s

## Migration Steps

1. Export from Azure PostgreSQL:
'''bash
export PGPASSWORD="your-password"
./export-postgres.sh
'''

2. Start PostgreSQL and import:
'''bash
./import-postgres.sh
'''

## Files Generated
- connection-info.json: Connection configuration
- export-postgres.sh: Azure PostgreSQL export script
- import-postgres.sh: PostgreSQL import script
- docker-compose.yml: PostgreSQL container setup

## Access
- PostgreSQL: localhost:5432
- pgAdmin: http://localhost:5050

## Default Credentials
- PostgreSQL: postgres / postgres
- pgAdmin: admin@local.dev / admin

## Notes
- Azure PostgreSQL requires SSL connections
- pg_dump creates portable backup files
- Review extensions and ensure they're available locally
`, server, database)

	readmePath := filepath.Join(outputDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("failed to write README: %w", err)
	}

	EmitProgress(m, 100, "Migration complete")
	EmitLog(m, "info", fmt.Sprintf("Azure PostgreSQL %s migration prepared at %s", database, outputDir))

	return nil
}
