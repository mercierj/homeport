package datarunbook

import (
	"strings"

	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func SQL(engine, database, script string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":     "sql",
		"engine":   engine,
		"database": database,
	}
	if strings.Contains(engine, "postgres") {
		metadata["DATABASE_URL"] = "postgres://postgres:changeme@postgres:5432/" + database
	} else if strings.Contains(engine, "mysql") || strings.Contains(engine, "mariadb") {
		metadata["DATABASE_URL"] = "mysql://appuser:changeme@mysql:3306/" + database
	} else if strings.Contains(engine, "mssql") {
		metadata["DATABASE_URL"] = "sqlserver://sa:YourStrong@Passw0rd@mssql:1433?database=" + database
	}
	return []domainrunbook.Step{
		input("collect-sql-credentials", "Collect SQL credentials", "Credentials", "source and target credentials provided", metadata),
		command("validate-sql-source", "Validate source SQL connection", "Credentials", []string{"sh", "-c", "echo validate source SQL credentials"}, "source connection succeeds", metadata),
		command("dump-restore-sql", "Dump and restore small database", "Sync", []string{"sh", script}, "dump and restore completed", metadata),
		input("configure-live-sql-replication", "Configure live SQL replication", "Sync", "replication configured when supported or consciously skipped", metadata),
		command("validate-sql-migration", "Validate SQL migration", "Validate", []string{"sh", "-c", "echo validate schemas tables rows sequences extensions checksums"}, "schema count, table count, row counts, sequences, extensions, and sampled checksums match", metadata),
		command("validate-app-sql-connection", "Validate app SQL connection", "Cutover", []string{"sh", "-c", "echo validate application container database connection"}, "application container can connect before cutover", metadata),
		rollback("rollback-sql-source-authority", "Keep source SQL database authoritative", metadata),
	}
}

func Redis(name, script string, tlsRequired, ha bool) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":           "redis",
		"cache":          name,
		"REDIS_HOST":     "redis",
		"REDIS_PORT":     "6379",
		"REDIS_PASSWORD": "${REDIS_PASSWORD}",
	}
	steps := []domainrunbook.Step{
		command("generate-redis-auth", "Generate Redis auth token", "Credentials", []string{"sh", "-c", "test -s config/redis/app-change.env"}, "Redis password or token stored for target", metadata),
		command("sync-redis-data", "Sync Redis data", "Sync", []string{"sh", script}, "RDB or DUMP/RESTORE sync completed", metadata),
		command("validate-redis-migration", "Validate Redis migration", "Validate", []string{"sh", "-c", "echo validate key count types ttls streams sampled values"}, "key count, types, TTLs, streams, and sampled values match", metadata),
		rollback("rollback-redis-source-authority", "Keep source Redis authoritative", metadata),
	}
	if tlsRequired {
		steps = append([]domainrunbook.Step{command("configure-redis-tls", "Configure Redis TLS proxy", "Credentials", []string{"sh", "-c", "test -s config/redis/tls.env"}, "TLS proxy configuration generated", metadata)}, steps...)
	}
	if ha {
		steps = append(steps, command("validate-redis-failover", "Validate Redis failover", "Validate", []string{"sh", "validate_redis.sh"}, "Sentinel or cluster failover validated", metadata))
	}
	return steps
}

func DynamoDB(table string, streamsEnabled bool) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                         "dynamodb",
		"table":                        table,
		"AWS_ENDPOINT_URL_DYNAMODB":    "http://scylladb:8000",
		"AWS_ACCESS_KEY_ID":            "homeport",
		"AWS_SECRET_ACCESS_KEY":        "homeport",
		"AWS_REGION":                   "us-east-1",
		"HOMEPORT_COMPAT_BACKEND":      "scylla-alternator",
		"HOMEPORT_COMPAT_API_PROTOCOL": "dynamodb",
	}
	steps := []domainrunbook.Step{
		command("provision-scylla-alternator", "Provision Scylla Alternator", "Provision", []string{"sh", "-c", "echo provision scylla alternator"}, "Alternator endpoint is reachable", metadata),
		command("migrate-dynamodb-table", "Migrate DynamoDB table metadata", "Sync", []string{"sh", "migrate_dynamodb.sh"}, "table, indexes, and TTL migrated", metadata),
		command("validate-dynamodb-sdk", "Validate DynamoDB SDK compatibility", "Validate", []string{"sh", "-c", "echo validate DynamoDB CRUD query scan"}, "AWS SDK CRUD, query, and scan pass against Alternator", metadata),
		rollback("rollback-dynamodb-source-authority", "Keep source DynamoDB table authoritative", metadata),
	}
	if streamsEnabled {
		steps = append(steps[:2], append([]domainrunbook.Step{command("configure-dynamodb-cdc", "Configure DynamoDB Streams CDC", "Sync", []string{"sh", "-c", "test -s config/scylladb/cdc.yaml"}, "Scylla CDC config exists for stream handoff", metadata)}, steps[2:]...)...)
	}
	return steps
}

func command(id, name, group string, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             domainrunbook.StepTypeCommand,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		SuccessCondition: success,
		Command:          command,
		Metadata:         clone(metadata),
	}
}

func input(id, name, group, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             domainrunbook.StepTypeInput,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "user",
		SuccessCondition: success,
		Metadata:         clone(metadata),
	}
}

func rollback(id, name string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            "Rollback",
		Type:             domainrunbook.StepTypeRollback,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "noop",
		SuccessCondition: "source remains authoritative until cutover passes",
		Metadata:         clone(metadata),
	}
}

func clone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
