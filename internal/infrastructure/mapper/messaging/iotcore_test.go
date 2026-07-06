package messaging

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestIoTCoreConformanceManagedAToZ(t *testing.T) {
	result, err := NewIoTCoreMapper().Map(context.Background(), managedIoTCoreFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated IoT Core migration", result.ManualSteps)
	}
	if result.DockerService.Image != "emqx/emqx:5.7.2" || result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA EMQX target: %#v", result.DockerService)
	}
	for _, file := range []string{"config/emqx/acl.conf", "config/iot/topic-rules.yaml", "config/iot/app-change.env", "config/iot/generated-iot.patch"} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	appEnv := string(result.Configs["config/iot/app-change.env"])
	for _, want := range []string{"APP_CHANGE_MODE=generated_patch", "SOURCE_IOT_THING=fleet-sensor", "TARGET_MQTT=emqx", "MQTT_URL=mqtt://emqx:1883"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_iot_core.sh", "provision_emqx_iot.sh", "migrate_iot_rules.sh", "validate_iot_mqtt.sh", "backup_iot_config.sh", "cutover_iot_devices.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-iot-core":     domainrunbook.StepTypeCommand,
		"provision-emqx-iot":  domainrunbook.StepTypeCommand,
		"migrate-iot-rules":   domainrunbook.StepTypeCommand,
		"validate-iot-mqtt":   domainrunbook.StepTypeCommand,
		"backup-iot-config":   domainrunbook.StepTypeCommand,
		"cutover-iot-devices": domainrunbook.StepTypeAPICall,
		"rollback-iot-source": domainrunbook.StepTypeRollback,
	} {
		if !hasIoTCoreRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewIoTCoreMapper(t *testing.T) {
	m := NewIoTCoreMapper()
	if m == nil {
		t.Fatal("NewIoTCoreMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeIoTThing {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeIoTThing)
	}
}

func managedIoTCoreFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "fleet-sensor",
		Type:   resource.TypeIoTThing,
		Name:   "fleet-sensor",
		Region: "eu-west-1",
		Config: map[string]interface{}{
			"thing_name": "fleet-sensor",
		},
	}
}

func hasIoTCoreRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}
