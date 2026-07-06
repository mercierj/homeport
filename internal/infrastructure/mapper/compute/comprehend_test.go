package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestComprehendConformanceManagedAToZ(t *testing.T) {
	result, err := NewComprehendMapper().Map(context.Background(), managedComprehendFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Comprehend migration", result.ManualSteps)
	}
	if result.DockerService.Image != "spacy/spacy:latest" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA spaCy target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/comprehend/nlp-pipeline.yaml", "config/comprehend/app-change.env", "config/comprehend/generated-client.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/comprehend/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_COMPREHEND_MODEL=support-classifier", "TARGET_NLP_ENGINE=spacy"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_comprehend_models.sh", "provision_spacy_pipeline.sh", "migrate_comprehend_models.sh", "validate_spacy_pipeline.sh", "backup_comprehend_config.sh", "cutover_comprehend_clients.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-comprehend-models":   domainrunbook.StepTypeCommand,
		"provision-spacy-pipeline":   domainrunbook.StepTypeCommand,
		"migrate-comprehend-models":  domainrunbook.StepTypeCommand,
		"validate-spacy-pipeline":    domainrunbook.StepTypeCommand,
		"backup-comprehend-config":   domainrunbook.StepTypeCommand,
		"cutover-comprehend-clients": domainrunbook.StepTypeAPICall,
		"rollback-comprehend-source": domainrunbook.StepTypeRollback,
	} {
		if !hasComprehendRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewComprehendMapper(t *testing.T) {
	m := NewComprehendMapper()
	if m == nil {
		t.Fatal("NewComprehendMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeComprehendClassifier {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeComprehendClassifier)
	}
}

func managedComprehendFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "support-classifier",
		Type:   resource.TypeComprehendClassifier,
		Name:   "support-classifier",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"name":          "support-classifier",
			"language_code": "en",
		},
	}
}

func hasComprehendRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
