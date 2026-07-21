package compat_test

import (
	"archive/zip"
	"bytes"
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
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestLambdaCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewLambdaAdapter())
	defer server.Close()

	client := lambda.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *lambda.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{
		FunctionName: aws.String("orders-handler"),
		Runtime:      types.RuntimeNodejs20x,
		Role:         aws.String("arn:aws:iam::000000000000:role/homeport"),
		Handler:      aws.String("index.handler"),
		Code:         &types.FunctionCode{ZipFile: []byte("fake zip")},
	})
	if err != nil {
		t.Fatalf("CreateFunction() error = %v", err)
	}
	if aws.ToString(created.FunctionName) != "orders-handler" {
		t.Fatalf("CreateFunction() name = %q, want orders-handler", aws.ToString(created.FunctionName))
	}

	got, err := client.GetFunction(context.Background(), &lambda.GetFunctionInput{
		FunctionName: aws.String("orders-handler"),
	})
	if err != nil {
		t.Fatalf("GetFunction() error = %v", err)
	}
	if got.Configuration == nil || aws.ToString(got.Configuration.Handler) != "index.handler" {
		t.Fatalf("GetFunction() configuration = %#v, want handler", got.Configuration)
	}

	updated, err := client.UpdateFunctionCode(context.Background(), &lambda.UpdateFunctionCodeInput{
		FunctionName: aws.String("orders-handler"),
		ZipFile:      []byte("new fake zip"),
	})
	if err != nil {
		t.Fatalf("UpdateFunctionCode() error = %v", err)
	}
	if aws.ToString(updated.FunctionName) != "orders-handler" {
		t.Fatalf("UpdateFunctionCode() name = %q, want orders-handler", aws.ToString(updated.FunctionName))
	}
	if aws.ToString(updated.RevisionId) == "" || aws.ToString(updated.RevisionId) == aws.ToString(created.RevisionId) {
		t.Fatalf("UpdateFunctionCode() revision = %q, want new revision after %q", aws.ToString(updated.RevisionId), aws.ToString(created.RevisionId))
	}

	invoked, err := client.Invoke(context.Background(), &lambda.InvokeInput{
		FunctionName: aws.String("orders-handler"),
		Payload:      []byte(`{"hello":"world"}`),
	})
	if err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if invoked.StatusCode != 200 || !bytes.Contains(invoked.Payload, []byte(`"function":"orders-handler"`)) {
		t.Fatalf("Invoke() = status %d payload %s, want function echo", invoked.StatusCode, invoked.Payload)
	}

	if _, err := client.DeleteFunction(context.Background(), &lambda.DeleteFunctionInput{
		FunctionName: aws.String("orders-handler"),
	}); err != nil {
		t.Fatalf("DeleteFunction() error = %v", err)
	}
	_, err = client.GetFunction(context.Background(), &lambda.GetFunctionInput{
		FunctionName: aws.String("orders-handler"),
	})
	if err == nil {
		t.Fatal("GetFunction(after delete) error = nil, want missing function")
	}
}

func TestLambdaCompatibilityAdapterListsFunctionsWithPagination(t *testing.T) {
	server := httptest.NewServer(compataws.NewLambdaAdapter())
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	for _, name := range []string{"alpha", "bravo"} {
		if _, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{FunctionName: aws.String(name), Runtime: types.RuntimeNodejs20x, Role: aws.String("arn:aws:iam::000000000000:role/homeport"), Handler: aws.String("index.handler"), Code: &types.FunctionCode{ZipFile: []byte("zip")}}); err != nil {
			t.Fatalf("CreateFunction(%s) error = %v", name, err)
		}
	}
	first, err := client.ListFunctions(context.Background(), &lambda.ListFunctionsInput{MaxItems: aws.Int32(1)})
	if err != nil || len(first.Functions) != 1 || first.NextMarker == nil {
		t.Fatalf("ListFunctions(first) = %#v, %v; want one function and marker", first, err)
	}
	second, err := client.ListFunctions(context.Background(), &lambda.ListFunctionsInput{MaxItems: aws.Int32(1), Marker: first.NextMarker})
	if err != nil || len(second.Functions) != 1 || second.NextMarker != nil {
		t.Fatalf("ListFunctions(second) = %#v, %v; want final function", second, err)
	}
}

func TestLambdaCompatibilityAdapterShapesAuthorizerFailure(t *testing.T) {
	server := httptest.NewServer(compataws.NewLambdaAdapter(
		compataws.WithLambdaAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
			return authz.Decision{}, errors.New("authorizer unavailable")
		})),
	))
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListFunctions(context.Background(), &lambda.ListFunctionsInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ServiceException" {
		t.Fatalf("ListFunctions(authorizer failure) error = %v, want ServiceException", err)
	}
}

func TestLambdaCompatibilityAdapterRejectsIncompleteCreateFunction(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/2015-03-31/functions", bytes.NewBufferString(`{}`))
	compataws.NewLambdaAdapter().ServeHTTP(recorder, request)
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "InvalidParameterValueException") {
		t.Fatalf("CreateFunction({}) = %d %s, want InvalidParameterValueException", recorder.Code, recorder.Body.String())
	}
}

func TestLambdaCompatibilityAdapterRejectsMalformedUpdateFunctionCode(t *testing.T) {
	adapter := compataws.NewLambdaAdapter()
	create := httptest.NewRequest("POST", "/2015-03-31/functions", bytes.NewBufferString(`{"FunctionName":"orders","Runtime":"nodejs20.x","Role":"arn:aws:iam::000000000000:role/homeport","Handler":"index.handler","Code":{}}`))
	adapter.ServeHTTP(httptest.NewRecorder(), create)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("PUT", "/2015-03-31/functions/orders/code", bytes.NewBufferString(`{"ZipFile":`))
	adapter.ServeHTTP(recorder, request)
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "InvalidParameterValueException") {
		t.Fatalf("UpdateFunctionCode(malformed) = %d %s, want InvalidParameterValueException", recorder.Code, recorder.Body.String())
	}
}

func TestLambdaCompatibilityAdapterAuthorizesMissingTagResourceBeforeLookup(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewLambdaAdapter(
		compataws.WithLambdaAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"lambda:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"lambda:ListTags"}, Resources: []string{"*"}},
		)),
		compataws.WithLambdaAuditSink(auditLog.Record),
	))
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListTags(context.Background(), &lambda.ListTagsInput{Resource: aws.String("arn:aws:lambda:us-east-1:000000000000:function:missing")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("ListTags(missing denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "lambda:ListTags", false)
}

func TestLambdaCompatibilityAdapterAuthorizesCodeSigningConfig(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewLambdaAdapter(
		compataws.WithLambdaAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"lambda:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"lambda:GetFunctionCodeSigningConfig"}, Resources: []string{"*"}},
		)),
		compataws.WithLambdaAuditSink(auditLog.Record),
	))
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{FunctionName: aws.String("signed"), Runtime: types.RuntimeNodejs20x, Role: aws.String("arn:aws:iam::000000000000:role/homeport"), Handler: aws.String("index.handler"), Code: &types.FunctionCode{ZipFile: []byte("zip")}}); err != nil {
		t.Fatalf("CreateFunction() error = %v", err)
	}
	_, err := client.GetFunctionCodeSigningConfig(context.Background(), &lambda.GetFunctionCodeSigningConfigInput{FunctionName: aws.String("signed")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("GetFunctionCodeSigningConfig(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "lambda:GetFunctionCodeSigningConfig", false)
}

func TestLambdaCompatibilityAdapterRejectsInvalidListFunctionsPagination(t *testing.T) {
	server := httptest.NewServer(compataws.NewLambdaAdapter())
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListFunctions(context.Background(), &lambda.ListFunctionsInput{Marker: aws.String("not-a-marker")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValueException" {
		t.Fatalf("ListFunctions(invalid marker) error = %v, want InvalidParameterValueException", err)
	}
}

func TestLambdaCompatibilityAdapterRejectsNonPositiveListFunctionsMaxItems(t *testing.T) {
	server := httptest.NewServer(compataws.NewLambdaAdapter())
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListFunctions(context.Background(), &lambda.ListFunctionsInput{MaxItems: aws.Int32(0)})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValueException" {
		t.Fatalf("ListFunctions(MaxItems=0) error = %v, want InvalidParameterValueException", err)
	}
}

func TestLambdaCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewLambdaAdapter())
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

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "function.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("write function zip: %v", err)
	}
	zipWriter := zip.NewWriter(zipFile)
	entry, err := zipWriter.Create("index.js")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write([]byte("exports.handler = async () => {};")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

	var created struct {
		FunctionName string `json:"FunctionName"`
		Handler      string `json:"Handler"`
		RevisionID   string `json:"RevisionId"`
	}
	if err := json.Unmarshal(runAWS(
		"lambda", "create-function",
		"--function-name", "orders-handler",
		"--runtime", "nodejs20.x",
		"--role", "arn:aws:iam::000000000000:role/homeport",
		"--handler", "index.handler",
		"--zip-file", "fileb://"+zipPath,
	), &created); err != nil {
		t.Fatalf("decode create-function output: %v", err)
	}
	if created.FunctionName != "orders-handler" || created.Handler != "index.handler" || created.RevisionID == "" {
		t.Fatalf("create-function = %#v, want orders-handler with handler and revision", created)
	}

	var got struct {
		Configuration struct {
			FunctionName string `json:"FunctionName"`
			Handler      string `json:"Handler"`
		} `json:"Configuration"`
	}
	if err := json.Unmarshal(runAWS("lambda", "get-function", "--function-name", "orders-handler"), &got); err != nil {
		t.Fatalf("decode get-function output: %v", err)
	}
	if got.Configuration.FunctionName != "orders-handler" || got.Configuration.Handler != "index.handler" {
		t.Fatalf("get-function = %#v, want orders-handler configuration", got.Configuration)
	}

	payloadPath := filepath.Join(dir, "invoke.json")
	var invoked struct {
		StatusCode int `json:"StatusCode"`
	}
	if err := json.Unmarshal(runAWS(
		"lambda", "invoke",
		"--function-name", "orders-handler",
		"--payload", `{"hello":"world"}`,
		"--cli-binary-format", "raw-in-base64-out",
		payloadPath,
	), &invoked); err != nil {
		t.Fatalf("decode invoke output: %v", err)
	}
	if invoked.StatusCode != 200 {
		t.Fatalf("invoke status = %d, want 200", invoked.StatusCode)
	}
	payload, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatalf("read invoke payload: %v", err)
	}
	if !bytes.Contains(payload, []byte(`"function":"orders-handler"`)) {
		t.Fatalf("invoke payload = %s, want function echo", payload)
	}

	runAWS("lambda", "delete-function", "--function-name", "orders-handler")
}

func TestLambdaCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewLambdaAdapter())
	defer server.Close()

	dir := t.TempDir()
	zipPath := filepath.Join(dir, "function.zip")
	zipFile, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("write function zip: %v", err)
	}
	zipWriter := zip.NewWriter(zipFile)
	entry, err := zipWriter.Create("index.js")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := entry.Write([]byte("exports.handler = async () => {};")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := zipFile.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}

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
    lambda = %q
  }
}

resource "aws_lambda_function" "deploy" {
  function_name    = "terraform-orders-handler"
  filename         = %q
  source_code_hash = filebase64sha256(%q)
  role             = "arn:aws:iam::000000000000:role/homeport"
  handler          = "index.handler"
  runtime          = "nodejs20.x"
  tags = {
    env = "test"
  }

  timeouts {
    create = "20s"
    delete = "20s"
  }
}

output "function_name" {
  value = aws_lambda_function.deploy.function_name
}
`, server.URL, zipPath, zipPath)
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

	out := runTerraform("output", "-raw", "function_name")
	if strings.TrimSpace(string(out)) != "terraform-orders-handler" {
		t.Fatalf("terraform output function_name = %q, want terraform-orders-handler", strings.TrimSpace(string(out)))
	}
}

func TestLambdaCompatibilityAdapterReturnsConflictForDuplicateCreate(t *testing.T) {
	server := httptest.NewServer(compataws.NewLambdaAdapter())
	defer server.Close()

	client := lambda.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *lambda.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	input := &lambda.CreateFunctionInput{
		FunctionName: aws.String("orders-handler"),
		Runtime:      types.RuntimeNodejs20x,
		Role:         aws.String("arn:aws:iam::000000000000:role/homeport"),
		Handler:      aws.String("index.handler"),
		Code:         &types.FunctionCode{ZipFile: []byte("fake zip")},
	}
	if _, err := client.CreateFunction(context.Background(), input); err != nil {
		t.Fatalf("CreateFunction() error = %v", err)
	}
	_, err := client.CreateFunction(context.Background(), input)
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceConflictException" {
		t.Fatalf("CreateFunction(duplicate) error = %v, want ResourceConflictException", err)
	}
}

func TestLambdaCompatibilityAdapterAuthorizesAndAuditsCreateFunction(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewLambdaAdapter(
		compataws.WithLambdaAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"lambda:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"lambda:CreateFunction"}, Resources: []string{"*"}},
		)),
		compataws.WithLambdaAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := lambda.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *lambda.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{
		FunctionName: aws.String("orders-handler"),
		Runtime:      types.RuntimeNodejs20x,
		Role:         aws.String("arn:aws:iam::000000000000:role/homeport"),
		Handler:      aws.String("index.handler"),
		Code:         &types.FunctionCode{ZipFile: []byte("fake zip")},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("CreateFunction(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "lambda:CreateFunction", false)

	_, err = client.GetFunction(context.Background(), &lambda.GetFunctionInput{
		FunctionName: aws.String("orders-handler"),
	})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("GetFunction(after denied create) error = %v, want ResourceNotFoundException", err)
	}
}

func TestLambdaCompatibilityAdapterAuthorizesAndAuditsDeleteFunction(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewLambdaAdapter(
		compataws.WithLambdaAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"lambda:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"lambda:DeleteFunction"}, Resources: []string{"*"}},
		)),
		compataws.WithLambdaAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := lambda.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *lambda.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	input := &lambda.CreateFunctionInput{
		FunctionName: aws.String("orders-handler"),
		Runtime:      types.RuntimeNodejs20x,
		Role:         aws.String("arn:aws:iam::000000000000:role/homeport"),
		Handler:      aws.String("index.handler"),
		Code:         &types.FunctionCode{ZipFile: []byte("fake zip")},
	}
	if _, err := client.CreateFunction(context.Background(), input); err != nil {
		t.Fatalf("CreateFunction() error = %v", err)
	}

	_, err := client.DeleteFunction(context.Background(), &lambda.DeleteFunctionInput{
		FunctionName: aws.String("orders-handler"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("DeleteFunction(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "lambda:DeleteFunction", false)

	got, err := client.GetFunction(context.Background(), &lambda.GetFunctionInput{
		FunctionName: aws.String("orders-handler"),
	})
	if err != nil {
		t.Fatalf("GetFunction(after denied delete) error = %v", err)
	}
	if got.Configuration == nil || aws.ToString(got.Configuration.FunctionName) != "orders-handler" {
		t.Fatalf("GetFunction(after denied delete) = %#v, want existing function", got.Configuration)
	}
}

func TestLambdaCompatibilityAdapterAuthorizesAndAuditsGetFunction(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewLambdaAdapter(
		compataws.WithLambdaAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"lambda:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"lambda:GetFunction"}, Resources: []string{"*"}},
		)),
		compataws.WithLambdaAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := lambda.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *lambda.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{
		FunctionName: aws.String("orders-handler"),
		Runtime:      types.RuntimeNodejs20x,
		Role:         aws.String("arn:aws:iam::000000000000:role/homeport"),
		Handler:      aws.String("index.handler"),
		Code:         &types.FunctionCode{ZipFile: []byte("fake zip")},
	}); err != nil {
		t.Fatalf("CreateFunction() error = %v", err)
	}

	_, err := client.GetFunction(context.Background(), &lambda.GetFunctionInput{
		FunctionName: aws.String("orders-handler"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("GetFunction(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "lambda:GetFunction", false)
}

func TestLambdaCompatibilityAdapterAuthorizesAndAuditsInvoke(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewLambdaAdapter(
		compataws.WithLambdaAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"lambda:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"lambda:Invoke"}, Resources: []string{"*"}},
		)),
		compataws.WithLambdaAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := lambda.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *lambda.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{
		FunctionName: aws.String("orders-handler"),
		Runtime:      types.RuntimeNodejs20x,
		Role:         aws.String("arn:aws:iam::000000000000:role/homeport"),
		Handler:      aws.String("index.handler"),
		Code:         &types.FunctionCode{ZipFile: []byte("fake zip")},
	}); err != nil {
		t.Fatalf("CreateFunction() error = %v", err)
	}

	_, err := client.Invoke(context.Background(), &lambda.InvokeInput{
		FunctionName: aws.String("orders-handler"),
		Payload:      []byte(`{"hello":"world"}`),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("Invoke(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "lambda:Invoke", false)

	if _, err := client.GetFunction(context.Background(), &lambda.GetFunctionInput{
		FunctionName: aws.String("orders-handler"),
	}); err != nil {
		t.Fatalf("GetFunction(after denied invoke) error = %v", err)
	}
}

func TestLambdaCompatibilityAdapterAuthorizesAndAuditsUpdateFunctionCode(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewLambdaAdapter(
		compataws.WithLambdaAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"lambda:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"lambda:UpdateFunctionCode"}, Resources: []string{"*"}},
		)),
		compataws.WithLambdaAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := lambda.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *lambda.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{
		FunctionName: aws.String("orders-handler"),
		Runtime:      types.RuntimeNodejs20x,
		Role:         aws.String("arn:aws:iam::000000000000:role/homeport"),
		Handler:      aws.String("index.handler"),
		Code:         &types.FunctionCode{ZipFile: []byte("fake zip")},
	})
	if err != nil {
		t.Fatalf("CreateFunction() error = %v", err)
	}

	_, err = client.UpdateFunctionCode(context.Background(), &lambda.UpdateFunctionCodeInput{
		FunctionName: aws.String("orders-handler"),
		ZipFile:      []byte("new fake zip"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("UpdateFunctionCode(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "lambda:UpdateFunctionCode", false)

	got, err := client.GetFunction(context.Background(), &lambda.GetFunctionInput{
		FunctionName: aws.String("orders-handler"),
	})
	if err != nil {
		t.Fatalf("GetFunction(after denied update) error = %v", err)
	}
	if got.Configuration == nil || aws.ToString(got.Configuration.RevisionId) != aws.ToString(created.RevisionId) {
		t.Fatalf("GetFunction(after denied update) revision = %#v, want %q", got.Configuration, aws.ToString(created.RevisionId))
	}
}

func TestLambdaCompatibilityAdapterAuthorizesAndAuditsTags(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewLambdaAdapter(
		compataws.WithLambdaAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"lambda:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"lambda:TagResource"}, Resources: []string{"*"}},
		)),
		compataws.WithLambdaAuditSink(auditLog.Record),
	))
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{FunctionName: aws.String("tagged"), Runtime: types.RuntimeNodejs20x, Role: aws.String("arn:aws:iam::000000000000:role/homeport"), Handler: aws.String("index.handler"), Code: &types.FunctionCode{ZipFile: []byte("zip")}})
	if err != nil {
		t.Fatalf("CreateFunction() error = %v", err)
	}
	_, err = client.TagResource(context.Background(), &lambda.TagResourceInput{Resource: created.FunctionArn, Tags: map[string]string{"env": "test"}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("TagResource(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "lambda:TagResource", false)
}

func TestLambdaCompatibilityAdapterAuthorizesAndAuditsListVersions(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewLambdaAdapter(
		compataws.WithLambdaAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"lambda:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"lambda:ListVersionsByFunction"}, Resources: []string{"*"}},
		)),
		compataws.WithLambdaAuditSink(auditLog.Record),
	))
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{FunctionName: aws.String("versioned"), Runtime: types.RuntimeNodejs20x, Role: aws.String("arn:aws:iam::000000000000:role/homeport"), Handler: aws.String("index.handler"), Code: &types.FunctionCode{ZipFile: []byte("zip")}}); err != nil {
		t.Fatalf("CreateFunction() error = %v", err)
	}
	_, err := client.ListVersionsByFunction(context.Background(), &lambda.ListVersionsByFunctionInput{FunctionName: aws.String("versioned")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("ListVersionsByFunction(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "lambda:ListVersionsByFunction", false)
}

func TestLambdaCompatibilityAdapterRejectsMalformedTagResource(t *testing.T) {
	server := httptest.NewServer(compataws.NewLambdaAdapter())
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{FunctionName: aws.String("tagged"), Runtime: types.RuntimeNodejs20x, Role: aws.String("arn:aws:iam::000000000000:role/homeport"), Handler: aws.String("index.handler"), Code: &types.FunctionCode{ZipFile: []byte("zip")}})
	if err != nil {
		t.Fatalf("CreateFunction() error = %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/2017-03-31/tags/"+aws.ToString(created.FunctionArn), strings.NewReader("{"))
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
	if resp.StatusCode != http.StatusBadRequest || body["__type"] != "InvalidParameterValueException" {
		t.Fatalf("malformed TagResource = status %d body %#v, want 400 InvalidParameterValueException", resp.StatusCode, body)
	}
}

func TestLambdaCompatibilityAdapterReturnsQuotaError(t *testing.T) {
	server := httptest.NewServer(compataws.NewLambdaAdapter(compataws.WithLambdaQuota(1)))
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	create := func(name string) error {
		_, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{FunctionName: aws.String(name), Runtime: types.RuntimeNodejs20x, Role: aws.String("arn:aws:iam::000000000000:role/homeport"), Handler: aws.String("index.handler"), Code: &types.FunctionCode{ZipFile: []byte("zip")}})
		return err
	}
	if err := create("first"); err != nil {
		t.Fatalf("CreateFunction(first) error = %v", err)
	}
	err := create("second")
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TooManyRequestsException" {
		t.Fatalf("CreateFunction(over quota) error = %v, want TooManyRequestsException", err)
	}
}

func TestLambdaCompatibilityAdapterCapsListFunctionsAtFifty(t *testing.T) {
	server := httptest.NewServer(compataws.NewLambdaAdapter())
	defer server.Close()
	client := lambda.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *lambda.Options) { o.BaseEndpoint = aws.String(server.URL) })
	for i := 0; i < 51; i++ {
		name := fmt.Sprintf("function-%02d", i)
		if _, err := client.CreateFunction(context.Background(), &lambda.CreateFunctionInput{FunctionName: aws.String(name), Runtime: types.RuntimeNodejs20x, Role: aws.String("arn:aws:iam::000000000000:role/homeport"), Handler: aws.String("index.handler"), Code: &types.FunctionCode{ZipFile: []byte("zip")}}); err != nil {
			t.Fatalf("CreateFunction(%s) error = %v", name, err)
		}
	}
	listed, err := client.ListFunctions(context.Background(), &lambda.ListFunctionsInput{MaxItems: aws.Int32(100)})
	if err != nil || len(listed.Functions) != 50 || listed.NextMarker == nil {
		t.Fatalf("ListFunctions(MaxItems=100) = %#v, %v; want 50 functions and marker", listed, err)
	}
}
