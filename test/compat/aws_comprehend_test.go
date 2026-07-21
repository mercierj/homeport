package compat_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/comprehend"
	comprehendtypes "github.com/aws/aws-sdk-go-v2/service/comprehend/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestComprehendCompatibilityAdapterLifecycleWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewComprehendAdapter())
	defer server.Close()
	client := comprehend.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(options *comprehend.Options) {
		options.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()

	created, err := client.CreateDocumentClassifier(ctx, &comprehend.CreateDocumentClassifierInput{
		DocumentClassifierName: aws.String("support-classifier"),
		DataAccessRoleArn:      aws.String("arn:aws:iam::000000000000:role/comprehend"),
		InputDataConfig:        &comprehendtypes.DocumentClassifierInputDataConfig{S3Uri: aws.String("s3://training/support.csv")},
		LanguageCode:           comprehendtypes.LanguageCodeEn,
	})
	if err != nil || aws.ToString(created.DocumentClassifierArn) == "" {
		t.Fatalf("CreateDocumentClassifier() = %#v, %v; want classifier ARN", created, err)
	}

	described, err := client.DescribeDocumentClassifier(ctx, &comprehend.DescribeDocumentClassifierInput{DocumentClassifierArn: created.DocumentClassifierArn})
	if err != nil || aws.ToString(described.DocumentClassifierProperties.DocumentClassifierArn) != aws.ToString(created.DocumentClassifierArn) {
		t.Fatalf("DescribeDocumentClassifier() = %#v, %v; want created classifier", described, err)
	}
	listed, err := client.ListDocumentClassifiers(ctx, &comprehend.ListDocumentClassifiersInput{})
	if err != nil || len(listed.DocumentClassifierPropertiesList) != 1 || aws.ToString(listed.DocumentClassifierPropertiesList[0].DocumentClassifierArn) != aws.ToString(created.DocumentClassifierArn) {
		t.Fatalf("ListDocumentClassifiers() = %#v, %v; want created classifier", listed, err)
	}
	if _, err := client.DeleteDocumentClassifier(ctx, &comprehend.DeleteDocumentClassifierInput{DocumentClassifierArn: created.DocumentClassifierArn}); err != nil {
		t.Fatalf("DeleteDocumentClassifier() error = %v", err)
	}
}

func TestComprehendCompatibilityAdapterPaginatesClassifiersWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewComprehendAdapter())
	defer server.Close()
	client := comprehend.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(options *comprehend.Options) { options.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	for _, name := range []string{"alpha", "bravo"} {
		if _, err := client.CreateDocumentClassifier(ctx, comprehendCreateInput(name)); err != nil {
			t.Fatalf("CreateDocumentClassifier(%s) error = %v", name, err)
		}
	}
	first, err := client.ListDocumentClassifiers(ctx, &comprehend.ListDocumentClassifiersInput{MaxResults: aws.Int32(1)})
	if err != nil || len(first.DocumentClassifierPropertiesList) != 1 || !strings.Contains(aws.ToString(first.DocumentClassifierPropertiesList[0].DocumentClassifierArn), "/alpha/") || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListDocumentClassifiers(first) = %#v, %v; want alpha and token", first, err)
	}
	second, err := client.ListDocumentClassifiers(ctx, &comprehend.ListDocumentClassifiersInput{MaxResults: aws.Int32(1), NextToken: first.NextToken})
	if err != nil || len(second.DocumentClassifierPropertiesList) != 1 || !strings.Contains(aws.ToString(second.DocumentClassifierPropertiesList[0].DocumentClassifierArn), "/bravo/") || second.NextToken != nil {
		t.Fatalf("ListDocumentClassifiers(second) = %#v, %v; want bravo without token", second, err)
	}
}

func comprehendCreateInput(name string) *comprehend.CreateDocumentClassifierInput {
	return &comprehend.CreateDocumentClassifierInput{DocumentClassifierName: aws.String(name), DataAccessRoleArn: aws.String("arn:aws:iam::000000000000:role/comprehend"), InputDataConfig: &comprehendtypes.DocumentClassifierInputDataConfig{S3Uri: aws.String("s3://training/support.csv")}, LanguageCode: comprehendtypes.LanguageCodeEn}
}

func TestComprehendCompatibilityAdapterAuthorizesAndAuditsCreate(t *testing.T) {
	decisions := []authz.Decision{}
	server := httptest.NewServer(compataws.NewComprehendAdapter(
		compataws.WithComprehendAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{Effect: authz.Deny, Actions: []string{"comprehend:CreateDocumentClassifier"}, Resources: []string{"*"}})),
		compataws.WithComprehendAuditSink(func(d authz.Decision) { decisions = append(decisions, d) }),
	))
	defer server.Close()
	client := comprehend.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *comprehend.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.CreateDocumentClassifier(context.Background(), comprehendCreateInput("denied"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDeniedException" || len(decisions) != 1 || decisions[0].Allowed {
		t.Fatalf("CreateDocumentClassifier(denied) = %v, decisions=%#v", err, decisions)
	}
}

func TestComprehendCompatibilityAdapterRejectsEmptyClassifierName(t *testing.T) {
	server := httptest.NewServer(compataws.NewComprehendAdapter())
	defer server.Close()
	client := comprehend.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *comprehend.Options) { o.BaseEndpoint = aws.String(server.URL) })

	_, err := client.CreateDocumentClassifier(context.Background(), comprehendCreateInput(""))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidRequestException" {
		t.Fatalf("CreateDocumentClassifier(empty name) = %v; want InvalidRequestException", err)
	}
}

func TestComprehendCompatibilityAdapterEnforcesClassifierQuota(t *testing.T) {
	server := httptest.NewServer(compataws.NewComprehendAdapter(compataws.WithComprehendQuota(1)))
	defer server.Close()
	client := comprehend.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *comprehend.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateDocumentClassifier(ctx, comprehendCreateInput("first")); err != nil {
		t.Fatalf("CreateDocumentClassifier(first) error = %v", err)
	}

	_, err := client.CreateDocumentClassifier(ctx, comprehendCreateInput("second"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceLimitExceededException" {
		t.Fatalf("CreateDocumentClassifier(second) = %v; want ResourceLimitExceededException", err)
	}
}

func TestComprehendCompatibilityAdapterRejectsDuplicateClassifierName(t *testing.T) {
	server := httptest.NewServer(compataws.NewComprehendAdapter())
	defer server.Close()
	client := comprehend.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *comprehend.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateDocumentClassifier(ctx, comprehendCreateInput("duplicate")); err != nil {
		t.Fatalf("CreateDocumentClassifier(first) error = %v", err)
	}

	_, err := client.CreateDocumentClassifier(ctx, comprehendCreateInput("duplicate"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceInUseException" {
		t.Fatalf("CreateDocumentClassifier(duplicate) = %v; want ResourceInUseException", err)
	}
}

func TestComprehendCompatibilityAdapterAuthorizesCreateOnClassifierParentARN(t *testing.T) {
	server := httptest.NewServer(compataws.NewComprehendAdapter(compataws.WithComprehendAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{Effect: authz.Allow, Actions: []string{"comprehend:CreateDocumentClassifier"}, Resources: []string{"arn:aws:comprehend:us-east-1:000000000000:document-classifier/scoped"}}))))
	defer server.Close()
	client := comprehend.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *comprehend.Options) { o.BaseEndpoint = aws.String(server.URL) })

	if _, err := client.CreateDocumentClassifier(context.Background(), comprehendCreateInput("scoped")); err != nil {
		t.Fatalf("CreateDocumentClassifier(scoped) error = %v; want authorization on the classifier parent ARN", err)
	}
}
