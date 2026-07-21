package compat_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestSecretsManagerCompatibilityAdapterWithAWSSDK(t *testing.T) {
	adapter := compataws.NewSecretsAdapter()
	adapter.PutSecret("app/db", "postgres://user:pass@db/app")
	server := httptest.NewServer(adapter)
	defer server.Close()

	client := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	got, err := client.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{
		SecretId: aws.String("app/db"),
	})
	if err != nil {
		t.Fatalf("GetSecretValue() error = %v", err)
	}
	if *got.SecretString != "postgres://user:pass@db/app" {
		t.Fatalf("SecretString = %q", *got.SecretString)
	}

	desc, err := client.DescribeSecret(context.Background(), &secretsmanager.DescribeSecretInput{
		SecretId: aws.String("app/db"),
	})
	if err != nil {
		t.Fatalf("DescribeSecret() error = %v", err)
	}
	if *desc.Name != "app/db" {
		t.Fatalf("DescribeSecret name = %q", *desc.Name)
	}

	list, err := client.ListSecrets(context.Background(), &secretsmanager.ListSecretsInput{})
	if err != nil {
		t.Fatalf("ListSecrets() error = %v", err)
	}
	if len(list.SecretList) != 1 || *list.SecretList[0].Name != "app/db" {
		t.Fatalf("ListSecrets() = %#v", list.SecretList)
	}
}

func TestSecretsManagerCompatibilityAdapterUpdatesSecretWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()
	client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{Name: aws.String("updated"), Description: aws.String("before"), SecretString: aws.String("one")}); err != nil {
		t.Fatalf("CreateSecret() error = %v", err)
	}
	if _, err := client.UpdateSecret(ctx, &secretsmanager.UpdateSecretInput{SecretId: aws.String("updated"), Description: aws.String("after"), SecretString: aws.String("two")}); err != nil {
		t.Fatalf("UpdateSecret() error = %v", err)
	}
	desc, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: aws.String("updated")})
	if err != nil || aws.ToString(desc.Description) != "after" {
		t.Fatalf("DescribeSecret() = %#v, %v; want updated description", desc, err)
	}
	value, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String("updated")})
	if err != nil || aws.ToString(value.SecretString) != "two" {
		t.Fatalf("GetSecretValue() = %#v, %v; want updated value", value, err)
	}
}

func TestSecretsManagerCompatibilityAdapterManagesResourcePolicyWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()
	client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	secretID := aws.String("app/policy")
	if _, err := client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{Name: secretID, SecretString: aws.String("value")}); err != nil {
		t.Fatalf("CreateSecret() error = %v", err)
	}

	assertPolicy := func(want string) {
		t.Helper()
		got, err := client.GetResourcePolicy(ctx, &secretsmanager.GetResourcePolicyInput{SecretId: secretID})
		if err != nil || aws.ToString(got.ResourcePolicy) != want {
			t.Fatalf("GetResourcePolicy() = %q, %v; want %q", aws.ToString(got.ResourcePolicy), err, want)
		}
	}
	for _, policy := range []string{
		`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:root"},"Action":"secretsmanager:GetSecretValue","Resource":"*"}]}`,
		`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","Action":"secretsmanager:DeleteSecret","Resource":"*"}]}`,
	} {
		if _, err := client.PutResourcePolicy(ctx, &secretsmanager.PutResourcePolicyInput{SecretId: secretID, ResourcePolicy: aws.String(policy)}); err != nil {
			t.Fatalf("PutResourcePolicy() error = %v", err)
		}
		assertPolicy(policy)
	}
	if _, err := client.DeleteResourcePolicy(ctx, &secretsmanager.DeleteResourcePolicyInput{SecretId: secretID}); err != nil {
		t.Fatalf("DeleteResourcePolicy() error = %v", err)
	}
	assertPolicy(`{"Version":"2012-10-17","Statement":[]}`)
}

func TestSecretsManagerCompatibilityAdapterManagesTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()
	client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	secretID := aws.String("app/tags")
	if _, err := client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{Name: secretID, SecretString: aws.String("value"), Tags: []types.Tag{{Key: aws.String("env"), Value: aws.String("test")}}}); err != nil {
		t.Fatalf("CreateSecret() error = %v", err)
	}
	assertTags := func(want map[string]string) {
		t.Helper()
		described, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: secretID})
		if err != nil {
			t.Fatalf("DescribeSecret() error = %v", err)
		}
		got := make(map[string]string, len(described.Tags))
		for _, tag := range described.Tags {
			got[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
		if !maps.Equal(got, want) {
			t.Fatalf("DescribeSecret() tags = %#v, want %#v", got, want)
		}
	}
	assertTags(map[string]string{"env": "test"})
	if _, err := client.TagResource(ctx, &secretsmanager.TagResourceInput{SecretId: secretID, Tags: []types.Tag{{Key: aws.String("env"), Value: aws.String("prod")}, {Key: aws.String("owner"), Value: aws.String("platform")}}}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	assertTags(map[string]string{"env": "prod", "owner": "platform"})
	if _, err := client.UntagResource(ctx, &secretsmanager.UntagResourceInput{SecretId: secretID, TagKeys: []string{"owner"}}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	assertTags(map[string]string{"env": "prod"})
}

func TestSecretsManagerCompatibilityAdapterDeniesAuditsAndDoesNotMutatePoliciesOrTags(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
		call   func(context.Context, *secretsmanager.Client, *string) error
	}{
		{"GetResourcePolicy", "secretsmanager:GetResourcePolicy", func(ctx context.Context, client *secretsmanager.Client, secretID *string) error {
			_, err := client.GetResourcePolicy(ctx, &secretsmanager.GetResourcePolicyInput{SecretId: secretID})
			return err
		}},
		{"PutResourcePolicy", "secretsmanager:PutResourcePolicy", func(ctx context.Context, client *secretsmanager.Client, secretID *string) error {
			_, err := client.PutResourcePolicy(ctx, &secretsmanager.PutResourcePolicyInput{SecretId: secretID, ResourcePolicy: aws.String(`{"changed":true}`)})
			return err
		}},
		{"DeleteResourcePolicy", "secretsmanager:DeleteResourcePolicy", func(ctx context.Context, client *secretsmanager.Client, secretID *string) error {
			_, err := client.DeleteResourcePolicy(ctx, &secretsmanager.DeleteResourcePolicyInput{SecretId: secretID})
			return err
		}},
		{"TagResource", "secretsmanager:TagResource", func(ctx context.Context, client *secretsmanager.Client, secretID *string) error {
			_, err := client.TagResource(ctx, &secretsmanager.TagResourceInput{SecretId: secretID, Tags: []types.Tag{{Key: aws.String("env"), Value: aws.String("prod")}}})
			return err
		}},
		{"UntagResource", "secretsmanager:UntagResource", func(ctx context.Context, client *secretsmanager.Client, secretID *string) error {
			_, err := client.UntagResource(ctx, &secretsmanager.UntagResourceInput{SecretId: secretID, TagKeys: []string{"env"}})
			return err
		}},
		{"DescribeSecret", "secretsmanager:DescribeSecret", func(ctx context.Context, client *secretsmanager.Client, secretID *string) error {
			_, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: secretID})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deny := false
			auditLog := authz.NewAuditLog()
			server := httptest.NewServer(compataws.NewSecretsAdapter(
				compataws.WithSecretsAuthorizer(authz.AuthorizerFunc(func(_ context.Context, req authz.Request) (authz.Decision, error) {
					allowed := !deny || req.Action != tc.action
					return authz.Decision{Request: req, Allowed: allowed}, nil
				})),
				compataws.WithSecretsAuditSink(auditLog.Record),
			))
			defer server.Close()
			client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(server.URL) })
			ctx := context.Background()
			secretID := aws.String("app/protected")
			const policy = `{"Version":"2012-10-17","Statement":[]}`
			if _, err := client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{Name: secretID, SecretString: aws.String("value"), Tags: []types.Tag{{Key: aws.String("env"), Value: aws.String("test")}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := client.PutResourcePolicy(ctx, &secretsmanager.PutResourcePolicyInput{SecretId: secretID, ResourcePolicy: aws.String(policy)}); err != nil {
				t.Fatal(err)
			}

			deny = true
			var apiErr smithy.APIError
			if err := tc.call(ctx, client, secretID); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
				t.Fatalf("%s(denied) error = %v, want AccessDenied", tc.name, err)
			}
			assertDecision(t, auditLog.Decisions(), tc.action, false)
			wantARN := "arn:aws:secretsmanager:us-east-1:000000000000:secret:app/protected-homeport"
			for _, decision := range auditLog.Decisions() {
				if decision.Request.Action == tc.action && !decision.Allowed && decision.Request.Resource != wantARN {
					t.Fatalf("%s audit resource = %q, want %q", tc.name, decision.Request.Resource, wantARN)
				}
			}
			deny = false

			gotPolicy, err := client.GetResourcePolicy(ctx, &secretsmanager.GetResourcePolicyInput{SecretId: secretID})
			if err != nil || aws.ToString(gotPolicy.ResourcePolicy) != policy {
				t.Fatalf("GetResourcePolicy(after denied %s) = %q, %v; want %q", tc.name, aws.ToString(gotPolicy.ResourcePolicy), err, policy)
			}
			described, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: secretID})
			if err != nil || len(described.Tags) != 1 || aws.ToString(described.Tags[0].Key) != "env" || aws.ToString(described.Tags[0].Value) != "test" {
				t.Fatalf("DescribeSecret(after denied %s) tags = %#v, %v; want env=test", tc.name, described.Tags, err)
			}
		})
	}
}

func TestSecretsManagerCompatibilityAdapterAuthorizesNamedCreateWithAWSSDK(t *testing.T) {
	secretARN := "arn:aws:secretsmanager:us-east-1:000000000000:secret:orders-homeport"
	server := httptest.NewServer(compataws.NewSecretsAdapter(compataws.WithSecretsAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{Effect: authz.Allow, Actions: []string{"secretsmanager:CreateSecret"}, Resources: []string{secretARN}}))))
	defer server.Close()
	client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{Name: aws.String("orders"), SecretString: aws.String("value")})
	if err != nil || aws.ToString(created.ARN) != secretARN {
		t.Fatalf("CreateSecret() = %#v, %v", created, err)
	}
}

func TestSecretsManagerCompatibilityAdapterReadsHyphenatedSecretByReturnedARN(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()
	client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{Name: aws.String("orders-api"), SecretString: aws.String("value")})
	if err != nil {
		t.Fatalf("CreateSecret() error = %v", err)
	}
	got, err := client.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{SecretId: created.ARN})
	if err != nil || aws.ToString(got.SecretString) != "value" {
		t.Fatalf("GetSecretValue(returned ARN) = %#v, %v", got, err)
	}
}

func TestSecretsManagerCompatibilityAdapterShapesAuthorizerFailure(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter(compataws.WithSecretsAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
		return authz.Decision{}, errors.New("authorizer unavailable")
	}))))
	defer server.Close()

	client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListSecrets(context.Background(), &secretsmanager.ListSecretsInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InternalServiceError" {
		t.Fatalf("ListSecrets(authorizer failure) error = %v, want InternalServiceError", err)
	}
}

func TestSecretsManagerCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()

	runAWS := func(args ...string) []byte {
		t.Helper()
		base := []string{"--endpoint-url", server.URL, "--region", "us-east-1", "--output", "json", "--no-cli-pager"}
		cmd := exec.Command("aws", append(base, args...)...)
		cmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID=homeport",
			"AWS_SECRET_ACCESS_KEY=homeport",
			"AWS_EC2_METADATA_DISABLED=true",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("aws %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}

	var created struct {
		Name      string `json:"Name"`
		VersionID string `json:"VersionId"`
	}
	if err := json.Unmarshal(runAWS("secretsmanager", "create-secret", "--name", "cli/db", "--secret-string", "postgres://first"), &created); err != nil {
		t.Fatalf("decode create-secret output: %v", err)
	}
	if created.Name != "cli/db" || created.VersionID == "" {
		t.Fatalf("create-secret = %#v, want name and version", created)
	}

	var got struct {
		SecretString string `json:"SecretString"`
		VersionID    string `json:"VersionId"`
	}
	if err := json.Unmarshal(runAWS("secretsmanager", "get-secret-value", "--secret-id", "cli/db"), &got); err != nil {
		t.Fatalf("decode get-secret-value output: %v", err)
	}
	if got.SecretString != "postgres://first" || got.VersionID != created.VersionID {
		t.Fatalf("get-secret-value = %#v, want first version", got)
	}

	runAWS("secretsmanager", "put-secret-value", "--secret-id", "cli/db", "--secret-string", "postgres://second")
	if err := json.Unmarshal(runAWS("secretsmanager", "get-secret-value", "--secret-id", "cli/db"), &got); err != nil {
		t.Fatalf("decode get-secret-value after update output: %v", err)
	}
	if got.SecretString != "postgres://second" {
		t.Fatalf("get-secret-value after update = %#v, want second value", got)
	}

	runAWS("secretsmanager", "delete-secret", "--secret-id", "cli/db", "--force-delete-without-recovery")
}

func TestSecretsManagerCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()

	dir := t.TempDir()
	config := fmt.Sprintf(`terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "5.47.0"
    }
  }
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "homeport"
  secret_key                  = "homeport"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  skip_region_validation      = true

  endpoints {
    secretsmanager = %q
  }
}

resource "aws_secretsmanager_secret" "deploy" {
  name                    = "terraform/db"
  recovery_window_in_days = 0
  tags = {
    env = "test"
  }
}

resource "aws_secretsmanager_secret_version" "deploy" {
  secret_id     = aws_secretsmanager_secret.deploy.id
  secret_string = "postgres://terraform"
}

output "secret_arn" {
  value = aws_secretsmanager_secret.deploy.arn
}
`, server.URL)
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(config), 0o600); err != nil {
		t.Fatalf("write Terraform config: %v", err)
	}

	runTerraform := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command("terraform", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID=homeport",
			"AWS_SECRET_ACCESS_KEY=homeport",
			"AWS_EC2_METADATA_DISABLED=true",
			"CHECKPOINT_DISABLE=1",
			"TF_IN_AUTOMATION=1",
			"TF_CLI_ARGS=-no-color",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("terraform %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}

	initCmd := exec.Command("terraform", "init", "-input=false")
	initCmd.Dir = dir
	initCmd.Env = append(os.Environ(),
		"AWS_EC2_METADATA_DISABLED=true",
		"CHECKPOINT_DISABLE=1",
		"TF_IN_AUTOMATION=1",
		"TF_CLI_ARGS=-no-color",
	)
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Skipf("terraform AWS provider unavailable: %v\n%s", err, out)
	}

	runTerraform("apply", "-input=false", "-auto-approve")
	defer runTerraform("destroy", "-input=false", "-auto-approve")

	if arn := strings.TrimSpace(string(runTerraform("output", "-raw", "secret_arn"))); arn == "" {
		t.Fatalf("terraform output secret_arn is empty")
	}
}

func TestSecretsManagerCompatibilityAdapterLifecycleAndVersionsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()

	client := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{
		Name:         aws.String("app/db"),
		SecretString: aws.String("postgres://first"),
	})
	if err != nil {
		t.Fatalf("CreateSecret() error = %v", err)
	}
	createdVersion := aws.ToString(created.VersionId)
	if createdVersion == "" {
		t.Fatal("CreateSecret VersionId is empty")
	}

	updated, err := client.PutSecretValue(context.Background(), &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String("app/db"),
		SecretString: aws.String("postgres://second"),
	})
	if err != nil {
		t.Fatalf("PutSecretValue() error = %v", err)
	}
	updatedVersion := aws.ToString(updated.VersionId)
	if updatedVersion == "" || updatedVersion == createdVersion {
		t.Fatalf("PutSecretValue VersionId = %q, want generated ClientRequestToken", updatedVersion)
	}

	got, err := client.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{
		SecretId: aws.String("app/db"),
	})
	if err != nil {
		t.Fatalf("GetSecretValue() error = %v", err)
	}
	if aws.ToString(got.SecretString) != "postgres://second" || aws.ToString(got.VersionId) != updatedVersion {
		t.Fatalf("GetSecretValue() = value %q version %q, want second/%s", aws.ToString(got.SecretString), aws.ToString(got.VersionId), updatedVersion)
	}

	desc, err := client.DescribeSecret(context.Background(), &secretsmanager.DescribeSecretInput{
		SecretId: aws.String("app/db"),
	})
	if err != nil {
		t.Fatalf("DescribeSecret() error = %v", err)
	}
	if _, ok := desc.VersionIdsToStages[createdVersion]; !ok {
		t.Fatalf("DescribeSecret versions = %#v, want created version retained", desc.VersionIdsToStages)
	}
	if stages := desc.VersionIdsToStages[updatedVersion]; len(stages) != 1 || stages[0] != "AWSCURRENT" {
		t.Fatalf("DescribeSecret current version stages = %#v, want AWSCURRENT", stages)
	}

	previous, err := client.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String("app/db"),
		VersionStage: aws.String("AWSPREVIOUS"),
	})
	if err != nil {
		t.Fatalf("GetSecretValue(AWSPREVIOUS) error = %v", err)
	}
	if aws.ToString(previous.SecretString) != "postgres://first" || aws.ToString(previous.VersionId) != createdVersion {
		t.Fatalf("GetSecretValue(AWSPREVIOUS) = value %q version %q, want first/%s", aws.ToString(previous.SecretString), aws.ToString(previous.VersionId), createdVersion)
	}

	_, err = client.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String("app/db"),
		VersionStage: aws.String("AWSPENDING"),
	})
	var missingStage smithy.APIError
	if err == nil || !errors.As(err, &missingStage) || missingStage.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("GetSecretValue(missing stage) error = %v, want ResourceNotFoundException", err)
	}

	if _, err := client.DeleteSecret(context.Background(), &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String("app/db"),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	}); err != nil {
		t.Fatalf("DeleteSecret() error = %v", err)
	}
	_, err = client.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{
		SecretId: aws.String("app/db"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("GetSecretValue(deleted) error = %v, want ResourceNotFoundException", err)
	}
}

func TestSecretsManagerCompatibilityAdapterRejectsDuplicateAndNamelessCreate(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()

	client := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{
		Name:         aws.String("app/db"),
		SecretString: aws.String("postgres://first"),
	}); err != nil {
		t.Fatalf("CreateSecret(first) error = %v", err)
	}

	_, err := client.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{
		Name:         aws.String("app/db"),
		SecretString: aws.String("postgres://second"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceExistsException" {
		t.Fatalf("CreateSecret(duplicate) error = %v, want ResourceExistsException", err)
	}

}

func TestSecretsManagerCompatibilityAdapterHonorsCreateSecretClientRequestToken(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()
	client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	input := &secretsmanager.CreateSecretInput{
		Name:               aws.String("app/create-retry"),
		SecretString:       aws.String("first"),
		ClientRequestToken: aws.String("12345678-1234-1234-1234-123456789012"),
	}
	created, err := client.CreateSecret(ctx, input)
	if err != nil || aws.ToString(created.VersionId) != aws.ToString(input.ClientRequestToken) {
		t.Fatalf("CreateSecret(first) = %#v, %v; want ClientRequestToken as VersionId", created, err)
	}
	replayed, err := client.CreateSecret(ctx, input)
	if err != nil || aws.ToString(replayed.VersionId) != aws.ToString(created.VersionId) {
		t.Fatalf("CreateSecret(replay) = %#v, %v; want version %q", replayed, err, aws.ToString(created.VersionId))
	}

	input.SecretString = aws.String("changed")
	_, err = client.CreateSecret(ctx, input)
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidRequestException" {
		t.Fatalf("CreateSecret(mismatched token) error = %v, want InvalidRequestException", err)
	}
	got, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: input.Name})
	if err != nil || aws.ToString(got.SecretString) != "first" || aws.ToString(got.VersionId) != aws.ToString(created.VersionId) {
		t.Fatalf("GetSecretValue(after mismatch) = %#v, %v; want unchanged first version", got, err)
	}

	input.SecretString = aws.String("first")
	input.ClientRequestToken = aws.String("87654321-4321-4321-4321-210987654321")
	_, err = client.CreateSecret(ctx, input)
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceExistsException" {
		t.Fatalf("CreateSecret(duplicate name) error = %v, want ResourceExistsException", err)
	}
}

func TestSecretsManagerCompatibilityAdapterReturnsLimitExceededWhenQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter(compataws.WithSecretsQuota(1)))
	defer server.Close()

	client := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{
		Name:         aws.String("app/one"),
		SecretString: aws.String("one"),
	}); err != nil {
		t.Fatalf("CreateSecret(first) error = %v", err)
	}

	_, err := client.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{
		Name:         aws.String("app/two"),
		SecretString: aws.String("two"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("CreateSecret(over quota) error = %v, want LimitExceededException", err)
	}
}

func TestSecretsManagerCompatibilityAdapterPaginatesListSecrets(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()

	client := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, name := range []string{"app/a", "app/b", "app/c"} {
		if _, err := client.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{
			Name:         aws.String(name),
			SecretString: aws.String(name),
		}); err != nil {
			t.Fatalf("CreateSecret(%s) error = %v", name, err)
		}
	}

	first, err := client.ListSecrets(context.Background(), &secretsmanager.ListSecretsInput{MaxResults: aws.Int32(2)})
	if err != nil {
		t.Fatalf("ListSecrets(first) error = %v", err)
	}
	if len(first.SecretList) != 2 || first.NextToken == nil {
		t.Fatalf("ListSecrets(first) = %#v, want two secrets and token", first)
	}

	second, err := client.ListSecrets(context.Background(), &secretsmanager.ListSecretsInput{
		MaxResults: aws.Int32(2),
		NextToken:  first.NextToken,
	})
	if err != nil {
		t.Fatalf("ListSecrets(second) error = %v", err)
	}
	if len(second.SecretList) != 1 || second.NextToken != nil {
		t.Fatalf("ListSecrets(second) = %#v, want final secret without token", second)
	}
}

func TestSecretsManagerCompatibilityAdapterReplaysPutSecretValueClientRequestToken(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()
	client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{Name: aws.String("app/retry"), SecretString: aws.String("first")})
	if err != nil {
		t.Fatalf("CreateSecret() error = %v", err)
	}
	input := &secretsmanager.PutSecretValueInput{SecretId: aws.String("app/retry"), SecretString: aws.String("second"), ClientRequestToken: aws.String("retry-token")}
	first, err := client.PutSecretValue(ctx, input)
	if err != nil {
		t.Fatalf("PutSecretValue(first) error = %v", err)
	}
	if aws.ToString(first.VersionId) != "retry-token" {
		t.Fatalf("PutSecretValue(first) VersionId = %q, want ClientRequestToken", aws.ToString(first.VersionId))
	}
	second, err := client.PutSecretValue(ctx, input)
	if err != nil || aws.ToString(second.VersionId) != aws.ToString(first.VersionId) {
		t.Fatalf("PutSecretValue(retry) = %#v, %v; want version %q", second, err, aws.ToString(first.VersionId))
	}
	desc, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: aws.String("app/retry")})
	if err != nil || len(desc.VersionIdsToStages) != 2 {
		t.Fatalf("DescribeSecret() = %#v, %v; want two versions", desc, err)
	}
	previous, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: aws.String("app/retry"), VersionStage: aws.String("AWSPREVIOUS")})
	if err != nil || aws.ToString(previous.SecretString) != "first" || aws.ToString(previous.VersionId) != aws.ToString(created.VersionId) {
		t.Fatalf("GetSecretValue(AWSPREVIOUS) = %#v, %v; want first version", previous, err)
	}
}

func TestSecretsManagerCompatibilityAdapterRejectsMismatchedPutSecretValueClientRequestToken(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()
	client := secretsmanager.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *secretsmanager.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	secretID := aws.String("app/retry-mismatch")
	if _, err := client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{Name: secretID, SecretString: aws.String("first")}); err != nil {
		t.Fatalf("CreateSecret() error = %v", err)
	}
	input := &secretsmanager.PutSecretValueInput{SecretId: secretID, SecretString: aws.String("second"), ClientRequestToken: aws.String("retry-token")}
	first, err := client.PutSecretValue(ctx, input)
	if err != nil {
		t.Fatalf("PutSecretValue(first) error = %v", err)
	}
	input.SecretString = aws.String("changed")
	_, err = client.PutSecretValue(ctx, input)
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidRequestException" {
		t.Fatalf("PutSecretValue(mismatched token) error = %v, want InvalidRequestException", err)
	}
	got, err := client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: secretID})
	if err != nil || aws.ToString(got.SecretString) != "second" || aws.ToString(got.VersionId) != aws.ToString(first.VersionId) {
		t.Fatalf("GetSecretValue() = %#v, %v; want unchanged second version", got, err)
	}
	desc, err := client.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{SecretId: secretID})
	if err != nil || len(desc.VersionIdsToStages) != 2 {
		t.Fatalf("DescribeSecret() = %#v, %v; want two versions", desc, err)
	}
}

func TestSecretsManagerCompatibilityAdapterRejectsInvalidListSecretsMaxResults(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter())
	defer server.Close()

	client := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, maxResults := range []int32{0, 101} {
		_, err := client.ListSecrets(context.Background(), &secretsmanager.ListSecretsInput{MaxResults: aws.Int32(maxResults)})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
			t.Fatalf("ListSecrets(MaxResults=%d) error = %v, want InvalidParameterException", maxResults, err)
		}
	}
}

func TestSecretsManagerCompatibilityAdapterAuthorizesCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter(
		compataws.WithSecretsAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"secretsmanager:CreateSecret"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_age", Values: []string{"10m"}},
			},
		})),
	))
	defer server.Close()

	allowed := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "10m"))
	})
	if _, err := allowed.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{Name: aws.String("app/allowed"), SecretString: aws.String("ok")}); err != nil {
		t.Fatalf("CreateSecret(allowed) error = %v", err)
	}

	denied := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "48h"))
	})
	if _, err := denied.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{Name: aws.String("app/denied"), SecretString: aws.String("blocked")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateSecret(denied) error = %v, want AccessDenied", err)
	}
}

func TestSecretsManagerCompatibilityAdapterAuthorizesClaimCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSecretsAdapter(
		compataws.WithSecretsAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"secretsmanager:CreateSecret"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "claim:mfa", Values: []string{"true"}},
			},
		})),
	))
	defer server.Close()

	allowed := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "true"))
	})
	if _, err := allowed.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{Name: aws.String("app/allowed"), SecretString: aws.String("ok")}); err != nil {
		t.Fatalf("CreateSecret(allowed) error = %v", err)
	}

	denied := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "false"))
	})
	if _, err := denied.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{Name: aws.String("app/denied"), SecretString: aws.String("blocked")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateSecret(denied) error = %v, want AccessDenied", err)
	}
}

func TestSecretsManagerCompatibilityAdapterAuthorizesExpiredCredentialAndPrincipalAttributeConditions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		condition authz.Condition
		allowed   string
		denied    string
	}{
		{
			name:      "expired-credential",
			condition: authz.Condition{Key: "credential_expired", Values: []string{"false"}},
			allowed:   "X-Homeport-Credential-Expired:false",
			denied:    "X-Homeport-Credential-Expired:true",
		},
		{
			name:      "principal-attribute",
			condition: authz.Condition{Key: "principal:department", Values: []string{"finance"}},
			allowed:   "X-Homeport-Principal-Attribute-Department:finance",
			denied:    "X-Homeport-Principal-Attribute-Department:engineering",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(compataws.NewSecretsAdapter(
				compataws.WithSecretsAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
					Effect:     authz.Allow,
					Actions:    []string{"secretsmanager:CreateSecret"},
					Resources:  []string{"*"},
					Conditions: []authz.Condition{tc.condition},
				})),
			))
			defer server.Close()

			allowedName, allowedValue, _ := strings.Cut(tc.allowed, ":")
			allowed := secretsmanager.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *secretsmanager.Options) {
				o.BaseEndpoint = aws.String(server.URL)
				o.APIOptions = append(o.APIOptions, sqsHeader(allowedName, allowedValue))
			})
			if _, err := allowed.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{Name: aws.String("app/" + tc.name + "/allowed"), SecretString: aws.String("ok")}); err != nil {
				t.Fatalf("CreateSecret(allowed %s) error = %v", tc.name, err)
			}

			deniedName, deniedValue, _ := strings.Cut(tc.denied, ":")
			denied := secretsmanager.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *secretsmanager.Options) {
				o.BaseEndpoint = aws.String(server.URL)
				o.APIOptions = append(o.APIOptions, sqsHeader(deniedName, deniedValue))
			})
			if _, err := denied.CreateSecret(context.Background(), &secretsmanager.CreateSecretInput{Name: aws.String("app/" + tc.name + "/denied"), SecretString: aws.String("blocked")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
				t.Fatalf("CreateSecret(denied %s) error = %v, want AccessDenied", tc.name, err)
			}
		})
	}
}

func TestSecretsManagerCompatibilityAdapterAuthorizesAndAuditsAWSSDKCalls(t *testing.T) {
	auditLog := authz.NewAuditLog()
	adapter := compataws.NewSecretsAdapter(
		compataws.WithSecretsAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"secretsmanager:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"secretsmanager:GetSecretValue"}, Resources: []string{"*"}},
		)),
		compataws.WithSecretsAuditSink(auditLog.Record),
	)
	adapter.PutSecret("app/db", "postgres://user:pass@db/app")
	server := httptest.NewServer(adapter)
	defer server.Close()

	client := secretsmanager.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *secretsmanager.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.GetSecretValue(context.Background(), &secretsmanager.GetSecretValueInput{
		SecretId: aws.String("app/db"),
	})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("GetSecretValue() error = %v, want AccessDenied", err)
	}
	if _, err := client.DescribeSecret(context.Background(), &secretsmanager.DescribeSecretInput{
		SecretId: aws.String("app/db"),
	}); err != nil {
		t.Fatalf("DescribeSecret() error = %v", err)
	}

	assertDecision(t, auditLog.Decisions(), "secretsmanager:GetSecretValue", false)
	assertDecision(t, auditLog.Decisions(), "secretsmanager:DescribeSecret", true)
}
