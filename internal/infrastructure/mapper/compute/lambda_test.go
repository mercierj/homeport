package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/resource"
	domainrunbook "github.com/homeport/homeport/internal/domain/runbook"
)

func TestLambdaConformanceManagedAToZ(t *testing.T) {
	result, err := NewLambdaMapper().Map(context.Background(), managedLambdaFixture())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ManualSteps) != 0 {
		t.Fatalf("manual steps = %#v, want generated Lambda migration", result.ManualSteps)
	}
	if result.DockerService.Deploy == nil || result.DockerService.Deploy.Replicas < 2 {
		t.Fatalf("service does not provision HA Lambda target: %#v", result.DockerService)
	}
	for _, file := range []string{
		"functions/orders-handler/Dockerfile",
		"functions/orders-handler/source.url",
		"config/lambda/app-change.env",
		"config/lambda/events.yaml",
		"config/lambda/permissions.json",
		"config/lambda/layers.yaml",
		"config/lambda/sample-event.json",
	} {
		if _, ok := result.Configs[file]; !ok {
			t.Fatalf("missing config %s", file)
		}
	}
	if strings.Contains(string(result.Configs["functions/orders-handler/Dockerfile"]), "TODO") {
		t.Fatalf("Dockerfile contains TODO:\n%s", result.Configs["functions/orders-handler/Dockerfile"])
	}
	appEnv := string(result.Configs["config/lambda/app-change.env"])
	for _, want := range []string{"SOURCE_FUNCTION=orders-handler", "TARGET_FUNCTION_URL=http://orders-handler:8080", "APP_CHANGE_MODE=adapter", "AWS_ENDPOINT_URL_LAMBDA=http://homeport:8080/api/v1/compat/aws/lambda"} {
		if !strings.Contains(appEnv, want) {
			t.Fatalf("app-change env missing %q:\n%s", want, appEnv)
		}
	}
	for _, file := range []string{"export_lambda_config.sh", "package_lambda_source.sh", "deploy_orders-handler.sh", "validate_lambda_invoke.sh", "backup_lambda_artifacts.sh", "cutover_lambda_adapter.sh"} {
		if _, ok := result.Scripts[file]; !ok {
			t.Fatalf("missing script %s", file)
		}
	}
	for id, stepType := range map[string]domainrunbook.StepType{
		"export-lambda-config":     domainrunbook.StepTypeCommand,
		"package-lambda-source":    domainrunbook.StepTypeCommand,
		"build-function-image":     domainrunbook.StepTypeCommand,
		"validate-function-invoke": domainrunbook.StepTypeCommand,
		"backup-lambda-artifacts":  domainrunbook.StepTypeCommand,
		"cutover-lambda-adapter":   domainrunbook.StepTypeAPICall,
		"rollback-function-source": domainrunbook.StepTypeRollback,
	} {
		if !hasLambdaRunbookStep(result, id, stepType) {
			t.Fatalf("missing %s runbook step: %#v", id, result.RunbookSteps)
		}
	}
}

func TestNewLambdaMapper(t *testing.T) {
	m := NewLambdaMapper()
	if m == nil {
		t.Fatal("NewLambdaMapper() returned nil")
	}
	if m.ResourceType() != resource.TypeLambdaFunction {
		t.Errorf("ResourceType() = %v, want %v", m.ResourceType(), resource.TypeLambdaFunction)
	}
}

func managedLambdaFixture() *resource.AWSResource {
	return &resource.AWSResource{
		ID:     "orders-handler",
		Type:   resource.TypeLambdaFunction,
		Name:   "orders-handler",
		Region: "eu-west-1",
		ARN:    "arn:aws:lambda:eu-west-1:123456789012:function:orders-handler",
		Config: map[string]interface{}{
			"function_name":          "orders-handler",
			"runtime":                "nodejs20.x",
			"handler":                "index.handler",
			"memory_size":            float64(512),
			"timeout":                float64(45),
			"role":                   "arn:aws:iam::123456789012:role/orders-lambda",
			"code_location":          "https://lambda-source.example/orders-handler.zip",
			"code_repository_type":   "S3",
			"environment":            map[string]interface{}{"variables": map[string]interface{}{"TABLE_NAME": "orders"}},
			"layers":                 []map[string]interface{}{{"arn": "arn:aws:lambda:eu-west-1:123456789012:layer:shared:1", "code_size": int64(1024)}},
			"dead_letter_config":     map[string]interface{}{"target_arn": "arn:aws:sqs:eu-west-1:123456789012:orders-dlq"},
			"vpc_subnet_ids":         []string{"subnet-1", "subnet-2"},
			"vpc_security_group_ids": []string{"sg-1"},
		},
	}
}

func hasLambdaRunbookStep(result *mapper.MappingResult, id string, stepType domainrunbook.StepType) bool {
	for _, step := range result.RunbookSteps {
		if step.ID == id && step.Type == stepType {
			return true
		}
	}
	return false
}

func TestLambdaMapper_ResourceType(t *testing.T) {
	m := NewLambdaMapper()
	got := m.ResourceType()
	want := resource.TypeLambdaFunction

	if got != want {
		t.Errorf("ResourceType() = %v, want %v", got, want)
	}
}

func TestLambdaMapper_Dependencies(t *testing.T) {
	m := NewLambdaMapper()
	deps := m.Dependencies()

	if deps == nil {
		t.Error("Dependencies() returned nil, want empty slice")
	}
}

func TestLambdaMapper_Validate(t *testing.T) {
	m := NewLambdaMapper()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
	}{
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
		{
			name: "wrong resource type",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeS3Bucket,
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "valid resource",
			res: &resource.AWSResource{
				ID:   "test-id",
				Type: resource.TypeLambdaFunction,
				Name: "test-lambda",
			},
			wantErr: false,
		},
		{
			name: "missing resource ID",
			res: &resource.AWSResource{
				ID:   "",
				Type: resource.TypeLambdaFunction,
				Name: "test-lambda",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := m.Validate(tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLambdaMapper_Map(t *testing.T) {
	m := NewLambdaMapper()
	ctx := context.Background()

	tests := []struct {
		name    string
		res     *resource.AWSResource
		wantErr bool
		check   func(*testing.T, *mapper.MappingResult)
	}{
		{
			name: "basic Lambda function - Node.js",
			res: &resource.AWSResource{
				ID:   "arn:aws:lambda:us-east-1:123456789012:function:my-function",
				Type: resource.TypeLambdaFunction,
				Name: "my-function",
				Config: map[string]interface{}{
					"function_name": "my-function",
					"runtime":       "nodejs18.x",
					"handler":       "index.handler",
					"memory_size":   float64(256),
					"timeout":       float64(30),
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService == nil {
					t.Fatal("DockerService is nil")
				}
				if result.DockerService.Image == "" {
					t.Error("DockerService.Image is empty")
				}
			},
		},
		{
			name: "Lambda function - Python",
			res: &resource.AWSResource{
				ID:   "arn:aws:lambda:us-east-1:123456789012:function:py-function",
				Type: resource.TypeLambdaFunction,
				Name: "py-function",
				Config: map[string]interface{}{
					"function_name": "py-function",
					"runtime":       "python3.11",
					"handler":       "main.handler",
				},
			},
			wantErr: false,
			check: func(t *testing.T, result *mapper.MappingResult) {
				if result == nil {
					t.Fatal("Map() returned nil result")
				}
				if result.DockerService == nil {
					t.Fatal("DockerService is nil")
				}
			},
		},
		{
			name:    "nil resource",
			res:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := m.Map(ctx, tt.res)
			if (err != nil) != tt.wantErr {
				t.Errorf("Map() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}
