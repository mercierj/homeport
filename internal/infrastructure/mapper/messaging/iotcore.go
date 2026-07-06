package messaging

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type IoTCoreMapper struct {
	*mapper.BaseMapper
}

func NewIoTCoreMapper() *IoTCoreMapper {
	return &IoTCoreMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeIoTThing, nil)}
}

func (m *IoTCoreMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	thingName := res.GetConfigString("thing_name")
	if thingName == "" {
		thingName = res.Name
	}
	if thingName == "" {
		thingName = "iot-thing"
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
	svc.Labels = map[string]string{"homeport.source": "aws_iot_thing", "homeport.thing": thingName, "homeport.target": "emqx"}
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD", "emqx", "ctl", "status"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}

	result.AddConfig("config/emqx/acl.conf", []byte(m.aclConfig(thingName)))
	result.AddConfig("config/iot/topic-rules.yaml", []byte(m.topicRules(thingName)))
	result.AddConfig("config/iot/app-change.env", []byte(m.appChange(thingName)))
	result.AddConfig("config/iot/generated-iot.patch", []byte(m.generatedPatch(thingName)))
	result.AddScript("export_iot_core.sh", []byte(m.exportScript(thingName, res.Region)))
	result.AddScript("provision_emqx_iot.sh", []byte(m.provisionScript(thingName)))
	result.AddScript("migrate_iot_rules.sh", []byte(m.migrateScript(thingName)))
	result.AddScript("validate_iot_mqtt.sh", []byte(m.validateScript(thingName)))
	result.AddScript("backup_iot_config.sh", []byte(m.backupScript(thingName)))
	result.AddScript("cutover_iot_devices.sh", []byte(m.cutoverScript(thingName)))
	for _, step := range iotCoreRunbook(thingName) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *IoTCoreMapper) aclConfig(thingName string) string {
	return fmt.Sprintf("{allow, {%q}, publish, [\"#\"]}.\n{allow, {%q}, subscribe, [\"#\"]}.\n", thingName, thingName)
}

func (m *IoTCoreMapper) topicRules(thingName string) string {
	return fmt.Sprintf("thing: %s\ntarget: emqx\nrules: []\n", thingName)
}

func (m *IoTCoreMapper) appChange(thingName string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_IOT_THING=%s\nTARGET_MQTT=emqx\nMQTT_URL=mqtt://emqx:1883\n", thingName)
}

func (m *IoTCoreMapper) generatedPatch(thingName string) string {
	return fmt.Sprintf("--- app.env\n+++ app.env\n@@\n-AWS_IOT_THING=%s\n+MQTT_URL=mqtt://emqx:1883\n+IOT_BROKER=emqx\n", thingName)
}

func (m *IoTCoreMapper) exportScript(thingName, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf("#!/bin/sh\nset -eu\nAWS_REGION=\"${AWS_REGION:-%s}\"\nTHING_NAME=\"${IOT_THING:-%s}\"\nOUTPUT_DIR=\"${IOT_EXPORT_DIR:-iot-export}\"\nmkdir -p \"$OUTPUT_DIR\"\naws iot describe-thing --region \"$AWS_REGION\" --thing-name \"$THING_NAME\" > \"$OUTPUT_DIR/thing.json\"\naws iot list-topic-rules --region \"$AWS_REGION\" > \"$OUTPUT_DIR/topic-rules.json\"\naws iot list-thing-principals --region \"$AWS_REGION\" --thing-name \"$THING_NAME\" > \"$OUTPUT_DIR/principals.json\"\necho \"Exported IoT Core thing $THING_NAME\"\n", region, thingName)
}

func (m *IoTCoreMapper) provisionScript(thingName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/emqx/acl.conf\ntest -s config/iot/topic-rules.yaml\necho \"EMQX ready for IoT thing %s\"\n", thingName)
}

func (m *IoTCoreMapper) migrateScript(thingName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s iot-export/thing.json\ntest -s config/iot/topic-rules.yaml\ngrep -q %q config/iot/topic-rules.yaml\necho \"IoT Core topic rules mapped to EMQX\"\n", thingName)
}

func (m *IoTCoreMapper) validateScript(thingName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/emqx/acl.conf\ngrep -q %q config/emqx/acl.conf\ntest -s config/iot/app-change.env\n", thingName)
}

func (m *IoTCoreMapper) backupScript(thingName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/%s-iot-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/emqx config/iot export_iot_core.sh migrate_iot_rules.sh validate_iot_mqtt.sh cutover_iot_devices.sh\necho \"$archive\"\n", thingName)
}

func (m *IoTCoreMapper) cutoverScript(thingName string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\n. config/iot/app-change.env\ntest \"$SOURCE_IOT_THING\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Patch IoT devices and rules to MQTT_URL=$MQTT_URL\"\n", thingName)
}

func iotCoreRunbook(thingName string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "iot", "source": "aws_iot_thing", "thing": thingName, "HOMEPORT_TARGET": "emqx", "HOMEPORT_APP_CHANGE": "generated_patch"}
	return []domainrunbook.Step{
		iotCoreStep("export-iot-core", "Export IoT Core assets", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_iot_core.sh"}, "thing, principals, and rules are exported", metadata),
		iotCoreStep("provision-emqx-iot", "Provision EMQX target", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_emqx_iot.sh"}, "EMQX ACL and rule configs are present", metadata),
		iotCoreStep("migrate-iot-rules", "Migrate IoT rules", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_iot_rules.sh"}, "topic rules are represented in MQTT-native config", metadata),
		iotCoreStep("validate-iot-mqtt", "Validate IoT MQTT path", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_iot_mqtt.sh"}, "EMQX config and generated app change validate", metadata),
		iotCoreStep("backup-iot-config", "Backup IoT migration config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_iot_config.sh"}, "IoT migration artifacts are archived", metadata),
		iotCoreStep("cutover-iot-devices", "Cut over IoT devices", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_iot_devices.sh"}, "devices and rules use EMQX endpoint", metadata),
		iotCoreStep("rollback-iot-source", "Keep IoT Core source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS IoT Core remains authoritative until EMQX validation passes", metadata),
	}
}

func iotCoreStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}
