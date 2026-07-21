package conformance

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	appcompat "github.com/homeport/homeport/internal/app/compat"
	domain "github.com/homeport/homeport/internal/domain/conformance"
	"gopkg.in/yaml.v3"
)

func TestLoadReturnsManifest(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`
provider: aws
service: S3
checks:
  discover: go test ./test/integration/aws -run S3
  cost: go test ./internal/domain/provider ./internal/app/providers
  provision: go test ./internal/infrastructure/mapper/... -run S3
  migrate: go test ./internal/app/datamigration -run S3
  api_compat: go test ./test/compat -run S3
  env_dns: go test ./internal/app/runbook ./internal/app/cutover
  ha: go test ./internal/domain/provider ./internal/infrastructure/generator/...
  backup: go test ./internal/app/backup
  validate: go test ./internal/app/runbook ./internal/app/metrics
  cutover: go test ./internal/app/cutover
  rollback: go test ./internal/app/cutover ./internal/app/backup
evidence:
  target: MinIO
  app_change_mode: adapter
`)
	if err := os.WriteFile(filepath.Join(dir, "aws-s3.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	manifest, err := NewService(dir).Load("aws", "S3")
	if err != nil {
		t.Fatal(err)
	}
	if missing := manifest.MissingChecks(); len(missing) != 0 {
		t.Fatalf("missing checks = %v", missing)
	}
	if manifest.Checks[domain.CheckDiscover] == "" || manifest.Evidence["target"] != "MinIO" {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestRunRejectsGoTestWithNoTestsRun(t *testing.T) {
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "go.mod"), []byte("module example.test/conformance\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pkgDir := filepath.Join(workDir, "pkg")
	if err := os.Mkdir(pkgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "pkg_test.go"), []byte(`package pkg

import "testing"

func TestExisting(t *testing.T) {}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := completeManifest("go test ./pkg -run Missing")

	err := NewService(t.TempDir()).Run(context.Background(), manifest, workDir)
	if err == nil || !strings.Contains(err.Error(), "ran no tests") {
		t.Fatalf("expected no-tests failure, got %v", err)
	}
}

func TestRunExecutesEveryRequiredCheck(t *testing.T) {
	workDir := t.TempDir()
	marker := filepath.Join(workDir, "checks.log")
	manifest := completeManifest("printf x >> checks.log")

	if err := NewService(t.TempDir()).Run(context.Background(), manifest, workDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(data); got != len(domain.RequiredChecks()) {
		t.Fatalf("ran %d checks, want %d", got, len(domain.RequiredChecks()))
	}
}

func TestAzureServiceBusManifestDocumentsAPICompatSeed(t *testing.T) {
	manifest, err := NewService(filepath.Join("..", "..", "..", "test", "conformance", "services")).Load("azure", "Service Bus")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Checks[domain.CheckAPICompat] != "GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run TestAzureServiceBusCompatibilityAdapter" {
		t.Fatalf("api_compat check = %q", manifest.Checks[domain.CheckAPICompat])
	}
	if manifest.Evidence["target"] != "HomePort Service Bus compatibility adapter seed for RabbitMQ with AMQP compatibility" {
		t.Fatalf("target evidence = %q", manifest.Evidence["target"])
	}
	if issues := manifest.PromotionIssues(); len(issues) == 0 {
		t.Fatal("Service Bus manifest is a seed and must not be promotion-ready yet")
	}
}

func TestAzureServiceBusManifestDocumentsAPICompatCoverage(t *testing.T) {
	manifest, err := NewService(filepath.Join("..", "..", "..", "test", "conformance", "services")).Load("azure", "Service Bus")
	if err != nil {
		t.Fatal(err)
	}
	coverage := manifest.Evidence["api_compat_covers"]
	for _, want := range []string{"provider_errors", "pagination", "idempotency", "authz", "quota", "audit", "sdk_lifecycle"} {
		if !strings.Contains(coverage, want) {
			t.Fatalf("api_compat_covers = %q, missing %q", coverage, want)
		}
	}
}

func TestGCPPubSubManifestDocumentsAPICompatCoverage(t *testing.T) {
	manifest, err := NewService(filepath.Join("..", "..", "..", "test", "conformance", "services")).Load("gcp", "Pub/Sub")
	if err != nil {
		t.Fatal(err)
	}
	coverage := manifest.Evidence["api_compat_covers"]
	for _, want := range []string{"provider_errors", "pagination", "idempotency", "authz", "quota", "audit", "expired_credential", "credential_age", "principal_attributes", "claims"} {
		if !strings.Contains(coverage, want) {
			t.Fatalf("api_compat_covers = %q, missing %q", coverage, want)
		}
	}
}

func TestAWSS3ManifestDocumentsAPICompatSeed(t *testing.T) {
	manifest, err := NewService(filepath.Join("..", "..", "..", "test", "conformance", "services")).Load("aws", "S3")
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Checks[domain.CheckAPICompat] != "GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run S3" {
		t.Fatalf("api_compat check = %q", manifest.Checks[domain.CheckAPICompat])
	}
	if manifest.Evidence["target"] != "local S3 compatibility adapter seed; MinIO is not deployed" {
		t.Fatalf("target evidence = %q", manifest.Evidence["target"])
	}
	if missing := manifest.MissingChecks(); len(missing) != len(domain.RequiredChecks())-1 {
		t.Fatalf("missing checks = %v, want every external check except api_compat", missing)
	}
}

func TestAWSServiceManifestsDocumentAPICompatSeeds(t *testing.T) {
	for _, tc := range []struct {
		service   string
		command   string
		target    string
		localSeed bool
	}{
		{
			service: "KMS",
			command: "GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run KMS",
			target:  "Vault Transit with HomePort KMS compatibility adapter",
		},
		{
			service:   "Secrets Manager",
			command:   "GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run SecretsManager",
			target:    "local Secrets Manager compatibility adapter seed; Vault is not deployed",
			localSeed: true,
		},
		{
			service: "Kinesis",
			command: "GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run Kinesis",
			target:  "Redpanda with HomePort Kinesis compatibility adapter",
		},
		{
			service: "SQS",
			command: "GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run SQS",
			target:  "RabbitMQ with HomePort SQS compatibility adapter",
		},
		{
			service:   "SNS",
			command:   "GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run SNS",
			target:    "local SNS compatibility adapter seed; NATS is not deployed",
			localSeed: true,
		},
		{
			service: "SES",
			command: "GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run SES",
			target:  "Postal with HomePort SES compatibility adapter",
		},
		{
			service:   "CloudWatch",
			command:   "GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run CloudWatchLogs",
			target:    "local CloudWatch Logs compatibility adapter seed; Loki is not deployed",
			localSeed: true,
		},
		{
			service: "IAM",
			command: "GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run IAM",
			target:  "Keycloak with HomePort IAM compatibility adapter",
		},
	} {
		t.Run(tc.service, func(t *testing.T) {
			manifest, err := NewService(filepath.Join("..", "..", "..", "test", "conformance", "services")).Load("aws", tc.service)
			if err != nil {
				t.Fatal(err)
			}
			if manifest.Checks[domain.CheckAPICompat] != tc.command {
				t.Fatalf("api_compat check = %q", manifest.Checks[domain.CheckAPICompat])
			}
			if manifest.Evidence["target"] != tc.target {
				t.Fatalf("target evidence = %q", manifest.Evidence["target"])
			}
			missing := manifest.MissingChecks()
			if tc.localSeed && len(missing) != len(domain.RequiredChecks())-1 {
				t.Fatalf("missing checks = %v, want every external check except api_compat", missing)
			}
			if !tc.localSeed && len(missing) != 0 {
				t.Fatalf("missing checks = %v", missing)
			}
		})
	}
}

func TestAWSCloudWatchManifestDocumentsAPICompatCoverage(t *testing.T) {
	manifest, err := NewService(filepath.Join("..", "..", "..", "test", "conformance", "services")).Load("aws", "CloudWatch")
	if err != nil {
		t.Fatal(err)
	}
	coverage := manifest.Evidence["api_compat_covers"]
	for _, want := range []string{"provider_errors", "pagination", "quota", "authz", "audit", "retention", "sdk_lifecycle"} {
		if !strings.Contains(coverage, want) {
			t.Fatalf("api_compat_covers = %q, missing %q", coverage, want)
		}
	}
}

func TestAWSEKSArtifactsDocumentK3sRuntimeMappingsAndMigration(t *testing.T) {
	backendData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "eks", "backend.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var backend struct {
		Backend struct {
			Target  string `yaml:"target"`
			Runtime struct {
				Image      string `yaml:"image"`
				HealthPath string `yaml:"health_path"`
			} `yaml:"runtime"`
			Persistence struct {
				Volume string `yaml:"volume"`
			} `yaml:"persistence"`
			Endpoint struct {
				Route string `yaml:"route"`
			} `yaml:"endpoint"`
		} `yaml:"backend"`
	}
	if err := yaml.Unmarshal(backendData, &backend); err != nil {
		t.Fatal(err)
	}
	if backend.Backend.Target != "k3s" || backend.Backend.Runtime.Image == "" || backend.Backend.Runtime.HealthPath == "" || backend.Backend.Persistence.Volume == "" || backend.Backend.Endpoint.Route != "/compat/aws/eks" {
		t.Fatalf("backend artifact = %#v", backend.Backend)
	}

	adapterData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "eks", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Adapter struct {
			Route    string            `yaml:"route"`
			Actions  map[string]string `yaml:"actions"`
			Errors   map[string]string `yaml:"errors"`
			Resource struct {
				Pattern string `yaml:"pattern"`
			} `yaml:"resource"`
			Pagination struct {
				Token string `yaml:"token"`
			} `yaml:"pagination"`
			Idempotency struct {
				Field   string   `yaml:"field"`
				Methods []string `yaml:"methods"`
			} `yaml:"idempotency"`
			Quota struct {
				MaxClusters int `yaml:"max_clusters_default"`
			} `yaml:"quota"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(adapterData, &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Adapter.Route != "/compat/aws/eks" || adapter.Adapter.Quota.MaxClusters == 0 {
		t.Fatalf("adapter artifact = %#v", adapter.Adapter)
	}
	if adapter.Adapter.Resource.Pattern != "arn:aws:eks:{region}:{account}:cluster/{id}" || adapter.Adapter.Pagination.Token != "nextToken" || adapter.Adapter.Idempotency.Field != "clientRequestToken" || len(adapter.Adapter.Idempotency.Methods) != 1 || adapter.Adapter.Idempotency.Methods[0] != "CreateCluster" {
		t.Fatalf("adapter contract = %#v", adapter.Adapter)
	}
	for _, action := range []string{"create", "describe", "list", "update", "delete"} {
		if adapter.Adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, adapter.Adapter.Actions)
		}
	}
	for _, code := range []string{"AccessDenied", "ResourceNotFoundException", "ResourceInUseException", "InvalidParameterException", "ResourceLimitExceededException", "ServerException"} {
		if adapter.Adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, adapter.Adapter.Errors)
		}
	}

	migrationData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "eks", "migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	migration := string(migrationData)
	for _, want := range []string{"# AWS EKS Migration", "Source import IDs", "aws_eks_cluster", "Unsupported actions", "Operator decisions", "Cutover", "Rollback", "K3s"} {
		if !strings.Contains(migration, want) {
			t.Fatalf("migration artifact missing %q", want)
		}
	}
}

func TestAWSECRArtifactsDocumentOCIRegistryMappingsAndMigration(t *testing.T) {
	backendData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "ecr", "backend.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var backend struct {
		Target  string `yaml:"target"`
		Runtime struct {
			Image       string `yaml:"image"`
			HealthCheck string `yaml:"healthcheck"`
		} `yaml:"runtime"`
		Persistence struct {
			Volume string `yaml:"volume"`
		} `yaml:"persistence"`
		Backup struct {
			Schedule string `yaml:"schedule"`
		} `yaml:"backup"`
		Endpoint struct {
			Route string `yaml:"route"`
		} `yaml:"endpoint"`
	}
	if err := yaml.Unmarshal(backendData, &backend); err != nil {
		t.Fatal(err)
	}
	if backend.Target != "oci-distribution" || backend.Runtime.Image == "" || backend.Runtime.HealthCheck == "" || backend.Persistence.Volume == "" || backend.Backup.Schedule == "" || backend.Endpoint.Route != "/compat/aws/ecr" {
		t.Fatalf("backend artifact = %#v", backend)
	}

	adapterData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "ecr", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Route    string            `yaml:"route"`
		Actions  map[string]string `yaml:"actions"`
		Resource struct {
			Pattern string `yaml:"pattern"`
		} `yaml:"resource"`
		Errors     map[string]string `yaml:"errors"`
		Pagination struct {
			Token string `yaml:"token"`
		} `yaml:"pagination"`
		Quota struct {
			MaxRepositories int `yaml:"max_repositories_default"`
		} `yaml:"quota"`
	}
	if err := yaml.Unmarshal(adapterData, &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Route != "/compat/aws/ecr" || adapter.Resource.Pattern != "arn:aws:ecr:{region}:{account}:repository/{id}" || adapter.Pagination.Token != "nextToken" || adapter.Quota.MaxRepositories == 0 {
		t.Fatalf("adapter artifact = %#v", adapter)
	}
	for _, action := range []string{"create", "describe", "list_images", "delete"} {
		if adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, adapter.Actions)
		}
	}
	for _, code := range []string{"AccessDeniedException", "RepositoryNotFoundException", "RepositoryAlreadyExistsException", "InvalidParameterException", "LimitExceededException", "ServerException"} {
		if adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, adapter.Errors)
		}
	}

	migrationData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "ecr", "migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	migration := string(migrationData)
	for _, want := range []string{"# AWS ECR Migration", "Source import IDs", "aws_ecr_repository", "Unsupported actions", "Operator decisions", "Cutover", "Rollback", "OCI Distribution registry"} {
		if !strings.Contains(migration, want) {
			t.Fatalf("migration artifact missing %q", want)
		}
	}
}

func TestAWSCognitoArtifactsDocumentKeycloakMappingsAndMigration(t *testing.T) {
	backendData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "cognito", "backend.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var backend struct {
		Backend struct {
			Target  string `yaml:"target"`
			Runtime struct {
				Image      string `yaml:"image"`
				HealthPath string `yaml:"health_path"`
			} `yaml:"runtime"`
			Persistence struct {
				Volume string `yaml:"volume"`
			} `yaml:"persistence"`
			Endpoint struct {
				Route string `yaml:"route"`
			} `yaml:"endpoint"`
		} `yaml:"backend"`
	}
	if err := yaml.Unmarshal(backendData, &backend); err != nil {
		t.Fatal(err)
	}
	if backend.Backend.Target != "keycloak" || backend.Backend.Runtime.Image == "" || backend.Backend.Runtime.HealthPath == "" || backend.Backend.Persistence.Volume == "" || backend.Backend.Endpoint.Route != "/compat/aws/cognito" {
		t.Fatalf("backend artifact = %#v", backend.Backend)
	}

	adapterData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "cognito", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Adapter struct {
			Route    string            `yaml:"route"`
			Actions  map[string]string `yaml:"actions"`
			Errors   map[string]string `yaml:"errors"`
			Resource struct {
				Pattern string `yaml:"pattern"`
			} `yaml:"resource"`
			Quota struct {
				Option string `yaml:"option"`
			} `yaml:"quota"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(adapterData, &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Adapter.Route != "/compat/aws/cognito" || adapter.Adapter.Resource.Pattern != "arn:aws:cognito-idp:{region}:{account}:userpool/{id}" || adapter.Adapter.Quota.Option != "WithCognitoUserPoolQuota" {
		t.Fatalf("adapter artifact = %#v", adapter.Adapter)
	}
	for _, action := range []string{"create_user_pool", "describe_user_pool", "list_user_pools", "update_user_pool", "delete_user_pool", "get_mfa_config", "manage_clients", "manage_domains", "manage_users", "manage_groups", "manage_tags"} {
		if adapter.Adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, adapter.Adapter.Actions)
		}
	}
	for _, code := range []string{"NotAuthorizedException", "ResourceNotFoundException", "LimitExceededException", "InvalidParameterException", "UsernameExistsException", "GroupExistsException", "UnsupportedOperation", "InternalErrorException"} {
		if adapter.Adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, adapter.Adapter.Errors)
		}
	}

	migrationData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "cognito", "migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	migration := string(migrationData)
	for _, want := range []string{"# AWS Cognito Migration", "Source import IDs", "aws_cognito_user_pool", "Unsupported actions", "Operator decisions", "Cutover", "Rollback", "Keycloak"} {
		if !strings.Contains(migration, want) {
			t.Fatalf("migration artifact missing %q", want)
		}
	}
}

func TestAWSEventBridgeArtifactsDocumentN8nMappingsAndMigration(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "eventbridge", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Adapter struct {
			Route    string            `yaml:"route"`
			Actions  map[string]string `yaml:"actions"`
			Errors   map[string]string `yaml:"errors"`
			Resource struct {
				Patterns []string `yaml:"patterns"`
			} `yaml:"resource"`
			Quota struct {
				Option string `yaml:"option"`
			} `yaml:"quota"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(data, &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Adapter.Route != "/compat/aws/eventbridge" || len(adapter.Adapter.Resource.Patterns) != 2 || adapter.Adapter.Resource.Patterns[0] != "arn:aws:events:us-east-1:000000000000:rule/{id}" || adapter.Adapter.Resource.Patterns[1] != "arn:aws:events:us-east-1:000000000000:rule/{event-bus}/{id}" || adapter.Adapter.Quota.Option != "WithEventBridgeRuleQuota" {
		t.Fatalf("adapter artifact = %#v", adapter.Adapter)
	}
	for _, action := range []string{"put_rule", "describe_rule", "list_rules", "put_events", "manage_targets", "enable_disable_rule", "delete_rule", "manage_tags"} {
		if adapter.Adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, adapter.Adapter.Actions)
		}
	}
	for _, code := range []string{"AccessDeniedException", "ResourceNotFoundException", "LimitExceededException", "ValidationException", "InvalidEventPatternException", "InvalidToken", "UnsupportedOperation", "InternalException"} {
		if adapter.Adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, adapter.Adapter.Errors)
		}
	}

	migrationData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "eventbridge", "migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# AWS EventBridge Migration", "Source import IDs", "aws_cloudwatch_event_rule", "Unsupported actions", "Operator decisions", "Cutover", "Rollback", "n8n"} {
		if !strings.Contains(string(migrationData), want) {
			t.Fatalf("migration artifact missing %q", want)
		}
	}
}

func TestAWSACMAdapterArtifactDocumentsLocalContract(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "acm", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Adapter struct {
			Route    string            `yaml:"route"`
			Target   string            `yaml:"target"`
			Actions  []string          `yaml:"actions"`
			Errors   map[string]string `yaml:"errors"`
			Resource struct {
				Pattern string `yaml:"pattern"`
			} `yaml:"resource"`
			Quota struct {
				Option string `yaml:"option"`
			} `yaml:"quota"`
			Pagination struct {
				Certificates []string `yaml:"certificates"`
			} `yaml:"pagination"`
			Idempotency struct {
				RequestCertificate string `yaml:"request_certificate"`
			} `yaml:"idempotency"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(data, &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Adapter.Route != "/compat/aws/acm" || adapter.Adapter.Target != "traefik-acme" || adapter.Adapter.Resource.Pattern != "arn:aws:acm:us-east-1:000000000000:certificate/{id}" || adapter.Adapter.Quota.Option != "WithACMCertificateQuota" || len(adapter.Adapter.Actions) != 7 || len(adapter.Adapter.Pagination.Certificates) != 2 || adapter.Adapter.Pagination.Certificates[0] != "MaxItems" || adapter.Adapter.Pagination.Certificates[1] != "NextToken" || adapter.Adapter.Idempotency.RequestCertificate != "IdempotencyToken" {
		t.Fatalf("adapter artifact = %#v", adapter.Adapter)
	}
	for _, action := range []string{"RequestCertificate", "DescribeCertificate", "ListCertificates", "DeleteCertificate", "ListTagsForCertificate", "AddTagsToCertificate", "RemoveTagsFromCertificate"} {
		found := false
		for _, got := range adapter.Adapter.Actions {
			if got == action {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing action %q in %#v", action, adapter.Adapter.Actions)
		}
	}
	for _, code := range []string{"AccessDenied", "ResourceNotFoundException", "LimitExceededException", "ValidationException", "UnsupportedOperation", "InternalFailure"} {
		if adapter.Adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, adapter.Adapter.Errors)
		}
	}
}

func TestAWSHTTPAdaptersHaveLocalArtifacts(t *testing.T) {
	artifactNames := map[string]string{
		"apigateway":     "api-gateway",
		"cloudwatchlogs": "cloudwatch",
		"secretsmanager": "secrets-manager",
	}
	for _, adapter := range appcompat.NewDefaultRegistry().List() {
		if adapter.Provider() != "aws" || len(adapter.Routes()) == 0 {
			continue
		}
		name := artifactNames[adapter.Service()]
		if name == "" {
			name = adapter.Service()
		}
		for _, file := range []string{"backend.yaml", "adapter.yaml", "migration.md"} {
			path := filepath.Join("..", "..", "..", "artifacts", "compat", "aws", name, file)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("%s adapter is missing %s: %v", adapter.Service(), path, err)
			}
		}
	}
}

func TestAzureServiceBusBackendArtifactDocumentsRabbitMQRuntime(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "azure", "service-bus", "backend.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Backend struct {
			Target  string `yaml:"target"`
			Runtime struct {
				Image       string            `yaml:"image"`
				HealthPath  string            `yaml:"health_path"`
				Environment map[string]string `yaml:"environment"`
			} `yaml:"runtime"`
			Persistence struct {
				Volume string `yaml:"volume"`
			} `yaml:"persistence"`
			Backup struct {
				Schedule string `yaml:"schedule"`
			} `yaml:"backup"`
			Endpoint struct {
				Route string `yaml:"route"`
			} `yaml:"endpoint"`
		} `yaml:"backend"`
	}
	if err := yaml.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Backend.Target != "rabbitmq-amqp" || artifact.Backend.Runtime.Image == "" || artifact.Backend.Runtime.HealthPath == "" {
		t.Fatalf("runtime backend = %#v", artifact.Backend)
	}
	if artifact.Backend.Persistence.Volume == "" || artifact.Backend.Backup.Schedule == "" || artifact.Backend.Endpoint.Route != "/compat/azure/service-bus" {
		t.Fatalf("backend artifact = %#v", artifact.Backend)
	}
}

func TestAzureServiceBusAdapterArtifactDocumentsMappings(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "azure", "service-bus", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Adapter struct {
			Route      string            `yaml:"route"`
			Actions    map[string]string `yaml:"actions"`
			Errors     map[string]string `yaml:"errors"`
			Pagination struct {
				Top       string `yaml:"top"`
				SkipToken string `yaml:"skiptoken"`
			} `yaml:"pagination"`
			Idempotency struct {
				Header string `yaml:"header"`
			} `yaml:"idempotency"`
			Quota struct {
				MaxQueues int `yaml:"max_queues_default"`
			} `yaml:"quota"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Adapter.Route != "/compat/azure/service-bus" {
		t.Fatalf("adapter route = %q", artifact.Adapter.Route)
	}
	for _, action := range []string{"read", "write", "delete"} {
		if artifact.Adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, artifact.Adapter.Actions)
		}
	}
	for _, code := range []string{"AuthorizationFailed", "ResourceNotFound", "Conflict", "BadRequest", "TooManyRequests", "InternalError"} {
		if artifact.Adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, artifact.Adapter.Errors)
		}
	}
	if artifact.Adapter.Pagination.Top != "$top" || artifact.Adapter.Pagination.SkipToken != "$skiptoken" || artifact.Adapter.Idempotency.Header != "Repeatability-Request-ID" || artifact.Adapter.Quota.MaxQueues == 0 {
		t.Fatalf("adapter artifact = %#v", artifact.Adapter)
	}
}

func TestAzureServiceBusMigrationArtifactDocumentsCutoverRollback(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "azure", "service-bus", "migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"# Azure Service Bus Migration",
		"Source import IDs",
		"azurerm_servicebus_namespace",
		"azurerm_servicebus_queue",
		"Unsupported actions",
		"Operator decisions",
		"Cutover",
		"Rollback",
		"RabbitMQ with AMQP compatibility",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("migration artifact missing %q", want)
		}
	}
}

func TestAWSSQSArtifactsDocumentRabbitMQRuntimeAndMappings(t *testing.T) {
	backendData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "sqs", "backend.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var backend struct {
		Backend struct {
			Target  string `yaml:"target"`
			Runtime struct {
				Image      string `yaml:"image"`
				HealthPath string `yaml:"health_path"`
			} `yaml:"runtime"`
			Persistence struct {
				Volume string `yaml:"volume"`
			} `yaml:"persistence"`
			Endpoint struct {
				Route string `yaml:"route"`
			} `yaml:"endpoint"`
		} `yaml:"backend"`
	}
	if err := yaml.Unmarshal(backendData, &backend); err != nil {
		t.Fatal(err)
	}
	if backend.Backend.Target != "rabbitmq" || backend.Backend.Runtime.Image == "" || backend.Backend.Runtime.HealthPath == "" || backend.Backend.Persistence.Volume == "" || backend.Backend.Endpoint.Route != "/compat/aws/sqs" {
		t.Fatalf("backend artifact = %#v", backend.Backend)
	}

	adapterData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "sqs", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Adapter struct {
			Route   string            `yaml:"route"`
			Actions map[string]string `yaml:"actions"`
			Errors  map[string]string `yaml:"errors"`
			Quota   struct {
				MaxQueues int `yaml:"max_queues_default"`
			} `yaml:"quota"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(adapterData, &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Adapter.Route != "/compat/aws/sqs" || adapter.Adapter.Quota.MaxQueues == 0 {
		t.Fatalf("adapter artifact = %#v", adapter.Adapter)
	}
	for _, action := range []string{"create", "attributes", "send", "receive", "delete"} {
		if adapter.Adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, adapter.Adapter.Actions)
		}
	}
	for _, code := range []string{"AccessDenied", "QueueDoesNotExist", "QueueNameExists", "InvalidParameterValue", "RequestThrottled", "InternalError"} {
		if adapter.Adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, adapter.Adapter.Errors)
		}
	}
}

func TestAWSSQSMigrationArtifactDocumentsCutoverRollback(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "sqs", "migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"# AWS SQS Migration",
		"Source import IDs",
		"aws_sqs_queue",
		"Unsupported actions",
		"Operator decisions",
		"Cutover",
		"Rollback",
		"RabbitMQ",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("migration artifact missing %q", want)
		}
	}
}

func TestAWSS3ArtifactsDocumentLocalSeedAndMappings(t *testing.T) {
	backendData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "s3", "backend.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var backend struct {
		Backend struct {
			Target   string `yaml:"target"`
			Status   string `yaml:"status"`
			Endpoint struct {
				Route string `yaml:"route"`
			} `yaml:"endpoint"`
		} `yaml:"backend"`
	}
	if err := yaml.Unmarshal(backendData, &backend); err != nil {
		t.Fatal(err)
	}
	if backend.Backend.Target != "minio" || backend.Backend.Status != "proposed-seed" || backend.Backend.Endpoint.Route != "/compat/aws/s3" {
		t.Fatalf("backend artifact = %#v", backend.Backend)
	}

	adapterData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "s3", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Adapter struct {
			Route   string            `yaml:"route"`
			Actions map[string]string `yaml:"actions"`
			Errors  map[string]string `yaml:"errors"`
			Quota   struct {
				ObjectQuota string `yaml:"object_quota"`
			} `yaml:"quota"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(adapterData, &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Adapter.Route != "/compat/aws/s3" || adapter.Adapter.Quota.ObjectQuota == "" {
		t.Fatalf("adapter artifact = %#v", adapter.Adapter)
	}
	for _, action := range []string{"create", "head", "put", "get", "delete"} {
		if adapter.Adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, adapter.Adapter.Actions)
		}
	}
	for _, code := range []string{"AccessDenied", "NoSuchBucket", "NoSuchKey", "BucketAlreadyOwnedByYou", "InvalidRequest", "SlowDown", "InternalError"} {
		if adapter.Adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, adapter.Adapter.Errors)
		}
	}
}

func TestAWSS3MigrationArtifactDocumentsLocalLimits(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "s3", "migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"# AWS S3 local compatibility seed",
		"Source identifiers",
		"aws_s3_bucket",
		"MinIO is a migration target only",
		"does not deploy it or prove persistence, backup, cutover, or rollback",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("migration artifact missing %q", want)
		}
	}
}

func TestAWSSecretsManagerAdapterArtifactDocumentsSDKPolicyAndTags(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "secrets-manager", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Adapter struct {
			Route      string            `yaml:"route"`
			Actions    map[string]string `yaml:"actions"`
			Errors     map[string]string `yaml:"errors"`
			Pagination struct {
				MaxResults string `yaml:"max_results"`
				NextToken  string `yaml:"next_token"`
				Range      string `yaml:"max_results_range"`
			} `yaml:"pagination"`
			Idempotency struct {
				PutSecretValue string `yaml:"put_secret_value"`
			} `yaml:"idempotency"`
			Quota struct {
				SecretQuota string `yaml:"secret_quota"`
			} `yaml:"quota"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Adapter.Route != "/compat/aws/secretsmanager" {
		t.Fatalf("adapter route = %q", artifact.Adapter.Route)
	}
	if artifact.Adapter.Pagination.MaxResults != "MaxResults" || artifact.Adapter.Pagination.NextToken != "NextToken" || artifact.Adapter.Pagination.Range != "1-100" || artifact.Adapter.Quota.SecretQuota != "configurable; default unlimited" {
		t.Fatalf("pagination/quota artifact = %#v / %#v", artifact.Adapter.Pagination, artifact.Adapter.Quota)
	}
	if artifact.Adapter.Errors["InvalidRequestException"] != "reused ClientRequestToken with different secret data" {
		t.Fatalf("InvalidRequestException mapping = %q", artifact.Adapter.Errors["InvalidRequestException"])
	}
	if artifact.Adapter.Idempotency.PutSecretValue != "ClientRequestToken becomes VersionId; identical data replays, different data returns InvalidRequestException" {
		t.Fatalf("PutSecretValue idempotency mapping = %q", artifact.Adapter.Idempotency.PutSecretValue)
	}
	for _, action := range []string{"create", "put_value", "get", "describe", "list", "get_policy", "put_policy", "delete_policy", "tag", "untag", "delete"} {
		if artifact.Adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, artifact.Adapter.Actions)
		}
	}
}

func TestAWSCloudWatchLogsAdapterArtifactDocumentsSupportedActions(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "cloudwatch", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Adapter struct {
			Route   string            `yaml:"route"`
			Actions map[string]string `yaml:"actions"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Adapter.Route != "/compat/aws/cloudwatchlogs" {
		t.Fatalf("adapter route = %q", artifact.Adapter.Route)
	}
	want := map[string]string{
		"create_group":       "logs:CreateLogGroup",
		"create_stream":      "logs:CreateLogStream",
		"delete_stream":      "logs:DeleteLogStream",
		"delete_group":       "logs:DeleteLogGroup",
		"put_retention":      "logs:PutRetentionPolicy",
		"delete_retention":   "logs:DeleteRetentionPolicy",
		"list_tags":          "logs:ListTagsLogGroup",
		"tag":                "logs:TagLogGroup",
		"untag":              "logs:UntagLogGroup",
		"list_resource_tags": "logs:ListTagsForResource",
		"tag_resource":       "logs:TagResource",
		"untag_resource":     "logs:UntagResource",
		"describe_groups":    "logs:DescribeLogGroups",
		"put_events":         "logs:PutLogEvents",
		"get_events":         "logs:GetLogEvents",
		"describe_streams":   "logs:DescribeLogStreams",
	}
	if !reflect.DeepEqual(artifact.Adapter.Actions, want) {
		t.Fatalf("action mappings = %#v, want %#v", artifact.Adapter.Actions, want)
	}
}

func TestAWSSNSArtifactsDocumentLocalSeedMappingsAndLimits(t *testing.T) {
	backendData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "sns", "backend.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var backend struct {
		Backend struct {
			Target   string `yaml:"target"`
			Status   string `yaml:"status"`
			Endpoint struct {
				Route string `yaml:"route"`
			} `yaml:"endpoint"`
		} `yaml:"backend"`
	}
	if err := yaml.Unmarshal(backendData, &backend); err != nil {
		t.Fatal(err)
	}
	if backend.Backend.Target != "nats" || backend.Backend.Status != "proposed-seed" || backend.Backend.Endpoint.Route != "/compat/aws/sns" {
		t.Fatalf("backend artifact = %#v", backend.Backend)
	}

	adapterData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "sns", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Adapter struct {
			Route       string            `yaml:"route"`
			Actions     map[string]string `yaml:"actions"`
			Errors      map[string]string `yaml:"errors"`
			Idempotency struct {
				Fields  map[string]string `yaml:"fields"`
				Methods []string          `yaml:"methods"`
			} `yaml:"idempotency"`
			Quota struct {
				TopicQuota string `yaml:"topic_quota"`
			} `yaml:"quota"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(adapterData, &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Adapter.Route != "/compat/aws/sns" || adapter.Adapter.Quota.TopicQuota == "" {
		t.Fatalf("adapter artifact = %#v", adapter.Adapter)
	}
	for _, action := range []string{"create", "attributes", "list", "set", "delete", "list_tags", "tag", "untag", "subscribe", "list_subscriptions", "list_subscriptions_by_topic", "unsubscribe", "publish"} {
		if adapter.Adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, adapter.Adapter.Actions)
		}
	}
	for _, code := range []string{"AccessDenied", "NotFound", "InvalidParameter", "Throttled", "InternalError"} {
		if adapter.Adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, adapter.Adapter.Errors)
		}
	}
	if !reflect.DeepEqual(adapter.Adapter.Idempotency.Fields, map[string]string{
		"CreateTopic": "Name", "DeleteTopic": "TopicArn", "Publish": "MessageDeduplicationId",
	}) || !reflect.DeepEqual(adapter.Adapter.Idempotency.Methods, []string{"CreateTopic", "DeleteTopic", "Publish"}) {
		t.Fatalf("idempotency artifact = %#v", adapter.Adapter.Idempotency)
	}

	migrationData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "sns", "migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(migrationData)
	for _, want := range []string{
		"# AWS SNS local compatibility seed",
		"Source identifiers",
		"aws_sns_topic",
		"NATS is a migration target only",
		"does not deploy it or prove persistence, backup, cutover, or rollback",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("migration artifact missing %q", want)
		}
	}
}

func TestAWSKinesisArtifactsDocumentRedpandaRuntimeMappingsAndMigration(t *testing.T) {
	backendData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "kinesis", "backend.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var backend struct {
		Backend struct {
			Target  string `yaml:"target"`
			Runtime struct {
				Image      string `yaml:"image"`
				HealthPath string `yaml:"health_path"`
			} `yaml:"runtime"`
			Persistence struct {
				Volume string `yaml:"volume"`
			} `yaml:"persistence"`
			Endpoint struct {
				Route string `yaml:"route"`
			} `yaml:"endpoint"`
		} `yaml:"backend"`
	}
	if err := yaml.Unmarshal(backendData, &backend); err != nil {
		t.Fatal(err)
	}
	if backend.Backend.Target != "redpanda" || backend.Backend.Runtime.Image == "" || backend.Backend.Runtime.HealthPath == "" || backend.Backend.Persistence.Volume == "" || backend.Backend.Endpoint.Route != "/compat/aws/kinesis" {
		t.Fatalf("backend artifact = %#v", backend.Backend)
	}

	adapterData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "kinesis", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Adapter struct {
			Route   string            `yaml:"route"`
			Actions map[string]string `yaml:"actions"`
			Errors  map[string]string `yaml:"errors"`
			Quota   struct {
				MaxStreams int `yaml:"max_streams_default"`
			} `yaml:"quota"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(adapterData, &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Adapter.Route != "/compat/aws/kinesis" || adapter.Adapter.Quota.MaxStreams == 0 {
		t.Fatalf("adapter artifact = %#v", adapter.Adapter)
	}
	for _, action := range []string{"create", "describe", "list", "update_shards", "delete"} {
		if adapter.Adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, adapter.Adapter.Actions)
		}
	}
	for _, code := range []string{"AccessDeniedException", "ResourceNotFoundException", "ResourceInUseException", "InvalidArgumentException", "LimitExceededException", "InternalFailure"} {
		if adapter.Adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, adapter.Adapter.Errors)
		}
	}

	migrationData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "kinesis", "migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(migrationData)
	for _, want := range []string{
		"# AWS Kinesis Migration",
		"Source import IDs",
		"aws_kinesis_stream",
		"Unsupported actions",
		"Operator decisions",
		"Cutover",
		"Rollback",
		"Redpanda",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("migration artifact missing %q", want)
		}
	}
}

func TestAWSKMSArtifactsDocumentVaultTransitRuntimeMappingsAndMigration(t *testing.T) {
	backendData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "kms", "backend.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var backend struct {
		Backend struct {
			Target  string `yaml:"target"`
			Runtime struct {
				Image      string `yaml:"image"`
				HealthPath string `yaml:"health_path"`
			} `yaml:"runtime"`
			Persistence struct {
				Volume string `yaml:"volume"`
			} `yaml:"persistence"`
			Endpoint struct {
				Route string `yaml:"route"`
			} `yaml:"endpoint"`
		} `yaml:"backend"`
	}
	if err := yaml.Unmarshal(backendData, &backend); err != nil {
		t.Fatal(err)
	}
	if backend.Backend.Target != "vault-transit" || backend.Backend.Runtime.Image == "" || backend.Backend.Runtime.HealthPath == "" || backend.Backend.Persistence.Volume == "" || backend.Backend.Endpoint.Route != "/compat/aws/kms" {
		t.Fatalf("backend artifact = %#v", backend.Backend)
	}

	adapterData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "kms", "adapter.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var adapter struct {
		Adapter struct {
			Route   string            `yaml:"route"`
			Actions map[string]string `yaml:"actions"`
			Errors  map[string]string `yaml:"errors"`
			Quota   struct {
				MaxKeys int `yaml:"max_keys_default"`
			} `yaml:"quota"`
		} `yaml:"adapter"`
	}
	if err := yaml.Unmarshal(adapterData, &adapter); err != nil {
		t.Fatal(err)
	}
	if adapter.Adapter.Route != "/compat/aws/kms" || adapter.Adapter.Quota.MaxKeys == 0 {
		t.Fatalf("adapter artifact = %#v", adapter.Adapter)
	}
	for _, action := range []string{"create", "describe", "list", "schedule_delete"} {
		if adapter.Adapter.Actions[action] == "" {
			t.Fatalf("missing action mapping %q in %#v", action, adapter.Adapter.Actions)
		}
	}
	for _, code := range []string{"AccessDeniedException", "NotFoundException", "AlreadyExistsException", "InvalidRequestException", "LimitExceededException", "KMSInternalException"} {
		if adapter.Adapter.Errors[code] == "" {
			t.Fatalf("missing error mapping %q in %#v", code, adapter.Adapter.Errors)
		}
	}

	migrationData, err := os.ReadFile(filepath.Join("..", "..", "..", "artifacts", "compat", "aws", "kms", "migration.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(migrationData)
	for _, want := range []string{
		"# AWS KMS Migration",
		"Source import IDs",
		"aws_kms_key",
		"Unsupported actions",
		"Operator decisions",
		"Cutover",
		"Rollback",
		"Vault Transit",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("migration artifact missing %q", want)
		}
	}
}

func completeManifest(command string) domain.Manifest {
	checks := map[domain.Check]string{}
	for _, check := range domain.RequiredChecks() {
		checks[check] = command
	}
	return domain.Manifest{
		Provider: "aws",
		Service:  "S3",
		Checks:   checks,
		Evidence: map[string]string{"target": "MinIO", "app_change_mode": "adapter"},
	}
}
