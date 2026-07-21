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
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	"github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestAPIGatewayCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{
		Name:        aws.String("orders-api"),
		Description: aws.String("orders"),
	})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	if aws.ToString(created.Id) == "" || aws.ToString(created.Name) != "orders-api" || aws.ToString(created.Description) != "orders" {
		t.Fatalf("CreateRestApi() = %#v, want orders-api", created)
	}

	got, err := client.GetRestApi(context.Background(), &apigateway.GetRestApiInput{RestApiId: created.Id})
	if err != nil {
		t.Fatalf("GetRestApi() error = %v", err)
	}
	if aws.ToString(got.Id) != aws.ToString(created.Id) || aws.ToString(got.Name) != "orders-api" {
		t.Fatalf("GetRestApi() = %#v, want created API", got)
	}

	listed, err := client.GetRestApis(context.Background(), &apigateway.GetRestApisInput{})
	if err != nil {
		t.Fatalf("GetRestApis() error = %v", err)
	}
	if len(listed.Items) != 1 || aws.ToString(listed.Items[0].Id) != aws.ToString(created.Id) {
		t.Fatalf("GetRestApis() = %#v, want created API", listed.Items)
	}

	updated, err := client.UpdateRestApi(context.Background(), &apigateway.UpdateRestApiInput{
		RestApiId: created.Id,
		PatchOperations: []types.PatchOperation{{
			Op:    types.OpReplace,
			Path:  aws.String("/name"),
			Value: aws.String("orders-api-v2"),
		}},
	})
	if err != nil {
		t.Fatalf("UpdateRestApi() error = %v", err)
	}
	if aws.ToString(updated.Name) != "orders-api-v2" {
		t.Fatalf("UpdateRestApi() = %#v, want renamed API", updated)
	}

	if _, err := client.DeleteRestApi(context.Background(), &apigateway.DeleteRestApiInput{RestApiId: created.Id}); err != nil {
		t.Fatalf("DeleteRestApi() error = %v", err)
	}
	listed, err = client.GetRestApis(context.Background(), &apigateway.GetRestApisInput{})
	if err != nil {
		t.Fatalf("GetRestApis(after delete) error = %v", err)
	}
	if len(listed.Items) != 0 {
		t.Fatalf("GetRestApis(after delete) = %#v, want no APIs", listed.Items)
	}
}

func TestAPIGatewayCompatibilityAdapterReturnsLimitExceededWhenRestAPIQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter(compataws.WithAPIGatewayRestAPIQuota(1)))
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")}); err != nil {
		t.Fatalf("CreateRestApi(first) error = %v", err)
	}
	_, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("billing-api")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("CreateRestApi(over quota) error = %v, want LimitExceededException", err)
	}

	listed, err := client.GetRestApis(context.Background(), &apigateway.GetRestApisInput{})
	if err != nil {
		t.Fatalf("GetRestApis() error = %v", err)
	}
	if len(listed.Items) != 1 || aws.ToString(listed.Items[0].Name) != "orders-api" {
		t.Fatalf("GetRestApis() = %#v, want only first API after over-quota create", listed.Items)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesNamedCreateRestApiWithAWSSDK(t *testing.T) {
	apiARN := "arn:aws:apigateway:us-east-1::/restapis/homeport-api-1"
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter(compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi"}, Resources: []string{apiARN}}))))
	defer server.Close()
	client := apigateway.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *apigateway.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil || aws.ToString(created.Id) != "homeport-api-1" {
		t.Fatalf("CreateRestApi() = %#v, %v", created, err)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsCreateRestApiWithAWSSDK(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
		compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:GetRestApis"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:CreateRestApi"}, Resources: []string{"*"}},
		)),
		compataws.WithAPIGatewayAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("denied-api")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("CreateRestApi(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "apigateway:CreateRestApi", false)

	listed, err := client.GetRestApis(context.Background(), &apigateway.GetRestApisInput{})
	if err != nil {
		t.Fatalf("GetRestApis() error = %v", err)
	}
	if len(listed.Items) != 0 {
		t.Fatalf("GetRestApis() = %#v, want no APIs after denied create", listed.Items)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsDeleteRestApiWithAWSSDK(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
		compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi", "apigateway:GetRestApi"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:DeleteRestApi"}, Resources: []string{"*"}},
		)),
		compataws.WithAPIGatewayAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	_, err = client.DeleteRestApi(context.Background(), &apigateway.DeleteRestApiInput{RestApiId: created.Id})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("DeleteRestApi(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "apigateway:DeleteRestApi", false)

	got, err := client.GetRestApi(context.Background(), &apigateway.GetRestApiInput{RestApiId: created.Id})
	if err != nil {
		t.Fatalf("GetRestApi(after denied delete) error = %v", err)
	}
	if aws.ToString(got.Name) != "orders-api" {
		t.Fatalf("GetRestApi(after denied delete) = %#v, want original API", got)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsUpdateRestApiWithAWSSDK(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
		compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi", "apigateway:GetRestApi"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:UpdateRestApi"}, Resources: []string{"*"}},
		)),
		compataws.WithAPIGatewayAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	_, err = client.UpdateRestApi(context.Background(), &apigateway.UpdateRestApiInput{
		RestApiId: created.Id,
		PatchOperations: []types.PatchOperation{{
			Op:    types.OpReplace,
			Path:  aws.String("/name"),
			Value: aws.String("orders-api-v2"),
		}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("UpdateRestApi(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "apigateway:UpdateRestApi", false)

	got, err := client.GetRestApi(context.Background(), &apigateway.GetRestApiInput{RestApiId: created.Id})
	if err != nil {
		t.Fatalf("GetRestApi(after denied update) error = %v", err)
	}
	if aws.ToString(got.Name) != "orders-api" {
		t.Fatalf("GetRestApi(after denied update) = %#v, want original API", got)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsGetRestApiWithAWSSDK(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
		compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:GetRestApi"}, Resources: []string{"*"}},
		)),
		compataws.WithAPIGatewayAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	_, err = client.GetRestApi(context.Background(), &apigateway.GetRestApiInput{RestApiId: created.Id})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("GetRestApi(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "apigateway:GetRestApi", false)
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsGetRestApisWithAWSSDK(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
		compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:GetRestApis"}, Resources: []string{"*"}},
		)),
		compataws.WithAPIGatewayAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")}); err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	_, err := client.GetRestApis(context.Background(), &apigateway.GetRestApisInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("GetRestApis(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "apigateway:GetRestApis", false)
}

func TestAPIGatewayCompatibilityAdapterPaginatesRestAPIsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, name := range []string{"orders-api", "billing-api", "fulfillment-api"} {
		if _, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String(name)}); err != nil {
			t.Fatalf("CreateRestApi(%s) error = %v", name, err)
		}
	}

	first, err := client.GetRestApis(context.Background(), &apigateway.GetRestApisInput{Limit: aws.Int32(2)})
	if err != nil {
		t.Fatalf("GetRestApis(first page) error = %v", err)
	}
	if len(first.Items) != 2 || aws.ToString(first.Position) == "" {
		t.Fatalf("GetRestApis(first page) = %#v position %q, want 2 items and next position", first.Items, aws.ToString(first.Position))
	}

	second, err := client.GetRestApis(context.Background(), &apigateway.GetRestApisInput{Limit: aws.Int32(2), Position: first.Position})
	if err != nil {
		t.Fatalf("GetRestApis(second page) error = %v", err)
	}
	if len(second.Items) != 1 || aws.ToString(second.Position) != "" {
		t.Fatalf("GetRestApis(second page) = %#v position %q, want final item and no next position", second.Items, aws.ToString(second.Position))
	}
}

func TestAPIGatewayCompatibilityAdapterTagsRestAPIsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{
		Name: aws.String("orders-api"),
		Tags: map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	arn := aws.String(fmt.Sprintf("arn:aws:apigateway:us-east-1::/restapis/%s", aws.ToString(created.Id)))

	tags, err := client.GetTags(context.Background(), &apigateway.GetTagsInput{ResourceArn: arn})
	if err != nil {
		t.Fatalf("GetTags() error = %v", err)
	}
	if tags.Tags["env"] != "test" {
		t.Fatalf("GetTags() = %#v, want env=test", tags.Tags)
	}

	if _, err := client.TagResource(context.Background(), &apigateway.TagResourceInput{
		ResourceArn: arn,
		Tags:        map[string]string{"owner": "platform"},
	}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	tags, err = client.GetTags(context.Background(), &apigateway.GetTagsInput{ResourceArn: arn})
	if err != nil {
		t.Fatalf("GetTags(after tag) error = %v", err)
	}
	if tags.Tags["env"] != "test" || tags.Tags["owner"] != "platform" {
		t.Fatalf("GetTags(after tag) = %#v, want merged env/owner tags", tags.Tags)
	}

	if _, err := client.UntagResource(context.Background(), &apigateway.UntagResourceInput{
		ResourceArn: arn,
		TagKeys:     []string{"env"},
	}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	tags, err = client.GetTags(context.Background(), &apigateway.GetTagsInput{ResourceArn: arn})
	if err != nil {
		t.Fatalf("GetTags(after untag) error = %v", err)
	}
	if _, ok := tags.Tags["env"]; ok || tags.Tags["owner"] != "platform" {
		t.Fatalf("GetTags(after untag) = %#v, want owner only", tags.Tags)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsTagOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, extraAllowed ...string) (*apigateway.Client, *authz.AuditLog, *string) {
		t.Helper()
		allowed := append([]string{"apigateway:CreateRestApi"}, extraAllowed...)
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{
			Name: aws.String("orders-api"),
			Tags: map[string]string{"env": "test"},
		})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		arn := aws.String(fmt.Sprintf("arn:aws:apigateway:us-east-1::/restapis/%s", aws.ToString(api.Id)))
		return client, auditLog, arn
	}

	t.Run("GetTags", func(t *testing.T) {
		client, auditLog, arn := setup(t, "apigateway:GetTags")
		_, err := client.GetTags(context.Background(), &apigateway.GetTagsInput{ResourceArn: arn})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetTags(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetTags", false)
	})

	t.Run("TagResource", func(t *testing.T) {
		client, auditLog, arn := setup(t, "apigateway:TagResource", "apigateway:GetTags")
		_, err := client.TagResource(context.Background(), &apigateway.TagResourceInput{
			ResourceArn: arn,
			Tags:        map[string]string{"owner": "platform"},
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("TagResource(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:TagResource", false)

		tags, err := client.GetTags(context.Background(), &apigateway.GetTagsInput{ResourceArn: arn})
		if err != nil {
			t.Fatalf("GetTags(after denied tag) error = %v", err)
		}
		if tags.Tags["env"] != "test" || tags.Tags["owner"] != "" {
			t.Fatalf("GetTags(after denied tag) = %#v, want original tags", tags.Tags)
		}
	})

	t.Run("UntagResource", func(t *testing.T) {
		client, auditLog, arn := setup(t, "apigateway:UntagResource", "apigateway:GetTags")
		_, err := client.UntagResource(context.Background(), &apigateway.UntagResourceInput{
			ResourceArn: arn,
			TagKeys:     []string{"env"},
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("UntagResource(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:UntagResource", false)

		tags, err := client.GetTags(context.Background(), &apigateway.GetTagsInput{ResourceArn: arn})
		if err != nil {
			t.Fatalf("GetTags(after denied untag) error = %v", err)
		}
		if tags.Tags["env"] != "test" {
			t.Fatalf("GetTags(after denied untag) = %#v, want env tag preserved", tags.Tags)
		}
	})
}

func TestAPIGatewayCompatibilityAdapterManagesResourcesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	if aws.ToString(created.RootResourceId) == "" {
		t.Fatalf("CreateRestApi().RootResourceId = empty")
	}

	listed, err := client.GetResources(context.Background(), &apigateway.GetResourcesInput{RestApiId: created.Id})
	if err != nil {
		t.Fatalf("GetResources() error = %v", err)
	}
	if len(listed.Items) != 1 || aws.ToString(listed.Items[0].Id) != aws.ToString(created.RootResourceId) || aws.ToString(listed.Items[0].Path) != "/" {
		t.Fatalf("GetResources() = %#v, want root resource", listed.Items)
	}

	child, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
		RestApiId: created.Id,
		ParentId:  created.RootResourceId,
		PathPart:  aws.String("orders"),
	})
	if err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	if aws.ToString(child.ParentId) != aws.ToString(created.RootResourceId) || aws.ToString(child.Path) != "/orders" {
		t.Fatalf("CreateResource() = %#v, want /orders child", child)
	}

	got, err := client.GetResource(context.Background(), &apigateway.GetResourceInput{
		RestApiId:  created.Id,
		ResourceId: child.Id,
	})
	if err != nil {
		t.Fatalf("GetResource() error = %v", err)
	}
	if aws.ToString(got.Id) != aws.ToString(child.Id) || aws.ToString(got.PathPart) != "orders" {
		t.Fatalf("GetResource() = %#v, want created child", got)
	}

	if _, err := client.DeleteResource(context.Background(), &apigateway.DeleteResourceInput{
		RestApiId:  created.Id,
		ResourceId: child.Id,
	}); err != nil {
		t.Fatalf("DeleteResource() error = %v", err)
	}
	listed, err = client.GetResources(context.Background(), &apigateway.GetResourcesInput{RestApiId: created.Id})
	if err != nil {
		t.Fatalf("GetResources(after delete) error = %v", err)
	}
	if len(listed.Items) != 1 || aws.ToString(listed.Items[0].Path) != "/" {
		t.Fatalf("GetResources(after delete) = %#v, want root only", listed.Items)
	}
}

func TestAPIGatewayCompatibilityAdapterPaginatesResourcesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	for _, pathPart := range []string{"alpha", "beta", "gamma"} {
		if _, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
			RestApiId: api.Id,
			ParentId:  api.RootResourceId,
			PathPart:  aws.String(pathPart),
		}); err != nil {
			t.Fatalf("CreateResource(%s) error = %v", pathPart, err)
		}
	}

	first, err := client.GetResources(context.Background(), &apigateway.GetResourcesInput{
		RestApiId: api.Id,
		Limit:     aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("GetResources(first page) error = %v", err)
	}
	if len(first.Items) != 2 || aws.ToString(first.Position) == "" {
		t.Fatalf("GetResources(first page) = %#v position %q, want 2 items and next position", first.Items, aws.ToString(first.Position))
	}

	second, err := client.GetResources(context.Background(), &apigateway.GetResourcesInput{
		RestApiId: api.Id,
		Limit:     aws.Int32(2),
		Position:  first.Position,
	})
	if err != nil {
		t.Fatalf("GetResources(second page) error = %v", err)
	}
	if len(second.Items) != 2 || aws.ToString(second.Position) != "" {
		t.Fatalf("GetResources(second page) = %#v position %q, want final 2 items and no next position", second.Items, aws.ToString(second.Position))
	}
}

func TestAPIGatewayCompatibilityAdapterRejectsDuplicateResourceWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	input := &apigateway.CreateResourceInput{
		RestApiId: api.Id,
		ParentId:  api.RootResourceId,
		PathPart:  aws.String("orders"),
	}
	if _, err := client.CreateResource(context.Background(), input); err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	_, err = client.CreateResource(context.Background(), input)
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ConflictException" {
		t.Fatalf("CreateResource(duplicate) error = %v, want ConflictException", err)
	}

	listed, err := client.GetResources(context.Background(), &apigateway.GetResourcesInput{RestApiId: api.Id})
	if err != nil {
		t.Fatalf("GetResources() error = %v", err)
	}
	if len(listed.Items) != 2 {
		t.Fatalf("GetResources() = %#v, want root plus one child", listed.Items)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsResourceOperationsWithAWSSDK(t *testing.T) {
	t.Run("CreateResource", func(t *testing.T) {
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi", "apigateway:GetResources"}, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:CreateResource"}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		defer server.Close()

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		_, err = client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
			RestApiId: api.Id,
			ParentId:  api.RootResourceId,
			PathPart:  aws.String("orders"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("CreateResource(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:CreateResource", false)

		listed, err := client.GetResources(context.Background(), &apigateway.GetResourcesInput{RestApiId: api.Id})
		if err != nil {
			t.Fatalf("GetResources() error = %v", err)
		}
		if len(listed.Items) != 1 {
			t.Fatalf("GetResources() = %#v, want root only after denied create", listed.Items)
		}
	})

	t.Run("GetResources", func(t *testing.T) {
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi"}, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:GetResources"}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		defer server.Close()

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		_, err = client.GetResources(context.Background(), &apigateway.GetResourcesInput{RestApiId: api.Id})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetResources(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetResources", false)
	})

	t.Run("GetResource", func(t *testing.T) {
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi", "apigateway:CreateResource"}, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:GetResource"}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		defer server.Close()

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		child, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
			RestApiId: api.Id,
			ParentId:  api.RootResourceId,
			PathPart:  aws.String("orders"),
		})
		if err != nil {
			t.Fatalf("CreateResource() error = %v", err)
		}
		_, err = client.GetResource(context.Background(), &apigateway.GetResourceInput{
			RestApiId:  api.Id,
			ResourceId: child.Id,
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetResource(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetResource", false)
	})

	t.Run("DeleteResource", func(t *testing.T) {
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi", "apigateway:CreateResource", "apigateway:GetResource"}, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:DeleteResource"}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		defer server.Close()

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		child, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
			RestApiId: api.Id,
			ParentId:  api.RootResourceId,
			PathPart:  aws.String("orders"),
		})
		if err != nil {
			t.Fatalf("CreateResource() error = %v", err)
		}
		_, err = client.DeleteResource(context.Background(), &apigateway.DeleteResourceInput{
			RestApiId:  api.Id,
			ResourceId: child.Id,
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("DeleteResource(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:DeleteResource", false)

		got, err := client.GetResource(context.Background(), &apigateway.GetResourceInput{
			RestApiId:  api.Id,
			ResourceId: child.Id,
		})
		if err != nil {
			t.Fatalf("GetResource(after denied delete) error = %v", err)
		}
		if aws.ToString(got.PathPart) != "orders" {
			t.Fatalf("GetResource(after denied delete) = %#v, want original resource", got)
		}
	})
}

func TestAPIGatewayCompatibilityAdapterManagesMethodsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
		RestApiId: api.Id,
		ParentId:  api.RootResourceId,
		PathPart:  aws.String("orders"),
	})
	if err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}

	method, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		AuthorizationType: aws.String("NONE"),
		ApiKeyRequired:    true,
	})
	if err != nil {
		t.Fatalf("PutMethod() error = %v", err)
	}
	if aws.ToString(method.HttpMethod) != "GET" || aws.ToString(method.AuthorizationType) != "NONE" || method.ApiKeyRequired == nil || !*method.ApiKeyRequired {
		t.Fatalf("PutMethod() = %#v, want GET/NONE/api-key", method)
	}

	got, err := client.GetMethod(context.Background(), &apigateway.GetMethodInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	})
	if err != nil {
		t.Fatalf("GetMethod() error = %v", err)
	}
	if aws.ToString(got.HttpMethod) != "GET" || aws.ToString(got.AuthorizationType) != "NONE" {
		t.Fatalf("GetMethod() = %#v, want stored method", got)
	}

	withMethods, err := client.GetResource(context.Background(), &apigateway.GetResourceInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		Embed:      []string{"methods"},
	})
	if err != nil {
		t.Fatalf("GetResource(embed methods) error = %v", err)
	}
	if withMethods.ResourceMethods["GET"].AuthorizationType == nil || aws.ToString(withMethods.ResourceMethods["GET"].AuthorizationType) != "NONE" {
		t.Fatalf("GetResource(embed methods) = %#v, want GET method", withMethods.ResourceMethods)
	}

	if _, err := client.DeleteMethod(context.Background(), &apigateway.DeleteMethodInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	}); err != nil {
		t.Fatalf("DeleteMethod() error = %v", err)
	}
	withMethods, err = client.GetResource(context.Background(), &apigateway.GetResourceInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		Embed:      []string{"methods"},
	})
	if err != nil {
		t.Fatalf("GetResource(after delete) error = %v", err)
	}
	if len(withMethods.ResourceMethods) != 0 {
		t.Fatalf("GetResource(after delete) methods = %#v, want none", withMethods.ResourceMethods)
	}
}

func TestAPIGatewayCompatibilityAdapterRejectsDuplicateMethodWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
		RestApiId: api.Id,
		ParentId:  api.RootResourceId,
		PathPart:  aws.String("orders"),
	})
	if err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		AuthorizationType: aws.String("NONE"),
	}); err != nil {
		t.Fatalf("PutMethod() error = %v", err)
	}
	_, err = client.PutMethod(context.Background(), &apigateway.PutMethodInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		AuthorizationType: aws.String("AWS_IAM"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ConflictException" {
		t.Fatalf("PutMethod(duplicate) error = %v, want ConflictException", err)
	}

	got, err := client.GetMethod(context.Background(), &apigateway.GetMethodInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	})
	if err != nil {
		t.Fatalf("GetMethod() error = %v", err)
	}
	if aws.ToString(got.AuthorizationType) != "NONE" {
		t.Fatalf("GetMethod() authorization type = %q, want original NONE", aws.ToString(got.AuthorizationType))
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsMethodOperationsWithAWSSDK(t *testing.T) {
	t.Run("PutMethod", func(t *testing.T) {
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi", "apigateway:CreateResource", "apigateway:GetResource"}, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:PutMethod"}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		defer server.Close()

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
			RestApiId: api.Id,
			ParentId:  api.RootResourceId,
			PathPart:  aws.String("orders"),
		})
		if err != nil {
			t.Fatalf("CreateResource() error = %v", err)
		}
		_, err = client.PutMethod(context.Background(), &apigateway.PutMethodInput{
			RestApiId:         api.Id,
			ResourceId:        resource.Id,
			HttpMethod:        aws.String("GET"),
			AuthorizationType: aws.String("NONE"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("PutMethod(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:PutMethod", false)

		got, err := client.GetResource(context.Background(), &apigateway.GetResourceInput{RestApiId: api.Id, ResourceId: resource.Id})
		if err != nil {
			t.Fatalf("GetResource() error = %v", err)
		}
		if len(got.ResourceMethods) != 0 {
			t.Fatalf("GetResource() methods = %#v, want none after denied put", got.ResourceMethods)
		}
	})

	t.Run("GetMethod", func(t *testing.T) {
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi", "apigateway:CreateResource", "apigateway:PutMethod"}, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:GetMethod"}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		defer server.Close()

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
			RestApiId: api.Id,
			ParentId:  api.RootResourceId,
			PathPart:  aws.String("orders"),
		})
		if err != nil {
			t.Fatalf("CreateResource() error = %v", err)
		}
		if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
			RestApiId:         api.Id,
			ResourceId:        resource.Id,
			HttpMethod:        aws.String("GET"),
			AuthorizationType: aws.String("NONE"),
		}); err != nil {
			t.Fatalf("PutMethod() error = %v", err)
		}
		_, err = client.GetMethod(context.Background(), &apigateway.GetMethodInput{
			RestApiId:  api.Id,
			ResourceId: resource.Id,
			HttpMethod: aws.String("GET"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetMethod(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetMethod", false)
	})

	t.Run("DeleteMethod", func(t *testing.T) {
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: []string{"apigateway:CreateRestApi", "apigateway:CreateResource", "apigateway:PutMethod", "apigateway:GetMethod"}, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{"apigateway:DeleteMethod"}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		defer server.Close()

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
			RestApiId: api.Id,
			ParentId:  api.RootResourceId,
			PathPart:  aws.String("orders"),
		})
		if err != nil {
			t.Fatalf("CreateResource() error = %v", err)
		}
		if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
			RestApiId:         api.Id,
			ResourceId:        resource.Id,
			HttpMethod:        aws.String("GET"),
			AuthorizationType: aws.String("NONE"),
		}); err != nil {
			t.Fatalf("PutMethod() error = %v", err)
		}
		_, err = client.DeleteMethod(context.Background(), &apigateway.DeleteMethodInput{
			RestApiId:  api.Id,
			ResourceId: resource.Id,
			HttpMethod: aws.String("GET"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("DeleteMethod(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:DeleteMethod", false)

		got, err := client.GetMethod(context.Background(), &apigateway.GetMethodInput{
			RestApiId:  api.Id,
			ResourceId: resource.Id,
			HttpMethod: aws.String("GET"),
		})
		if err != nil {
			t.Fatalf("GetMethod(after denied delete) error = %v", err)
		}
		if aws.ToString(got.AuthorizationType) != "NONE" {
			t.Fatalf("GetMethod(after denied delete) = %#v, want original method", got)
		}
	})
}

func TestAPIGatewayCompatibilityAdapterManagesIntegrationsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
		RestApiId: api.Id,
		ParentId:  api.RootResourceId,
		PathPart:  aws.String("orders"),
	})
	if err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		AuthorizationType: aws.String("NONE"),
	}); err != nil {
		t.Fatalf("PutMethod() error = %v", err)
	}

	integration, err := client.PutIntegration(context.Background(), &apigateway.PutIntegrationInput{
		RestApiId:             api.Id,
		ResourceId:            resource.Id,
		HttpMethod:            aws.String("GET"),
		Type:                  types.IntegrationTypeHttp,
		IntegrationHttpMethod: aws.String("POST"),
		Uri:                   aws.String("https://example.com/orders"),
		PassthroughBehavior:   aws.String("WHEN_NO_MATCH"),
	})
	if err != nil {
		t.Fatalf("PutIntegration() error = %v", err)
	}
	if integration.Type != types.IntegrationTypeHttp || aws.ToString(integration.HttpMethod) != "POST" || aws.ToString(integration.Uri) != "https://example.com/orders" {
		t.Fatalf("PutIntegration() = %#v, want HTTP integration", integration)
	}

	got, err := client.GetIntegration(context.Background(), &apigateway.GetIntegrationInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	})
	if err != nil {
		t.Fatalf("GetIntegration() error = %v", err)
	}
	if got.Type != types.IntegrationTypeHttp || aws.ToString(got.HttpMethod) != "POST" || aws.ToString(got.PassthroughBehavior) != "WHEN_NO_MATCH" {
		t.Fatalf("GetIntegration() = %#v, want stored integration", got)
	}

	method, err := client.GetMethod(context.Background(), &apigateway.GetMethodInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	})
	if err != nil {
		t.Fatalf("GetMethod() error = %v", err)
	}
	if method.MethodIntegration == nil || method.MethodIntegration.Type != types.IntegrationTypeHttp {
		t.Fatalf("GetMethod().MethodIntegration = %#v, want HTTP integration", method.MethodIntegration)
	}

	if _, err := client.DeleteIntegration(context.Background(), &apigateway.DeleteIntegrationInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	}); err != nil {
		t.Fatalf("DeleteIntegration() error = %v", err)
	}
	method, err = client.GetMethod(context.Background(), &apigateway.GetMethodInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	})
	if err != nil {
		t.Fatalf("GetMethod(after delete integration) error = %v", err)
	}
	if method.MethodIntegration != nil {
		t.Fatalf("GetMethod(after delete integration).MethodIntegration = %#v, want nil", method.MethodIntegration)
	}
}

func TestAPIGatewayCompatibilityAdapterRejectsDuplicateIntegrationWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
		RestApiId: api.Id,
		ParentId:  api.RootResourceId,
		PathPart:  aws.String("orders"),
	})
	if err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		AuthorizationType: aws.String("NONE"),
	}); err != nil {
		t.Fatalf("PutMethod() error = %v", err)
	}
	if _, err := client.PutIntegration(context.Background(), &apigateway.PutIntegrationInput{
		RestApiId:             api.Id,
		ResourceId:            resource.Id,
		HttpMethod:            aws.String("GET"),
		Type:                  types.IntegrationTypeHttp,
		IntegrationHttpMethod: aws.String("POST"),
		Uri:                   aws.String("https://example.com/orders"),
	}); err != nil {
		t.Fatalf("PutIntegration() error = %v", err)
	}
	_, err = client.PutIntegration(context.Background(), &apigateway.PutIntegrationInput{
		RestApiId:             api.Id,
		ResourceId:            resource.Id,
		HttpMethod:            aws.String("GET"),
		Type:                  types.IntegrationTypeHttp,
		IntegrationHttpMethod: aws.String("POST"),
		Uri:                   aws.String("https://example.com/other"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ConflictException" {
		t.Fatalf("PutIntegration(duplicate) error = %v, want ConflictException", err)
	}

	got, err := client.GetIntegration(context.Background(), &apigateway.GetIntegrationInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	})
	if err != nil {
		t.Fatalf("GetIntegration() error = %v", err)
	}
	if aws.ToString(got.Uri) != "https://example.com/orders" {
		t.Fatalf("GetIntegration() URI = %q, want original URI", aws.ToString(got.Uri))
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsIntegrationOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, extraAllowed ...string) (*apigateway.Client, *authz.AuditLog, *string, *string) {
		t.Helper()
		allowed := []string{"apigateway:CreateRestApi", "apigateway:CreateResource", "apigateway:PutMethod"}
		allowed = append(allowed, extraAllowed...)
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
			RestApiId: api.Id,
			ParentId:  api.RootResourceId,
			PathPart:  aws.String("orders"),
		})
		if err != nil {
			t.Fatalf("CreateResource() error = %v", err)
		}
		if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
			RestApiId:         api.Id,
			ResourceId:        resource.Id,
			HttpMethod:        aws.String("GET"),
			AuthorizationType: aws.String("NONE"),
		}); err != nil {
			t.Fatalf("PutMethod() error = %v", err)
		}
		return client, auditLog, api.Id, resource.Id
	}

	t.Run("PutIntegration", func(t *testing.T) {
		client, auditLog, apiID, resourceID := setup(t, "apigateway:PutIntegration", "apigateway:GetMethod")
		_, err := client.PutIntegration(context.Background(), &apigateway.PutIntegrationInput{
			RestApiId:             apiID,
			ResourceId:            resourceID,
			HttpMethod:            aws.String("GET"),
			Type:                  types.IntegrationTypeHttp,
			IntegrationHttpMethod: aws.String("POST"),
			Uri:                   aws.String("https://example.com/orders"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("PutIntegration(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:PutIntegration", false)

		method, err := client.GetMethod(context.Background(), &apigateway.GetMethodInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
		})
		if err != nil {
			t.Fatalf("GetMethod() error = %v", err)
		}
		if method.MethodIntegration != nil {
			t.Fatalf("GetMethod().MethodIntegration = %#v, want nil after denied put", method.MethodIntegration)
		}
	})

	t.Run("GetIntegration", func(t *testing.T) {
		client, auditLog, apiID, resourceID := setup(t, "apigateway:GetIntegration", "apigateway:PutIntegration")
		if _, err := client.PutIntegration(context.Background(), &apigateway.PutIntegrationInput{
			RestApiId:             apiID,
			ResourceId:            resourceID,
			HttpMethod:            aws.String("GET"),
			Type:                  types.IntegrationTypeHttp,
			IntegrationHttpMethod: aws.String("POST"),
			Uri:                   aws.String("https://example.com/orders"),
		}); err != nil {
			t.Fatalf("PutIntegration() error = %v", err)
		}
		_, err := client.GetIntegration(context.Background(), &apigateway.GetIntegrationInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetIntegration(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetIntegration", false)
	})

	t.Run("DeleteIntegration", func(t *testing.T) {
		client, auditLog, apiID, resourceID := setup(t, "apigateway:DeleteIntegration", "apigateway:PutIntegration", "apigateway:GetIntegration")
		if _, err := client.PutIntegration(context.Background(), &apigateway.PutIntegrationInput{
			RestApiId:             apiID,
			ResourceId:            resourceID,
			HttpMethod:            aws.String("GET"),
			Type:                  types.IntegrationTypeHttp,
			IntegrationHttpMethod: aws.String("POST"),
			Uri:                   aws.String("https://example.com/orders"),
		}); err != nil {
			t.Fatalf("PutIntegration() error = %v", err)
		}
		_, err := client.DeleteIntegration(context.Background(), &apigateway.DeleteIntegrationInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("DeleteIntegration(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:DeleteIntegration", false)

		got, err := client.GetIntegration(context.Background(), &apigateway.GetIntegrationInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
		})
		if err != nil {
			t.Fatalf("GetIntegration(after denied delete) error = %v", err)
		}
		if aws.ToString(got.Uri) != "https://example.com/orders" {
			t.Fatalf("GetIntegration(after denied delete) = %#v, want original integration", got)
		}
	})
}

func TestAPIGatewayCompatibilityAdapterManagesMethodResponsesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
		RestApiId: api.Id,
		ParentId:  api.RootResourceId,
		PathPart:  aws.String("orders"),
	})
	if err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		AuthorizationType: aws.String("NONE"),
	}); err != nil {
		t.Fatalf("PutMethod() error = %v", err)
	}

	response, err := client.PutMethodResponse(context.Background(), &apigateway.PutMethodResponseInput{
		RestApiId:          api.Id,
		ResourceId:         resource.Id,
		HttpMethod:         aws.String("GET"),
		StatusCode:         aws.String("200"),
		ResponseModels:     map[string]string{"application/json": "Empty"},
		ResponseParameters: map[string]bool{"method.response.header.X-Request-Id": true},
	})
	if err != nil {
		t.Fatalf("PutMethodResponse() error = %v", err)
	}
	if aws.ToString(response.StatusCode) != "200" || response.ResponseModels["application/json"] != "Empty" {
		t.Fatalf("PutMethodResponse() = %#v, want 200 JSON response", response)
	}

	got, err := client.GetMethodResponse(context.Background(), &apigateway.GetMethodResponseInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
		StatusCode: aws.String("200"),
	})
	if err != nil {
		t.Fatalf("GetMethodResponse() error = %v", err)
	}
	if aws.ToString(got.StatusCode) != "200" || !got.ResponseParameters["method.response.header.X-Request-Id"] {
		t.Fatalf("GetMethodResponse() = %#v, want stored response", got)
	}

	method, err := client.GetMethod(context.Background(), &apigateway.GetMethodInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	})
	if err != nil {
		t.Fatalf("GetMethod() error = %v", err)
	}
	if method.MethodResponses["200"].StatusCode == nil || aws.ToString(method.MethodResponses["200"].StatusCode) != "200" {
		t.Fatalf("GetMethod().MethodResponses = %#v, want 200 response", method.MethodResponses)
	}

	if _, err := client.DeleteMethodResponse(context.Background(), &apigateway.DeleteMethodResponseInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
		StatusCode: aws.String("200"),
	}); err != nil {
		t.Fatalf("DeleteMethodResponse() error = %v", err)
	}
	method, err = client.GetMethod(context.Background(), &apigateway.GetMethodInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	})
	if err != nil {
		t.Fatalf("GetMethod(after delete response) error = %v", err)
	}
	if len(method.MethodResponses) != 0 {
		t.Fatalf("GetMethod(after delete response).MethodResponses = %#v, want none", method.MethodResponses)
	}
}

func TestAPIGatewayCompatibilityAdapterRejectsDuplicateMethodResponseWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
		RestApiId: api.Id,
		ParentId:  api.RootResourceId,
		PathPart:  aws.String("orders"),
	})
	if err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		AuthorizationType: aws.String("NONE"),
	}); err != nil {
		t.Fatalf("PutMethod() error = %v", err)
	}
	if _, err := client.PutMethodResponse(context.Background(), &apigateway.PutMethodResponseInput{
		RestApiId:          api.Id,
		ResourceId:         resource.Id,
		HttpMethod:         aws.String("GET"),
		StatusCode:         aws.String("200"),
		ResponseModels:     map[string]string{"application/json": "Empty"},
		ResponseParameters: map[string]bool{"method.response.header.X-Request-Id": true},
	}); err != nil {
		t.Fatalf("PutMethodResponse() error = %v", err)
	}
	_, err = client.PutMethodResponse(context.Background(), &apigateway.PutMethodResponseInput{
		RestApiId:          api.Id,
		ResourceId:         resource.Id,
		HttpMethod:         aws.String("GET"),
		StatusCode:         aws.String("200"),
		ResponseModels:     map[string]string{"application/json": "Error"},
		ResponseParameters: map[string]bool{"method.response.header.X-Other": true},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ConflictException" {
		t.Fatalf("PutMethodResponse(duplicate) error = %v, want ConflictException", err)
	}

	got, err := client.GetMethodResponse(context.Background(), &apigateway.GetMethodResponseInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
		StatusCode: aws.String("200"),
	})
	if err != nil {
		t.Fatalf("GetMethodResponse() error = %v", err)
	}
	if got.ResponseModels["application/json"] != "Empty" || !got.ResponseParameters["method.response.header.X-Request-Id"] {
		t.Fatalf("GetMethodResponse() = %#v, want original response", got)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsMethodResponseOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, extraAllowed ...string) (*apigateway.Client, *authz.AuditLog, *string, *string) {
		t.Helper()
		allowed := []string{"apigateway:CreateRestApi", "apigateway:CreateResource", "apigateway:PutMethod"}
		allowed = append(allowed, extraAllowed...)
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
			RestApiId: api.Id,
			ParentId:  api.RootResourceId,
			PathPart:  aws.String("orders"),
		})
		if err != nil {
			t.Fatalf("CreateResource() error = %v", err)
		}
		if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
			RestApiId:         api.Id,
			ResourceId:        resource.Id,
			HttpMethod:        aws.String("GET"),
			AuthorizationType: aws.String("NONE"),
		}); err != nil {
			t.Fatalf("PutMethod() error = %v", err)
		}
		return client, auditLog, api.Id, resource.Id
	}

	t.Run("PutMethodResponse", func(t *testing.T) {
		client, auditLog, apiID, resourceID := setup(t, "apigateway:PutMethodResponse", "apigateway:GetMethod")
		_, err := client.PutMethodResponse(context.Background(), &apigateway.PutMethodResponseInput{
			RestApiId:          apiID,
			ResourceId:         resourceID,
			HttpMethod:         aws.String("GET"),
			StatusCode:         aws.String("200"),
			ResponseModels:     map[string]string{"application/json": "Empty"},
			ResponseParameters: map[string]bool{"method.response.header.X-Request-Id": true},
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("PutMethodResponse(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:PutMethodResponse", false)

		method, err := client.GetMethod(context.Background(), &apigateway.GetMethodInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
		})
		if err != nil {
			t.Fatalf("GetMethod() error = %v", err)
		}
		if len(method.MethodResponses) != 0 {
			t.Fatalf("GetMethod().MethodResponses = %#v, want none after denied put", method.MethodResponses)
		}
	})

	t.Run("GetMethodResponse", func(t *testing.T) {
		client, auditLog, apiID, resourceID := setup(t, "apigateway:GetMethodResponse", "apigateway:PutMethodResponse")
		if _, err := client.PutMethodResponse(context.Background(), &apigateway.PutMethodResponseInput{
			RestApiId:      apiID,
			ResourceId:     resourceID,
			HttpMethod:     aws.String("GET"),
			StatusCode:     aws.String("200"),
			ResponseModels: map[string]string{"application/json": "Empty"},
		}); err != nil {
			t.Fatalf("PutMethodResponse() error = %v", err)
		}
		_, err := client.GetMethodResponse(context.Background(), &apigateway.GetMethodResponseInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
			StatusCode: aws.String("200"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetMethodResponse(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetMethodResponse", false)
	})

	t.Run("DeleteMethodResponse", func(t *testing.T) {
		client, auditLog, apiID, resourceID := setup(t, "apigateway:DeleteMethodResponse", "apigateway:PutMethodResponse", "apigateway:GetMethodResponse")
		if _, err := client.PutMethodResponse(context.Background(), &apigateway.PutMethodResponseInput{
			RestApiId:      apiID,
			ResourceId:     resourceID,
			HttpMethod:     aws.String("GET"),
			StatusCode:     aws.String("200"),
			ResponseModels: map[string]string{"application/json": "Empty"},
		}); err != nil {
			t.Fatalf("PutMethodResponse() error = %v", err)
		}
		_, err := client.DeleteMethodResponse(context.Background(), &apigateway.DeleteMethodResponseInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
			StatusCode: aws.String("200"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("DeleteMethodResponse(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:DeleteMethodResponse", false)

		got, err := client.GetMethodResponse(context.Background(), &apigateway.GetMethodResponseInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
			StatusCode: aws.String("200"),
		})
		if err != nil {
			t.Fatalf("GetMethodResponse(after denied delete) error = %v", err)
		}
		if got.ResponseModels["application/json"] != "Empty" {
			t.Fatalf("GetMethodResponse(after denied delete) = %#v, want original response", got)
		}
	})
}

func TestAPIGatewayCompatibilityAdapterManagesIntegrationResponsesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
		RestApiId: api.Id,
		ParentId:  api.RootResourceId,
		PathPart:  aws.String("orders"),
	})
	if err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		AuthorizationType: aws.String("NONE"),
	}); err != nil {
		t.Fatalf("PutMethod() error = %v", err)
	}
	if _, err := client.PutIntegration(context.Background(), &apigateway.PutIntegrationInput{
		RestApiId:             api.Id,
		ResourceId:            resource.Id,
		HttpMethod:            aws.String("GET"),
		Type:                  types.IntegrationTypeHttp,
		IntegrationHttpMethod: aws.String("POST"),
		Uri:                   aws.String("https://example.com/orders"),
	}); err != nil {
		t.Fatalf("PutIntegration() error = %v", err)
	}

	response, err := client.PutIntegrationResponse(context.Background(), &apigateway.PutIntegrationResponseInput{
		RestApiId:          api.Id,
		ResourceId:         resource.Id,
		HttpMethod:         aws.String("GET"),
		StatusCode:         aws.String("200"),
		ResponseParameters: map[string]string{"method.response.header.X-Request-Id": "integration.response.header.X-Request-Id"},
		ResponseTemplates:  map[string]string{"application/json": "$input.body"},
		SelectionPattern:   aws.String("2\\d{2}"),
	})
	if err != nil {
		t.Fatalf("PutIntegrationResponse() error = %v", err)
	}
	if aws.ToString(response.StatusCode) != "200" || response.ResponseTemplates["application/json"] != "$input.body" {
		t.Fatalf("PutIntegrationResponse() = %#v, want 200 JSON response", response)
	}

	got, err := client.GetIntegrationResponse(context.Background(), &apigateway.GetIntegrationResponseInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
		StatusCode: aws.String("200"),
	})
	if err != nil {
		t.Fatalf("GetIntegrationResponse() error = %v", err)
	}
	if aws.ToString(got.StatusCode) != "200" || got.ResponseParameters["method.response.header.X-Request-Id"] != "integration.response.header.X-Request-Id" {
		t.Fatalf("GetIntegrationResponse() = %#v, want stored response", got)
	}

	integration, err := client.GetIntegration(context.Background(), &apigateway.GetIntegrationInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	})
	if err != nil {
		t.Fatalf("GetIntegration() error = %v", err)
	}
	if integration.IntegrationResponses["200"].StatusCode == nil || aws.ToString(integration.IntegrationResponses["200"].StatusCode) != "200" {
		t.Fatalf("GetIntegration().IntegrationResponses = %#v, want 200 response", integration.IntegrationResponses)
	}

	if _, err := client.DeleteIntegrationResponse(context.Background(), &apigateway.DeleteIntegrationResponseInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
		StatusCode: aws.String("200"),
	}); err != nil {
		t.Fatalf("DeleteIntegrationResponse() error = %v", err)
	}
	integration, err = client.GetIntegration(context.Background(), &apigateway.GetIntegrationInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
	})
	if err != nil {
		t.Fatalf("GetIntegration(after delete response) error = %v", err)
	}
	if len(integration.IntegrationResponses) != 0 {
		t.Fatalf("GetIntegration(after delete response).IntegrationResponses = %#v, want none", integration.IntegrationResponses)
	}
}

func TestAPIGatewayCompatibilityAdapterRejectsDuplicateIntegrationResponseWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
		RestApiId: api.Id,
		ParentId:  api.RootResourceId,
		PathPart:  aws.String("orders"),
	})
	if err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		AuthorizationType: aws.String("NONE"),
	}); err != nil {
		t.Fatalf("PutMethod() error = %v", err)
	}
	if _, err := client.PutIntegration(context.Background(), &apigateway.PutIntegrationInput{
		RestApiId:             api.Id,
		ResourceId:            resource.Id,
		HttpMethod:            aws.String("GET"),
		Type:                  types.IntegrationTypeHttp,
		IntegrationHttpMethod: aws.String("POST"),
		Uri:                   aws.String("https://example.com/orders"),
	}); err != nil {
		t.Fatalf("PutIntegration() error = %v", err)
	}
	if _, err := client.PutIntegrationResponse(context.Background(), &apigateway.PutIntegrationResponseInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		StatusCode:        aws.String("200"),
		ResponseTemplates: map[string]string{"application/json": "$input.body"},
	}); err != nil {
		t.Fatalf("PutIntegrationResponse() error = %v", err)
	}
	_, err = client.PutIntegrationResponse(context.Background(), &apigateway.PutIntegrationResponseInput{
		RestApiId:         api.Id,
		ResourceId:        resource.Id,
		HttpMethod:        aws.String("GET"),
		StatusCode:        aws.String("200"),
		ResponseTemplates: map[string]string{"application/json": "{\"error\":true}"},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ConflictException" {
		t.Fatalf("PutIntegrationResponse(duplicate) error = %v, want ConflictException", err)
	}

	got, err := client.GetIntegrationResponse(context.Background(), &apigateway.GetIntegrationResponseInput{
		RestApiId:  api.Id,
		ResourceId: resource.Id,
		HttpMethod: aws.String("GET"),
		StatusCode: aws.String("200"),
	})
	if err != nil {
		t.Fatalf("GetIntegrationResponse() error = %v", err)
	}
	if got.ResponseTemplates["application/json"] != "$input.body" {
		t.Fatalf("GetIntegrationResponse() = %#v, want original response", got)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsIntegrationResponseOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, extraAllowed ...string) (*apigateway.Client, *authz.AuditLog, *string, *string) {
		t.Helper()
		allowed := []string{"apigateway:CreateRestApi", "apigateway:CreateResource", "apigateway:PutMethod", "apigateway:PutIntegration"}
		allowed = append(allowed, extraAllowed...)
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		resource, err := client.CreateResource(context.Background(), &apigateway.CreateResourceInput{
			RestApiId: api.Id,
			ParentId:  api.RootResourceId,
			PathPart:  aws.String("orders"),
		})
		if err != nil {
			t.Fatalf("CreateResource() error = %v", err)
		}
		if _, err := client.PutMethod(context.Background(), &apigateway.PutMethodInput{
			RestApiId:         api.Id,
			ResourceId:        resource.Id,
			HttpMethod:        aws.String("GET"),
			AuthorizationType: aws.String("NONE"),
		}); err != nil {
			t.Fatalf("PutMethod() error = %v", err)
		}
		if _, err := client.PutIntegration(context.Background(), &apigateway.PutIntegrationInput{
			RestApiId:             api.Id,
			ResourceId:            resource.Id,
			HttpMethod:            aws.String("GET"),
			Type:                  types.IntegrationTypeHttp,
			IntegrationHttpMethod: aws.String("POST"),
			Uri:                   aws.String("https://example.com/orders"),
		}); err != nil {
			t.Fatalf("PutIntegration() error = %v", err)
		}
		return client, auditLog, api.Id, resource.Id
	}

	t.Run("PutIntegrationResponse", func(t *testing.T) {
		client, auditLog, apiID, resourceID := setup(t, "apigateway:PutIntegrationResponse", "apigateway:GetIntegration")
		_, err := client.PutIntegrationResponse(context.Background(), &apigateway.PutIntegrationResponseInput{
			RestApiId:         apiID,
			ResourceId:        resourceID,
			HttpMethod:        aws.String("GET"),
			StatusCode:        aws.String("200"),
			ResponseTemplates: map[string]string{"application/json": "$input.body"},
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("PutIntegrationResponse(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:PutIntegrationResponse", false)

		integration, err := client.GetIntegration(context.Background(), &apigateway.GetIntegrationInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
		})
		if err != nil {
			t.Fatalf("GetIntegration() error = %v", err)
		}
		if len(integration.IntegrationResponses) != 0 {
			t.Fatalf("GetIntegration().IntegrationResponses = %#v, want none after denied put", integration.IntegrationResponses)
		}
	})

	t.Run("GetIntegrationResponse", func(t *testing.T) {
		client, auditLog, apiID, resourceID := setup(t, "apigateway:GetIntegrationResponse", "apigateway:PutIntegrationResponse")
		if _, err := client.PutIntegrationResponse(context.Background(), &apigateway.PutIntegrationResponseInput{
			RestApiId:         apiID,
			ResourceId:        resourceID,
			HttpMethod:        aws.String("GET"),
			StatusCode:        aws.String("200"),
			ResponseTemplates: map[string]string{"application/json": "$input.body"},
		}); err != nil {
			t.Fatalf("PutIntegrationResponse() error = %v", err)
		}
		_, err := client.GetIntegrationResponse(context.Background(), &apigateway.GetIntegrationResponseInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
			StatusCode: aws.String("200"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetIntegrationResponse(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetIntegrationResponse", false)
	})

	t.Run("DeleteIntegrationResponse", func(t *testing.T) {
		client, auditLog, apiID, resourceID := setup(t, "apigateway:DeleteIntegrationResponse", "apigateway:PutIntegrationResponse", "apigateway:GetIntegrationResponse")
		if _, err := client.PutIntegrationResponse(context.Background(), &apigateway.PutIntegrationResponseInput{
			RestApiId:         apiID,
			ResourceId:        resourceID,
			HttpMethod:        aws.String("GET"),
			StatusCode:        aws.String("200"),
			ResponseTemplates: map[string]string{"application/json": "$input.body"},
		}); err != nil {
			t.Fatalf("PutIntegrationResponse() error = %v", err)
		}
		_, err := client.DeleteIntegrationResponse(context.Background(), &apigateway.DeleteIntegrationResponseInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
			StatusCode: aws.String("200"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("DeleteIntegrationResponse(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:DeleteIntegrationResponse", false)

		got, err := client.GetIntegrationResponse(context.Background(), &apigateway.GetIntegrationResponseInput{
			RestApiId:  apiID,
			ResourceId: resourceID,
			HttpMethod: aws.String("GET"),
			StatusCode: aws.String("200"),
		})
		if err != nil {
			t.Fatalf("GetIntegrationResponse(after denied delete) error = %v", err)
		}
		if got.ResponseTemplates["application/json"] != "$input.body" {
			t.Fatalf("GetIntegrationResponse(after denied delete) = %#v, want original response", got)
		}
	})
}

func TestAPIGatewayCompatibilityAdapterManagesDeploymentsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}

	created, err := client.CreateDeployment(context.Background(), &apigateway.CreateDeploymentInput{
		RestApiId:        api.Id,
		Description:      aws.String("initial release"),
		StageName:        aws.String("prod"),
		Variables:        map[string]string{"color": "blue"},
		StageDescription: aws.String("production"),
	})
	if err != nil {
		t.Fatalf("CreateDeployment() error = %v", err)
	}
	if aws.ToString(created.Id) == "" || aws.ToString(created.Description) != "initial release" || created.CreatedDate == nil {
		t.Fatalf("CreateDeployment() = %#v, want id/description/created date", created)
	}

	got, err := client.GetDeployment(context.Background(), &apigateway.GetDeploymentInput{
		RestApiId:    api.Id,
		DeploymentId: created.Id,
	})
	if err != nil {
		t.Fatalf("GetDeployment() error = %v", err)
	}
	if aws.ToString(got.Id) != aws.ToString(created.Id) || aws.ToString(got.Description) != "initial release" {
		t.Fatalf("GetDeployment() = %#v, want created deployment", got)
	}

	listed, err := client.GetDeployments(context.Background(), &apigateway.GetDeploymentsInput{RestApiId: api.Id})
	if err != nil {
		t.Fatalf("GetDeployments() error = %v", err)
	}
	if len(listed.Items) != 1 || aws.ToString(listed.Items[0].Id) != aws.ToString(created.Id) {
		t.Fatalf("GetDeployments() = %#v, want created deployment", listed.Items)
	}

	if _, err := client.DeleteDeployment(context.Background(), &apigateway.DeleteDeploymentInput{
		RestApiId:    api.Id,
		DeploymentId: created.Id,
	}); err != nil {
		t.Fatalf("DeleteDeployment() error = %v", err)
	}
	listed, err = client.GetDeployments(context.Background(), &apigateway.GetDeploymentsInput{RestApiId: api.Id})
	if err != nil {
		t.Fatalf("GetDeployments(after delete) error = %v", err)
	}
	if len(listed.Items) != 0 {
		t.Fatalf("GetDeployments(after delete) = %#v, want none", listed.Items)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsDeploymentOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, extraAllowed ...string) (*apigateway.Client, *authz.AuditLog, *string) {
		t.Helper()
		allowed := append([]string{"apigateway:CreateRestApi"}, extraAllowed...)
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		return client, auditLog, api.Id
	}

	t.Run("CreateDeployment", func(t *testing.T) {
		client, auditLog, apiID := setup(t, "apigateway:CreateDeployment", "apigateway:GetDeployments")
		_, err := client.CreateDeployment(context.Background(), &apigateway.CreateDeploymentInput{
			RestApiId:   apiID,
			Description: aws.String("denied"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("CreateDeployment(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:CreateDeployment", false)

		listed, err := client.GetDeployments(context.Background(), &apigateway.GetDeploymentsInput{RestApiId: apiID})
		if err != nil {
			t.Fatalf("GetDeployments() error = %v", err)
		}
		if len(listed.Items) != 0 {
			t.Fatalf("GetDeployments() = %#v, want no deployments after denied create", listed.Items)
		}
	})

	t.Run("GetDeployments", func(t *testing.T) {
		client, auditLog, apiID := setup(t, "apigateway:GetDeployments")
		_, err := client.GetDeployments(context.Background(), &apigateway.GetDeploymentsInput{RestApiId: apiID})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetDeployments(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetDeployments", false)
	})

	t.Run("GetDeployment", func(t *testing.T) {
		client, auditLog, apiID := setup(t, "apigateway:GetDeployment", "apigateway:CreateDeployment")
		deployment, err := client.CreateDeployment(context.Background(), &apigateway.CreateDeploymentInput{
			RestApiId:   apiID,
			Description: aws.String("initial release"),
		})
		if err != nil {
			t.Fatalf("CreateDeployment() error = %v", err)
		}
		_, err = client.GetDeployment(context.Background(), &apigateway.GetDeploymentInput{
			RestApiId:    apiID,
			DeploymentId: deployment.Id,
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetDeployment(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetDeployment", false)
	})

	t.Run("DeleteDeployment", func(t *testing.T) {
		client, auditLog, apiID := setup(t, "apigateway:DeleteDeployment", "apigateway:CreateDeployment", "apigateway:GetDeployment")
		deployment, err := client.CreateDeployment(context.Background(), &apigateway.CreateDeploymentInput{
			RestApiId:   apiID,
			Description: aws.String("initial release"),
		})
		if err != nil {
			t.Fatalf("CreateDeployment() error = %v", err)
		}
		_, err = client.DeleteDeployment(context.Background(), &apigateway.DeleteDeploymentInput{
			RestApiId:    apiID,
			DeploymentId: deployment.Id,
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("DeleteDeployment(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:DeleteDeployment", false)

		got, err := client.GetDeployment(context.Background(), &apigateway.GetDeploymentInput{
			RestApiId:    apiID,
			DeploymentId: deployment.Id,
		})
		if err != nil {
			t.Fatalf("GetDeployment(after denied delete) error = %v", err)
		}
		if aws.ToString(got.Description) != "initial release" {
			t.Fatalf("GetDeployment(after denied delete) = %#v, want original deployment", got)
		}
	})
}

func TestAPIGatewayCompatibilityAdapterPaginatesDeploymentsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	for _, description := range []string{"first", "second", "third"} {
		if _, err := client.CreateDeployment(context.Background(), &apigateway.CreateDeploymentInput{
			RestApiId:   api.Id,
			Description: aws.String(description),
		}); err != nil {
			t.Fatalf("CreateDeployment(%s) error = %v", description, err)
		}
	}

	first, err := client.GetDeployments(context.Background(), &apigateway.GetDeploymentsInput{
		RestApiId: api.Id,
		Limit:     aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("GetDeployments(first page) error = %v", err)
	}
	if len(first.Items) != 2 || aws.ToString(first.Position) == "" {
		t.Fatalf("GetDeployments(first page) = %#v position %q, want 2 items and next position", first.Items, aws.ToString(first.Position))
	}

	second, err := client.GetDeployments(context.Background(), &apigateway.GetDeploymentsInput{
		RestApiId: api.Id,
		Limit:     aws.Int32(2),
		Position:  first.Position,
	})
	if err != nil {
		t.Fatalf("GetDeployments(second page) error = %v", err)
	}
	if len(second.Items) != 1 || aws.ToString(second.Position) != "" {
		t.Fatalf("GetDeployments(second page) = %#v position %q, want final item and no next position", second.Items, aws.ToString(second.Position))
	}
}

func TestAPIGatewayCompatibilityAdapterManagesStagesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	deployment, err := client.CreateDeployment(context.Background(), &apigateway.CreateDeploymentInput{
		RestApiId:   api.Id,
		Description: aws.String("initial release"),
	})
	if err != nil {
		t.Fatalf("CreateDeployment() error = %v", err)
	}

	created, err := client.CreateStage(context.Background(), &apigateway.CreateStageInput{
		RestApiId:    api.Id,
		DeploymentId: deployment.Id,
		StageName:    aws.String("prod"),
		Description:  aws.String("production"),
		Variables:    map[string]string{"color": "blue"},
	})
	if err != nil {
		t.Fatalf("CreateStage() error = %v", err)
	}
	if aws.ToString(created.StageName) != "prod" || aws.ToString(created.DeploymentId) != aws.ToString(deployment.Id) || created.Variables["color"] != "blue" {
		t.Fatalf("CreateStage() = %#v, want prod stage", created)
	}

	got, err := client.GetStage(context.Background(), &apigateway.GetStageInput{
		RestApiId: api.Id,
		StageName: aws.String("prod"),
	})
	if err != nil {
		t.Fatalf("GetStage() error = %v", err)
	}
	if aws.ToString(got.StageName) != "prod" || aws.ToString(got.Description) != "production" {
		t.Fatalf("GetStage() = %#v, want created stage", got)
	}
	updated, err := client.UpdateStage(context.Background(), &apigateway.UpdateStageInput{
		RestApiId: api.Id,
		StageName: aws.String("prod"),
		PatchOperations: []types.PatchOperation{
			{Op: types.OpReplace, Path: aws.String("/description"), Value: aws.String("production v2")},
			{Op: types.OpReplace, Path: aws.String("/variables/color"), Value: aws.String("green")},
		},
	})
	if err != nil || aws.ToString(updated.Description) != "production v2" || updated.Variables["color"] != "green" {
		t.Fatalf("UpdateStage() = %#v, %v; want updated description and variable", updated, err)
	}

	listed, err := client.GetStages(context.Background(), &apigateway.GetStagesInput{RestApiId: api.Id})
	if err != nil {
		t.Fatalf("GetStages() error = %v", err)
	}
	if len(listed.Item) != 1 || aws.ToString(listed.Item[0].StageName) != "prod" {
		t.Fatalf("GetStages() = %#v, want prod stage", listed.Item)
	}

	if _, err := client.DeleteStage(context.Background(), &apigateway.DeleteStageInput{
		RestApiId: api.Id,
		StageName: aws.String("prod"),
	}); err != nil {
		t.Fatalf("DeleteStage() error = %v", err)
	}
	listed, err = client.GetStages(context.Background(), &apigateway.GetStagesInput{RestApiId: api.Id})
	if err != nil {
		t.Fatalf("GetStages(after delete) error = %v", err)
	}
	if len(listed.Item) != 0 {
		t.Fatalf("GetStages(after delete) = %#v, want none", listed.Item)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsStageOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, extraAllowed ...string) (*apigateway.Client, *authz.AuditLog, *string, *string) {
		t.Helper()
		allowed := append([]string{"apigateway:CreateRestApi", "apigateway:CreateDeployment"}, extraAllowed...)
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
		if err != nil {
			t.Fatalf("CreateRestApi() error = %v", err)
		}
		deployment, err := client.CreateDeployment(context.Background(), &apigateway.CreateDeploymentInput{RestApiId: api.Id})
		if err != nil {
			t.Fatalf("CreateDeployment() error = %v", err)
		}
		return client, auditLog, api.Id, deployment.Id
	}

	t.Run("CreateStage", func(t *testing.T) {
		client, auditLog, apiID, deploymentID := setup(t, "apigateway:CreateStage", "apigateway:GetStages")
		_, err := client.CreateStage(context.Background(), &apigateway.CreateStageInput{
			RestApiId:    apiID,
			DeploymentId: deploymentID,
			StageName:    aws.String("prod"),
			Description:  aws.String("denied"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("CreateStage(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:CreateStage", false)

		listed, err := client.GetStages(context.Background(), &apigateway.GetStagesInput{RestApiId: apiID})
		if err != nil {
			t.Fatalf("GetStages() error = %v", err)
		}
		if len(listed.Item) != 0 {
			t.Fatalf("GetStages() = %#v, want no stages after denied create", listed.Item)
		}
	})

	t.Run("GetStages", func(t *testing.T) {
		client, auditLog, apiID, _ := setup(t, "apigateway:GetStages")
		_, err := client.GetStages(context.Background(), &apigateway.GetStagesInput{RestApiId: apiID})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetStages(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetStages", false)
	})

	t.Run("GetStage", func(t *testing.T) {
		client, auditLog, apiID, deploymentID := setup(t, "apigateway:GetStage", "apigateway:CreateStage")
		if _, err := client.CreateStage(context.Background(), &apigateway.CreateStageInput{
			RestApiId:    apiID,
			DeploymentId: deploymentID,
			StageName:    aws.String("prod"),
			Description:  aws.String("production"),
		}); err != nil {
			t.Fatalf("CreateStage() error = %v", err)
		}
		_, err := client.GetStage(context.Background(), &apigateway.GetStageInput{
			RestApiId: apiID,
			StageName: aws.String("prod"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetStage(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetStage", false)
	})

	t.Run("UpdateStage", func(t *testing.T) {
		client, auditLog, apiID, deploymentID := setup(t, "apigateway:UpdateStage", "apigateway:CreateStage", "apigateway:GetStage")
		if _, err := client.CreateStage(context.Background(), &apigateway.CreateStageInput{RestApiId: apiID, DeploymentId: deploymentID, StageName: aws.String("prod"), Description: aws.String("production")}); err != nil {
			t.Fatalf("CreateStage() error = %v", err)
		}
		_, err := client.UpdateStage(context.Background(), &apigateway.UpdateStageInput{RestApiId: apiID, StageName: aws.String("prod"), PatchOperations: []types.PatchOperation{{Op: types.OpReplace, Path: aws.String("/description"), Value: aws.String("denied")}}})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("UpdateStage(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:UpdateStage", false)
		got, err := client.GetStage(context.Background(), &apigateway.GetStageInput{RestApiId: apiID, StageName: aws.String("prod")})
		if err != nil || aws.ToString(got.Description) != "production" {
			t.Fatalf("GetStage(after denied update) = %#v, %v; want original description", got, err)
		}
	})

	t.Run("DeleteStage", func(t *testing.T) {
		client, auditLog, apiID, deploymentID := setup(t, "apigateway:DeleteStage", "apigateway:CreateStage", "apigateway:GetStage")
		if _, err := client.CreateStage(context.Background(), &apigateway.CreateStageInput{
			RestApiId:    apiID,
			DeploymentId: deploymentID,
			StageName:    aws.String("prod"),
			Description:  aws.String("production"),
		}); err != nil {
			t.Fatalf("CreateStage() error = %v", err)
		}
		_, err := client.DeleteStage(context.Background(), &apigateway.DeleteStageInput{
			RestApiId: apiID,
			StageName: aws.String("prod"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("DeleteStage(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:DeleteStage", false)

		got, err := client.GetStage(context.Background(), &apigateway.GetStageInput{
			RestApiId: apiID,
			StageName: aws.String("prod"),
		})
		if err != nil {
			t.Fatalf("GetStage(after denied delete) error = %v", err)
		}
		if aws.ToString(got.Description) != "production" {
			t.Fatalf("GetStage(after denied delete) = %#v, want original stage", got)
		}
	})
}

func TestAPIGatewayCompatibilityAdapterRejectsDuplicateStageWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	api, err := client.CreateRestApi(context.Background(), &apigateway.CreateRestApiInput{Name: aws.String("orders-api")})
	if err != nil {
		t.Fatalf("CreateRestApi() error = %v", err)
	}
	deployment, err := client.CreateDeployment(context.Background(), &apigateway.CreateDeploymentInput{RestApiId: api.Id})
	if err != nil {
		t.Fatalf("CreateDeployment() error = %v", err)
	}
	input := &apigateway.CreateStageInput{
		RestApiId:    api.Id,
		DeploymentId: deployment.Id,
		StageName:    aws.String("prod"),
		Description:  aws.String("original"),
	}
	if _, err := client.CreateStage(context.Background(), input); err != nil {
		t.Fatalf("CreateStage() error = %v", err)
	}
	input.Description = aws.String("overwritten")
	_, err = client.CreateStage(context.Background(), input)
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ConflictException" {
		t.Fatalf("CreateStage(duplicate) error = %v, want ConflictException", err)
	}

	got, err := client.GetStage(context.Background(), &apigateway.GetStageInput{
		RestApiId: api.Id,
		StageName: aws.String("prod"),
	})
	if err != nil {
		t.Fatalf("GetStage() error = %v", err)
	}
	if aws.ToString(got.Description) != "original" {
		t.Fatalf("GetStage() description = %q, want original", aws.ToString(got.Description))
	}
}

func TestAPIGatewayCompatibilityAdapterManagesCustomDomainsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	certificateARN := "arn:aws:acm:us-east-1:123456789012:certificate/orders"
	created, err := client.CreateDomainName(context.Background(), &apigateway.CreateDomainNameInput{
		DomainName:             aws.String("api.example.test"),
		RegionalCertificateArn: aws.String(certificateARN),
		EndpointConfiguration: &types.EndpointConfiguration{
			Types: []types.EndpointType{types.EndpointTypeRegional},
		},
		SecurityPolicy: types.SecurityPolicyTls12,
		Tags:           map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("CreateDomainName() error = %v", err)
	}
	if aws.ToString(created.DomainName) != "api.example.test" || aws.ToString(created.RegionalCertificateArn) != certificateARN {
		t.Fatalf("CreateDomainName() = %#v, want custom domain", created)
	}
	if created.DomainNameStatus != types.DomainNameStatusAvailable || aws.ToString(created.RegionalDomainName) == "" {
		t.Fatalf("CreateDomainName() status/domain = %s/%q, want AVAILABLE regional domain", created.DomainNameStatus, aws.ToString(created.RegionalDomainName))
	}
	if len(created.EndpointConfiguration.Types) != 1 || created.EndpointConfiguration.Types[0] != types.EndpointTypeRegional || created.Tags["env"] != "test" {
		t.Fatalf("CreateDomainName() endpoint/tags = %#v/%#v, want REGIONAL tagged domain", created.EndpointConfiguration, created.Tags)
	}

	got, err := client.GetDomainName(context.Background(), &apigateway.GetDomainNameInput{
		DomainName: aws.String("api.example.test"),
	})
	if err != nil {
		t.Fatalf("GetDomainName() error = %v", err)
	}
	if aws.ToString(got.DomainName) != "api.example.test" || aws.ToString(got.DomainNameArn) == "" {
		t.Fatalf("GetDomainName() = %#v, want created domain", got)
	}

	listed, err := client.GetDomainNames(context.Background(), &apigateway.GetDomainNamesInput{})
	if err != nil {
		t.Fatalf("GetDomainNames() error = %v", err)
	}
	if len(listed.Items) != 1 || aws.ToString(listed.Items[0].DomainName) != "api.example.test" {
		t.Fatalf("GetDomainNames() = %#v, want created domain", listed.Items)
	}

	if _, err := client.DeleteDomainName(context.Background(), &apigateway.DeleteDomainNameInput{
		DomainName: aws.String("api.example.test"),
	}); err != nil {
		t.Fatalf("DeleteDomainName() error = %v", err)
	}
	listed, err = client.GetDomainNames(context.Background(), &apigateway.GetDomainNamesInput{})
	if err != nil {
		t.Fatalf("GetDomainNames(after delete) error = %v", err)
	}
	if len(listed.Items) != 0 {
		t.Fatalf("GetDomainNames(after delete) = %#v, want none", listed.Items)
	}
}

func TestAPIGatewayCompatibilityAdapterAuthorizesAndAuditsCustomDomainOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, extraAllowed ...string) (*apigateway.Client, *authz.AuditLog) {
		t.Helper()
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewAPIGatewayAdapter(
			compataws.WithAPIGatewayAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: extraAllowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithAPIGatewayAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := apigateway.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *apigateway.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})
		return client, auditLog
	}

	t.Run("CreateDomainName", func(t *testing.T) {
		client, auditLog := setup(t, "apigateway:CreateDomainName", "apigateway:GetDomainNames")
		_, err := client.CreateDomainName(context.Background(), &apigateway.CreateDomainNameInput{
			DomainName: aws.String("api.example.test"),
			EndpointConfiguration: &types.EndpointConfiguration{
				Types: []types.EndpointType{types.EndpointTypeRegional},
			},
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("CreateDomainName(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:CreateDomainName", false)

		listed, err := client.GetDomainNames(context.Background(), &apigateway.GetDomainNamesInput{})
		if err != nil {
			t.Fatalf("GetDomainNames() error = %v", err)
		}
		if len(listed.Items) != 0 {
			t.Fatalf("GetDomainNames() = %#v, want no domains after denied create", listed.Items)
		}
	})

	t.Run("GetDomainNames", func(t *testing.T) {
		client, auditLog := setup(t, "apigateway:GetDomainNames")
		_, err := client.GetDomainNames(context.Background(), &apigateway.GetDomainNamesInput{})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetDomainNames(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetDomainNames", false)
	})

	t.Run("GetDomainName", func(t *testing.T) {
		client, auditLog := setup(t, "apigateway:GetDomainName", "apigateway:CreateDomainName")
		if _, err := client.CreateDomainName(context.Background(), &apigateway.CreateDomainNameInput{
			DomainName: aws.String("api.example.test"),
			EndpointConfiguration: &types.EndpointConfiguration{
				Types: []types.EndpointType{types.EndpointTypeRegional},
			},
		}); err != nil {
			t.Fatalf("CreateDomainName() error = %v", err)
		}
		_, err := client.GetDomainName(context.Background(), &apigateway.GetDomainNameInput{
			DomainName: aws.String("api.example.test"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("GetDomainName(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:GetDomainName", false)
	})

	t.Run("DeleteDomainName", func(t *testing.T) {
		client, auditLog := setup(t, "apigateway:DeleteDomainName", "apigateway:CreateDomainName", "apigateway:GetDomainName")
		if _, err := client.CreateDomainName(context.Background(), &apigateway.CreateDomainNameInput{
			DomainName: aws.String("api.example.test"),
			EndpointConfiguration: &types.EndpointConfiguration{
				Types: []types.EndpointType{types.EndpointTypeRegional},
			},
		}); err != nil {
			t.Fatalf("CreateDomainName() error = %v", err)
		}
		_, err := client.DeleteDomainName(context.Background(), &apigateway.DeleteDomainNameInput{
			DomainName: aws.String("api.example.test"),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("DeleteDomainName(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "apigateway:DeleteDomainName", false)

		got, err := client.GetDomainName(context.Background(), &apigateway.GetDomainNameInput{
			DomainName: aws.String("api.example.test"),
		})
		if err != nil {
			t.Fatalf("GetDomainName(after denied delete) error = %v", err)
		}
		if aws.ToString(got.DomainName) != "api.example.test" {
			t.Fatalf("GetDomainName(after denied delete) = %#v, want original domain", got)
		}
	})
}

func TestAPIGatewayCompatibilityAdapterTagsCustomDomainsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	domainName := "api.example.test"
	if _, err := client.CreateDomainName(context.Background(), &apigateway.CreateDomainNameInput{
		DomainName: aws.String(domainName),
		EndpointConfiguration: &types.EndpointConfiguration{
			Types: []types.EndpointType{types.EndpointTypeRegional},
		},
		Tags: map[string]string{"env": "test"},
	}); err != nil {
		t.Fatalf("CreateDomainName() error = %v", err)
	}
	arn := aws.String(fmt.Sprintf("arn:aws:apigateway:us-east-1::/domainnames/%s", domainName))

	tags, err := client.GetTags(context.Background(), &apigateway.GetTagsInput{ResourceArn: arn})
	if err != nil {
		t.Fatalf("GetTags() error = %v", err)
	}
	if tags.Tags["env"] != "test" {
		t.Fatalf("GetTags() = %#v, want env=test", tags.Tags)
	}

	if _, err := client.TagResource(context.Background(), &apigateway.TagResourceInput{
		ResourceArn: arn,
		Tags:        map[string]string{"owner": "platform"},
	}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	tags, err = client.GetTags(context.Background(), &apigateway.GetTagsInput{ResourceArn: arn})
	if err != nil {
		t.Fatalf("GetTags(after tag) error = %v", err)
	}
	if tags.Tags["env"] != "test" || tags.Tags["owner"] != "platform" {
		t.Fatalf("GetTags(after tag) = %#v, want merged env/owner tags", tags.Tags)
	}

	if _, err := client.UntagResource(context.Background(), &apigateway.UntagResourceInput{
		ResourceArn: arn,
		TagKeys:     []string{"env"},
	}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	tags, err = client.GetTags(context.Background(), &apigateway.GetTagsInput{ResourceArn: arn})
	if err != nil {
		t.Fatalf("GetTags(after untag) error = %v", err)
	}
	if _, ok := tags.Tags["env"]; ok || tags.Tags["owner"] != "platform" {
		t.Fatalf("GetTags(after untag) = %#v, want owner only", tags.Tags)
	}
}

func TestAPIGatewayCompatibilityAdapterRejectsDuplicateCustomDomainWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	input := &apigateway.CreateDomainNameInput{
		DomainName:             aws.String("api.example.test"),
		RegionalCertificateArn: aws.String("arn:aws:acm:us-east-1:123456789012:certificate/orders"),
		EndpointConfiguration: &types.EndpointConfiguration{
			Types: []types.EndpointType{types.EndpointTypeRegional},
		},
	}
	if _, err := client.CreateDomainName(context.Background(), input); err != nil {
		t.Fatalf("CreateDomainName() error = %v", err)
	}
	_, err := client.CreateDomainName(context.Background(), input)
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ConflictException" {
		t.Fatalf("CreateDomainName(duplicate) error = %v, want ConflictException", err)
	}

	listed, err := client.GetDomainNames(context.Background(), &apigateway.GetDomainNamesInput{})
	if err != nil {
		t.Fatalf("GetDomainNames() error = %v", err)
	}
	if len(listed.Items) != 1 || aws.ToString(listed.Items[0].DomainName) != "api.example.test" {
		t.Fatalf("GetDomainNames() = %#v, want one original domain", listed.Items)
	}
}

func TestAPIGatewayCompatibilityAdapterPaginatesCustomDomainsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()

	client := apigateway.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *apigateway.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, domainName := range []string{"api-a.example.test", "api-b.example.test", "api-c.example.test"} {
		if _, err := client.CreateDomainName(context.Background(), &apigateway.CreateDomainNameInput{
			DomainName: aws.String(domainName),
			EndpointConfiguration: &types.EndpointConfiguration{
				Types: []types.EndpointType{types.EndpointTypeRegional},
			},
		}); err != nil {
			t.Fatalf("CreateDomainName(%s) error = %v", domainName, err)
		}
	}

	first, err := client.GetDomainNames(context.Background(), &apigateway.GetDomainNamesInput{Limit: aws.Int32(2)})
	if err != nil {
		t.Fatalf("GetDomainNames(first page) error = %v", err)
	}
	if len(first.Items) != 2 || aws.ToString(first.Position) == "" {
		t.Fatalf("GetDomainNames(first page) = %#v position %q, want 2 items and next position", first.Items, aws.ToString(first.Position))
	}

	second, err := client.GetDomainNames(context.Background(), &apigateway.GetDomainNamesInput{
		Limit:    aws.Int32(2),
		Position: first.Position,
	})
	if err != nil {
		t.Fatalf("GetDomainNames(second page) error = %v", err)
	}
	if len(second.Items) != 1 || aws.ToString(second.Position) != "" {
		t.Fatalf("GetDomainNames(second page) = %#v position %q, want final item and no next position", second.Items, aws.ToString(second.Position))
	}
}

func TestAPIGatewayCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
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
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(runAWS("apigateway", "create-rest-api", "--name", "cli-orders-api", "--description", "orders"), &created); err != nil {
		t.Fatalf("decode create-rest-api output: %v", err)
	}
	if created.ID == "" || created.Name != "cli-orders-api" || created.Description != "orders" {
		t.Fatalf("create-rest-api = %#v, want cli-orders-api", created)
	}

	var got struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(runAWS("apigateway", "get-rest-api", "--rest-api-id", created.ID), &got); err != nil {
		t.Fatalf("decode get-rest-api output: %v", err)
	}
	if got.ID != created.ID || got.Name != "cli-orders-api" {
		t.Fatalf("get-rest-api = %#v, want created API", got)
	}

	var listed struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(runAWS("apigateway", "get-rest-apis"), &listed); err != nil {
		t.Fatalf("decode get-rest-apis output: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0].ID != created.ID {
		t.Fatalf("get-rest-apis = %#v, want created API", listed.Items)
	}

	var updated struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(runAWS("apigateway", "update-rest-api",
		"--rest-api-id", created.ID,
		"--patch-operations", "op=replace,path=/name,value=cli-orders-api-v2",
	), &updated); err != nil {
		t.Fatalf("decode update-rest-api output: %v", err)
	}
	if updated.Name != "cli-orders-api-v2" {
		t.Fatalf("update-rest-api = %#v, want renamed API", updated)
	}

	runAWS("apigateway", "delete-rest-api", "--rest-api-id", created.ID)
	if err := json.Unmarshal(runAWS("apigateway", "get-rest-apis"), &listed); err != nil {
		t.Fatalf("decode get-rest-apis after delete output: %v", err)
	}
	if len(listed.Items) != 0 {
		t.Fatalf("get-rest-apis after delete = %#v, want no APIs", listed.Items)
	}
}

func TestAPIGatewayCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
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
    apigateway = %q
  }
}

resource "aws_api_gateway_rest_api" "deploy" {
  name        = "terraform-orders-api"
  description = "orders"
  tags = {
    env = "test"
  }
}

output "rest_api_id" {
  value = aws_api_gateway_rest_api.deploy.id
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

	if id := strings.TrimSpace(string(runTerraform("output", "-raw", "rest_api_id"))); id == "" {
		t.Fatalf("terraform output rest_api_id is empty")
	}
}

func TestAPIGatewayCompatibilityAdapterRejectsMalformedCreateRestAPI(t *testing.T) {
	server := httptest.NewServer(compataws.NewAPIGatewayAdapter())
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/restapis", strings.NewReader("{"))
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
	if resp.StatusCode != http.StatusBadRequest || body["__type"] != "BadRequestException" {
		t.Fatalf("malformed CreateRestApi = status %d body %#v, want 400 BadRequestException", resp.StatusCode, body)
	}
}
