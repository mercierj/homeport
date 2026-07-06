package compute

import (
	"context"
	"fmt"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

type RekognitionMapper struct {
	*mapper.BaseMapper
}

func NewRekognitionMapper() *RekognitionMapper {
	return &RekognitionMapper{BaseMapper: mapper.NewBaseMapper(resource.TypeRekognitionCollection, nil)}
}

func (m *RekognitionMapper) Map(_ context.Context, res *resource.AWSResource) (*mapper.MappingResult, error) {
	if err := m.Validate(res); err != nil {
		return nil, err
	}
	name := res.GetConfigString("collection_id")
	if name == "" {
		name = res.GetConfigString("name")
	}
	if name == "" {
		name = res.Name
	}

	result := mapper.NewMappingResult("opencv")
	svc := result.DockerService
	svc.Image = "opencv/opencv:latest"
	svc.Command = []string{"python3", "-m", "http.server", "8080"}
	svc.Ports = []string{"8080:8080"}
	svc.Volumes = []string{"./data/rekognition:/data", "./config/rekognition:/config"}
	svc.Networks = []string{"homeport"}
	svc.Deploy = &mapper.DeployConfig{Replicas: 2}
	svc.Restart = "unless-stopped"
	svc.Labels = map[string]string{
		"homeport.source":                 "aws_rekognition_collection",
		"homeport.rekognition_collection": name,
		"homeport.target":                 "opencv",
	}

	result.AddConfig("config/rekognition/vision-pipeline.yaml", []byte(m.pipeline(name)))
	result.AddConfig("config/rekognition/app-change.env", []byte(m.appChange(name)))
	result.AddConfig("config/rekognition/generated-client.patch", []byte(m.generatedPatch(name)))
	result.AddScript("export_rekognition_assets.sh", []byte(m.exportScript(name, res.Region)))
	result.AddScript("provision_opencv_pipeline.sh", []byte(m.provisionScript(name)))
	result.AddScript("migrate_rekognition_assets.sh", []byte(m.migrateScript(name)))
	result.AddScript("validate_opencv_pipeline.sh", []byte(m.validateScript(name)))
	result.AddScript("backup_rekognition_config.sh", []byte(m.backupScript(name)))
	result.AddScript("cutover_rekognition_clients.sh", []byte(m.cutoverScript(name)))
	for _, step := range rekognitionRunbook(name) {
		result.AddRunbookStep(step)
	}
	return result, nil
}

func (m *RekognitionMapper) pipeline(name string) string {
	return fmt.Sprintf(`collection: %s
source_api: rekognition
target_engine: opencv
input_dir: /data/input
output_dir: /data/output
app_change_mode: generated_patch
`, name)
}

func (m *RekognitionMapper) appChange(name string) string {
	return fmt.Sprintf(`APP_CHANGE_MODE=generated_patch
SOURCE_REKOGNITION_COLLECTION=%s
TARGET_VISION_ENGINE=opencv
OPENCV_PIPELINE_CONFIG=config/rekognition/vision-pipeline.yaml
GENERATED_PATCH=config/rekognition/generated-client.patch
`, name)
}

func (m *RekognitionMapper) generatedPatch(name string) string {
	return fmt.Sprintf(`--- a/app/vision.env
+++ b/app/vision.env
@@
-AWS_REKOGNITION_COLLECTION=%s
+VISION_ENGINE=opencv
+VISION_PIPELINE_CONFIG=config/rekognition/vision-pipeline.yaml
`, name)
}

func (m *RekognitionMapper) exportScript(name, region string) string {
	if region == "" {
		region = "us-east-1"
	}
	return fmt.Sprintf(`#!/bin/sh
set -eu
AWS_REGION=%q
COLLECTION_ID=%q
OUTPUT_DIR="./rekognition-export"
mkdir -p "$OUTPUT_DIR"
aws rekognition describe-collection --collection-id "$COLLECTION_ID" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/collection.json"
aws rekognition list-faces --collection-id "$COLLECTION_ID" --region "$AWS_REGION" --output json > "$OUTPUT_DIR/faces.json"
`, region, name)
}

func (m *RekognitionMapper) provisionScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/rekognition/vision-pipeline.yaml\necho \"OpenCV pipeline ready for %s\"\n", name)
}

func (m *RekognitionMapper) migrateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s rekognition-export/collection.json\ngrep -q %q config/rekognition/vision-pipeline.yaml\necho \"Rekognition collection %s mapped to OpenCV pipeline\"\n", name, name)
}

func (m *RekognitionMapper) validateScript(name string) string {
	return fmt.Sprintf("#!/bin/sh\nset -eu\ntest -s config/rekognition/app-change.env\ntest -s config/rekognition/generated-client.patch\ngrep -q %q config/rekognition/vision-pipeline.yaml\npython3 - <<'PY'\nimport cv2\nprint(cv2.__version__)\nPY\n", name)
}

func (m *RekognitionMapper) backupScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
archive="${BACKUP_DIR:-./backups}/%s-rekognition-$(date +%%Y%%m%%d%%H%%M%%S).tgz"
mkdir -p "$(dirname "$archive")"
tar -czf "$archive" config/rekognition rekognition-export export_rekognition_assets.sh provision_opencv_pipeline.sh migrate_rekognition_assets.sh validate_opencv_pipeline.sh cutover_rekognition_clients.sh
echo "$archive"
`, name)
}

func (m *RekognitionMapper) cutoverScript(name string) string {
	return fmt.Sprintf(`#!/bin/sh
set -eu
. config/rekognition/app-change.env
test "$SOURCE_REKOGNITION_COLLECTION" = %q
test "$APP_CHANGE_MODE" = "generated_patch"
test -s "$GENERATED_PATCH"
echo "Apply $GENERATED_PATCH and route vision clients to $OPENCV_PIPELINE_CONFIG"
`, name)
}

func rekognitionRunbook(name string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                "computer-vision",
		"source":              "aws_rekognition_collection",
		"collection":          name,
		"HOMEPORT_TARGET":     "opencv",
		"HOMEPORT_APP_CHANGE": "generated_patch",
	}
	return []domainrunbook.Step{
		rekognitionStep("export-rekognition-assets", "Export Rekognition assets", "Discovery", domainrunbook.StepTypeCommand, []string{"sh", "export_rekognition_assets.sh"}, "Rekognition collection assets are exported", metadata),
		rekognitionStep("provision-opencv-pipeline", "Provision OpenCV pipeline", "Provision", domainrunbook.StepTypeCommand, []string{"sh", "provision_opencv_pipeline.sh"}, "OpenCV pipeline config is rendered", metadata),
		rekognitionStep("migrate-rekognition-assets", "Migrate Rekognition assets", "Migrate", domainrunbook.StepTypeCommand, []string{"sh", "migrate_rekognition_assets.sh"}, "collection metadata maps to OpenCV pipeline", metadata),
		rekognitionStep("validate-opencv-pipeline", "Validate OpenCV pipeline", "Validate", domainrunbook.StepTypeCommand, []string{"sh", "validate_opencv_pipeline.sh"}, "OpenCV runtime and generated patch validate", metadata),
		rekognitionStep("backup-rekognition-config", "Backup Rekognition config", "Backup", domainrunbook.StepTypeCommand, []string{"sh", "backup_rekognition_config.sh"}, "Rekognition migration artifacts are archived", metadata),
		rekognitionStep("cutover-rekognition-clients", "Cut over Rekognition clients", "Cutover", domainrunbook.StepTypeAPICall, []string{"sh", "cutover_rekognition_clients.sh"}, "clients use generated OpenCV patch", metadata),
		rekognitionStep("rollback-rekognition-source", "Keep Rekognition source authoritative", "Rollback", domainrunbook.StepTypeRollback, nil, "AWS Rekognition remains authoritative until OpenCV validation passes", metadata),
	}
}

func rekognitionStep(id, name, group string, stepType domainrunbook.StepType, command []string, success string, metadata map[string]string) domainrunbook.Step {
	executor := "shell"
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
		Metadata:         metadata,
	}
}
