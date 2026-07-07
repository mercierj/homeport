package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type IoTHubMapper struct {
	*mapper.BaseMapper
}

func NewIoTHubMapper() *IoTHubMapper {
	return &IoTHubMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeAzureIoTHub, nil)}
}

func (m *IoTHubMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	hubName := res.GetConfigString("name")
	if hubName == "" {
		hubName = res.Name
	}
	if hubName == "" {
		hubName = "iothub"
	}

	result := mapper.NewMappingResult("emqx")
	svc := result.DockerService
	svc.Image = "emqx/emqx:5.7.2"
	svc.Environment = map[string]string{"EMQX_DASHBOARD__DEFAULT_USERNAME": "admin", "EMQX_DASHBOARD__DEFAULT_PASSWORD": "public"}
	svc.Ports = []string{"1883:1883", "8083:8083", "18083:18083"}
	svc.Volumes = []string{"./config/emqx:/opt/emqx/etc/conf.d", "./data/emqx:/opt/emqx/data"}
	svc.Networks = []string{"homeport"}
	svc.Restart = "unless-stopped"
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Labels = map[string]string{"homeport.source": "azurerm_iothub", "homeport.hub": hubName, "homeport.target": "emqx"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "emqx", "ctl", "status"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}

	result.AddConfig("config/emqx/acl.conf", []byte(m.aclConfig(hubName)))
	result.AddConfig("config/iothub/topic-routes.yaml", []byte(m.topicRoutes(hubName)))
	result.AddConfig("config/iothub/app-change.env", []byte(m.appChange(hubName)))
	result.AddConfig("config/iothub/generated-iothub.patch", []byte(m.generatedPatch(hubName)))
	result.AddScript("export_iothub.sh", []byte(m.exportScript(hubName, res.Region)))
	result.AddScript("provision_emqx_iothub.sh", []byte(m.provisionScript(hubName)))
	result.AddScript("migrate_iothub_routes.sh", []byte(m.migrateScript(hubName)))
	result.AddScript("validate_iothub_mqtt.sh", []byte(m.validateScript(hubName)))
	result.AddScript("backup_iothub_config.sh", []byte(m.backupScript(hubName)))
	result.AddScript("cutover_iothub_devices.sh", []byte(m.cutoverScript(hubName)))
	for _, step := range iotHubRunbook(hubName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *IoTHubMapper) aclConfig(hubName string) string {
	return fmt.Sprintf("{allow, {%q}, publish, [\"#\"]}.\n{allow, {%q}, subscribe, [\"#\"]}.\n", hubName, hubName)
}

func (m *IoTHubMapper) topicRoutes(hubName string) string {
	return fmt.Sprintf("hub: %s\ntarget: emqx\nroutes:\n  - source: devices/+/messages/events/#\n    target: emqx\n", hubName)
}

func (m *IoTHubMapper) appChange(hubName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_IOT_HUB=%s\nTARGET_MQTT=emqx\nMQTT_URL=mqtt://emqx:1883\n", hubName)
}

func (m *IoTHubMapper) generatedPatch(hubName string) string {
	return fmt.Sprintf("--- app.env\n+++ app.env\n@@\n-AZURE_IOT_HUB=%s\n+MQTT_URL=mqtt://emqx:1883\n+IOT_BROKER=emqx\n", hubName)
}

func (m *IoTHubMapper) exportScript(hubName, region string) string {
	if region == "" {
		region = "westeurope"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nAZURE_LOCATION=\"${AZURE_LOCATION:-%s}\"\nHUB_NAME=\"${IOT_HUB:-%s}\"\nOUTPUT_DIR=\"${IOTHUB_EXPORT_DIR:-iothub-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naz iot hub show --name \"$HUB_NAME\" > \"$OUTPUT_DIR/hub.json\"\naz iot hub route list --hub-name \"$HUB_NAME\" > \"$OUTPUT_DIR/routes.json\"\naz iot hub device-identity list --hub-name \"$HUB_NAME\" > \"$OUTPUT_DIR/devices.json\"\necho \"Exported IoT Hub $HUB_NAME in $AZURE_LOCATION\"\n", region, hubName)
}

func (m *IoTHubMapper) provisionScript(hubName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/emqx/acl.conf\ntest -s config/iothub/topic-routes.yaml\necho \"EMQX ready for IoT Hub %s\"\n", hubName)
}

func (m *IoTHubMapper) migrateScript(hubName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/iothub/topic-routes.yaml\ngrep -q %q config/iothub/topic-routes.yaml\necho \"IoT Hub routes mapped to EMQX\"\n", hubName)
}

func (m *IoTHubMapper) validateScript(hubName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/emqx/acl.conf\ngrep -q %q config/emqx/acl.conf\ntest -s config/iothub/app-change.env\n", hubName)
}

func (m *IoTHubMapper) backupScript(hubName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-iothub-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/emqx config/iothub iothub-export 2>/dev/null || tar -czf \"$archive\" config/emqx config/iothub\necho \"$archive\"\n", hubName)
}

func (m *IoTHubMapper) cutoverScript(hubName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/iothub/app-change.env\ntest \"$SOURCE_IOT_HUB\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch IoT Hub devices and routes to MQTT_URL=$MQTT_URL\"\n", hubName)
}

func iotHubRunbook(hubName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "iot", "source": "azurerm_iothub", "hub": hubName, "HOMEPORT_TARGET": "emqx", "HOMEPORT_APP_CHANGE": "generated_patch"}
	return []domainrunbook.Step{
		iotHubStep("export-iothub", "Export IoT Hub assets", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_iothub.sh"}, "hub, devices, and routes are exported", metadata),
		iotHubStep("provision-emqx-iothub", "Provision EMQX target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_emqx_iothub.sh"}, "EMQX ACL and route configs are present", metadata),
		iotHubStep("migrate-iothub-routes", "Migrate IoT Hub routes", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_iothub_routes.sh"}, "topic routes are represented in MQTT-native config", metadata),
		iotHubStep("validate-iothub-mqtt", "Validate IoT Hub MQTT path", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_iothub_mqtt.sh"}, "EMQX config and generated app change validate", metadata),
		iotHubStep("backup-iothub-config", "Backup IoT Hub migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_iothub_config.sh"}, "IoT Hub migration artifacts are archived", metadata),
		iotHubStep("cutover-iothub-devices", "Cut over IoT Hub devices", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_iothub_devices.sh"}, "devices and routes use EMQX endpoint", metadata),
		iotHubStep("rollback-iothub-source", "Keep IoT Hub source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Azure IoT Hub remains authoritative until EMQX validation passes", metadata),
	}
}

func iotHubStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
