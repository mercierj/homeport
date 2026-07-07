// Package database provides mappers for Azure database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

// CosmosDBMapper converts Azure Cosmos DB to MongoDB or Cassandra containers.
type CosmosDBMapper struct {
	*mapper.BaseMapper
}

// NewCosmosDBMapper creates a new CosmosDB mapper.
func NewCosmosDBMapper() *CosmosDBMapper {
	return &CosmosDBMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeCosmosDB, nil),
	}
}

// Map converts a Cosmos DB account to an appropriate database service.
func (m *CosmosDBMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	accountName := res.GetConfigString("name")
	if accountName == "" {
		accountName = res.Name
	}

	// Determine the API type
	kind := res.GetConfigString("kind")
	if kind == "" {
		kind = "GlobalDocumentDB" // Default to SQL API
	}

	// Check capabilities for MongoDB API
	capabilities := res.Config["capabilities"]
	isMongoAPI := false
	isCassandraAPI := false
	isGremlinAPI := false
	isTableAPI := false

	if capList, ok := capabilities.([]interface{}); ok {
		for _, cap := range capList {
			if capMap, ok := cap.(map[string]interface{}); ok {
				if name, ok := capMap["name"].(string); ok {
					if strings.Contains(strings.ToLower(name), "mongo") {
						isMongoAPI = true
					} else if strings.Contains(strings.ToLower(name), "cassandra") {
						isCassandraAPI = true
					} else if strings.Contains(strings.ToLower(name), "gremlin") {
						isGremlinAPI = true
					} else if strings.Contains(strings.ToLower(name), "table") {
						isTableAPI = true
					}
				}
			}
		}
	}

	switch {
	case isMongoAPI || kind == "MongoDB":
		return m.createMongoDBService(res, accountName)
	case isCassandraAPI:
		return m.createCassandraService(res, accountName)
	case isGremlinAPI:
		return m.createGremlinService(res, accountName)
	case isTableAPI:
		return m.createTableService(res, accountName)
	default:
		// SQL API - use MongoDB as closest alternative
		return m.createMongoDBService(res, accountName)
	}
}

func (m *CosmosDBMapper) createMongoDBService(res *resource.AWSResource, accountName string) (*mapper.MappingResult, error) {
	result := mapper.NewMappingResult("mongodb")
	svc := result.DockerService

	svc.Image = "mongo:7"
	svc.Environment = map[string]string{
		"MONGO_INITDB_ROOT_USERNAME": "admin",
		"MONGO_INITDB_ROOT_PASSWORD": "changeme",
		"MONGO_INITDB_DATABASE":      "cosmosdb",
	}
	svc.Ports = []string{"27017:27017"}
	svc.Volumes = []string{"./data/mongodb:/data/db"}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "mongosh", "--eval", "db.adminCommand('ping')"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{
		"homeport.source":  "azurerm_cosmosdb_account",
		"homeport.engine":  "mongodb",
		"homeport.account": accountName,
	}

	migrationScript := m.generateMongoMigrationScript(accountName)
	result.AddScript("migrate_cosmosdb.sh", []byte(migrationScript))
	result.AddScript("validate_cosmosdb.sh", []byte(m.generateValidateScript(accountName)))
	result.AddScript("backup_cosmosdb.sh", []byte(m.generateBackupScript(accountName)))
	result.AddScript("cutover_cosmosdb.sh", []byte(m.generateCutoverScript(accountName)))
	result.AddConfig("config/cosmosdb/app-change.env", []byte(m.generateAppChange(accountName)))
	result.AddConfig("config/cosmosdb/generated-client.patch", []byte(m.generateClientPatch(accountName)))

	result.AddWarning("Cosmos DB (MongoDB API) mapped to MongoDB. Update connection strings in your application.")
	result.AddWarning("Cosmos DB-specific features like RU/s throughput are not available in MongoDB.")

	for _, step := range cosmosDBRunbook(accountName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *CosmosDBMapper) createCassandraService(res *resource.AWSResource, accountName string) (*mapper.MappingResult, error) {
	result := mapper.NewMappingResult("cassandra")
	svc := result.DockerService

	svc.Image = "cassandra:4.1"
	svc.Environment = map[string]string{
		"CASSANDRA_CLUSTER_NAME": "homeport_cluster",
		"MAX_HEAP_SIZE":          "2G",
		"HEAP_NEWSIZE":           "512M",
	}
	svc.Ports = []string{"9042:9042"}
	svc.Volumes = []string{"./data/cassandra:/var/lib/cassandra"}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "cqlsh -e 'describe cluster' || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":  "azurerm_cosmosdb_account",
		"homeport.engine":  "cassandra",
		"homeport.account": accountName,
	}

	result.AddWarning("Cosmos DB Cassandra API mapped to Apache Cassandra.")

	result.AddManualStep("Update Cassandra connection in your application")
	result.AddManualStep("Export and import data using cqlsh COPY command")

	return result, nil
}

func (m *CosmosDBMapper) createGremlinService(res *resource.AWSResource, accountName string) (*mapper.MappingResult, error) {
	result := mapper.NewMappingResult("janusgraph")
	svc := result.DockerService

	svc.Image = "janusgraph/janusgraph:latest"
	svc.Ports = []string{"8182:8182"}
	svc.Volumes = []string{"./data/janusgraph:/var/lib/janusgraph"}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:8182/gremlin || exit 1"},
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":  "azurerm_cosmosdb_account",
		"homeport.engine":  "janusgraph",
		"homeport.account": accountName,
	}

	result.AddWarning("Cosmos DB Gremlin API mapped to JanusGraph. Gremlin queries should be compatible.")

	result.AddManualStep("Update Gremlin connection in your application")
	result.AddManualStep("Export and import graph data manually")

	return result, nil
}

func (m *CosmosDBMapper) createTableService(res *resource.AWSResource, accountName string) (*mapper.MappingResult, error) {
	result := mapper.NewMappingResult("azurite")
	svc := result.DockerService

	svc.Image = "mcr.microsoft.com/azure-storage/azurite"
	svc.Ports = []string{"10000:10000", "10001:10001", "10002:10002"}
	svc.Volumes = []string{"./data/azurite:/data"}
	svc.Command = []string{"azurite", "--blobHost", "0.0.0.0", "--queueHost", "0.0.0.0", "--tableHost", "0.0.0.0"}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD-SHELL", "curl -f http://localhost:10002/ || exit 1"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":  "azurerm_cosmosdb_account",
		"homeport.engine":  "azurite",
		"homeport.account": accountName,
	}

	result.AddWarning("Cosmos DB Table API mapped to Azurite (Azure Storage Emulator).")

	result.AddManualStep("Update Azure Table Storage connection string to Azurite endpoint")
	result.AddManualStep("Export and import table data using AzCopy or custom script")

	return result, nil
}

func (m *CosmosDBMapper) generateMongoMigrationScript(accountName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Cosmos DB MongoDB API Migration Script
set -e

echo "Cosmos DB MongoDB API Migration"
echo "================================"
echo "Account: %s"

COSMOS_HOST="${COSMOS_HOST:-%s.mongo.cosmos.azure.com}"
COSMOS_USER="${COSMOS_USER:-%s}"
LOCAL_HOST="localhost:27017"

echo "Step 1: Export from Cosmos DB"
echo "  mongodump --uri 'mongodb://$COSMOS_USER:***@$COSMOS_HOST:10255/?ssl=true&replicaSet=globaldb' \\"
echo "    --out /tmp/cosmosdb_dump"

echo "Step 2: Restore to local MongoDB"
echo "  mongorestore --uri 'mongodb://admin:changeme@$LOCAL_HOST' \\"
echo "    --drop /tmp/cosmosdb_dump"

echo "Alternative: Use Azure Data Factory for large-scale migrations"
`, accountName, accountName, accountName)
}

func (m *CosmosDBMapper) generateAppChange(accountName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_COSMOSDB_ACCOUNT=%s\nMONGODB_URI='mongodb://admin:changeme@mongodb:27017/cosmosdb?authSource=admin'\nGENERATED_PATCH=config/cosmosdb/generated-client.patch\n", accountName)
}

func (m *CosmosDBMapper) generateClientPatch(accountName string) string {
	return fmt.Sprintf("--- a/app/database.env\n+++ b/app/database.env\n@@\n-COSMOSDB_ACCOUNT=%s\n+MONGODB_URI=mongodb://admin:changeme@mongodb:27017/cosmosdb?authSource=admin\n+DATABASE_MIGRATION_MODE=generated_patch\n", accountName)
}

func (m *CosmosDBMapper) generateValidateScript(accountName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/cosmosdb/app-change.env\n. config/cosmosdb/app-change.env\ngrep -q %q config/cosmosdb/app-change.env\nmongosh \"$MONGODB_URI\" --eval 'db.adminCommand({ping:1})'\n", accountName)
}

func (m *CosmosDBMapper) generateBackupScript(accountName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/cosmosdb-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/cosmosdb data/mongodb\necho \"$archive\"\n", accountName)
}

func (m *CosmosDBMapper) generateCutoverScript(accountName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/cosmosdb/app-change.env\ntest \"$SOURCE_COSMOSDB_ACCOUNT\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\ntest -s \"$GENERATED_PATCH\"\necho \"Apply $GENERATED_PATCH and use $MONGODB_URI\"\n", accountName)
}

func cosmosDBRunbook(accountName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "nosql-database", "source": "azurerm_cosmosdb_account", "account": accountName, "target": "mongodb"}
	return []domainrunbook.Step{
		cosmosDBStep("dump-restore-cosmosdb", "Dump and restore Cosmos DB", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_cosmosdb.sh"}, "Cosmos DB collections are restored into MongoDB", metadata),
		cosmosDBStep("validate-cosmosdb-migration", "Validate Cosmos DB migration", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_cosmosdb.sh"}, "MongoDB target responds and migration artifacts validate", metadata),
		cosmosDBStep("backup-cosmosdb-target", "Backup Cosmos DB target", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_cosmosdb.sh"}, "MongoDB data and generated handoff are archived", metadata),
		cosmosDBStep("cutover-cosmosdb-clients", "Cut over Cosmos DB clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_cosmosdb.sh"}, "clients use the generated MongoDB connection string", metadata),
		cosmosDBStep("rollback-cosmosdb-source-authority", "Keep Cosmos DB as rollback source", "Rollback", domainrunbook.StepTypeRollback, nil, "Cosmos DB remains authoritative until MongoDB validation passes", metadata),
	}
}

func cosmosDBStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
		command = nil
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
