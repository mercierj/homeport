package compat_test

import (
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
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestACMCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewACMAdapter())
	defer server.Close()

	client := acm.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *acm.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	requested, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{
		DomainName: aws.String("example.test"),
	})
	if err != nil {
		t.Fatalf("RequestCertificate() error = %v", err)
	}
	if aws.ToString(requested.CertificateArn) == "" {
		t.Fatal("RequestCertificate() returned empty CertificateArn")
	}

	described, err := client.DescribeCertificate(context.Background(), &acm.DescribeCertificateInput{
		CertificateArn: requested.CertificateArn,
	})
	if err != nil {
		t.Fatalf("DescribeCertificate() error = %v", err)
	}
	if described.Certificate == nil || aws.ToString(described.Certificate.DomainName) != "example.test" || described.Certificate.Status != types.CertificateStatusIssued {
		t.Fatalf("DescribeCertificate() = %#v, want issued example.test cert", described.Certificate)
	}

	listed, err := client.ListCertificates(context.Background(), &acm.ListCertificatesInput{})
	if err != nil {
		t.Fatalf("ListCertificates() error = %v", err)
	}
	if len(listed.CertificateSummaryList) != 1 || aws.ToString(listed.CertificateSummaryList[0].CertificateArn) != aws.ToString(requested.CertificateArn) {
		t.Fatalf("ListCertificates() = %#v, want requested cert", listed.CertificateSummaryList)
	}

	if _, err := client.DeleteCertificate(context.Background(), &acm.DeleteCertificateInput{
		CertificateArn: requested.CertificateArn,
	}); err != nil {
		t.Fatalf("DeleteCertificate() error = %v", err)
	}
	listed, err = client.ListCertificates(context.Background(), &acm.ListCertificatesInput{})
	if err != nil {
		t.Fatalf("ListCertificates(after delete) error = %v", err)
	}
	if len(listed.CertificateSummaryList) != 0 {
		t.Fatalf("ListCertificates(after delete) = %#v, want no certs", listed.CertificateSummaryList)
	}
}

func TestACMCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewACMAdapter())
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

	var requested struct {
		CertificateARN string `json:"CertificateArn"`
	}
	if err := json.Unmarshal(runAWS("acm", "request-certificate", "--domain-name", "example.test"), &requested); err != nil {
		t.Fatalf("decode request-certificate output: %v", err)
	}
	if requested.CertificateARN == "" {
		t.Fatal("request-certificate returned empty CertificateArn")
	}

	var described struct {
		Certificate struct {
			CertificateARN string `json:"CertificateArn"`
			DomainName     string `json:"DomainName"`
			Status         string `json:"Status"`
		} `json:"Certificate"`
	}
	if err := json.Unmarshal(runAWS("acm", "describe-certificate", "--certificate-arn", requested.CertificateARN), &described); err != nil {
		t.Fatalf("decode describe-certificate output: %v", err)
	}
	if described.Certificate.CertificateARN != requested.CertificateARN || described.Certificate.DomainName != "example.test" || described.Certificate.Status != "ISSUED" {
		t.Fatalf("describe-certificate = %#v, want issued example.test cert", described.Certificate)
	}

	var listed struct {
		CertificateSummaryList []struct {
			CertificateARN string `json:"CertificateArn"`
		} `json:"CertificateSummaryList"`
	}
	if err := json.Unmarshal(runAWS("acm", "list-certificates"), &listed); err != nil {
		t.Fatalf("decode list-certificates output: %v", err)
	}
	if len(listed.CertificateSummaryList) != 1 || listed.CertificateSummaryList[0].CertificateARN != requested.CertificateARN {
		t.Fatalf("list-certificates = %#v, want requested cert", listed.CertificateSummaryList)
	}

	runAWS("acm", "delete-certificate", "--certificate-arn", requested.CertificateARN)
}

func TestACMCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewACMAdapter())
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
    acm = %q
  }
}

resource "aws_acm_certificate" "deploy" {
  domain_name       = "terraform.example.test"
  validation_method = "DNS"
  tags = {
    env = "test"
  }
}

output "certificate_arn" {
  value = aws_acm_certificate.deploy.arn
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

	runTerraform("init", "-input=false")
	runTerraform("apply", "-input=false", "-auto-approve")
	defer runTerraform("destroy", "-input=false", "-auto-approve")

	out := runTerraform("output", "-raw", "certificate_arn")
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("terraform output certificate_arn is empty")
	}
}

func TestACMCompatibilityAdapterAuthorizesAndAuditsRequestCertificate(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewACMAdapter(
		compataws.WithACMAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"acm:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"acm:RequestCertificate"}, Resources: []string{"*"}},
		)),
		compataws.WithACMAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := acm.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *acm.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{
		DomainName: aws.String("example.test"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("RequestCertificate(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "acm:RequestCertificate", false)

	listed, err := client.ListCertificates(context.Background(), &acm.ListCertificatesInput{})
	if err != nil {
		t.Fatalf("ListCertificates(after denied request) error = %v", err)
	}
	if len(listed.CertificateSummaryList) != 0 {
		t.Fatalf("ListCertificates(after denied request) = %#v, want no certs", listed.CertificateSummaryList)
	}
}

func TestACMCompatibilityAdapterAuthorizesAndAuditsDescribeCertificate(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewACMAdapter(
		compataws.WithACMAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"acm:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"acm:DescribeCertificate"}, Resources: []string{"*"}},
		)),
		compataws.WithACMAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := acm.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *acm.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	requested, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{
		DomainName: aws.String("example.test"),
	})
	if err != nil {
		t.Fatalf("RequestCertificate() error = %v", err)
	}

	_, err = client.DescribeCertificate(context.Background(), &acm.DescribeCertificateInput{
		CertificateArn: requested.CertificateArn,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("DescribeCertificate(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "acm:DescribeCertificate", false)

	listed, err := client.ListCertificates(context.Background(), &acm.ListCertificatesInput{})
	if err != nil {
		t.Fatalf("ListCertificates(after denied describe) error = %v", err)
	}
	if len(listed.CertificateSummaryList) != 1 || aws.ToString(listed.CertificateSummaryList[0].CertificateArn) != aws.ToString(requested.CertificateArn) {
		t.Fatalf("ListCertificates(after denied describe) = %#v, want existing cert", listed.CertificateSummaryList)
	}
}

func TestACMCompatibilityAdapterAuthorizesMissingCertificateBeforeLookup(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewACMAdapter(
		compataws.WithACMAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Deny, Actions: []string{"acm:DescribeCertificate", "acm:DeleteCertificate"}, Resources: []string{"*"}},
		)),
		compataws.WithACMAuditSink(auditLog.Record),
	))
	defer server.Close()
	client := acm.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *acm.Options) { o.BaseEndpoint = aws.String(server.URL) })
	missing := aws.String("arn:aws:acm:us-east-1:000000000000:certificate/missing")
	for _, call := range []struct {
		name string
		run  func() error
	}{
		{"DescribeCertificate", func() error {
			_, err := client.DescribeCertificate(context.Background(), &acm.DescribeCertificateInput{CertificateArn: missing})
			return err
		}},
		{"DeleteCertificate", func() error {
			_, err := client.DeleteCertificate(context.Background(), &acm.DeleteCertificateInput{CertificateArn: missing})
			return err
		}},
	} {
		var apiErr smithy.APIError
		if err := call.run(); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("%s(missing denied) error = %v, want AccessDenied", call.name, err)
		}
	}
	assertDecision(t, auditLog.Decisions(), "acm:DescribeCertificate", false)
	assertDecision(t, auditLog.Decisions(), "acm:DeleteCertificate", false)
}

func TestACMCompatibilityAdapterShapesAuthorizerFailure(t *testing.T) {
	server := httptest.NewServer(compataws.NewACMAdapter(
		compataws.WithACMAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
			return authz.Decision{}, errors.New("authorizer unavailable")
		})),
	))
	defer server.Close()
	client := acm.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *acm.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{DomainName: aws.String("example.test")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InternalFailure" {
		t.Fatalf("RequestCertificate(authorizer failure) error = %v, want InternalFailure", err)
	}
}

func TestACMCompatibilityAdapterAuthorizesAndAuditsListCertificates(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewACMAdapter(
		compataws.WithACMAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"acm:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"acm:ListCertificates"}, Resources: []string{"*"}},
		)),
		compataws.WithACMAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := acm.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *acm.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{
		DomainName: aws.String("example.test"),
	}); err != nil {
		t.Fatalf("RequestCertificate() error = %v", err)
	}

	_, err := client.ListCertificates(context.Background(), &acm.ListCertificatesInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("ListCertificates(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "acm:ListCertificates", false)
}

func TestACMCompatibilityAdapterAuthorizesAndAuditsDeleteCertificate(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewACMAdapter(
		compataws.WithACMAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"acm:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"acm:DeleteCertificate"}, Resources: []string{"*"}},
		)),
		compataws.WithACMAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := acm.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *acm.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	requested, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{
		DomainName: aws.String("example.test"),
	})
	if err != nil {
		t.Fatalf("RequestCertificate() error = %v", err)
	}

	_, err = client.DeleteCertificate(context.Background(), &acm.DeleteCertificateInput{
		CertificateArn: requested.CertificateArn,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("DeleteCertificate(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "acm:DeleteCertificate", false)

	listed, err := client.ListCertificates(context.Background(), &acm.ListCertificatesInput{})
	if err != nil {
		t.Fatalf("ListCertificates(after denied delete) error = %v", err)
	}
	if len(listed.CertificateSummaryList) != 1 || aws.ToString(listed.CertificateSummaryList[0].CertificateArn) != aws.ToString(requested.CertificateArn) {
		t.Fatalf("ListCertificates(after denied delete) = %#v, want existing cert", listed.CertificateSummaryList)
	}
}

func TestACMCompatibilityAdapterPaginatesCertificatesAndEnforcesQuota(t *testing.T) {
	server := httptest.NewServer(compataws.NewACMAdapter(compataws.WithACMCertificateQuota(2)))
	defer server.Close()

	client := acm.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *acm.Options) { o.BaseEndpoint = aws.String(server.URL) })

	for _, domain := range []string{"a.example.test", "b.example.test"} {
		if _, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{DomainName: aws.String(domain)}); err != nil {
			t.Fatalf("RequestCertificate(%q) error = %v", domain, err)
		}
	}

	first, err := client.ListCertificates(context.Background(), &acm.ListCertificatesInput{MaxItems: aws.Int32(1)})
	if err != nil || len(first.CertificateSummaryList) != 1 || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListCertificates(first page) = %#v, %v; want one result and next token", first, err)
	}
	second, err := client.ListCertificates(context.Background(), &acm.ListCertificatesInput{MaxItems: aws.Int32(1), NextToken: first.NextToken})
	if err != nil || len(second.CertificateSummaryList) != 1 || aws.ToString(second.NextToken) != "" {
		t.Fatalf("ListCertificates(second page) = %#v, %v; want final result", second, err)
	}

	_, err = client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{DomainName: aws.String("c.example.test")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("RequestCertificate(over quota) error = %v, want LimitExceededException", err)
	}
}

func TestACMCompatibilityAdapterRejectsInvalidDomainName(t *testing.T) {
	server := httptest.NewServer(compataws.NewACMAdapter())
	defer server.Close()
	client := acm.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *acm.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{DomainName: aws.String("not a domain")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
		t.Fatalf("RequestCertificate(invalid domain) error = %v, want ValidationException", err)
	}
}

func TestACMCompatibilityAdapterMakesRequestCertificateIdempotent(t *testing.T) {
	server := httptest.NewServer(compataws.NewACMAdapter(compataws.WithACMCertificateQuota(1)))
	defer server.Close()

	client := acm.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *acm.Options) { o.BaseEndpoint = aws.String(server.URL) })

	input := &acm.RequestCertificateInput{DomainName: aws.String("retry.example.test"), IdempotencyToken: aws.String("retry-token")}
	first, err := client.RequestCertificate(context.Background(), input)
	if err != nil {
		t.Fatalf("RequestCertificate(first) error = %v", err)
	}
	second, err := client.RequestCertificate(context.Background(), input)
	if err != nil || aws.ToString(second.CertificateArn) != aws.ToString(first.CertificateArn) {
		t.Fatalf("RequestCertificate(retry) = %#v, %v; want first certificate ARN %q", second, err, aws.ToString(first.CertificateArn))
	}
	listed, err := client.ListCertificates(context.Background(), &acm.ListCertificatesInput{})
	if err != nil || len(listed.CertificateSummaryList) != 1 {
		t.Fatalf("ListCertificates() = %#v, %v; want one certificate", listed, err)
	}
}

func TestACMCompatibilityAdapterManagesAndAuthorizesCertificateTags(t *testing.T) {
	auditLog := authz.NewAuditLog()
	deniedARN := ""
	server := httptest.NewServer(compataws.NewACMAdapter(
		compataws.WithACMAuthorizer(authz.AuthorizerFunc(func(_ context.Context, req authz.Request) (authz.Decision, error) {
			return authz.Decision{Request: req, Allowed: req.Action != "acm:AddTagsToCertificate" || req.Resource != deniedARN}, nil
		})),
		compataws.WithACMAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := acm.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *acm.Options) { o.BaseEndpoint = aws.String(server.URL) })

	allowed, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{DomainName: aws.String("allowed.example.test")})
	if err != nil {
		t.Fatalf("RequestCertificate(allowed) error = %v", err)
	}
	if _, err := client.AddTagsToCertificate(context.Background(), &acm.AddTagsToCertificateInput{
		CertificateArn: allowed.CertificateArn,
		Tags:           []types.Tag{{Key: aws.String("env"), Value: aws.String("test")}},
	}); err != nil {
		t.Fatalf("AddTagsToCertificate() error = %v", err)
	}
	tags, err := client.ListTagsForCertificate(context.Background(), &acm.ListTagsForCertificateInput{CertificateArn: allowed.CertificateArn})
	if err != nil || len(tags.Tags) != 1 || aws.ToString(tags.Tags[0].Key) != "env" {
		t.Fatalf("ListTagsForCertificate() = %#v, %v; want env tag", tags, err)
	}
	if _, err := client.RemoveTagsFromCertificate(context.Background(), &acm.RemoveTagsFromCertificateInput{
		CertificateArn: allowed.CertificateArn,
		Tags:           []types.Tag{{Key: aws.String("env")}},
	}); err != nil {
		t.Fatalf("RemoveTagsFromCertificate() error = %v", err)
	}

	denied, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{DomainName: aws.String("denied.example.test")})
	if err != nil {
		t.Fatalf("RequestCertificate(denied target) error = %v", err)
	}
	deniedARN = aws.ToString(denied.CertificateArn)
	_, err = client.AddTagsToCertificate(context.Background(), &acm.AddTagsToCertificateInput{CertificateArn: denied.CertificateArn, Tags: []types.Tag{{Key: aws.String("env"), Value: aws.String("prod")}}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("AddTagsToCertificate(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "acm:AddTagsToCertificate", false)
}

func TestACMCompatibilityAdapterRejectsExpiredCredential(t *testing.T) {
	server := httptest.NewServer(compataws.NewACMAdapter(compataws.WithACMAuthorizer(authz.NewPolicyAuthorizer(
		authz.Rule{Effect: authz.Allow, Actions: []string{"acm:*"}, Resources: []string{"*"}},
		authz.Rule{
			Effect:    authz.Deny,
			Actions:   []string{"acm:RequestCertificate"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_expired", Values: []string{"true"}},
			},
		},
	))))
	defer server.Close()
	client := acm.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *acm.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Expired", "true"))
	})
	_, err := client.RequestCertificate(context.Background(), &acm.RequestCertificateInput{DomainName: aws.String("expired.example")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("RequestCertificate(expired credential) error = %v, want AccessDenied", err)
	}
}
