package compat_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestS3SDKPutGetObjectAgainstCompatibleEndpoint(t *testing.T) {
	objects := map[string][]byte{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/bucket/")
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			objects[key] = body
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if _, err := w.Write(objects[key]); err != nil {
				t.Errorf("write response: %v", err)
			}
		default:
			http.Error(w, "unsupported", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	_, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("hello.txt"),
		Body:   bytes.NewReader([]byte("hello")),
	})
	if err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}

	got, err := client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("hello.txt"),
	})
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	body, _ := io.ReadAll(got.Body)
	if string(body) != "hello" {
		t.Fatalf("GetObject body = %q, want hello", body)
	}
}

func TestS3CompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	if _, err := client.HeadBucket(context.Background(), &s3.HeadBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("HeadBucket() error = %v", err)
	}
	if _, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("hello.txt"),
		Body:   bytes.NewReader([]byte("hello")),
	}); err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}
	got, err := client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("hello.txt"),
	})
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	body, _ := io.ReadAll(got.Body)
	if string(body) != "hello" {
		t.Fatalf("GetObject body = %q, want hello", body)
	}
	if _, err := client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("hello.txt"),
	}); err != nil {
		t.Fatalf("DeleteObject() error = %v", err)
	}
}

func TestS3CompatibilityAdapterRejectsDeletingNonEmptyBucket(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()
	client := s3.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *s3.Options) { o.BaseEndpoint = aws.String(server.URL); o.UsePathStyle = true })
	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("non-empty")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	if _, err := client.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String("non-empty"), Key: aws.String("object"), Body: bytes.NewReader([]byte("content"))}); err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}
	_, err := client.DeleteBucket(context.Background(), &s3.DeleteBucketInput{Bucket: aws.String("non-empty")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "BucketNotEmpty" {
		t.Fatalf("DeleteBucket(non-empty) error = %v, want BucketNotEmpty", err)
	}
	if _, err := client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String("non-empty"), Key: aws.String("object")}); err != nil {
		t.Fatalf("DeleteObject() error = %v", err)
	}
	if _, err := client.DeleteBucket(context.Background(), &s3.DeleteBucketInput{Bucket: aws.String("non-empty")}); err != nil {
		t.Fatalf("DeleteBucket(empty) error = %v", err)
	}
}

func TestS3CompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	runAWS := func(args ...string) []byte {
		t.Helper()
		base := []string{"--endpoint-url", server.URL, "--region", "us-east-1", "--output", "json", "--no-cli-pager"}
		cmd := exec.Command("aws", append(base, args...)...)
		cmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID=homeport",
			"AWS_SECRET_ACCESS_KEY=homeport",
			"AWS_EC2_METADATA_DISABLED=true",
			"AWS_REQUEST_CHECKSUM_CALCULATION=when_required",
			"AWS_RESPONSE_CHECKSUM_VALIDATION=when_required",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("aws %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}

	dir := t.TempDir()
	source := filepath.Join(dir, "hello.txt")
	target := filepath.Join(dir, "downloaded.txt")
	if err := os.WriteFile(source, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	runAWS("s3api", "create-bucket", "--bucket", "cli-bucket")
	runAWS("s3api", "put-object", "--bucket", "cli-bucket", "--key", "hello.txt", "--body", source)
	runAWS("s3api", "get-object", "--bucket", "cli-bucket", "--key", "hello.txt", target)

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read downloaded object: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("downloaded object = %q, want hello", got)
	}

	runAWS("s3api", "delete-object", "--bucket", "cli-bucket", "--key", "hello.txt")
}

func TestS3CompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewS3Adapter())
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
  s3_use_path_style           = true

  endpoints {
    s3 = %q
  }
}

resource "aws_s3_bucket" "deploy" {
  bucket        = "terraform-bucket"
  force_destroy = true
}

output "bucket_id" {
  value = aws_s3_bucket.deploy.id
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
			"AWS_REQUEST_CHECKSUM_CALCULATION=when_required",
			"AWS_RESPONSE_CHECKSUM_VALIDATION=when_required",
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

	out := runTerraform("output", "-raw", "bucket_id")
	if strings.TrimSpace(string(out)) != "terraform-bucket" {
		t.Fatalf("terraform output bucket_id = %q, want terraform-bucket", strings.TrimSpace(string(out)))
	}
}

func TestS3CompatibilityAdapterReturnsNoSuchKeyWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	_, err := client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("missing.txt"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NoSuchKey" {
		t.Fatalf("GetObject(missing) error = %v, want NoSuchKey", err)
	}
}

func TestS3CompatibilityAdapterReturnsConflictForDuplicateBucket(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket(first) error = %v", err)
	}
	_, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "BucketAlreadyOwnedByYou" {
		t.Fatalf("CreateBucket(duplicate) error = %v, want BucketAlreadyOwnedByYou", err)
	}
}

func TestS3CompatibilityAdapterRejectsInvalidBucketName(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	_, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("Invalid_Bucket")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidBucketName" {
		t.Fatalf("CreateBucket(invalid name) error = %v, want InvalidBucketName", err)
	}
}

func TestS3CompatibilityAdapterReturnsNotImplementedForUnsupportedActions(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL+"/bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unsupported action request error = %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotImplemented || !strings.Contains(string(body), "<Code>NotImplemented</Code>") {
		t.Fatalf("unsupported action response = status %d body %q, want 501 NotImplemented", resp.StatusCode, body)
	}
}

func TestS3CompatibilityAdapterReturnsObjectETags(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	put, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("hello.txt"),
		Body:   strings.NewReader("hello"),
	})
	if err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}
	if aws.ToString(put.ETag) != `"5d41402abc4b2a76b9719d911017c592"` {
		t.Fatalf("PutObject ETag = %q, want MD5 ETag", aws.ToString(put.ETag))
	}

	got, err := client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("hello.txt"),
	})
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	if aws.ToString(got.ETag) != aws.ToString(put.ETag) {
		t.Fatalf("GetObject ETag = %q, want %q", aws.ToString(got.ETag), aws.ToString(put.ETag))
	}
}

func TestS3CompatibilityAdapterReturnsRequestIDHeaderOnSuccessfulCalls(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CreateBucket request error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CreateBucket status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Amz-RequestId"); got != "homeport" {
		t.Fatalf("X-Amz-RequestId = %q, want homeport", got)
	}
}

func TestS3CompatibilityAdapterReplaysIdempotentCreateBucket(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	for i := 0; i < 2; i++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Idempotency-Key", "create-bucket")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("CreateBucket request %d error = %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("CreateBucket request %d status = %d, want 200", i+1, resp.StatusCode)
		}
	}
}

func TestS3CompatibilityAdapterReplaysIdempotentPutObject(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CreateBucket request error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CreateBucket status = %d, want 200", resp.StatusCode)
	}

	var firstETag string
	for i, body := range []string{"first", "second"} {
		req, err = http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket/key.txt", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Idempotency-Key", "put-object")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PutObject request %d error = %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PutObject request %d status = %d, want 200", i+1, resp.StatusCode)
		}
		if i == 0 {
			firstETag = resp.Header.Get("ETag")
		} else if got := resp.Header.Get("ETag"); got != firstETag {
			t.Fatalf("PutObject replay ETag = %q, want %q", got, firstETag)
		}
	}

	resp, err = http.Get(server.URL + "/bucket/key.txt")
	if err != nil {
		t.Fatalf("GetObject request error = %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "first" {
		t.Fatalf("GetObject body = %q, want first", got)
	}
}

func TestS3CompatibilityAdapterReplaysIdempotentPutBucketTagging(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CreateBucket request error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CreateBucket status = %d, want 200", resp.StatusCode)
	}

	for i, body := range []string{
		`<Tagging><TagSet><Tag><Key>env</Key><Value>prod</Value></Tag></TagSet></Tagging>`,
		`<Tagging><TagSet><Tag><Key>env</Key><Value>dev</Value></Tag></TagSet></Tagging>`,
	} {
		req, err = http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket?tagging", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Idempotency-Key", "put-tags")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PutBucketTagging request %d error = %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("PutBucketTagging request %d status = %d, want 200", i+1, resp.StatusCode)
		}
	}

	resp, err = http.Get(server.URL + "/bucket?tagging")
	if err != nil {
		t.Fatalf("GetBucketTagging request error = %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(got), "<Value>prod</Value>") || strings.Contains(string(got), "<Value>dev</Value>") {
		t.Fatalf("GetBucketTagging body = %q, want original prod tags", got)
	}
}

func TestS3CompatibilityAdapterDeleteObjectIsIdempotent(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	for _, req := range []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPut, path: "/bucket"},
		{method: http.MethodPut, path: "/bucket/key.txt", body: "hello"},
	} {
		request, err := http.NewRequestWithContext(context.Background(), req.method, server.URL+req.path, strings.NewReader(req.body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("%s %s request error = %v", req.method, req.path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s %s status = %d, want 200", req.method, req.path, resp.StatusCode)
		}
	}

	for i := 0; i < 2; i++ {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, server.URL+"/bucket/key.txt", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("DeleteObject request %d error = %v", i+1, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("DeleteObject request %d status = %d, want 204", i+1, resp.StatusCode)
		}
	}

	resp, err := http.Get(server.URL + "/bucket/key.txt")
	if err != nil {
		t.Fatalf("GetObject request error = %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "<Code>NoSuchKey</Code>") {
		t.Fatalf("GetObject after deletes = status %d body %q, want 404 NoSuchKey", resp.StatusCode, body)
	}
}

func TestS3CompatibilityAdapterRoundTripsBucketTags(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	if _, err := client.PutBucketTagging(context.Background(), &s3.PutBucketTaggingInput{
		Bucket: aws.String("bucket"),
		Tagging: &s3types.Tagging{TagSet: []s3types.Tag{
			{Key: aws.String("env"), Value: aws.String("dev")},
			{Key: aws.String("owner"), Value: aws.String("homeport")},
		}},
	}); err != nil {
		t.Fatalf("PutBucketTagging() error = %v", err)
	}

	got, err := client.GetBucketTagging(context.Background(), &s3.GetBucketTaggingInput{Bucket: aws.String("bucket")})
	if err != nil {
		t.Fatalf("GetBucketTagging() error = %v", err)
	}
	tags := map[string]string{}
	for _, tag := range got.TagSet {
		tags[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	if tags["env"] != "dev" || tags["owner"] != "homeport" {
		t.Fatalf("GetBucketTagging() = %#v, want env/owner tags", tags)
	}
}

func TestS3CompatibilityAdapterReturnsMalformedXMLForInvalidBucketTags(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CreateBucket request error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CreateBucket status = %d, want 200", resp.StatusCode)
	}

	for _, tags := range []string{"<Tagging>", "<Tagging/>"} {
		req, err = http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket?tagging", strings.NewReader(tags))
		if err != nil {
			t.Fatal(err)
		}
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("PutBucketTagging request error = %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "<Code>MalformedXML</Code>") {
			t.Fatalf("PutBucketTagging(%q) response = status %d body %q, want 400 MalformedXML", tags, resp.StatusCode, body)
		}
	}
}

func TestS3CompatibilityAdapterMapsBackendTimeoutToInternalError(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter(
		compataws.WithS3BackendErrorForMethod(http.MethodPut, errors.New("minio timeout")),
	))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CreateBucket request error = %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(string(body), "<Code>InternalError</Code>") {
		t.Fatalf("CreateBucket backend timeout response = status %d body %q, want 500 InternalError", resp.StatusCode, body)
	}

	req, err = http.NewRequestWithContext(context.Background(), http.MethodHead, server.URL+"/bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("HeadBucket request error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("HeadBucket status = %d, want 404 after failed create", resp.StatusCode)
	}
}

func TestS3CompatibilityAdapterReturnsNoSuchBucketForMissingBucketAcrossSupportedCalls(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "ListObjectsV2", method: http.MethodGet, path: "/missing"},
		{name: "GetBucketTagging", method: http.MethodGet, path: "/missing?tagging"},
		{name: "PutBucketTagging", method: http.MethodPut, path: "/missing?tagging", body: "<Tagging/>"},
		{name: "PutObject", method: http.MethodPut, path: "/missing/key.txt", body: "hello"},
		{name: "GetObject", method: http.MethodGet, path: "/missing/key.txt"},
		{name: "DeleteObject", method: http.MethodDelete, path: "/missing/key.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), tc.method, server.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("%s request error = %v", tc.name, err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(body), "<Code>NoSuchBucket</Code>") {
				t.Fatalf("%s response = status %d body %q, want 404 NoSuchBucket", tc.name, resp.StatusCode, body)
			}
		})
	}
}

func TestS3CompatibilityAdapterReturnsNoSuchTagSetForUntaggedBucket(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	_, err := client.GetBucketTagging(context.Background(), &s3.GetBucketTaggingInput{Bucket: aws.String("bucket")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NoSuchTagSet" {
		t.Fatalf("GetBucketTagging(untagged) error = %v, want NoSuchTagSet", err)
	}
}

func TestS3CompatibilityAdapterAuthorizesAndAuditsAWSSDKCalls(t *testing.T) {
	auditLog := authz.NewFileAuditLog(filepath.Join(t.TempDir(), "s3-audit.jsonl"))
	server := httptest.NewServer(compataws.NewS3Adapter(
		compataws.WithS3Authorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"s3:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"s3:GetObject"}, Resources: []string{"*"}},
		)),
		compataws.WithS3AuditSink(func(decision authz.Decision) {
			if err := auditLog.Record(decision); err != nil {
				t.Errorf("Record() error = %v", err)
			}
		}),
	))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	_, err := client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("missing.txt"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("GetObject(denied) error = %v, want AccessDenied", err)
	}

	decisions, err := auditLog.Decisions()
	if err != nil {
		t.Fatalf("Decisions() error = %v", err)
	}
	assertDecision(t, decisions, "s3:CreateBucket", true)
	assertDecision(t, decisions, "s3:GetObject", false)
}

func TestS3CompatibilityAdapterAuthorizesUserAgentCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter(
		compataws.WithS3Authorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"s3:CreateBucket"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "user_agent", Values: []string{"aws-sdk-go-v2*"}},
			},
		})),
	))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
}

func TestS3CompatibilityAdapterAuthorizesRequestIDCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter(
		compataws.WithS3Authorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"s3:CreateBucket"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "request_id", Values: []string{"homeport"}},
			},
		})),
	))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
}

func TestS3CompatibilityAdapterRejectsExpiredCredential(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter(
		compataws.WithS3Authorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"s3:*"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"s3:CreateBucket"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_expired", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Homeport-Credential-Expired", "true")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CreateBucket request error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("CreateBucket status = %d, want 403", resp.StatusCode)
	}
}

func TestS3CompatibilityAdapterAuthorizesClaimCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter(
		compataws.WithS3Authorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"s3:*"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"s3:CreateBucket"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "claim:mfa", Values: []string{"false"}},
				},
			},
		)),
	))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Homeport-Claim-Mfa", "false")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CreateBucket request error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("CreateBucket status = %d, want 403", resp.StatusCode)
	}
}

func TestS3CompatibilityAdapterAuthorizesCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter(
		compataws.WithS3Authorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"s3:*"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"s3:CreateBucket"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_age", Values: []string{"48h"}},
				},
			},
		)),
	))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/bucket", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Homeport-Credential-Age", "48h")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("CreateBucket request error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("CreateBucket status = %d, want 403", resp.StatusCode)
	}
}

func TestS3CompatibilityAdapterAuthorizesRegionCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter(
		compataws.WithS3Authorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"s3:CreateBucket"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "region", Values: []string{"us-east-1"}},
			},
		})),
	))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
}

func TestS3CompatibilityAdapterAuthorizesCurrentTimeCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter(
		compataws.WithS3Authorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"s3:CreateBucket"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "current_time", Values: []string{"2000-01-01T00:00:00Z/2100-01-01T00:00:00Z"}},
			},
		})),
	))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
}

func TestS3CompatibilityAdapterAuthorizesBucketTagCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter(
		compataws.WithS3Authorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"s3:CreateBucket", "s3:PutBucketTagging", "s3:PutObject"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"s3:GetObject"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "tag:env", Values: []string{"dev"}},
				},
			},
		)),
	))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	if _, err := client.PutBucketTagging(context.Background(), &s3.PutBucketTaggingInput{
		Bucket: aws.String("bucket"),
		Tagging: &s3types.Tagging{TagSet: []s3types.Tag{
			{Key: aws.String("env"), Value: aws.String("dev")},
		}},
	}); err != nil {
		t.Fatalf("PutBucketTagging() error = %v", err)
	}
	if _, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("hello.txt"),
		Body:   strings.NewReader("hello"),
	}); err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}
	if _, err := client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("hello.txt"),
	}); err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
}

func TestS3CompatibilityAdapterReturnsSlowDownWhenObjectQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter(compataws.WithS3ObjectQuota(1)))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	if _, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("one.txt"),
		Body:   bytes.NewReader([]byte("one")),
	}); err != nil {
		t.Fatalf("PutObject(first) error = %v", err)
	}
	if _, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("one.txt"),
		Body:   bytes.NewReader([]byte("one again")),
	}); err != nil {
		t.Fatalf("PutObject(overwrite) error = %v", err)
	}

	_, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String("two.txt"),
		Body:   bytes.NewReader([]byte("two")),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "SlowDown" {
		t.Fatalf("PutObject(second new object) error = %v, want SlowDown", err)
	}
}

func TestS3CompatibilityAdapterPaginatesListObjectsV2(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	for _, key := range []string{"a.txt", "b.txt"} {
		if _, err := client.PutObject(context.Background(), &s3.PutObjectInput{
			Bucket: aws.String("bucket"),
			Key:    aws.String(key),
			Body:   strings.NewReader(key),
		}); err != nil {
			t.Fatalf("PutObject(%s) error = %v", key, err)
		}
	}

	first, err := client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
		Bucket:  aws.String("bucket"),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		t.Fatalf("ListObjectsV2(first) error = %v", err)
	}
	if len(first.Contents) != 1 || *first.Contents[0].Key != "a.txt" || !*first.IsTruncated || first.NextContinuationToken == nil {
		t.Fatalf("ListObjectsV2(first) = %#v, want first page with token", first)
	}

	second, err := client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
		Bucket:            aws.String("bucket"),
		MaxKeys:           aws.Int32(1),
		ContinuationToken: first.NextContinuationToken,
	})
	if err != nil {
		t.Fatalf("ListObjectsV2(second) error = %v", err)
	}
	if len(second.Contents) != 1 || *second.Contents[0].Key != "b.txt" || *second.IsTruncated {
		t.Fatalf("ListObjectsV2(second) = %#v, want final page", second)
	}
}

func TestS3CompatibilityAdapterEscapesListObjectsV2Keys(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}
	key := `folder/a&b<"c'>.txt`
	if _, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String("bucket"),
		Key:    aws.String(key),
		Body:   strings.NewReader("hello"),
	}); err != nil {
		t.Fatalf("PutObject() error = %v", err)
	}

	got, err := client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{Bucket: aws.String("bucket")})
	if err != nil {
		t.Fatalf("ListObjectsV2() error = %v", err)
	}
	if len(got.Contents) != 1 || aws.ToString(got.Contents[0].Key) != key {
		t.Fatalf("ListObjectsV2 key = %q, want %q", aws.ToString(got.Contents[0].Key), key)
	}
}

func TestS3CompatibilityAdapterRejectsInvalidListObjectsV2Token(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}

	_, err := client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
		Bucket:            aws.String("bucket"),
		ContinuationToken: aws.String("not-a-token"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidRequest" {
		t.Fatalf("ListObjectsV2(invalid token) error = %v, want InvalidRequest", err)
	}
}

func TestS3CompatibilityAdapterRejectsInvalidListObjectsV2MaxKeys(t *testing.T) {
	server := httptest.NewServer(compataws.NewS3Adapter())
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.UsePathStyle = true
	})

	if _, err := client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("bucket")}); err != nil {
		t.Fatalf("CreateBucket() error = %v", err)
	}

	_, err := client.ListObjectsV2(context.Background(), &s3.ListObjectsV2Input{
		Bucket:  aws.String("bucket"),
		MaxKeys: aws.Int32(-1),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidRequest" {
		t.Fatalf("ListObjectsV2(invalid max keys) error = %v, want InvalidRequest", err)
	}
}
