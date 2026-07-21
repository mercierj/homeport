package compat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestKMSCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	plain := []byte("deploy without AWS KMS")
	encrypted, err := client.Encrypt(context.Background(), &kms.EncryptInput{
		KeyId:     aws.String("alias/homeport"),
		Plaintext: plain,
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if bytes.Equal(encrypted.CiphertextBlob, plain) {
		t.Fatal("CiphertextBlob should not equal plaintext")
	}

	decrypted, err := client.Decrypt(context.Background(), &kms.DecryptInput{
		CiphertextBlob: encrypted.CiphertextBlob,
		KeyId:          aws.String("alias/homeport"),
	})
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	if !bytes.Equal(decrypted.Plaintext, plain) {
		t.Fatalf("Plaintext = %q, want %q", decrypted.Plaintext, plain)
	}

	mac, err := client.GenerateMac(context.Background(), &kms.GenerateMacInput{
		KeyId:        aws.String("alias/homeport-hmac"),
		Message:      []byte("message"),
		MacAlgorithm: types.MacAlgorithmSpecHmacSha256,
	})
	if err != nil {
		t.Fatalf("GenerateMac() error = %v", err)
	}
	verified, err := client.VerifyMac(context.Background(), &kms.VerifyMacInput{
		KeyId:        aws.String("alias/homeport-hmac"),
		Message:      []byte("message"),
		Mac:          mac.Mac,
		MacAlgorithm: types.MacAlgorithmSpecHmacSha256,
	})
	if err != nil {
		t.Fatalf("VerifyMac() error = %v", err)
	}
	if !verified.MacValid {
		t.Fatal("MacValid = false, want true")
	}
}

func TestKMSCompatibilityAdapterKeyLifecycleWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{
		Description: aws.String("HomePort key"),
	})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	keyID := aws.ToString(created.KeyMetadata.KeyId)
	if keyID == "" || aws.ToString(created.KeyMetadata.Arn) == "" || created.KeyMetadata.CreationDate == nil {
		t.Fatalf("CreateKey metadata = %#v, want key id, ARN, and creation date", created.KeyMetadata)
	}

	desc, err := client.DescribeKey(context.Background(), &kms.DescribeKeyInput{KeyId: aws.String(keyID)})
	if err != nil {
		t.Fatalf("DescribeKey() error = %v", err)
	}
	if aws.ToString(desc.KeyMetadata.Description) != "HomePort key" || desc.KeyMetadata.KeyState != types.KeyStateEnabled || desc.KeyMetadata.CreationDate == nil {
		t.Fatalf("DescribeKey metadata = %#v, want enabled HomePort key with creation date", desc.KeyMetadata)
	}

	keys, err := client.ListKeys(context.Background(), &kms.ListKeysInput{})
	if err != nil {
		t.Fatalf("ListKeys() error = %v", err)
	}
	if len(keys.Keys) != 1 || aws.ToString(keys.Keys[0].KeyId) != keyID {
		t.Fatalf("ListKeys() = %#v, want created key", keys.Keys)
	}

	deleted, err := client.ScheduleKeyDeletion(context.Background(), &kms.ScheduleKeyDeletionInput{
		KeyId:               aws.String(keyID),
		PendingWindowInDays: aws.Int32(7),
	})
	if err != nil {
		t.Fatalf("ScheduleKeyDeletion() error = %v", err)
	}
	if deleted.DeletionDate == nil || aws.ToString(deleted.KeyId) != keyID || deleted.KeyState != types.KeyStatePendingDeletion || aws.ToInt32(deleted.PendingWindowInDays) != 7 {
		t.Fatalf("ScheduleKeyDeletion() = %#v, want deletion date, key id, pending state, and window", deleted)
	}

	desc, err = client.DescribeKey(context.Background(), &kms.DescribeKeyInput{KeyId: aws.String(keyID)})
	if err != nil {
		t.Fatalf("DescribeKey(after deletion scheduled) error = %v", err)
	}
	if desc.KeyMetadata.KeyState != types.KeyStatePendingDeletion || aws.ToInt32(desc.KeyMetadata.PendingDeletionWindowInDays) != 7 {
		t.Fatalf("DescribeKey after ScheduleKeyDeletion = %#v, want pending state and 7-day window", desc.KeyMetadata)
	}
}

func TestKMSCompatibilityAdapterRejectsDecryptWithDisabledKeyState(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	encrypted, err := client.Encrypt(context.Background(), &kms.EncryptInput{KeyId: created.KeyMetadata.KeyId, Plaintext: []byte("secret")})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	if _, err := client.ScheduleKeyDeletion(context.Background(), &kms.ScheduleKeyDeletionInput{KeyId: created.KeyMetadata.KeyId, PendingWindowInDays: aws.Int32(7)}); err != nil {
		t.Fatalf("ScheduleKeyDeletion() error = %v", err)
	}
	_, err = client.Decrypt(context.Background(), &kms.DecryptInput{KeyId: created.KeyMetadata.KeyId, CiphertextBlob: encrypted.CiphertextBlob})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "KMSInvalidStateException" {
		t.Fatalf("Decrypt(pending deletion) error = %v, want KMSInvalidStateException", err)
	}
}

func TestKMSCompatibilityAdapterDisablesAndEnablesKey(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateKey(ctx, &kms.CreateKeyInput{})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	if _, err := client.DisableKey(ctx, &kms.DisableKeyInput{KeyId: created.KeyMetadata.KeyId}); err != nil {
		t.Fatalf("DisableKey() error = %v", err)
	}
	disabled, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: created.KeyMetadata.KeyId})
	if err != nil || disabled.KeyMetadata.KeyState != types.KeyStateDisabled || disabled.KeyMetadata.Enabled {
		t.Fatalf("DescribeKey(disabled) = %#v, %v; want disabled state", disabled, err)
	}
	if _, err := client.EnableKey(ctx, &kms.EnableKeyInput{KeyId: created.KeyMetadata.KeyId}); err != nil {
		t.Fatalf("EnableKey() error = %v", err)
	}
	enabled, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: created.KeyMetadata.KeyId})
	if err != nil || enabled.KeyMetadata.KeyState != types.KeyStateEnabled || !enabled.KeyMetadata.Enabled {
		t.Fatalf("DescribeKey(enabled) = %#v, %v; want enabled state", enabled, err)
	}
}

func TestKMSCompatibilityAdapterUpdatesKeyDescription(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateKey(ctx, &kms.CreateKeyInput{Description: aws.String("before")})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	if _, err := client.UpdateKeyDescription(ctx, &kms.UpdateKeyDescriptionInput{KeyId: created.KeyMetadata.KeyId, Description: aws.String("after")}); err != nil {
		t.Fatalf("UpdateKeyDescription() error = %v", err)
	}
	described, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: created.KeyMetadata.KeyId})
	if err != nil || aws.ToString(described.KeyMetadata.Description) != "after" {
		t.Fatalf("DescribeKey() = %#v, %v; want updated description", described, err)
	}
}

func TestKMSCompatibilityAdapterRejectsInvalidDescriptionUpdateWithoutMutation(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateKey(ctx, &kms.CreateKeyInput{Description: aws.String("original")})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	_, err = client.UpdateKeyDescription(ctx, &kms.UpdateKeyDescriptionInput{KeyId: created.KeyMetadata.KeyId, Description: aws.String(strings.Repeat("x", 8193))})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
		t.Fatalf("UpdateKeyDescription(oversize) error = %v, want ValidationException", err)
	}
	if _, err := client.ScheduleKeyDeletion(ctx, &kms.ScheduleKeyDeletionInput{KeyId: created.KeyMetadata.KeyId, PendingWindowInDays: aws.Int32(7)}); err != nil {
		t.Fatalf("ScheduleKeyDeletion() error = %v", err)
	}
	_, err = client.UpdateKeyDescription(ctx, &kms.UpdateKeyDescriptionInput{KeyId: created.KeyMetadata.KeyId, Description: aws.String("changed")})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "KMSInvalidStateException" {
		t.Fatalf("UpdateKeyDescription(pending) error = %v, want KMSInvalidStateException", err)
	}
	described, err := client.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: created.KeyMetadata.KeyId})
	if err != nil || aws.ToString(described.KeyMetadata.Description) != "original" {
		t.Fatalf("DescribeKey() = %#v, %v; want unchanged description", described, err)
	}
}

func TestKMSCompatibilityAdapterCancelsScheduledKeyDeletion(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	if _, err := client.ScheduleKeyDeletion(context.Background(), &kms.ScheduleKeyDeletionInput{KeyId: created.KeyMetadata.KeyId, PendingWindowInDays: aws.Int32(7)}); err != nil {
		t.Fatalf("ScheduleKeyDeletion() error = %v", err)
	}
	cancelled, err := client.CancelKeyDeletion(context.Background(), &kms.CancelKeyDeletionInput{KeyId: created.KeyMetadata.KeyId})
	if err != nil || aws.ToString(cancelled.KeyId) != aws.ToString(created.KeyMetadata.KeyId) {
		t.Fatalf("CancelKeyDeletion() = %#v, %v; want key id", cancelled, err)
	}
	described, err := client.DescribeKey(context.Background(), &kms.DescribeKeyInput{KeyId: created.KeyMetadata.KeyId})
	if err != nil || described.KeyMetadata.KeyState != types.KeyStateDisabled {
		t.Fatalf("DescribeKey(after cancel) = %#v, %v; want disabled", described, err)
	}
	if _, err := client.EnableKey(context.Background(), &kms.EnableKeyInput{KeyId: created.KeyMetadata.KeyId}); err != nil {
		t.Fatalf("EnableKey(after cancel) error = %v", err)
	}
	described, err = client.DescribeKey(context.Background(), &kms.DescribeKeyInput{KeyId: created.KeyMetadata.KeyId})
	if err != nil || described.KeyMetadata.KeyState != types.KeyStateEnabled {
		t.Fatalf("DescribeKey(after explicit enable) = %#v, %v; want enabled", described, err)
	}
}

func TestKMSCompatibilityAdapterRejectsCancelKeyDeletionOutsidePendingState(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateKey(ctx, &kms.CreateKeyInput{})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	for _, state := range []string{"enabled", "disabled"} {
		if state == "disabled" {
			if _, err := client.DisableKey(ctx, &kms.DisableKeyInput{KeyId: created.KeyMetadata.KeyId}); err != nil {
				t.Fatalf("DisableKey() error = %v", err)
			}
		}
		_, err := client.CancelKeyDeletion(ctx, &kms.CancelKeyDeletionInput{KeyId: created.KeyMetadata.KeyId})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "KMSInvalidStateException" {
			t.Fatalf("CancelKeyDeletion(%s) error = %v, want KMSInvalidStateException", state, err)
		}
	}
}

func TestKMSCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewKMSAdapter())
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
		KeyMetadata struct {
			KeyID       string `json:"KeyId"`
			Arn         string `json:"Arn"`
			Description string `json:"Description"`
			KeyState    string `json:"KeyState"`
		} `json:"KeyMetadata"`
	}
	if err := json.Unmarshal(runAWS("kms", "create-key", "--description", "HomePort CLI key"), &created); err != nil {
		t.Fatalf("decode create-key output: %v", err)
	}
	if created.KeyMetadata.KeyID == "" || created.KeyMetadata.Arn == "" || created.KeyMetadata.KeyState != "Enabled" {
		t.Fatalf("create-key = %#v, want enabled key metadata", created.KeyMetadata)
	}

	var described struct {
		KeyMetadata struct {
			KeyID       string `json:"KeyId"`
			Description string `json:"Description"`
		} `json:"KeyMetadata"`
	}
	if err := json.Unmarshal(runAWS("kms", "describe-key", "--key-id", created.KeyMetadata.KeyID), &described); err != nil {
		t.Fatalf("decode describe-key output: %v", err)
	}
	if described.KeyMetadata.KeyID != created.KeyMetadata.KeyID || described.KeyMetadata.Description != "HomePort CLI key" {
		t.Fatalf("describe-key = %#v, want created key", described.KeyMetadata)
	}

	var listed struct {
		Keys []struct {
			KeyID string `json:"KeyId"`
		} `json:"Keys"`
	}
	if err := json.Unmarshal(runAWS("kms", "list-keys"), &listed); err != nil {
		t.Fatalf("decode list-keys output: %v", err)
	}
	if len(listed.Keys) != 1 || listed.Keys[0].KeyID != created.KeyMetadata.KeyID {
		t.Fatalf("list-keys = %#v, want created key", listed.Keys)
	}

	runAWS("kms", "schedule-key-deletion", "--key-id", created.KeyMetadata.KeyID, "--pending-window-in-days", "7")
}

func TestKMSCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewKMSAdapter())
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
    kms = %q
  }
}

resource "aws_kms_key" "deploy" {
  description             = "HomePort Terraform key"
  deletion_window_in_days = 7
  tags = {
    env = "test"
  }
}

output "key_id" {
  value = aws_kms_key.deploy.key_id
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

	if keyID := strings.TrimSpace(string(runTerraform("output", "-raw", "key_id"))); keyID == "" {
		t.Fatalf("terraform output key_id is empty")
	}
}

func TestKMSCompatibilityAdapterReturnsNotFoundForMissingKey(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.DescribeKey(context.Background(), &kms.DescribeKeyInput{KeyId: aws.String("missing")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NotFoundException" {
		t.Fatalf("DescribeKey(missing) error = %v, want NotFoundException", err)
	}
}

func TestKMSCompatibilityAdapterReturnsNotFoundForMissingCryptoKey(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for name, call := range map[string]func() error{
		"Encrypt": func() error {
			_, err := client.Encrypt(context.Background(), &kms.EncryptInput{
				KeyId:     aws.String("missing"),
				Plaintext: []byte("secret"),
			})
			return err
		},
		"GenerateMac": func() error {
			_, err := client.GenerateMac(context.Background(), &kms.GenerateMacInput{
				KeyId:        aws.String("missing"),
				Message:      []byte("message"),
				MacAlgorithm: types.MacAlgorithmSpecHmacSha256,
			})
			return err
		},
		"VerifyMac": func() error {
			_, err := client.VerifyMac(context.Background(), &kms.VerifyMacInput{
				KeyId:        aws.String("missing"),
				Message:      []byte("message"),
				Mac:          []byte("mac"),
				MacAlgorithm: types.MacAlgorithmSpecHmacSha256,
			})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			var apiErr smithy.APIError
			if err := call(); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NotFoundException" {
				t.Fatalf("%s(missing key) error = %v, want NotFoundException", name, err)
			}
		})
	}
}

func TestKMSCompatibilityAdapterRejectsEncryptWithPendingDeletionKey(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String("pending")})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	if _, err := client.ScheduleKeyDeletion(context.Background(), &kms.ScheduleKeyDeletionInput{
		KeyId:               created.KeyMetadata.KeyId,
		PendingWindowInDays: aws.Int32(7),
	}); err != nil {
		t.Fatalf("ScheduleKeyDeletion() error = %v", err)
	}

	_, err = client.Encrypt(context.Background(), &kms.EncryptInput{
		KeyId:     created.KeyMetadata.KeyId,
		Plaintext: []byte("blocked"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "KMSInvalidStateException" {
		t.Fatalf("Encrypt(pending deletion key) error = %v, want KMSInvalidStateException", err)
	}
}

func TestKMSCompatibilityAdapterReturnsLimitExceededWhenKeyQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter(compataws.WithKMSKeyQuota(1)))
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String("first")}); err != nil {
		t.Fatalf("CreateKey(first) error = %v", err)
	}
	_, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String("second")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("CreateKey(over quota) error = %v, want LimitExceededException", err)
	}
}

func TestKMSCompatibilityAdapterPaginatesListKeys(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, description := range []string{"first", "second", "third"} {
		if _, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String(description)}); err != nil {
			t.Fatalf("CreateKey(%s) error = %v", description, err)
		}
	}

	first, err := client.ListKeys(context.Background(), &kms.ListKeysInput{Limit: aws.Int32(2)})
	if err != nil {
		t.Fatalf("ListKeys(first) error = %v", err)
	}
	if len(first.Keys) != 2 || !first.Truncated || aws.ToString(first.NextMarker) == "" {
		t.Fatalf("ListKeys(first) = %#v, want two keys and next marker", first)
	}

	second, err := client.ListKeys(context.Background(), &kms.ListKeysInput{
		Limit:  aws.Int32(2),
		Marker: first.NextMarker,
	})
	if err != nil {
		t.Fatalf("ListKeys(second) error = %v", err)
	}
	if len(second.Keys) != 1 || second.Truncated || aws.ToString(second.NextMarker) != "" {
		t.Fatalf("ListKeys(second) = %#v, want final key", second)
	}
}

func TestKMSCompatibilityAdapterRejectsInvalidListKeysMarker(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListKeys(context.Background(), &kms.ListKeysInput{Marker: aws.String("not-a-marker")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidMarkerException" {
		t.Fatalf("ListKeys(invalid marker) error = %v, want InvalidMarkerException", err)
	}
}

func TestKMSCompatibilityAdapterRejectsInvalidListKeysLimit(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	for _, limit := range []int32{0, 1001} {
		_, err := client.ListKeys(context.Background(), &kms.ListKeysInput{Limit: aws.Int32(limit)})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
			t.Fatalf("ListKeys(Limit=%d) error = %v, want ValidationException", limit, err)
		}
	}
}

func TestKMSCompatibilityAdapterRejectsInvalidDeletionWindow(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	for _, days := range []int32{0, 31} {
		created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{})
		if err != nil {
			t.Fatalf("CreateKey() error = %v", err)
		}
		_, err = client.ScheduleKeyDeletion(context.Background(), &kms.ScheduleKeyDeletionInput{KeyId: created.KeyMetadata.KeyId, PendingWindowInDays: aws.Int32(days)})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
			t.Fatalf("ScheduleKeyDeletion(PendingWindowInDays=%d) error = %v, want ValidationException", days, err)
		}
		described, err := client.DescribeKey(context.Background(), &kms.DescribeKeyInput{KeyId: created.KeyMetadata.KeyId})
		if err != nil || described.KeyMetadata.KeyState != types.KeyStateEnabled {
			t.Fatalf("DescribeKey(after invalid window) = %#v, %v; want enabled key", described, err)
		}
	}
}

func TestKMSCompatibilityAdapterPaginatesResourceTags(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Tags: []types.Tag{{TagKey: aws.String("alpha"), TagValue: aws.String("one")}, {TagKey: aws.String("bravo"), TagValue: aws.String("two")}}})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	first, err := client.ListResourceTags(context.Background(), &kms.ListResourceTagsInput{KeyId: created.KeyMetadata.KeyId, Limit: aws.Int32(1)})
	if err != nil || len(first.Tags) != 1 || !first.Truncated || first.NextMarker == nil {
		t.Fatalf("ListResourceTags(first) = %#v, %v; want one tag and marker", first, err)
	}
	second, err := client.ListResourceTags(context.Background(), &kms.ListResourceTagsInput{KeyId: created.KeyMetadata.KeyId, Limit: aws.Int32(1), Marker: first.NextMarker})
	if err != nil || len(second.Tags) != 1 || second.Truncated || second.NextMarker != nil {
		t.Fatalf("ListResourceTags(second) = %#v, %v; want final tag", second, err)
	}
}

func TestKMSCompatibilityAdapterRejectsInvalidResourceTagPagination(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Tags: []types.Tag{{TagKey: aws.String("alpha"), TagValue: aws.String("one")}}})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	for _, limit := range []int32{0, 51} {
		_, err = client.ListResourceTags(context.Background(), &kms.ListResourceTagsInput{KeyId: created.KeyMetadata.KeyId, Limit: aws.Int32(limit)})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
			t.Fatalf("ListResourceTags(Limit=%d) error = %v, want ValidationException", limit, err)
		}
	}
	_, err = client.ListResourceTags(context.Background(), &kms.ListResourceTagsInput{KeyId: created.KeyMetadata.KeyId, Marker: aws.String("forged")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidMarkerException" {
		t.Fatalf("ListResourceTags(invalid marker) error = %v, want InvalidMarkerException", err)
	}
}

func TestKMSCompatibilityAdapterRejectsInvalidTags(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Tags: []types.Tag{{TagKey: aws.String(""), TagValue: aws.String("value")}}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TagException" {
		t.Fatalf("CreateKey(empty tag key) error = %v, want TagException", err)
	}
	tags := make([]types.Tag, 51)
	for i := range tags {
		tags[i] = types.Tag{TagKey: aws.String(fmt.Sprintf("tag-%d", i)), TagValue: aws.String("value")}
	}
	_, err = client.CreateKey(context.Background(), &kms.CreateKeyInput{Tags: tags})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TagException" {
		t.Fatalf("CreateKey(51 tags) error = %v, want TagException", err)
	}
}

func TestKMSCompatibilityAdapterRejectsInvalidTagResourceTags(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()
	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	_, err = client.TagResource(context.Background(), &kms.TagResourceInput{KeyId: created.KeyMetadata.KeyId, Tags: []types.Tag{{TagKey: aws.String(""), TagValue: aws.String("value")}}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TagException" {
		t.Fatalf("TagResource(empty tag key) error = %v, want TagException", err)
	}
}

func TestKMSCompatibilityAdapterAuthorizesCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter(
		compataws.WithKMSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"kms:CreateKey"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_age", Values: []string{"10m"}},
			},
		})),
	))
	defer server.Close()

	allowed := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "10m"))
	})
	if _, err := allowed.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String("allowed")}); err != nil {
		t.Fatalf("CreateKey(allowed) error = %v", err)
	}

	denied := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "48h"))
	})
	if _, err := denied.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String("denied")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateKey(denied) error = %v, want AccessDenied", err)
	}
}

func TestKMSCompatibilityAdapterAuthorizesClaimCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter(
		compataws.WithKMSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"kms:CreateKey"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "claim:mfa", Values: []string{"true"}},
			},
		})),
	))
	defer server.Close()

	allowed := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "true"))
	})
	if _, err := allowed.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String("allowed")}); err != nil {
		t.Fatalf("CreateKey(allowed) error = %v", err)
	}

	denied := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "false"))
	})
	if _, err := denied.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String("denied")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateKey(denied) error = %v, want AccessDenied", err)
	}
}

func TestKMSCompatibilityAdapterAuthorizesExpiredCredentialAndPrincipalAttributeConditions(t *testing.T) {
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
			server := httptest.NewServer(compataws.NewKMSAdapter(
				compataws.WithKMSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
					Effect:     authz.Allow,
					Actions:    []string{"kms:CreateKey"},
					Resources:  []string{"*"},
					Conditions: []authz.Condition{tc.condition},
				})),
			))
			defer server.Close()

			allowedName, allowedValue, _ := strings.Cut(tc.allowed, ":")
			allowed := kms.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *kms.Options) {
				o.BaseEndpoint = aws.String(server.URL)
				o.APIOptions = append(o.APIOptions, sqsHeader(allowedName, allowedValue))
			})
			if _, err := allowed.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String(tc.name + "-allowed")}); err != nil {
				t.Fatalf("CreateKey(allowed %s) error = %v", tc.name, err)
			}

			deniedName, deniedValue, _ := strings.Cut(tc.denied, ":")
			denied := kms.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *kms.Options) {
				o.BaseEndpoint = aws.String(server.URL)
				o.APIOptions = append(o.APIOptions, sqsHeader(deniedName, deniedValue))
			})
			if _, err := denied.CreateKey(context.Background(), &kms.CreateKeyInput{Description: aws.String(tc.name + "-denied")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
				t.Fatalf("CreateKey(denied %s) error = %v, want AccessDenied", tc.name, err)
			}
		})
	}
}

func TestKMSCompatibilityAdapterAuthorizesAndAuditsAWSSDKCalls(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewKMSAdapter(
		compataws.WithKMSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"kms:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"kms:Decrypt"}, Resources: []string{"*"}},
		)),
		compataws.WithKMSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	encrypted, err := client.Encrypt(context.Background(), &kms.EncryptInput{
		KeyId:     aws.String("alias/homeport"),
		Plaintext: []byte("secret"),
	})
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}
	_, err = client.Decrypt(context.Background(), &kms.DecryptInput{
		CiphertextBlob: encrypted.CiphertextBlob,
		KeyId:          aws.String("alias/homeport"),
	})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("Decrypt() error = %v, want AccessDenied", err)
	}

	assertDecision(t, auditLog.Decisions(), "kms:Encrypt", true)
	assertDecision(t, auditLog.Decisions(), "kms:Decrypt", false)
}

func TestKMSCompatibilityAdapterAuthorizesAndAuditsPutKeyPolicyWithoutMutation(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewKMSAdapter(
		compataws.WithKMSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"kms:CreateKey", "kms:GetKeyPolicy"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"kms:PutKeyPolicy"}, Resources: []string{"*"}},
		)),
		compataws.WithKMSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	originalPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}`
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Policy: aws.String(originalPolicy)})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	_, err = client.PutKeyPolicy(context.Background(), &kms.PutKeyPolicyInput{
		KeyId:      created.KeyMetadata.KeyId,
		PolicyName: aws.String("default"),
		Policy:     aws.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}`),
	})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("PutKeyPolicy(denied) error = %v, want AccessDenied", err)
	}

	policy, err := client.GetKeyPolicy(context.Background(), &kms.GetKeyPolicyInput{KeyId: created.KeyMetadata.KeyId, PolicyName: aws.String("default")})
	if err != nil {
		t.Fatalf("GetKeyPolicy() error = %v", err)
	}
	if aws.ToString(policy.Policy) != originalPolicy {
		t.Fatalf("GetKeyPolicy() = %q, want original policy %q after denied update", aws.ToString(policy.Policy), originalPolicy)
	}
	assertDecision(t, auditLog.Decisions(), "kms:PutKeyPolicy", false)
}

func TestKMSCompatibilityAdapterRejectsMalformedPutKeyPolicyWithoutMutation(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	originalPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}`
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Policy: aws.String(originalPolicy)})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	_, err = client.PutKeyPolicy(context.Background(), &kms.PutKeyPolicyInput{
		KeyId:      created.KeyMetadata.KeyId,
		PolicyName: aws.String("default"),
		Policy:     aws.String(`{"Version":`),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "MalformedPolicyDocumentException" {
		t.Fatalf("PutKeyPolicy(malformed) error = %v, want MalformedPolicyDocumentException", err)
	}

	policy, err := client.GetKeyPolicy(context.Background(), &kms.GetKeyPolicyInput{KeyId: created.KeyMetadata.KeyId, PolicyName: aws.String("default")})
	if err != nil {
		t.Fatalf("GetKeyPolicy() error = %v", err)
	}
	if aws.ToString(policy.Policy) != originalPolicy {
		t.Fatalf("GetKeyPolicy() = %q, want original policy %q after malformed update", aws.ToString(policy.Policy), originalPolicy)
	}
}

func TestKMSCompatibilityAdapterRejectsOversizedPutKeyPolicyWithoutMutation(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	originalPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}`
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Policy: aws.String(originalPolicy)})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	_, err = client.PutKeyPolicy(context.Background(), &kms.PutKeyPolicyInput{
		KeyId:      created.KeyMetadata.KeyId,
		PolicyName: aws.String("default"),
		Policy:     aws.String(`"` + strings.Repeat("x", 32769) + `"`),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("PutKeyPolicy(oversized) error = %v, want LimitExceededException", err)
	}

	policy, err := client.GetKeyPolicy(context.Background(), &kms.GetKeyPolicyInput{KeyId: created.KeyMetadata.KeyId, PolicyName: aws.String("default")})
	if err != nil {
		t.Fatalf("GetKeyPolicy() error = %v", err)
	}
	if aws.ToString(policy.Policy) != originalPolicy {
		t.Fatalf("GetKeyPolicy() = %q, want original policy %q after oversized update", aws.ToString(policy.Policy), originalPolicy)
	}
}

func TestKMSCompatibilityAdapterRejectsNonDocumentAndUnsupportedCharacterPutKeyPolicies(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	originalPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}`
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Policy: aws.String(originalPolicy)})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	for _, policyDocument := range []string{`[]`, `{"Statement":"€"}`} {
		_, err := client.PutKeyPolicy(context.Background(), &kms.PutKeyPolicyInput{
			KeyId:      created.KeyMetadata.KeyId,
			PolicyName: aws.String("default"),
			Policy:     aws.String(policyDocument),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "MalformedPolicyDocumentException" {
			t.Fatalf("PutKeyPolicy(%q) error = %v, want MalformedPolicyDocumentException", policyDocument, err)
		}
	}

	policy, err := client.GetKeyPolicy(context.Background(), &kms.GetKeyPolicyInput{KeyId: created.KeyMetadata.KeyId, PolicyName: aws.String("default")})
	if err != nil {
		t.Fatalf("GetKeyPolicy() error = %v", err)
	}
	if aws.ToString(policy.Policy) != originalPolicy {
		t.Fatalf("GetKeyPolicy() = %q, want original policy %q after invalid updates", aws.ToString(policy.Policy), originalPolicy)
	}
}

func TestKMSCompatibilityAdapterRejectsIncompletePutKeyPolicyWithoutMutation(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	originalPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}`
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Policy: aws.String(originalPolicy)})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	for _, policyDocument := range []string{
		`{}`,
		`{"Version":"2012-10-17","Statement":[]}`,
		`{"Statement":[{"Effect":"Allow"}]}`,
		`{"Version":"2012-10-17","Statement":{}}`,
		`{"Version":"2012-10-17","Statement":[null]}`,
		`{"Version":"2012-10-17","Statement":[1]}`,
		`{"Version":"2012-10-17","Statement":["text"]}`,
	} {
		_, err := client.PutKeyPolicy(context.Background(), &kms.PutKeyPolicyInput{
			KeyId:      created.KeyMetadata.KeyId,
			PolicyName: aws.String("default"),
			Policy:     aws.String(policyDocument),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "MalformedPolicyDocumentException" {
			t.Fatalf("PutKeyPolicy(%q) error = %v, want MalformedPolicyDocumentException", policyDocument, err)
		}
	}

	policy, err := client.GetKeyPolicy(context.Background(), &kms.GetKeyPolicyInput{KeyId: created.KeyMetadata.KeyId, PolicyName: aws.String("default")})
	if err != nil {
		t.Fatalf("GetKeyPolicy() error = %v", err)
	}
	if aws.ToString(policy.Policy) != originalPolicy {
		t.Fatalf("GetKeyPolicy() = %q, want original policy %q after incomplete updates", aws.ToString(policy.Policy), originalPolicy)
	}
}

func TestKMSCompatibilityAdapterRejectsNonDefaultPolicyNameWithoutMutation(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	originalPolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}`
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Policy: aws.String(originalPolicy)})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}

	_, err = client.PutKeyPolicy(context.Background(), &kms.PutKeyPolicyInput{
		KeyId:      created.KeyMetadata.KeyId,
		PolicyName: aws.String("alternate"),
		Policy:     aws.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow"}]}`),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "UnsupportedOperationException" {
		t.Fatalf("PutKeyPolicy(non-default name) error = %v, want UnsupportedOperationException", err)
	}
	_, err = client.GetKeyPolicy(context.Background(), &kms.GetKeyPolicyInput{KeyId: created.KeyMetadata.KeyId, PolicyName: aws.String("alternate")})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "UnsupportedOperationException" {
		t.Fatalf("GetKeyPolicy(non-default name) error = %v, want UnsupportedOperationException", err)
	}

	policy, err := client.GetKeyPolicy(context.Background(), &kms.GetKeyPolicyInput{KeyId: created.KeyMetadata.KeyId, PolicyName: aws.String("default")})
	if err != nil {
		t.Fatalf("GetKeyPolicy() error = %v", err)
	}
	if aws.ToString(policy.Policy) != originalPolicy {
		t.Fatalf("GetKeyPolicy() = %q, want original policy %q after invalid name", aws.ToString(policy.Policy), originalPolicy)
	}
}

func TestKMSCompatibilityAdapterRejectsMalformedCreateKeyPolicyWithoutCreatingKey(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{Policy: aws.String(`{"Version":`)})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "MalformedPolicyDocumentException" {
		t.Fatalf("CreateKey(malformed policy) error = %v, want MalformedPolicyDocumentException", err)
	}
	keys, err := client.ListKeys(context.Background(), &kms.ListKeysInput{})
	if err != nil {
		t.Fatalf("ListKeys() error = %v", err)
	}
	if len(keys.Keys) != 0 {
		t.Fatalf("ListKeys() = %#v, want no key after malformed policy", keys.Keys)
	}
}

func TestKMSCompatibilityAdapterCreatesAWSStyleDefaultPolicyWhenPolicyIsOmitted(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	policy, err := client.GetKeyPolicy(context.Background(), &kms.GetKeyPolicyInput{KeyId: created.KeyMetadata.KeyId, PolicyName: aws.String("default")})
	if err != nil {
		t.Fatalf("GetKeyPolicy() error = %v", err)
	}
	for _, want := range []string{`"Version":"2012-10-17"`, `"Principal"`, `"Action":"kms:*"`, `"Resource":"*"`} {
		if !strings.Contains(aws.ToString(policy.Policy), want) {
			t.Fatalf("default policy = %q, missing %s", aws.ToString(policy.Policy), want)
		}
	}
}

func TestKMSCompatibilityAdapterAcceptsSingleStatementObjectPolicy(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter())
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kms.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	created, err := client.CreateKey(context.Background(), &kms.CreateKeyInput{})
	if err != nil {
		t.Fatalf("CreateKey() error = %v", err)
	}
	policyDocument := `{"Version":"2012-10-17","Statement":{"Effect":"Allow"}}`
	if _, err := client.PutKeyPolicy(context.Background(), &kms.PutKeyPolicyInput{
		KeyId:      created.KeyMetadata.KeyId,
		PolicyName: aws.String("default"),
		Policy:     aws.String(policyDocument),
	}); err != nil {
		t.Fatalf("PutKeyPolicy(single statement) error = %v", err)
	}
	policy, err := client.GetKeyPolicy(context.Background(), &kms.GetKeyPolicyInput{KeyId: created.KeyMetadata.KeyId, PolicyName: aws.String("default")})
	if err != nil {
		t.Fatalf("GetKeyPolicy() error = %v", err)
	}
	if aws.ToString(policy.Policy) != policyDocument {
		t.Fatalf("GetKeyPolicy() = %q, want %q", aws.ToString(policy.Policy), policyDocument)
	}
}

func TestKMSCompatibilityAdapterShapesAuthorizerFailure(t *testing.T) {
	server := httptest.NewServer(compataws.NewKMSAdapter(compataws.WithKMSAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
		return authz.Decision{}, errors.New("authorizer unavailable")
	}))))
	defer server.Close()

	client := kms.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kms.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListKeys(context.Background(), &kms.ListKeysInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "KMSInternalException" {
		t.Fatalf("ListKeys(authorizer failure) error = %v, want KMSInternalException", err)
	}
}
