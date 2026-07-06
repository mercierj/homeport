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
	for _, service := range []string{"s3", "dynamodb", "redis", "sqs", "sns", "kinesis", "secretsmanager", "ssm", "cloudwatchlogs"} {
		adapter, err := registry.Get("aws", service)
		if err != nil {
			t.Fatalf("Get(aws, %s) error = %v", service, err)
		}
		if adapter.Provider() == "" || adapter.Service() == "" {
			t.Fatalf("adapter %s has empty identity", service)
		}
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

	redis, err := registry.Get("aws", "redis")
	if err != nil {
		t.Fatal(err)
	}
	if got := redis.TargetEnv()["REDIS_HOST"]; got != "redis" {
		t.Fatalf("Redis host = %q, want redis", got)
	}
}
