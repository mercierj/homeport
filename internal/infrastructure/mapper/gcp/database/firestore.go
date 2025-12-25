// Package database provides mappers for GCP database services.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudexit/cloudexit/internal/domain/mapper"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// FirestoreMapper converts GCP Firestore to MongoDB containers.
type FirestoreMapper struct {
	*mapper.BaseMapper
}

// NewFirestoreMapper creates a new Firestore mapper.
func NewFirestoreMapper() *FirestoreMapper {
	return &FirestoreMapper{
		BaseMapper: mapper.NewBaseMapper(resource.TypeFirestore, nil),
	}
}

// Map converts a Firestore database to a MongoDB service.
func (m *FirestoreMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}

	dbName := res.GetConfigString("name")
	if dbName == "" {
		dbName = res.Name
	}

	result := mapper.NewMappingResult("mongodb")
	svc := result.DockerService

	svc.Image = "mongo:7"
	svc.Environment = map[string]string{
		"MONGO_INITDB_ROOT_USERNAME": "admin",
		"MONGO_INITDB_ROOT_PASSWORD": "changeme",
		"MONGO_INITDB_DATABASE":      "firestore_db",
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
		"cloudexit.source":   "google_firestore_database",
		"cloudexit.engine":   "mongodb",
		"cloudexit.database": dbName,
	}

	migrationScript := m.generateMigrationScript(dbName)
	result.AddScript("migrate_firestore.sh", []byte(migrationScript))

	result.AddWarning("Firestore is a document database. MongoDB provides similar document storage but with different query syntax.")
	result.AddWarning("Firestore real-time listeners need to be replaced with MongoDB Change Streams.")
	result.AddWarning("Firestore security rules need to be implemented at application level with MongoDB.")

	result.AddManualStep("Update database credentials in docker-compose.yml")
	result.AddManualStep("Export Firestore data and transform to MongoDB format")
	result.AddManualStep("Update application code to use MongoDB driver instead of Firestore SDK")

	return result, nil
}

func (m *FirestoreMapper) generateMigrationScript(dbName string) string {
	return fmt.Sprintf(`#!/bin/bash
# Firestore to MongoDB Migration Script
set -e

echo "Firestore to MongoDB Migration"
echo "==============================="
echo "Database: %s"

echo "Step 1: Export from Firestore"
echo "  gcloud firestore export gs://BUCKET/firestore-export"

echo "Step 2: Download export"
echo "  gsutil -m cp -r gs://BUCKET/firestore-export ."

echo "Step 3: Transform data"
echo "  # Firestore exports are in a specific format"
echo "  # Use a tool like firestore-to-mongo-converter"
echo "  # Or write a custom script to transform the data"

echo "Step 4: Import to MongoDB"
echo "  mongoimport --uri 'mongodb://admin:changeme@localhost:27017' \\"
echo "    --db firestore_db --collection COLLECTION --file data.json"

echo "For more info on data migration patterns, see MongoDB documentation"
`, dbName)
}
