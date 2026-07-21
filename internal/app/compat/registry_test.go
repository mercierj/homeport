package compat

import (
	"net/http"
	"strings"
	"testing"
)

type testAdapter struct {
	provider string
	service  string
}

func (a testAdapter) Provider() string                             { return a.provider }
func (a testAdapter) Service() string                              { return a.service }
func (a testAdapter) Routes() []string                             { return nil }
func (a testAdapter) TargetEnv() map[string]string                 { return nil }
func (a testAdapter) ConformanceChecks() []string                  { return nil }
func (a testAdapter) ServeHTTP(http.ResponseWriter, *http.Request) {}

func TestRegistryRejectsDuplicateAdapter(t *testing.T) {
	registry := NewRegistry()
	adapter := testAdapter{provider: "aws", service: "sqs"}

	if err := registry.Register(adapter); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
	err := registry.Register(adapter)
	if err == nil {
		t.Fatal("Register() error = nil, want duplicate error")
	}
	if !strings.Contains(err.Error(), "aws/sqs") {
		t.Fatalf("duplicate error = %q, want adapter key", err.Error())
	}
}

func TestRegistryUnknownAdapterHasClearError(t *testing.T) {
	_, err := NewRegistry().Get("aws", "missing")
	if err == nil {
		t.Fatal("Get() error = nil, want unknown adapter error")
	}
	if !strings.Contains(err.Error(), "unknown compat adapter aws/missing") {
		t.Fatalf("unknown error = %q", err.Error())
	}
}

func TestDefaultRegistryIncludesBuiltins(t *testing.T) {
	registry := NewDefaultRegistry()
	for _, service := range []string{"s3", "dynamodb", "redis", "sqs", "sns", "kinesis", "secretsmanager", "kms", "ssm", "cloudwatchlogs", "lambda", "eventbridge", "acm", "ses", "cognito", "ecs", "apigateway", "efs", "eks", "iam"} {
		adapter, err := registry.Get("aws", service)
		if err != nil {
			t.Fatalf("Get(aws, %s) error = %v", service, err)
		}
		if adapter.Provider() == "" || adapter.Service() == "" {
			t.Fatalf("adapter %s has empty identity", service)
		}
	}
	if _, err := registry.Get("gcp", "pub-sub"); err != nil {
		t.Fatalf("Get(gcp, pub-sub) error = %v", err)
	}
	if _, err := registry.Get("azure", "service-bus"); err != nil {
		t.Fatalf("Get(azure, service-bus) error = %v", err)
	}
}

func TestNativeCompatibleMetadata(t *testing.T) {
	registry := NewDefaultRegistry()

	s3, err := registry.Get("aws", "s3")
	if err != nil {
		t.Fatal(err)
	}
	if got := s3.TargetEnv()["AWS_ENDPOINT_URL_S3"]; got == "" {
		t.Fatalf("S3 endpoint env is empty")
	}
	if got := s3.TargetEnv()["AWS_S3_FORCE_PATH_STYLE"]; got != "true" {
		t.Fatalf("S3 path style = %q, want true", got)
	}

	dynamodb, err := registry.Get("aws", "dynamodb")
	if err != nil {
		t.Fatal(err)
	}
	if got := dynamodb.TargetEnv()["AWS_ENDPOINT_URL_DYNAMODB"]; got == "" {
		t.Fatalf("DynamoDB endpoint env is empty")
	}

	lambda, err := registry.Get("aws", "lambda")
	if err != nil {
		t.Fatal(err)
	}
	if got := lambda.TargetEnv()["AWS_ENDPOINT_URL_LAMBDA"]; got == "" {
		t.Fatalf("Lambda endpoint env is empty")
	}

	eventbridge, err := registry.Get("aws", "eventbridge")
	if err != nil {
		t.Fatal(err)
	}
	if got := eventbridge.TargetEnv()["AWS_ENDPOINT_URL_EVENTBRIDGE"]; got == "" {
		t.Fatalf("EventBridge endpoint env is empty")
	}

	acm, err := registry.Get("aws", "acm")
	if err != nil {
		t.Fatal(err)
	}
	if got := acm.TargetEnv()["AWS_ENDPOINT_URL_ACM"]; got == "" {
		t.Fatalf("ACM endpoint env is empty")
	}

	ses, err := registry.Get("aws", "ses")
	if err != nil {
		t.Fatal(err)
	}
	if got := ses.TargetEnv()["AWS_ENDPOINT_URL_SES"]; got == "" {
		t.Fatalf("SES endpoint env is empty")
	}

	cognito, err := registry.Get("aws", "cognito")
	if err != nil {
		t.Fatal(err)
	}
	if got := cognito.TargetEnv()["AWS_ENDPOINT_URL_COGNITO_IDP"]; got == "" {
		t.Fatalf("Cognito endpoint env is empty")
	}

	ecs, err := registry.Get("aws", "ecs")
	if err != nil {
		t.Fatal(err)
	}
	if got := ecs.TargetEnv()["AWS_ENDPOINT_URL_ECS"]; got == "" {
		t.Fatalf("ECS endpoint env is empty")
	}

	apigateway, err := registry.Get("aws", "apigateway")
	if err != nil {
		t.Fatal(err)
	}
	if got := apigateway.TargetEnv()["AWS_ENDPOINT_URL_APIGATEWAY"]; got == "" {
		t.Fatalf("API Gateway endpoint env is empty")
	}

	efs, err := registry.Get("aws", "efs")
	if err != nil {
		t.Fatal(err)
	}
	if got := efs.TargetEnv()["AWS_ENDPOINT_URL_EFS"]; got == "" {
		t.Fatalf("EFS endpoint env is empty")
	}

	eks, err := registry.Get("aws", "eks")
	if err != nil {
		t.Fatal(err)
	}
	if got := eks.TargetEnv()["AWS_ENDPOINT_URL_EKS"]; got == "" {
		t.Fatalf("EKS endpoint env is empty")
	}

	iam, err := registry.Get("aws", "iam")
	if err != nil {
		t.Fatal(err)
	}
	if got := iam.TargetEnv()["AWS_ENDPOINT_URL_IAM"]; got == "" {
		t.Fatalf("IAM endpoint env is empty")
	}

	redis, err := registry.Get("aws", "redis")
	if err != nil {
		t.Fatal(err)
	}
	if got := redis.TargetEnv()["REDIS_HOST"]; got != "redis" {
		t.Fatalf("Redis host = %q, want redis", got)
	}
}
