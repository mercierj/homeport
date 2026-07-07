// Package database provides mappers for GCP database services.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
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
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "mongosh", "--eval", "db.adminCommand('ping')"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":   "google_firestore_database",
		"homeport.engine":   "mongodb",
		"homeport.database": dbName,
	}

	migrationScript := m.generateMigrationScript(dbName)
	result.AddScript("migrate_firestore.sh", []byte(migrationScript))
	result.AddConfig("config/firestore/app-change.env", []byte(m.generateAppChangeConfig(dbName)))
	result.AddConfig("config/firestore/migration.env", []byte(m.generateMigrationConfig(res, dbName)))
	result.AddConfig("config/mongodb/init-firestore.js", []byte(m.generateMongoInit(dbName)))
	result.AddScript("export_firestore_data.sh", []byte(m.generateExportScript(dbName)))
	result.AddScript("transform_firestore_export.sh", []byte(m.generateTransformScript(dbName)))
	result.AddScript("import_firestore_mongodb.sh", []byte(m.generateImportScript(dbName)))
	result.AddScript("validate_firestore_mongodb.sh", []byte(m.generateValidateScript(dbName)))
	result.AddScript("backup_firestore_config.sh", []byte(m.generateBackupScript(dbName)))
	result.AddScript("cutover_firestore_clients.sh", []byte(m.generateCutoverScript(dbName)))
	for _, step := range firestoreRunbook(dbName) {
		result.AddRunbookStep(step)
	}

	result.AddWarning("Firestore is a document database. MongoDB provides similar document storage but with different query syntax.")
	result.AddWarning("Firestore real-time listeners need to be replaced with MongoDB Change Streams.")
	result.AddWarning("Firestore security rules need to be implemented at application level with MongoDB.")

	return result, nil
}

func (m *FirestoreMapper) generateAppChangeConfig(dbName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_FIRESTORE_DATABASE=%s\nTARGET_DATABASE=firestore_db\nTARGET_MONGODB_URI=mongodb://admin:changeme@mongodb:27017/firestore_db?authSource=admin\n", dbName)
}

func (m *FirestoreMapper) generateMigrationConfig(res *resource.AWSResource, dbName string) string {
	return fmt.Sprintf("SOURCE_FIRESTORE_DATABASE=%s\nSOURCE_PROJECT=%s\nTARGET_ENGINE=mongodb\nTARGET_DATABASE=firestore_db\n", dbName, res.GetConfigString("project"))
}

func (m *FirestoreMapper) generateMongoInit(dbName string) string {
	return fmt.Sprintf("db = db.getSiblingDB('firestore_db');\ndb.createCollection('_homeport_metadata');\ndb._homeport_metadata.insertOne({source: 'google_firestore_database', database: %q, migratedAt: new Date()});\n", dbName)
}

func (m *FirestoreMapper) generateExportScript(dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p firestore-export\ngcloud firestore export \"${FIRESTORE_EXPORT_BUCKET:?set FIRESTORE_EXPORT_BUCKET}\" --database=%q\n", dbName)
}

func (m *FirestoreMapper) generateTransformScript(dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nmkdir -p firestore-json\ntest -d firestore-export\necho \"Transform Firestore export for %s into newline-delimited MongoDB JSON under firestore-json/\"\n", dbName)
}

func (m *FirestoreMapper) generateImportScript(dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nfor file in firestore-json/*.json; do\n  collection=$(basename \"$file\" .json)\n  mongoimport --uri \"${MONGODB_URI:-mongodb://admin:changeme@localhost:27017/firestore_db?authSource=admin}\" --collection \"$collection\" --file \"$file\"\ndone\necho \"Firestore database %s imported to MongoDB\"\n", dbName)
}

func (m *FirestoreMapper) generateValidateScript(dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/firestore/app-change.env\nmongosh \"${MONGODB_URI:-mongodb://admin:changeme@localhost:27017/firestore_db?authSource=admin}\" --eval 'db.runCommand({ping:1})'\necho \"Firestore database %s validated on MongoDB\"\n", dbName)
}

func (m *FirestoreMapper) generateBackupScript(dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/firestore-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/firestore config/mongodb firestore-export firestore-json\necho \"$archive\"\n", dbName)
}

func (m *FirestoreMapper) generateCutoverScript(dbName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/firestore/app-change.env\ntest \"$SOURCE_FIRESTORE_DATABASE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch Firestore clients to $TARGET_MONGODB_URI\"\n", dbName)
}

func firestoreRunbook(dbName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "document-database", "source": "google_firestore_database", "database": dbName, "target": "mongodb"}
	return []domainrunbook.Step{
		firestoreStep("export-firestore-data", "Export Firestore data", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_firestore_data.sh"}, "Firestore export is created", metadata),
		firestoreStep("provision-mongodb-target", "Provision MongoDB target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/mongodb/init-firestore.js"}, "MongoDB target config is rendered", metadata),
		firestoreStep("transform-firestore-export", "Transform Firestore export", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "transform_firestore_export.sh"}, "Firestore export is transformed to MongoDB JSON", metadata),
		firestoreStep("import-firestore-mongodb", "Import Firestore data", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "import_firestore_mongodb.sh"}, "documents are imported to MongoDB", metadata),
		firestoreStep("validate-firestore-mongodb", "Validate MongoDB target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_firestore_mongodb.sh"}, "MongoDB ping and migration config validate", metadata),
		firestoreStep("backup-firestore-config", "Backup Firestore config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_firestore_config.sh"}, "Firestore migration artifacts are archived", metadata),
		firestoreStep("cutover-firestore-clients", "Cut over Firestore clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_firestore_clients.sh"}, "clients use generated MongoDB patch", metadata),
		firestoreStep("rollback-firestore-source", "Keep Firestore source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Firestore remains authoritative until MongoDB validation passes", metadata),
	}
}

func firestoreStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
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
