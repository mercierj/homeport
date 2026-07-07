package database

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type AISearchMapper struct {
	*mapper.BaseMapper
}

func NewAISearchMapper() *AISearchMapper {
	return &AISearchMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAzureAISearch, nil)}
}

func (m *AISearchMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("name")
	if name == "" {
		name = res.Name
	}

	result := mapper.NewMappingResult("opensearch")
	svc := result.DockerService
	svc.Image = "opensearchproject/opensearch:2.15.0"
	svc.Environment = map[string]string{"cluster.name": "homeport-opensearch", "discovery.type": "single-node", "plugins.security.disabled": "true", "OPENSEARCH_JAVA_OPTS": "-Xms512m -Xmx512m", "SOURCE_AI_SEARCH_SERVICE": name}
	svc.Ports = []string{"9200:9200", "9600:9600"}
	svc.Volumes = []string{"./data/ai-search:/usr/share/opensearch/data", "./config/ai-search:/usr/share/opensearch/config/homeport"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "curl -fsS http://localhost:9200/_cluster/health >/dev/null"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeAzureAISearch), "homeport.search_service": name, "homeport.target": "opensearch"}

	result.AddConfig("config/ai-search/domain-map.yaml", []byte(aiSearchDomainMap(name, res.GetConfigString("location"))))
	result.AddConfig("config/ai-search/app-change.env", []byte(aiSearchAppChange(name)))
	result.AddConfig("config/ai-search/snapshot-repository.json", []byte("{\n  \"type\": \"fs\",\n  \"settings\": {\"location\": \"/usr/share/opensearch/data/snapshots\"}\n}\n"))
	result.AddScript("export_ai_search_service.sh", []byte(aiSearchExportScript(name)))
	result.AddScript("provision_ai_search_opensearch.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/ai-search/domain-map.yaml\necho \"OpenSearch ready for Azure AI Search %s\"\n", name)))
	result.AddScript("migrate_ai_search_indexes.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ngrep -q %q config/ai-search/domain-map.yaml\necho \"Azure AI Search indexes staged for OpenSearch\"\n", name)))
	result.AddScript("validate_ai_search_indexes.sh", []byte("#!/bin/sh\nset -eu\ntest -s config/ai-search/app-change.env\ntest -s config/ai-search/snapshot-repository.json\n"))
	result.AddScript("backup_ai_search_config.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/ai-search-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/ai-search ai-search-export 2>/dev/null || tar -czf \"$archive\" config/ai-search\necho \"$archive\"\n", name)))
	result.AddScript("cutover_ai_search_clients.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\n. config/ai-search/app-change.env\ntest \"$SOURCE_AI_SEARCH_SERVICE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply app changes and use $OPENSEARCH_ENDPOINT\"\n", name)))
	for _, step := range aiSearchRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func aiSearchDomainMap(name, location string) string {
	return fmt.Sprintf("source_service: %s\nlocation: %s\ntarget_cluster: opensearch\napp_change_mode: generated_patch\n", name, location)
}

func aiSearchAppChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_AI_SEARCH_SERVICE=%s\nOPENSEARCH_ENDPOINT=http://opensearch:9200\nTARGET_OPENSEARCH_URL=http://opensearch:9200\n", name)
}

func aiSearchExportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nSERVICE_NAME=%q\nOUTPUT_DIR=\"${OUTPUT_DIR:-./ai-search-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naz search service show --name \"$SERVICE_NAME\" --resource-group \"${AZURE_RESOURCE_GROUP}\" > \"$OUTPUT_DIR/service.json\"\n", name)
}

func aiSearchRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "search", "source": "azurerm_search_service", "service": name, "target": "opensearch"}
	return []domainrunbook.Step{
		aiSearchStep("export-ai-search-service", "Export AI Search service", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_ai_search_service.sh"}, "AI Search service is exported", metadata),
		aiSearchStep("provision-ai-search", "Provision OpenSearch", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_ai_search_opensearch.sh"}, "OpenSearch config is rendered", metadata),
		aiSearchStep("migrate-ai-search-indexes", "Migrate AI Search indexes", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_ai_search_indexes.sh"}, "indexes are staged for OpenSearch", metadata),
		aiSearchStep("validate-ai-search", "Validate AI Search target", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_ai_search_indexes.sh"}, "OpenSearch handoff config validates", metadata),
		aiSearchStep("backup-ai-search-config", "Backup AI Search config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_ai_search_config.sh"}, "AI Search migration artifacts are archived", metadata),
		aiSearchStep("cutover-ai-search-clients", "Cut over AI Search clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_ai_search_clients.sh"}, "clients use generated OpenSearch endpoint", metadata),
		aiSearchStep("rollback-ai-search-source", "Keep AI Search source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AI Search remains authoritative until OpenSearch validation passes", metadata),
	}
}

func aiSearchStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
