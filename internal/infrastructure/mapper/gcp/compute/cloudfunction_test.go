package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestCloudFunctionConformanceManagedAToZ(t *testing.T) {
	result, err := NewCloudFunctionMapper().Map(context.Background(), managedCloudFunctionFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Cloud Functions migration", result.ManualSteps)
	}
	if result.DockerService.Build == nil || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA built function: %#v", result.DockerService)
	}
	for _, file := range []string{
		"functions/hello-api/Dockerfile",
		"functions/hello-api/index.js",
		"functions/hello-api/package.json",
		"config/cloud-functions/app-change.env",
		"config/cloud-functions/function-report.yaml",
	} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/cloud-functions/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_CLOUD_FUNCTION=hello-api", "TARGET_FUNCTION_ENDPOINT=http://hello-api:8080"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"backup_cloud_function.sh", "validate_cloud_function.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"discover-cloud-function":    domainrunbook.StepTypeCommand,
		"build-cloud-function":       domainrunbook.StepTypeCommand,
		"deploy-cloud-function":      domainrunbook.StepTypeCommand,
		"validate-cloud-function":    domainrunbook.StepTypeCommand,
		"backup-cloud-function":      domainrunbook.StepTypeCommand,
		"cutover-cloud-function-url": domainrunbook.StepTypeAPICall,
		"rollback-cloud-function":    domainrunbook.StepTypeRollback,
	} {
		if !hasCloudFunctionRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func managedCloudFunctionFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:   "projects/demo/locations/europe-west1/functions/hello-api",
		Type: resource.TypeCloudFunction,
		Name: "hello-api",
		Config: map[string]interface{}{
			"name":              "hello-api",
			"runtime":           "nodejs20",
			"entry_point":       "helloWorld",
			"available_memory":  "512M",
			"timeout":           float64(60),
			"https_trigger_url": "https://europe-west1-demo.cloudfunctions.net/hello-api",
		},
	}
}

func hasCloudFunctionRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
