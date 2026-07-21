package compat_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestECRCompatibilityAdapterRepositoryLifecycleWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECRAdapter())
	defer server.Close()
	client := ecr.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ecr.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateRepository(context.Background(), &ecr.CreateRepositoryInput{RepositoryName: aws.String("orders")})
	if err != nil || aws.ToString(created.Repository.RepositoryName) != "orders" {
		t.Fatalf("CreateRepository() = %#v, %v; want orders repository", created, err)
	}
	described, err := client.DescribeRepositories(context.Background(), &ecr.DescribeRepositoriesInput{RepositoryNames: []string{"orders"}})
	if err != nil || len(described.Repositories) != 1 || aws.ToString(described.Repositories[0].RepositoryName) != "orders" {
		t.Fatalf("DescribeRepositories() = %#v, %v; want orders repository", described, err)
	}
	images, err := client.ListImages(context.Background(), &ecr.ListImagesInput{RepositoryName: aws.String("orders")})
	if err != nil || len(images.ImageIds) != 0 {
		t.Fatalf("ListImages() = %#v, %v; want empty image list", images, err)
	}
	deleted, err := client.DeleteRepository(context.Background(), &ecr.DeleteRepositoryInput{RepositoryName: aws.String("orders")})
	if err != nil || aws.ToString(deleted.Repository.RepositoryName) != "orders" {
		t.Fatalf("DeleteRepository() = %#v, %v; want deleted orders repository", deleted, err)
	}
}

func TestECRCompatibilityAdapterPaginatesRepositories(t *testing.T) {
	server := httptest.NewServer(compataws.NewECRAdapter())
	defer server.Close()
	client := ecr.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ecr.Options) { o.BaseEndpoint = aws.String(server.URL) })
	for _, name := range []string{"alpha", "bravo"} {
		if _, err := client.CreateRepository(context.Background(), &ecr.CreateRepositoryInput{RepositoryName: aws.String(name)}); err != nil {
			t.Fatalf("CreateRepository(%s) error = %v", name, err)
		}
	}
	first, err := client.DescribeRepositories(context.Background(), &ecr.DescribeRepositoriesInput{MaxResults: aws.Int32(1)})
	if err != nil || len(first.Repositories) != 1 || aws.ToString(first.NextToken) == "" {
		t.Fatalf("DescribeRepositories(first) = %#v, %v; want one repository and token", first, err)
	}
	second, err := client.DescribeRepositories(context.Background(), &ecr.DescribeRepositoriesInput{MaxResults: aws.Int32(1), NextToken: first.NextToken})
	if err != nil || len(second.Repositories) != 1 || second.NextToken != nil {
		t.Fatalf("DescribeRepositories(second) = %#v, %v; want final repository", second, err)
	}
}

func TestECRCompatibilityAdapterAuthorizesRepositoryCreation(t *testing.T) {
	server := httptest.NewServer(compataws.NewECRAdapter(compataws.WithECRAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{Effect: authz.Deny, Actions: []string{"ecr:CreateRepository"}, Resources: []string{"*"}}))))
	defer server.Close()
	client := ecr.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ecr.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.CreateRepository(context.Background(), &ecr.CreateRepositoryInput{RepositoryName: aws.String("denied")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDeniedException" {
		t.Fatalf("CreateRepository(denied) error = %v, want AccessDeniedException", err)
	}
}

func TestECRCompatibilityAdapterAuthorizesDescribeForNamedRepository(t *testing.T) {
	ordersARN := "arn:aws:ecr:us-east-1:000000000000:repository/orders"
	server := httptest.NewServer(compataws.NewECRAdapter(compataws.WithECRAuthorizer(authz.NewPolicyAuthorizer(
		authz.Rule{Effect: authz.Allow, Actions: []string{"ecr:CreateRepository"}, Resources: []string{"*"}},
		authz.Rule{Effect: authz.Allow, Actions: []string{"ecr:DescribeRepositories"}, Resources: []string{ordersARN}},
	))))
	defer server.Close()
	client := ecr.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ecr.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateRepository(context.Background(), &ecr.CreateRepositoryInput{RepositoryName: aws.String("orders")}); err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	described, err := client.DescribeRepositories(context.Background(), &ecr.DescribeRepositoriesInput{RepositoryNames: []string{"orders"}})
	if err != nil || len(described.Repositories) != 1 || aws.ToString(described.Repositories[0].RepositoryName) != "orders" {
		t.Fatalf("DescribeRepositories() = %#v, %v; want authorized orders repository", described, err)
	}
}

func TestECRCompatibilityAdapterRejectsRepositoryOverQuota(t *testing.T) {
	server := httptest.NewServer(compataws.NewECRAdapter(compataws.WithECRRepositoryQuota(1)))
	defer server.Close()
	client := ecr.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ecr.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateRepository(context.Background(), &ecr.CreateRepositoryInput{RepositoryName: aws.String("first")}); err != nil {
		t.Fatalf("CreateRepository(first) error = %v", err)
	}
	_, err := client.CreateRepository(context.Background(), &ecr.CreateRepositoryInput{RepositoryName: aws.String("second")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("CreateRepository(second) error = %v, want LimitExceededException", err)
	}
}

func TestECRCompatibilityAdapterRejectsOutOfBoundsRepositoryNames(t *testing.T) {
	server := httptest.NewServer(compataws.NewECRAdapter())
	defer server.Close()
	client := ecr.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ecr.Options) { o.BaseEndpoint = aws.String(server.URL) })
	for _, name := range []string{"a", strings.Repeat("a", 257)} {
		_, err := client.CreateRepository(context.Background(), &ecr.CreateRepositoryInput{RepositoryName: aws.String(name)})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
			t.Fatalf("CreateRepository(%q) error = %v, want InvalidParameterException", name, err)
		}
	}
}

func TestECRCompatibilityAdapterManagesRepositoryTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECRAdapter())
	defer server.Close()
	client := ecr.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ecr.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateRepository(context.Background(), &ecr.CreateRepositoryInput{
		RepositoryName: aws.String("orders"),
		Tags:           []types.Tag{{Key: aws.String("team"), Value: aws.String("platform")}},
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	tags, err := client.ListTagsForResource(context.Background(), &ecr.ListTagsForResourceInput{ResourceArn: created.Repository.RepositoryArn})
	if err != nil || len(tags.Tags) != 1 || aws.ToString(tags.Tags[0].Key) != "team" || aws.ToString(tags.Tags[0].Value) != "platform" {
		t.Fatalf("ListTagsForResource() = %#v, %v; want create-time tag", tags, err)
	}
	if _, err := client.TagResource(context.Background(), &ecr.TagResourceInput{ResourceArn: created.Repository.RepositoryArn, Tags: []types.Tag{{Key: aws.String("environment"), Value: aws.String("test")}}}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	if _, err := client.UntagResource(context.Background(), &ecr.UntagResourceInput{ResourceArn: created.Repository.RepositoryArn, TagKeys: []string{"team"}}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	tags, err = client.ListTagsForResource(context.Background(), &ecr.ListTagsForResourceInput{ResourceArn: created.Repository.RepositoryArn})
	if err != nil || len(tags.Tags) != 1 || aws.ToString(tags.Tags[0].Key) != "environment" || aws.ToString(tags.Tags[0].Value) != "test" {
		t.Fatalf("ListTagsForResource(after update) = %#v, %v; want remaining update tag", tags, err)
	}
}

func TestECRCompatibilityAdapterManagesImageManifestMetadata(t *testing.T) {
	server := httptest.NewServer(compataws.NewECRAdapter())
	defer server.Close()
	client := ecr.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ecr.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateRepository(ctx, &ecr.CreateRepositoryInput{RepositoryName: aws.String("orders")}); err != nil {
		t.Fatal(err)
	}
	put, err := client.PutImage(ctx, &ecr.PutImageInput{RepositoryName: aws.String("orders"), ImageTag: aws.String("v1"), ImageManifest: aws.String(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)})
	if err != nil || put.Image == nil || put.Image.ImageId == nil || aws.ToString(put.Image.ImageId.ImageDigest) == "" || aws.ToString(put.Image.ImageId.ImageTag) != "v1" {
		t.Fatalf("PutImage() = %#v, %v", put, err)
	}
	listed, err := client.ListImages(ctx, &ecr.ListImagesInput{RepositoryName: aws.String("orders")})
	if err != nil || len(listed.ImageIds) != 1 || aws.ToString(listed.ImageIds[0].ImageTag) != "v1" || aws.ToString(listed.ImageIds[0].ImageDigest) != aws.ToString(put.Image.ImageId.ImageDigest) {
		t.Fatalf("ListImages() = %#v, %v", listed, err)
	}
	described, err := client.DescribeImages(ctx, &ecr.DescribeImagesInput{RepositoryName: aws.String("orders"), ImageIds: []types.ImageIdentifier{{ImageTag: aws.String("v1")}}})
	if err != nil || len(described.ImageDetails) != 1 || aws.ToString(described.ImageDetails[0].ImageDigest) != aws.ToString(put.Image.ImageId.ImageDigest) {
		t.Fatalf("DescribeImages() = %#v, %v", described, err)
	}
	fetched, err := client.BatchGetImage(ctx, &ecr.BatchGetImageInput{RepositoryName: aws.String("orders"), ImageIds: []types.ImageIdentifier{{ImageTag: aws.String("v1")}}})
	if err != nil || len(fetched.Images) != 1 || aws.ToString(fetched.Images[0].ImageManifest) != `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}` {
		t.Fatalf("BatchGetImage() = %#v, %v", fetched, err)
	}
	deleted, err := client.BatchDeleteImage(ctx, &ecr.BatchDeleteImageInput{RepositoryName: aws.String("orders"), ImageIds: []types.ImageIdentifier{{ImageTag: aws.String("v1")}}})
	if err != nil || len(deleted.ImageIds) != 1 || aws.ToString(deleted.ImageIds[0].ImageTag) != "v1" {
		t.Fatalf("BatchDeleteImage() = %#v, %v", deleted, err)
	}
	listed, err = client.ListImages(ctx, &ecr.ListImagesInput{RepositoryName: aws.String("orders")})
	if err != nil || len(listed.ImageIds) != 0 {
		t.Fatalf("ListImages(after delete) = %#v, %v", listed, err)
	}
}

func TestECRCompatibilityAdapterPaginatesImages(t *testing.T) {
	server := httptest.NewServer(compataws.NewECRAdapter())
	defer server.Close()
	client := ecr.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ecr.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateRepository(ctx, &ecr.CreateRepositoryInput{RepositoryName: aws.String("orders")}); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"alpha", "bravo"} {
		if _, err := client.PutImage(ctx, &ecr.PutImageInput{RepositoryName: aws.String("orders"), ImageTag: aws.String(tag), ImageManifest: aws.String(`{"schemaVersion":2}`)}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := client.ListImages(ctx, &ecr.ListImagesInput{RepositoryName: aws.String("orders"), MaxResults: aws.Int32(1)})
	if err != nil || len(first.ImageIds) != 1 || aws.ToString(first.ImageIds[0].ImageTag) != "alpha" || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListImages(first page) = %#v, %v", first, err)
	}
	second, err := client.ListImages(ctx, &ecr.ListImagesInput{RepositoryName: aws.String("orders"), MaxResults: aws.Int32(1), NextToken: first.NextToken})
	if err != nil || len(second.ImageIds) != 1 || aws.ToString(second.ImageIds[0].ImageTag) != "bravo" || second.NextToken != nil {
		t.Fatalf("ListImages(second page) = %#v, %v", second, err)
	}
}
