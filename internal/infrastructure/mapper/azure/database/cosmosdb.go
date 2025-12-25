// Package database provides mappers for Azure database services.
package database

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
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
	svc.Labels = map[string]string{
		"cloudexit.source":  "azurerm_cosmosdb_account",
		"cloudexit.engine":  "mongodb",
		"cloudexit.account": accountName,
	}

	migrationScript := m.generateMongoMigrationScript(accountName)
	result.AddScript("migrate_cosmosdb.sh", []byte(migrationScript))

	result.AddWarning("Cosmos DB (MongoDB API) mapped to MongoDB. Update connection strings in your application.")
	result.AddWarning("Cosmos DB-specific features like RU/s throughput are not available in MongoDB.")

	result.AddManualStep("Update MongoDB connection string in your application")
	result.AddManualStep("Export data from Cosmos DB using Azure Data Factory or mongodump")
	result.AddManualStep("Import data using mongorestore")

	return result, nil
}

func (m *CosmosDBMapper) createCassandraService(res *resource.AWSResource, accountName string) (*mapper.MappingResult, error) {
	result := mapper.NewMappingResult("cassandra")
	svc := result.DockerService

	svc.Image = "cassandra:4.1"
	svc.Environment = map[string]string{
		"CASSANDRA_CLUSTER_NAME": "cloudexit_cluster",
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
		"cloudexit.source":  "azurerm_cosmosdb_account",
		"cloudexit.engine":  "cassandra",
		"cloudexit.account": accountName,
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
		"cloudexit.source":  "azurerm_cosmosdb_account",
		"cloudexit.engine":  "janusgraph",
		"cloudexit.account": accountName,
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
		"cloudexit.source":  "azurerm_cosmosdb_account",
		"cloudexit.engine":  "azurite",
		"cloudexit.account": accountName,
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
