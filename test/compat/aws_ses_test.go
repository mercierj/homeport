package compat_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestSESCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := ses.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ses.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.VerifyDomainIdentity(context.Background(), &ses.VerifyDomainIdentityInput{
		Domain: aws.String("example.com"),
	})
	if err != nil {
		t.Fatalf("VerifyDomainIdentity() error = %v", err)
	}
	if aws.ToString(created.VerificationToken) == "" {
		t.Fatalf("VerifyDomainIdentity() token is empty")
	}

	attrs, err := client.GetIdentityVerificationAttributes(context.Background(), &ses.GetIdentityVerificationAttributesInput{
		Identities: []string{"example.com"},
	})
	if err != nil {
		t.Fatalf("GetIdentityVerificationAttributes() error = %v", err)
	}
	got := attrs.VerificationAttributes["example.com"]
	if aws.ToString(got.VerificationToken) != aws.ToString(created.VerificationToken) || got.VerificationStatus != types.VerificationStatusPending {
		t.Fatalf("GetIdentityVerificationAttributes() = %#v, want pending identity with token", got)
	}

	listed, err := client.ListIdentities(context.Background(), &ses.ListIdentitiesInput{
		IdentityType: types.IdentityTypeDomain,
	})
	if err != nil {
		t.Fatalf("ListIdentities() error = %v", err)
	}
	if len(listed.Identities) != 1 || listed.Identities[0] != "example.com" {
		t.Fatalf("ListIdentities() = %#v, want example.com", listed.Identities)
	}

	if _, err := client.DeleteIdentity(context.Background(), &ses.DeleteIdentityInput{Identity: aws.String("example.com")}); err != nil {
		t.Fatalf("DeleteIdentity() error = %v", err)
	}
	listed, err = client.ListIdentities(context.Background(), &ses.ListIdentitiesInput{})
	if err != nil {
		t.Fatalf("ListIdentities(after delete) error = %v", err)
	}
	if len(listed.Identities) != 0 {
		t.Fatalf("ListIdentities(after delete) = %#v, want no identities", listed.Identities)
	}
}

func TestSESCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewSESAdapter())
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
		VerificationToken string `json:"VerificationToken"`
	}
	if err := json.Unmarshal(runAWS("ses", "verify-domain-identity", "--domain", "example.org"), &created); err != nil {
		t.Fatalf("decode verify-domain-identity output: %v", err)
	}
	if created.VerificationToken == "" {
		t.Fatalf("verify-domain-identity token is empty")
	}

	var attrs struct {
		VerificationAttributes map[string]struct {
			VerificationStatus string `json:"VerificationStatus"`
			VerificationToken  string `json:"VerificationToken"`
		} `json:"VerificationAttributes"`
	}
	if err := json.Unmarshal(runAWS("ses", "get-identity-verification-attributes", "--identities", "example.org"), &attrs); err != nil {
		t.Fatalf("decode get-identity-verification-attributes output: %v", err)
	}
	if attrs.VerificationAttributes["example.org"].VerificationToken != created.VerificationToken || attrs.VerificationAttributes["example.org"].VerificationStatus != "Pending" {
		t.Fatalf("get-identity-verification-attributes = %#v, want pending identity with token", attrs.VerificationAttributes)
	}

	var listed struct {
		Identities []string `json:"Identities"`
	}
	if err := json.Unmarshal(runAWS("ses", "list-identities", "--identity-type", "Domain"), &listed); err != nil {
		t.Fatalf("decode list-identities output: %v", err)
	}
	if len(listed.Identities) != 1 || listed.Identities[0] != "example.org" {
		t.Fatalf("list-identities = %#v, want example.org", listed.Identities)
	}

	runAWS("ses", "delete-identity", "--identity", "example.org")
	if err := json.Unmarshal(runAWS("ses", "list-identities"), &listed); err != nil {
		t.Fatalf("decode list-identities after delete output: %v", err)
	}
	if len(listed.Identities) != 0 {
		t.Fatalf("list-identities after delete = %#v, want no identities", listed.Identities)
	}
}

func TestSESCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewSESAdapter())
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
    ses = %q
  }
}

resource "aws_ses_domain_identity" "example" {
  domain = "example.net"
}

output "verification_token" {
  value = aws_ses_domain_identity.example.verification_token
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

	token := strings.TrimSpace(string(runTerraform("output", "-raw", "verification_token")))
	if token == "" {
		t.Fatalf("terraform output verification_token is empty")
	}
}

func TestSESCompatibilityAdapterManagesIdentityPoliciesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{Domain: aws.String("example.com")}); err != nil {
		t.Fatalf("VerifyDomainIdentity() error = %v", err)
	}

	policy := `{"Version":"2012-10-17","Statement":[]}`
	if _, err := client.PutIdentityPolicy(ctx, &ses.PutIdentityPolicyInput{
		Identity:   aws.String("example.com"),
		PolicyName: aws.String("senders"),
		Policy:     aws.String(policy),
	}); err != nil {
		t.Fatalf("PutIdentityPolicy() error = %v", err)
	}

	listed, err := client.ListIdentityPolicies(ctx, &ses.ListIdentityPoliciesInput{Identity: aws.String("example.com")})
	if err != nil {
		t.Fatalf("ListIdentityPolicies() error = %v", err)
	}
	if strings.Join(listed.PolicyNames, ",") != "senders" {
		t.Fatalf("ListIdentityPolicies() = %#v, want senders", listed.PolicyNames)
	}

	got, err := client.GetIdentityPolicies(ctx, &ses.GetIdentityPoliciesInput{
		Identity:    aws.String("example.com"),
		PolicyNames: []string{"senders", "missing"},
	})
	if err != nil {
		t.Fatalf("GetIdentityPolicies() error = %v", err)
	}
	if got.Policies["senders"] != policy {
		t.Fatalf("GetIdentityPolicies()[senders] = %q, want %q", got.Policies["senders"], policy)
	}
	if _, ok := got.Policies["missing"]; ok {
		t.Fatalf("GetIdentityPolicies()[missing] returned unexpectedly: %#v", got.Policies)
	}

	if _, err := client.DeleteIdentityPolicy(ctx, &ses.DeleteIdentityPolicyInput{
		Identity:   aws.String("example.com"),
		PolicyName: aws.String("senders"),
	}); err != nil {
		t.Fatalf("DeleteIdentityPolicy() error = %v", err)
	}
	listed, err = client.ListIdentityPolicies(ctx, &ses.ListIdentityPoliciesInput{Identity: aws.String("example.com")})
	if err != nil {
		t.Fatalf("ListIdentityPolicies(after delete) error = %v", err)
	}
	if len(listed.PolicyNames) != 0 {
		t.Fatalf("ListIdentityPolicies(after delete) = %#v, want no policies", listed.PolicyNames)
	}
}

func TestSESCompatibilityAdapterManagesDomainDkimWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	created, err := client.VerifyDomainDkim(ctx, &ses.VerifyDomainDkimInput{Domain: aws.String("example.com")})
	if err != nil {
		t.Fatalf("VerifyDomainDkim() error = %v", err)
	}
	if len(created.DkimTokens) != 3 {
		t.Fatalf("VerifyDomainDkim() tokens = %#v, want three tokens", created.DkimTokens)
	}

	attrs, err := client.GetIdentityDkimAttributes(ctx, &ses.GetIdentityDkimAttributesInput{
		Identities: []string{"example.com", "missing.example"},
	})
	if err != nil {
		t.Fatalf("GetIdentityDkimAttributes() error = %v", err)
	}
	got := attrs.DkimAttributes["example.com"]
	if !got.DkimEnabled || got.DkimVerificationStatus != types.VerificationStatusPending || strings.Join(got.DkimTokens, ",") != strings.Join(created.DkimTokens, ",") {
		t.Fatalf("GetIdentityDkimAttributes()[example.com] = %#v, want pending DKIM attributes with returned tokens", got)
	}
	if _, ok := attrs.DkimAttributes["missing.example"]; ok {
		t.Fatalf("GetIdentityDkimAttributes()[missing.example] returned unexpectedly: %#v", attrs.DkimAttributes)
	}

	if _, err := client.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{Domain: aws.String("example.com")}); err != nil {
		t.Fatalf("VerifyDomainIdentity(after dkim) error = %v", err)
	}
	attrs, err = client.GetIdentityDkimAttributes(ctx, &ses.GetIdentityDkimAttributesInput{Identities: []string{"example.com"}})
	if err != nil {
		t.Fatalf("GetIdentityDkimAttributes(after identity verify) error = %v", err)
	}
	if strings.Join(attrs.DkimAttributes["example.com"].DkimTokens, ",") != strings.Join(created.DkimTokens, ",") {
		t.Fatalf("GetIdentityDkimAttributes(after identity verify) tokens = %#v, want stable tokens %#v", attrs.DkimAttributes["example.com"].DkimTokens, created.DkimTokens)
	}

	again, err := client.VerifyDomainDkim(ctx, &ses.VerifyDomainDkimInput{Domain: aws.String("example.com")})
	if err != nil {
		t.Fatalf("VerifyDomainDkim(second) error = %v", err)
	}
	if strings.Join(again.DkimTokens, ",") != strings.Join(created.DkimTokens, ",") {
		t.Fatalf("VerifyDomainDkim(second) tokens = %#v, want stable tokens %#v", again.DkimTokens, created.DkimTokens)
	}
}

func TestSESCompatibilityAdapterSendsEmailWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{Domain: aws.String("example.com")}); err != nil {
		t.Fatalf("VerifyDomainIdentity() error = %v", err)
	}

	sent, err := client.SendEmail(ctx, sesEmail("sender@example.com", "recipient@example.net"))
	if err != nil {
		t.Fatalf("SendEmail() error = %v", err)
	}
	if aws.ToString(sent.MessageId) == "" {
		t.Fatalf("SendEmail() message id is empty")
	}

	_, err = client.SendEmail(ctx, sesEmail("sender@unverified.example", "recipient@example.net"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "MessageRejected" {
		t.Fatalf("SendEmail(unverified source) error = %v, want MessageRejected", err)
	}
}

func TestSESCompatibilityAdapterSendsRawEmailWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{Domain: aws.String("example.com")}); err != nil {
		t.Fatalf("VerifyDomainIdentity() error = %v", err)
	}

	sent, err := client.SendRawEmail(ctx, sesRawEmail("sender@example.com", "recipient@example.net"))
	if err != nil {
		t.Fatalf("SendRawEmail() error = %v", err)
	}
	if aws.ToString(sent.MessageId) == "" {
		t.Fatalf("SendRawEmail() message id is empty")
	}

	_, err = client.SendRawEmail(ctx, sesRawEmail("sender@unverified.example", "recipient@example.net"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "MessageRejected" {
		t.Fatalf("SendRawEmail(unverified source) error = %v, want MessageRejected", err)
	}
}

func TestSESCompatibilityAdapterSendsTemplatedEmailWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{Domain: aws.String("example.com")}); err != nil {
		t.Fatalf("VerifyDomainIdentity() error = %v", err)
	}

	if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}

	sent, err := client.SendTemplatedEmail(ctx, sesTemplatedEmail("sender@example.com", "recipient@example.net", "welcome"))
	if err != nil {
		t.Fatalf("SendTemplatedEmail() error = %v", err)
	}
	if aws.ToString(sent.MessageId) == "" {
		t.Fatalf("SendTemplatedEmail() message id is empty")
	}

	_, err = client.SendTemplatedEmail(ctx, sesTemplatedEmail("sender@example.com", "recipient@example.net", "missing"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TemplateDoesNotExist" {
		t.Fatalf("SendTemplatedEmail(missing template) error = %v, want TemplateDoesNotExist", err)
	}

	_, err = client.SendTemplatedEmail(ctx, sesTemplatedEmail("sender@unverified.example", "recipient@example.net", "welcome"))
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "MessageRejected" {
		t.Fatalf("SendTemplatedEmail(unverified source) error = %v, want MessageRejected", err)
	}
}

func TestSESCompatibilityAdapterRejectsTemplatedEmailWithoutRecipients(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{Domain: aws.String("example.com")}); err != nil {
		t.Fatalf("VerifyDomainIdentity() error = %v", err)
	}
	if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	_, err := client.SendTemplatedEmail(ctx, &ses.SendTemplatedEmailInput{
		Source:       aws.String("sender@example.com"),
		Template:     aws.String("welcome"),
		TemplateData: aws.String(`{"name":"HomePort"}`),
		Destination:  &types.Destination{},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("SendTemplatedEmail(without recipients) error = %v, want InvalidParameterValue", err)
	}
}

func TestSESCompatibilityAdapterSendsBulkTemplatedEmailWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{Domain: aws.String("example.com")}); err != nil {
		t.Fatalf("VerifyDomainIdentity() error = %v", err)
	}
	if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}

	sent, err := client.SendBulkTemplatedEmail(ctx, sesBulkTemplatedEmail("sender@example.com", "welcome", "one@example.net", "two@example.net"))
	if err != nil {
		t.Fatalf("SendBulkTemplatedEmail() error = %v", err)
	}
	if len(sent.Status) != 2 {
		t.Fatalf("SendBulkTemplatedEmail() status count = %d, want 2", len(sent.Status))
	}
	for i, status := range sent.Status {
		if status.Status != types.BulkEmailStatusSuccess || aws.ToString(status.MessageId) == "" || status.Error != nil {
			t.Fatalf("SendBulkTemplatedEmail() status[%d] = %#v, want success with message id", i, status)
		}
	}

	_, err = client.SendBulkTemplatedEmail(ctx, sesBulkTemplatedEmail("sender@example.com", "missing", "one@example.net"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TemplateDoesNotExist" {
		t.Fatalf("SendBulkTemplatedEmail(missing template) error = %v, want TemplateDoesNotExist", err)
	}

	_, err = client.SendBulkTemplatedEmail(ctx, sesBulkTemplatedEmail("sender@unverified.example", "welcome", "one@example.net"))
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "MessageRejected" {
		t.Fatalf("SendBulkTemplatedEmail(unverified source) error = %v, want MessageRejected", err)
	}
}

func TestSESCompatibilityAdapterRejectsBulkTemplatedEmailWithoutRecipients(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{Domain: aws.String("example.com")}); err != nil {
		t.Fatalf("VerifyDomainIdentity() error = %v", err)
	}
	if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	_, err := client.SendBulkTemplatedEmail(ctx, &ses.SendBulkTemplatedEmailInput{
		Source:              aws.String("sender@example.com"),
		Template:            aws.String("welcome"),
		DefaultTemplateData: aws.String(`{"name":"HomePort"}`),
		Destinations:        []types.BulkEmailDestination{{Destination: &types.Destination{}}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("SendBulkTemplatedEmail(without recipients) error = %v, want InvalidParameterValue", err)
	}
}

func TestSESCompatibilityAdapterManagesEmailIdentitiesWithSESv2AWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesV2Client(server.URL)
	ctx := context.Background()
	created, err := client.CreateEmailIdentity(ctx, &sesv2.CreateEmailIdentityInput{
		EmailIdentity: aws.String("example.com"),
		Tags: []sesv2types.Tag{
			{Key: aws.String("env"), Value: aws.String("test")},
		},
	})
	if err != nil {
		t.Fatalf("CreateEmailIdentity() error = %v", err)
	}
	if created.IdentityType != sesv2types.IdentityTypeDomain || created.VerifiedForSendingStatus {
		t.Fatalf("CreateEmailIdentity() = %#v, want pending domain identity", created)
	}

	got, err := client.GetEmailIdentity(ctx, &sesv2.GetEmailIdentityInput{EmailIdentity: aws.String("example.com")})
	if err != nil {
		t.Fatalf("GetEmailIdentity() error = %v", err)
	}
	if got.IdentityType != sesv2types.IdentityTypeDomain ||
		got.VerificationStatus != sesv2types.VerificationStatusPending ||
		len(got.Tags) != 1 ||
		aws.ToString(got.Tags[0].Key) != "env" ||
		aws.ToString(got.Tags[0].Value) != "test" {
		t.Fatalf("GetEmailIdentity() = %#v, want pending domain identity with tag", got)
	}

	listed, err := client.ListEmailIdentities(ctx, &sesv2.ListEmailIdentitiesInput{})
	if err != nil {
		t.Fatalf("ListEmailIdentities() error = %v", err)
	}
	if len(listed.EmailIdentities) != 1 || aws.ToString(listed.EmailIdentities[0].IdentityName) != "example.com" {
		t.Fatalf("ListEmailIdentities() = %#v, want example.com", listed.EmailIdentities)
	}

	if _, err := client.DeleteEmailIdentity(ctx, &sesv2.DeleteEmailIdentityInput{EmailIdentity: aws.String("example.com")}); err != nil {
		t.Fatalf("DeleteEmailIdentity() error = %v", err)
	}
	listed, err = client.ListEmailIdentities(ctx, &sesv2.ListEmailIdentitiesInput{})
	if err != nil {
		t.Fatalf("ListEmailIdentities(after delete) error = %v", err)
	}
	if len(listed.EmailIdentities) != 0 {
		t.Fatalf("ListEmailIdentities(after delete) = %#v, want empty list", listed.EmailIdentities)
	}
}

func TestSESCompatibilityAdapterReturnsSESv2IdentityConflictAndNotFound(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesV2Client(server.URL)
	ctx := context.Background()
	if _, err := client.CreateEmailIdentity(ctx, &sesv2.CreateEmailIdentityInput{EmailIdentity: aws.String("example.com")}); err != nil {
		t.Fatalf("CreateEmailIdentity() error = %v", err)
	}
	_, err := client.CreateEmailIdentity(ctx, &sesv2.CreateEmailIdentityInput{EmailIdentity: aws.String("example.com")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AlreadyExistsException" {
		t.Fatalf("CreateEmailIdentity(duplicate) error = %v, want AlreadyExistsException", err)
	}

	_, err = client.DeleteEmailIdentity(ctx, &sesv2.DeleteEmailIdentityInput{EmailIdentity: aws.String("missing.example")})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NotFoundException" {
		t.Fatalf("DeleteEmailIdentity(missing) error = %v, want NotFoundException", err)
	}
}

func TestSESCompatibilityAdapterPaginatesSESv2EmailIdentities(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesV2Client(server.URL)
	ctx := context.Background()
	for _, name := range []string{"alpha.example", "beta.example"} {
		if _, err := client.CreateEmailIdentity(ctx, &sesv2.CreateEmailIdentityInput{EmailIdentity: aws.String(name)}); err != nil {
			t.Fatalf("CreateEmailIdentity(%s) error = %v", name, err)
		}
	}
	first, err := client.ListEmailIdentities(ctx, &sesv2.ListEmailIdentitiesInput{PageSize: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListEmailIdentities(first) error = %v", err)
	}
	if len(first.EmailIdentities) != 1 || aws.ToString(first.EmailIdentities[0].IdentityName) != "alpha.example" || first.NextToken == nil {
		t.Fatalf("ListEmailIdentities(first) = %#v token=%v, want alpha and token", first.EmailIdentities, first.NextToken)
	}
	second, err := client.ListEmailIdentities(ctx, &sesv2.ListEmailIdentitiesInput{PageSize: aws.Int32(1), NextToken: first.NextToken})
	if err != nil {
		t.Fatalf("ListEmailIdentities(second) error = %v", err)
	}
	if len(second.EmailIdentities) != 1 || aws.ToString(second.EmailIdentities[0].IdentityName) != "beta.example" || second.NextToken != nil {
		t.Fatalf("ListEmailIdentities(second) = %#v token=%v, want beta and no token", second.EmailIdentities, second.NextToken)
	}
}

func TestSESCompatibilityAdapterAuthorizesAndAuditsSESv2EmailIdentity(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewSESAdapter(
		compataws.WithSESAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ses:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"ses:CreateEmailIdentity"}, Resources: []string{"*"}},
		)),
		compataws.WithSESAuditSink(auditLog.Record),
	))
	defer server.Close()

	_, err := sesV2Client(server.URL).CreateEmailIdentity(context.Background(), &sesv2.CreateEmailIdentityInput{
		EmailIdentity: aws.String("denied.example"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("CreateEmailIdentity(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ses:CreateEmailIdentity", false)
}

func TestSESCompatibilityAdapterAuthorizesTagBeforeCheckingIdentity(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewSESAdapter(
		compataws.WithSESAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ses:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"ses:TagResource"}, Resources: []string{"*"}},
		)),
		compataws.WithSESAuditSink(auditLog.Record),
	))
	defer server.Close()

	_, err := sesV2Client(server.URL).TagResource(context.Background(), &sesv2.TagResourceInput{
		ResourceArn: aws.String("arn:aws:ses:us-east-1:000000000000:identity/missing.example"),
		Tags:        []sesv2types.Tag{{Key: aws.String("env"), Value: aws.String("test")}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("TagResource(denied missing identity) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ses:TagResource", false)
}

func TestSESCompatibilityAdapterManagesSESv2IdentityTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesV2Client(server.URL)
	ctx := context.Background()
	if _, err := client.CreateEmailIdentity(ctx, &sesv2.CreateEmailIdentityInput{EmailIdentity: aws.String("example.com")}); err != nil {
		t.Fatalf("CreateEmailIdentity() error = %v", err)
	}
	arn := aws.String("arn:aws:ses:us-east-1:000000000000:identity/example.com")
	if _, err := client.TagResource(ctx, &sesv2.TagResourceInput{
		ResourceArn: arn,
		Tags: []sesv2types.Tag{
			{Key: aws.String("env"), Value: aws.String("test")},
			{Key: aws.String("owner"), Value: aws.String("billing")},
		},
	}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}

	tags, err := client.ListTagsForResource(ctx, &sesv2.ListTagsForResourceInput{ResourceArn: arn})
	if err != nil {
		t.Fatalf("ListTagsForResource() error = %v", err)
	}
	if len(tags.Tags) != 2 || aws.ToString(tags.Tags[0].Key) != "env" || aws.ToString(tags.Tags[1].Key) != "owner" {
		t.Fatalf("ListTagsForResource() = %#v, want sorted env and owner tags", tags.Tags)
	}

	if _, err := client.UntagResource(ctx, &sesv2.UntagResourceInput{ResourceArn: arn, TagKeys: []string{"env"}}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	tags, err = client.ListTagsForResource(ctx, &sesv2.ListTagsForResourceInput{ResourceArn: arn})
	if err != nil {
		t.Fatalf("ListTagsForResource(after untag) error = %v", err)
	}
	if len(tags.Tags) != 1 || aws.ToString(tags.Tags[0].Key) != "owner" {
		t.Fatalf("ListTagsForResource(after untag) = %#v, want only owner", tags.Tags)
	}
}

func TestSESCompatibilityAdapterManagesTemplatesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	if _, err := client.CreateTemplate(ctx, sesTemplate("alpha")); err != nil {
		t.Fatalf("CreateTemplate(alpha) error = %v", err)
	}

	first, err := client.ListTemplates(ctx, &ses.ListTemplatesInput{MaxItems: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListTemplates(first) error = %v", err)
	}
	if len(first.TemplatesMetadata) != 1 || aws.ToString(first.TemplatesMetadata[0].Name) != "alpha" || first.NextToken == nil {
		t.Fatalf("ListTemplates(first) = %#v token=%v, want alpha and token", first.TemplatesMetadata, first.NextToken)
	}
	second, err := client.ListTemplates(ctx, &ses.ListTemplatesInput{MaxItems: aws.Int32(1), NextToken: first.NextToken})
	if err != nil {
		t.Fatalf("ListTemplates(second) error = %v", err)
	}
	if len(second.TemplatesMetadata) != 1 || aws.ToString(second.TemplatesMetadata[0].Name) != "welcome" || second.NextToken != nil {
		t.Fatalf("ListTemplates(second) = %#v token=%v, want welcome and no token", second.TemplatesMetadata, second.NextToken)
	}

	got, err := client.GetTemplate(ctx, &ses.GetTemplateInput{TemplateName: aws.String("welcome")})
	if err != nil {
		t.Fatalf("GetTemplate() error = %v", err)
	}
	if aws.ToString(got.Template.TemplateName) != "welcome" ||
		aws.ToString(got.Template.SubjectPart) != "Welcome {{name}}" ||
		aws.ToString(got.Template.TextPart) != "Hello {{name}}" {
		t.Fatalf("GetTemplate() = %#v, want stored template parts", got.Template)
	}

	if _, err := client.DeleteTemplate(ctx, &ses.DeleteTemplateInput{TemplateName: aws.String("welcome")}); err != nil {
		t.Fatalf("DeleteTemplate() error = %v", err)
	}

	_, err = client.GetTemplate(ctx, &ses.GetTemplateInput{TemplateName: aws.String("welcome")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TemplateDoesNotExist" {
		t.Fatalf("GetTemplate(deleted) error = %v, want TemplateDoesNotExist", err)
	}
}

func TestSESCompatibilityAdapterRejectsDuplicateTemplateWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	_, err := client.CreateTemplate(ctx, sesTemplate("welcome"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AlreadyExists" {
		t.Fatalf("CreateTemplate(duplicate) error = %v, want AlreadyExists", err)
	}
}

func TestSESCompatibilityAdapterUpdatesAndRendersTemplatesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	if _, err := client.UpdateTemplate(ctx, sesUpdateTemplate("welcome")); err != nil {
		t.Fatalf("UpdateTemplate() error = %v", err)
	}

	got, err := client.GetTemplate(ctx, &ses.GetTemplateInput{TemplateName: aws.String("welcome")})
	if err != nil {
		t.Fatalf("GetTemplate(after update) error = %v", err)
	}
	if aws.ToString(got.Template.SubjectPart) != "Updated {{name}}" ||
		aws.ToString(got.Template.TextPart) != "Hi {{name}}" ||
		aws.ToString(got.Template.HtmlPart) != "<p>Hi {{name}}</p>" {
		t.Fatalf("GetTemplate(after update) = %#v, want updated parts", got.Template)
	}

	rendered, err := client.TestRenderTemplate(ctx, &ses.TestRenderTemplateInput{
		TemplateName: aws.String("welcome"),
		TemplateData: aws.String(`{"name":"HomePort"}`),
	})
	if err != nil {
		t.Fatalf("TestRenderTemplate() error = %v", err)
	}
	if aws.ToString(rendered.RenderedTemplate) != "Updated HomePort\n<p>Hi HomePort</p>\nHi HomePort" {
		t.Fatalf("TestRenderTemplate() = %q, want rendered subject/html/text", aws.ToString(rendered.RenderedTemplate))
	}

	_, err = client.UpdateTemplate(ctx, sesUpdateTemplate("missing"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TemplateDoesNotExist" {
		t.Fatalf("UpdateTemplate(missing) error = %v, want TemplateDoesNotExist", err)
	}

	_, err = client.TestRenderTemplate(ctx, &ses.TestRenderTemplateInput{
		TemplateName: aws.String("missing"),
		TemplateData: aws.String(`{"name":"HomePort"}`),
	})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TemplateDoesNotExist" {
		t.Fatalf("TestRenderTemplate(missing) error = %v, want TemplateDoesNotExist", err)
	}
}

func TestSESCompatibilityAdapterRejectsMissingTemplateRenderAttributeWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	ctx := context.Background()
	if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
		t.Fatalf("CreateTemplate() error = %v", err)
	}
	_, err := client.TestRenderTemplate(ctx, &ses.TestRenderTemplateInput{
		TemplateName: aws.String("welcome"),
		TemplateData: aws.String(`{}`),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "MissingRenderingAttribute" {
		t.Fatalf("TestRenderTemplate(missing attribute) error = %v, want MissingRenderingAttribute", err)
	}
}

func TestSESCompatibilityAdapterAuthorizesAndAuditsSupportedActions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
		call   func(context.Context, *ses.Client) error
	}{
		{
			name:   "verify",
			action: "ses:VerifyDomainIdentity",
			call: func(ctx context.Context, client *ses.Client) error {
				_, err := client.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{Domain: aws.String("denied.example")})
				return err
			},
		},
		{
			name:   "get",
			action: "ses:GetIdentityVerificationAttributes",
			call: func(ctx context.Context, client *ses.Client) error {
				_, err := client.GetIdentityVerificationAttributes(ctx, &ses.GetIdentityVerificationAttributesInput{Identities: []string{"allowed.example"}})
				return err
			},
		},
		{
			name:   "list",
			action: "ses:ListIdentities",
			call: func(ctx context.Context, client *ses.Client) error {
				_, err := client.ListIdentities(ctx, &ses.ListIdentitiesInput{IdentityType: types.IdentityTypeDomain})
				return err
			},
		},
		{
			name:   "delete",
			action: "ses:DeleteIdentity",
			call: func(ctx context.Context, client *ses.Client) error {
				_, err := client.DeleteIdentity(ctx, &ses.DeleteIdentityInput{Identity: aws.String("allowed.example")})
				return err
			},
		},
		{
			name:   "verify_dkim",
			action: "ses:VerifyDomainDkim",
			call: func(ctx context.Context, client *ses.Client) error {
				_, err := client.VerifyDomainDkim(ctx, &ses.VerifyDomainDkimInput{Domain: aws.String("denied.example")})
				return err
			},
		},
		{
			name:   "get_dkim",
			action: "ses:GetIdentityDkimAttributes",
			call: func(ctx context.Context, client *ses.Client) error {
				_, err := client.GetIdentityDkimAttributes(ctx, &ses.GetIdentityDkimAttributesInput{Identities: []string{"allowed.example"}})
				return err
			},
		},
		{
			name:   "send",
			action: "ses:SendEmail",
			call: func(ctx context.Context, client *ses.Client) error {
				_, err := client.SendEmail(ctx, sesEmail("sender@allowed.example", "recipient@example.net"))
				return err
			},
		},
		{
			name:   "send_raw",
			action: "ses:SendRawEmail",
			call: func(ctx context.Context, client *ses.Client) error {
				_, err := client.SendRawEmail(ctx, sesRawEmail("sender@allowed.example", "recipient@example.net"))
				return err
			},
		},
		{
			name:   "create_template",
			action: "ses:CreateTemplate",
			call: func(ctx context.Context, client *ses.Client) error {
				_, err := client.CreateTemplate(ctx, sesTemplate("welcome"))
				return err
			},
		},
		{
			name:   "get_template",
			action: "ses:GetTemplate",
			call: func(ctx context.Context, client *ses.Client) error {
				if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
					return err
				}
				_, err := client.GetTemplate(ctx, &ses.GetTemplateInput{TemplateName: aws.String("welcome")})
				return err
			},
		},
		{
			name:   "delete_template",
			action: "ses:DeleteTemplate",
			call: func(ctx context.Context, client *ses.Client) error {
				if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
					return err
				}
				_, err := client.DeleteTemplate(ctx, &ses.DeleteTemplateInput{TemplateName: aws.String("welcome")})
				return err
			},
		},
		{
			name:   "list_templates",
			action: "ses:ListTemplates",
			call: func(ctx context.Context, client *ses.Client) error {
				if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
					return err
				}
				_, err := client.ListTemplates(ctx, &ses.ListTemplatesInput{})
				return err
			},
		},
		{
			name:   "update_template",
			action: "ses:UpdateTemplate",
			call: func(ctx context.Context, client *ses.Client) error {
				if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
					return err
				}
				_, err := client.UpdateTemplate(ctx, sesUpdateTemplate("welcome"))
				return err
			},
		},
		{
			name:   "test_render_template",
			action: "ses:TestRenderTemplate",
			call: func(ctx context.Context, client *ses.Client) error {
				if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
					return err
				}
				_, err := client.TestRenderTemplate(ctx, &ses.TestRenderTemplateInput{
					TemplateName: aws.String("welcome"),
					TemplateData: aws.String(`{"name":"HomePort"}`),
				})
				return err
			},
		},
		{
			name:   "send_templated",
			action: "ses:SendTemplatedEmail",
			call: func(ctx context.Context, client *ses.Client) error {
				if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
					return err
				}
				_, err := client.SendTemplatedEmail(ctx, sesTemplatedEmail("sender@allowed.example", "recipient@example.net", "welcome"))
				return err
			},
		},
		{
			name:   "send_bulk_templated",
			action: "ses:SendBulkTemplatedEmail",
			call: func(ctx context.Context, client *ses.Client) error {
				if _, err := client.CreateTemplate(ctx, sesTemplate("welcome")); err != nil {
					return err
				}
				_, err := client.SendBulkTemplatedEmail(ctx, sesBulkTemplatedEmail("sender@allowed.example", "welcome", "recipient@example.net"))
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			auditLog := authz.NewAuditLog()
			server := httptest.NewServer(compataws.NewSESAdapter(
				compataws.WithSESAuthorizer(authz.NewPolicyAuthorizer(
					authz.Rule{Effect: authz.Allow, Actions: []string{"ses:*"}, Resources: []string{"*"}},
					authz.Rule{Effect: authz.Deny, Actions: []string{tc.action}, Resources: []string{"*"}},
				)),
				compataws.WithSESAuditSink(auditLog.Record),
			))
			defer server.Close()

			client := sesClient(server.URL)
			ctx := context.Background()
			if tc.name != "verify" {
				if _, err := client.VerifyDomainIdentity(ctx, &ses.VerifyDomainIdentityInput{Domain: aws.String("allowed.example")}); err != nil {
					t.Fatalf("VerifyDomainIdentity(seed) error = %v", err)
				}
			}

			err := tc.call(ctx, client)
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
				t.Fatalf("%s denied error = %v, want AccessDenied", tc.action, err)
			}
			assertDecision(t, auditLog.Decisions(), tc.action, false)

			listed, err := client.ListIdentities(ctx, &ses.ListIdentitiesInput{})
			if tc.name == "list" {
				if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
					t.Fatalf("ListIdentities(after denied list) error = %v, want AccessDenied", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ListIdentities(after denied %s) error = %v", tc.name, err)
			}
			if tc.name == "verify" && len(listed.Identities) != 0 {
				t.Fatalf("ListIdentities(after denied verify) = %#v, want no identities", listed.Identities)
			}
			if tc.name == "delete" && (len(listed.Identities) != 1 || listed.Identities[0] != "allowed.example") {
				t.Fatalf("ListIdentities(after denied delete) = %#v, want allowed.example", listed.Identities)
			}
		})
	}
}

func TestSESCompatibilityAdapterPaginatesListIdentities(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	for _, domain := range []string{"a.example", "b.example", "c.example"} {
		if _, err := client.VerifyDomainIdentity(context.Background(), &ses.VerifyDomainIdentityInput{Domain: aws.String(domain)}); err != nil {
			t.Fatalf("VerifyDomainIdentity(%s) error = %v", domain, err)
		}
	}

	first, err := client.ListIdentities(context.Background(), &ses.ListIdentitiesInput{
		IdentityType: types.IdentityTypeDomain,
		MaxItems:     aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListIdentities(first) error = %v", err)
	}
	if strings.Join(first.Identities, ",") != "a.example,b.example" || first.NextToken == nil {
		t.Fatalf("ListIdentities(first) = %#v token=%v, want first two identities and token", first.Identities, first.NextToken)
	}

	second, err := client.ListIdentities(context.Background(), &ses.ListIdentitiesInput{
		IdentityType: types.IdentityTypeDomain,
		MaxItems:     aws.Int32(2),
		NextToken:    first.NextToken,
	})
	if err != nil {
		t.Fatalf("ListIdentities(second) error = %v", err)
	}
	if strings.Join(second.Identities, ",") != "c.example" || second.NextToken != nil {
		t.Fatalf("ListIdentities(second) = %#v token=%v, want final identity and no token", second.Identities, second.NextToken)
	}

	_, err = client.ListIdentities(context.Background(), &ses.ListIdentitiesInput{NextToken: aws.String("not-a-token")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("ListIdentities(invalid token) error = %v, want InvalidParameterValue", err)
	}
}

func TestSESCompatibilityAdapterRejectsUnauthorizedIdentityInBatchRead(t *testing.T) {
	firstResource := "arn:aws:ses:us-east-1:000000000000:identity/first.example"
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewSESAdapter(
		compataws.WithSESAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ses:VerifyDomainIdentity", "ses:ListIdentities"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Allow, Actions: []string{"ses:GetIdentityVerificationAttributes"}, Resources: []string{firstResource}},
		)),
		compataws.WithSESAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := sesClient(server.URL)
	for _, domain := range []string{"first.example", "second.example"} {
		if _, err := client.VerifyDomainIdentity(context.Background(), &ses.VerifyDomainIdentityInput{Domain: aws.String(domain)}); err != nil {
			t.Fatalf("VerifyDomainIdentity(%s) error = %v", domain, err)
		}
	}

	_, err := client.GetIdentityVerificationAttributes(context.Background(), &ses.GetIdentityVerificationAttributesInput{
		Identities: []string{"first.example", "second.example"},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("GetIdentityVerificationAttributes(batch denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ses:GetIdentityVerificationAttributes", false)
}

func TestSESCompatibilityAdapterRejectsInvalidListIdentitiesMaxItems(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()

	client := sesClient(server.URL)
	for _, maxItems := range []int32{0, 1001} {
		_, err := client.ListIdentities(context.Background(), &ses.ListIdentitiesInput{MaxItems: aws.Int32(maxItems)})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
			t.Fatalf("ListIdentities(MaxItems=%d) error = %v, want InvalidParameterValue", maxItems, err)
		}
	}
}

func TestSESCompatibilityAdapterReturnsLimitExceededWhenIdentityQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter(compataws.WithSESIdentityQuota(1)))
	defer server.Close()

	client := sesClient(server.URL)
	if _, err := client.VerifyDomainIdentity(context.Background(), &ses.VerifyDomainIdentityInput{Domain: aws.String("one.example")}); err != nil {
		t.Fatalf("VerifyDomainIdentity(first) error = %v", err)
	}

	_, err := client.VerifyDomainIdentity(context.Background(), &ses.VerifyDomainIdentityInput{Domain: aws.String("two.example")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceeded" {
		t.Fatalf("VerifyDomainIdentity(over quota) error = %v, want LimitExceeded", err)
	}

	listed, err := client.ListIdentities(context.Background(), &ses.ListIdentitiesInput{})
	if err != nil {
		t.Fatalf("ListIdentities(after quota) error = %v", err)
	}
	if strings.Join(listed.Identities, ",") != "one.example" {
		t.Fatalf("ListIdentities(after quota) = %#v, want only one.example", listed.Identities)
	}
}

func sesEmail(source, recipient string) *ses.SendEmailInput {
	return &ses.SendEmailInput{
		Source: aws.String(source),
		Destination: &types.Destination{
			ToAddresses: []string{recipient},
		},
		Message: &types.Message{
			Subject: &types.Content{Data: aws.String("HomePort SES compatibility")},
			Body: &types.Body{
				Text: &types.Content{Data: aws.String("hello from homeport")},
			},
		},
	}
}

func sesRawEmail(source, recipient string) *ses.SendRawEmailInput {
	return &ses.SendRawEmailInput{
		Source:       aws.String(source),
		Destinations: []string{recipient},
		RawMessage: &types.RawMessage{
			Data: []byte("From: " + source + "\r\nTo: " + recipient + "\r\nSubject: HomePort SES compatibility\r\n\r\nhello from homeport"),
		},
	}
}

func sesTemplate(name string) *ses.CreateTemplateInput {
	return &ses.CreateTemplateInput{
		Template: &types.Template{
			TemplateName: aws.String(name),
			SubjectPart:  aws.String("Welcome {{name}}"),
			TextPart:     aws.String("Hello {{name}}"),
		},
	}
}

func sesUpdateTemplate(name string) *ses.UpdateTemplateInput {
	return &ses.UpdateTemplateInput{
		Template: &types.Template{
			TemplateName: aws.String(name),
			SubjectPart:  aws.String("Updated {{name}}"),
			HtmlPart:     aws.String("<p>Hi {{name}}</p>"),
			TextPart:     aws.String("Hi {{name}}"),
		},
	}
}

func sesTemplatedEmail(source, recipient, template string) *ses.SendTemplatedEmailInput {
	return &ses.SendTemplatedEmailInput{
		Source: aws.String(source),
		Destination: &types.Destination{
			ToAddresses: []string{recipient},
		},
		Template:     aws.String(template),
		TemplateData: aws.String(`{"name":"HomePort"}`),
	}
}

func sesBulkTemplatedEmail(source, template string, recipients ...string) *ses.SendBulkTemplatedEmailInput {
	destinations := make([]types.BulkEmailDestination, 0, len(recipients))
	for _, recipient := range recipients {
		destinations = append(destinations, types.BulkEmailDestination{
			Destination:             &types.Destination{ToAddresses: []string{recipient}},
			ReplacementTemplateData: aws.String(`{"name":"HomePort"}`),
		})
	}
	return &ses.SendBulkTemplatedEmailInput{
		Source:              aws.String(source),
		Template:            aws.String(template),
		DefaultTemplateData: aws.String(`{"name":"HomePort"}`),
		Destinations:        destinations,
	}
}

func sesClient(endpoint string) *ses.Client {
	return ses.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ses.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
}

func TestSESCompatibilityAdapterRejectsExpiredCredential(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter(compataws.WithSESAuthorizer(authz.NewPolicyAuthorizer(
		authz.Rule{Effect: authz.Allow, Actions: []string{"ses:*"}, Resources: []string{"*"}},
		authz.Rule{
			Effect:    authz.Deny,
			Actions:   []string{"ses:VerifyDomainIdentity"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_expired", Values: []string{"true"}},
			},
		},
	))))
	defer server.Close()

	client := ses.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ses.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Expired", "true"))
	})
	_, err := client.VerifyDomainIdentity(context.Background(), &ses.VerifyDomainIdentityInput{Domain: aws.String("expired.example")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("VerifyDomainIdentity(expired credential) error = %v, want AccessDenied", err)
	}
}

func sesV2Client(endpoint string) *sesv2.Client {
	return sesv2.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sesv2.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
}

func TestSESCompatibilityAdapterRejectsMalformedCreateEmailIdentity(t *testing.T) {
	server := httptest.NewServer(compataws.NewSESAdapter())
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/v2/email/identities", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest || resp.Header.Get("x-amzn-errortype") != "BadRequestException" {
		t.Fatalf("malformed CreateEmailIdentity = status %d error type %q body %#v, want 400 BadRequestException", resp.StatusCode, resp.Header.Get("x-amzn-errortype"), body)
	}
}
