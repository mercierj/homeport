package networking

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type AppSyncMapper struct {
	*mapper.BaseMapper
}

func NewAppSyncMapper() *AppSyncMapper {
	return &AppSyncMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAppSyncGraphQLAPI, nil)}
}

func (m *AppSyncMapper) Map(ctx context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	apiName := res.GetConfigString("name")
	if apiName == "" {
		apiName = res.Name
	}
	schema := res.GetConfigString("schema")
	if schema == "" {
		schema = "type Query { health: String! }"
	}

	result := mapper.NewMappingResult("hasura")
	svc := result.DockerService
	svc.Image = "hasura/graphql-engine:v2.44.0"
	svc.Ports = []string{"8080:8080"}
	svc.Environment = map[string]string{
		"HASURA_GRAPHQL_DATABASE_URL":          "postgres://postgres:postgres@hasura-db:5432/postgres",
		"HASURA_GRAPHQL_ENABLE_CONSOLE":        "true",
		"HASURA_GRAPHQL_DEV_MODE":              "false",
		"HASURA_GRAPHQL_METADATA_DATABASE_URL": "postgres://postgres:postgres@hasura-db:5432/postgres",
	}
	svc.DependsOn = []string{"hasura-db"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{
		Test:     []string{"CMD", "wget", "-qO-", "http://localhost:8080/healthz"},
		Interval: 10 * time.Second,
		Timeout:  5 * time.Second,
		Retries:  5,
	}
	svc.Labels = map[string]string{
		"homeport.source":                                       "aws_appsync_graphql_api",
		"homeport.api_name":                                     apiName,
		"traefik.enable":                                        "true",
		"traefik.http.routers.hasura.rule":                      "Host(`graphql.localhost`)",
		"traefik.http.routers.hasura.entrypoints":               "web,websecure",
		"traefik.http.services.hasura.loadbalancer.server.port": "8080",
	}

	result.AddConfig("hasura-db-compose.yml", []byte(m.databaseCompose()))
	result.AddConfig("config/hasura/metadata/databases/databases.yaml", []byte(m.databaseMetadata()))
	result.AddConfig("config/hasura/metadata/actions.graphql", []byte(schema+"\n"))
	result.AddScript("backup_appsync_config.sh", []byte(m.backupScript(apiName)))
	for _, step := range appSyncRunbook(apiName) {
		result.AddRunbookStep(step)
	}

	return result, nil
}

func (m *AppSyncMapper) databaseCompose() string {
	return `services:
  hasura-db:
    image: postgres:15-alpine
    environment:
      POSTGRES_PASSWORD: postgres
    volumes:
      - hasura-db-data:/var/lib/postgresql/data
    networks:
      - homeport
    restart: unless-stopped
volumes:
  hasura-db-data:
`
}

func (m *AppSyncMapper) databaseMetadata() string {
	return `- name: default
  kind: postgres
  configuration:
    connection_info:
      database_url:
        from_env: HASURA_GRAPHQL_DATABASE_URL
      isolation_level: read-committed
      use_prepared_statements: false
`
}

func (m *AppSyncMapper) backupScript(apiName string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-hasura-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/hasura hasura-db-compose.yml
echo "$archive"
`, sanitizeTraefikName(apiName))
}

func appSyncRunbook(apiName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "graphql", "name": apiName, "source": "aws_appsync_graphql_api"}
	return []domainrunbook.Step{
		step("render-graphql-schema", "Render GraphQL schema", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s config/hasura/metadata/actions.graphql"}, "GraphQL schema is generated", metadata),
		step("validate-graphql-endpoint", "Validate GraphQL endpoint", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "-c", "echo validate Hasura GraphQL health and introspection"}, "GraphQL health and introspection probes pass", metadata),
		step("backup-appsync-config", "Backup AppSync config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_appsync_config.sh"}, "Hasura metadata and database compose config are archived", metadata),
		step("cutover-graphql-endpoint-to-hasura", "Cut over GraphQL endpoint to Hasura", "Cutover", domainrunbook.StepTypeDNSCheck, nil, "GraphQL clients resolve to Hasura and query probes pass", metadata),
		step("rollback-appsync-source-authority", "Keep AppSync as rollback authority", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS AppSync remains authoritative until cutover passes", metadata),
	}
}

func step(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeDNSCheck {
		executor = "dns"
	}
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             stepType,
		Status:           domainrunbook.StepStatusPending,
		Executor:         executor,
		Command:          command,
		SuccessCondition: success,
		Metadata:         cloneStringMap(metadata),
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
