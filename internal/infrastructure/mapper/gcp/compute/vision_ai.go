package compute

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type VisionAIMapper struct {
	*mapper.BaseMapper
}

func NewVisionAIMapper() *VisionAIMapper {
	return &VisionAIMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeVisionAIService, nil)}
}

func (m *VisionAIMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := firstNonEmptyVisionAI(res.GetConfigString("name"), res.Name, "vision-ai")
	location := firstNonEmptyVisionAI(res.GetConfigString("location"), res.GetConfigString("region"), "global")

	result := mapper.NewMappingResult("vision-ai")
	svc := result.DockerService
	svc.Image = "jrottenberg/opencv:4.6.0-ubuntu"
	svc.Command = []string{"sleep", "infinity"}
	svc.Environment = map[string]string{"SOURCE_VISION_AI_SERVICE": name, "TARGET_VISION_BACKEND": "opencv"}
	svc.Volumes = []string{"./vision-ai/input:/input", "./vision-ai/output:/output"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.HealthCheck = &mapper.HealthCheck{Test: []string{"CMD-SHELL", "python3 - <<'PY'\nimport cv2\nprint(cv2.__version__)\nPY"}, Interval: 30 * time.Second, Timeout: 10 * time.Second, Retries: 5}
	svc.Labels = map[string]string{"homeport.source": string(resource.TypeVisionAIService), "homeport.service": name, "homeport.target": "opencv"}

	result.AddConfig("docker-compose.vision-ai.yml", []byte(visionAICompose(name)))
	result.AddConfig("config/vision-ai/app-change.env", []byte(visionAIAppChange(name)))
	result.AddConfig("config/vision-ai/service-report.yaml", []byte(visionAIReport(name, location, res.GetConfigString("service"))))
	result.AddConfig("config/vision-ai/generated-client.patch", []byte(visionAIPatch(name)))
	result.AddScript("export_vision_ai_service.sh", []byte(visionAIExportScript(name)))
	result.AddScript("migrate_vision_ai_service.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/vision-ai/service-report.yaml\nmkdir -p vision-ai/input vision-ai/output\necho \"Vision AI service %s mapped to OpenCV\"\n", name)))
	result.AddScript("validate_vision_ai_service.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s docker-compose.vision-ai.yml\ngrep -q %q config/vision-ai/app-change.env\n", name)))
	result.AddScript("backup_vision_ai_config.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\narchive=\"${BACKUP_DIR:-./backups}/vision-ai-%s-$(date +%%Y%%m%%d%%H%%M%%S).tgz\"\nmkdir -p \"$(dirname \"$archive\")\"\ntar -czf \"$archive\" config/vision-ai vision-ai-export docker-compose.vision-ai.yml 2>/dev/null || tar -czf \"$archive\" config/vision-ai docker-compose.vision-ai.yml\necho \"$archive\"\n", sanitizeComputeName(name))))
	result.AddScript("cutover_vision_ai_clients.sh", []byte(fmt.Sprintf("#!/bin/sh\nset -eu\n. config/vision-ai/app-change.env\ntest \"$SOURCE_VISION_AI_SERVICE\" = %q\ntest \"$APP_CHANGE_MODE\" = \"generated_patch\"\necho \"Apply $GENERATED_PATCH and route vision jobs to $TARGET_VISION_BACKEND\"\n", name)))
	for _, step := range visionAIRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func visionAICompose(name string) string {
	return fmt.Sprintf(`services:
  vision-ai:
    image: jrottenberg/opencv:4.6.0-ubuntu
    command: ["sleep", "infinity"]
    environment:
      SOURCE_VISION_AI_SERVICE: %s
      TARGET_VISION_BACKEND: opencv
    volumes:
      - ./vision-ai/input:/input
      - ./vision-ai/output:/output
`, name)
}

func visionAIAppChange(name string) string {
	return fmt.Sprintf("APP_CHANGE_MODE=generated_patch\nSOURCE_VISION_AI_SERVICE=%s\nTARGET_VISION_BACKEND=opencv\nGENERATED_PATCH=config/vision-ai/generated-client.patch\n", name)
}

func visionAIReport(name, location, service string) string {
	return fmt.Sprintf("source: google_vision_ai_service\nservice: %s\nlocation: %s\napi: %s\ntarget: opencv\n", name, location, service)
}

func visionAIPatch(name string) string {
	return fmt.Sprintf("--- a/app/vision.env\n+++ b/app/vision.env\n@@\n-VISION_AI_SERVICE=%s\n+VISION_BACKEND=opencv\n+VISION_INPUT_DIR=vision-ai/input\n+VISION_OUTPUT_DIR=vision-ai/output\n", name)
}

func visionAIExportScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\nSERVICE_NAME=%q\nOUTPUT_DIR=\"${OUTPUT_DIR:-./vision-ai-export}\"\nmkdir -p \"$OUTPUT_DIR\" vision-ai/input vision-ai/output\ngcloud services list --enabled --filter=\"name:vision.googleapis.com\" --format=json > \"$OUTPUT_DIR/service.json\"\necho \"$SERVICE_NAME\" > \"$OUTPUT_DIR/source-service.txt\"\n", name)
}

func visionAIRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{"kind": "computer-vision", "source": "google_vision_ai_service", "service": name, "target": "opencv"}
	return []domainrunbook.Step{
		visionAIStep("export-vision-ai-service", "Export Vision AI service", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_vision_ai_service.sh"}, "Vision AI service is exported", metadata),
		visionAIStep("provision-opencv-vision", "Provision OpenCV vision", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "-c", "test -s docker-compose.vision-ai.yml"}, "OpenCV compose target is rendered", metadata),
		visionAIStep("migrate-vision-ai-service", "Migrate Vision AI service", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_vision_ai_service.sh"}, "vision config is staged for OpenCV", metadata),
		visionAIStep("validate-opencv-vision", "Validate OpenCV vision", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_vision_ai_service.sh"}, "OpenCV handoff config validates", metadata),
		visionAIStep("backup-vision-ai-config", "Backup Vision AI config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_vision_ai_config.sh"}, "Vision AI migration artifacts are archived", metadata),
		visionAIStep("cutover-vision-ai-clients", "Cut over Vision AI clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_vision_ai_clients.sh"}, "clients use generated vision patch", metadata),
		visionAIStep("rollback-vision-ai-service", "Keep Vision AI source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "Vision AI remains authoritative until OpenCV validation passes", metadata),
	}
}

func visionAIStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
	if stepType == domainrunbook.StepTypeRollback {
		executor = "noop"
	}
	return domainrunbook.Step{ID: id, Name: name, Group: group, Type: stepType, Status: domainrunbook.StepStatusPending, Executor: executor, Command: command, SuccessCondition: success, Metadata: metadata}
}

func firstNonEmptyVisionAI(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
