package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestIoTHubConformanceManagedAToZ(t *testing.T) {
	result, err := NewIoTHubMapper().Map(context.Background(), managedIoTHubFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated IoT Hub migration", result.ManualSteps)
	}
	if result.DockerService.Image != "emqx/emqx:5.7.2" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA EMQX target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/emqx/acl.conf", "config/iothub/topic-routes.yaml", "config/iothub/app-change.env", "config/iothub/generated-iothub.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/iothub/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_IOT_HUB=fleet-hub", "TARGET_MQTT=emqx", "MQTT_URL=mqtt://emqx:1883"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_iothub.sh", "provision_emqx_iothub.sh", "migrate_iothub_routes.sh", "validate_iothub_mqtt.sh", "backup_iothub_config.sh", "cutover_iothub_devices.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-iothub":          domainrunbook.StepTypeCommand,
		"provision-emqx-iothub":  domainrunbook.StepTypeCommand,
		"migrate-iothub-routes":  domainrunbook.StepTypeCommand,
		"validate-iothub-mqtt":   domainrunbook.StepTypeCommand,
		"backup-iothub-config":   domainrunbook.StepTypeCommand,
		"cutover-iothub-devices": domainrunbook.StepTypeAPICall,
		"rollback-iothub-source": domainrunbook.StepTypeRollback,
	} {
		if !hasIoTHubRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewIoTHubMapper(t *testing.T) {
	m := NewIoTHubMapper()
	if m == nil {
		t.Fatal("NewIoTHubMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeAzureIoTHub {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeAzureIoTHub)
	}
}

func managedIoTHubFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "/subscriptions/demo/resourceGroups/rg/providers/Microsoft.Devices/IotHubs/fleet-hub",
		Type:   resource.TypeAzureIoTHub,
		Name:   "fleet-hub",
		Region: "westeurope",
		Config: map[string]interface{}{
			"name":     "fleet-hub",
			"location": "westeurope",
			"sku":      "S1",
		},
	}
}

func hasIoTHubRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
