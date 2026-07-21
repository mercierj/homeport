package compat_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/appsync"
	"github.com/aws/aws-sdk-go-v2/service/appsync/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestAppSyncCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateGraphqlApi(ctx, &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey})
	if err != nil || aws.ToString(created.GraphqlApi.ApiId) == "" {
		t.Fatalf("CreateGraphqlApi() = %#v, %v", created, err)
	}
	got, err := client.GetGraphqlApi(ctx, &appsync.GetGraphqlApiInput{ApiId: created.GraphqlApi.ApiId})
	if err != nil || aws.ToString(got.GraphqlApi.Name) != "orders" {
		t.Fatalf("GetGraphqlApi() = %#v, %v", got, err)
	}
	listed, err := client.ListGraphqlApis(ctx, &appsync.ListGraphqlApisInput{})
	if err != nil || len(listed.GraphqlApis) != 1 {
		t.Fatalf("ListGraphqlApis() = %#v, %v", listed, err)
	}
	if _, err := client.UpdateGraphqlApi(ctx, &appsync.UpdateGraphqlApiInput{ApiId: created.GraphqlApi.ApiId, Name: aws.String("orders-v2"), AuthenticationType: types.AuthenticationTypeApiKey}); err != nil {
		t.Fatalf("UpdateGraphqlApi() error = %v", err)
	}
	if _, err := client.DeleteGraphqlApi(ctx, &appsync.DeleteGraphqlApiInput{ApiId: created.GraphqlApi.ApiId}); err != nil {
		t.Fatalf("DeleteGraphqlApi() error = %v", err)
	}
}

func TestAppSyncCompatibilityAdapterPaginatesGraphqlAPIs(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	for _, name := range []string{"alpha", "bravo"} {
		if _, err := client.CreateGraphqlApi(ctx, &appsync.CreateGraphqlApiInput{Name: aws.String(name), AuthenticationType: types.AuthenticationTypeApiKey}); err != nil {
			t.Fatal(err)
		}
	}

	first, err := client.ListGraphqlApis(ctx, &appsync.ListGraphqlApisInput{MaxResults: 1})
	if err != nil || len(first.GraphqlApis) != 1 || aws.ToString(first.GraphqlApis[0].Name) != "alpha" || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListGraphqlApis(first page) = %#v, %v", first, err)
	}
	second, err := client.ListGraphqlApis(ctx, &appsync.ListGraphqlApisInput{MaxResults: 1, NextToken: first.NextToken})
	if err != nil || len(second.GraphqlApis) != 1 || aws.ToString(second.GraphqlApis[0].Name) != "bravo" || second.NextToken != nil {
		t.Fatalf("ListGraphqlApis(second page) = %#v, %v", second, err)
	}
}

func TestAppSyncCompatibilityAdapterRejectsInvalidGraphqlAPIPagination(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	for _, input := range []*appsync.ListGraphqlApisInput{
		{NextToken: aws.String("unknown")},
		{MaxResults: 26},
	} {
		_, err := client.ListGraphqlApis(context.Background(), input)
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "BadRequestException" {
			t.Fatalf("ListGraphqlApis(%#v) error = %v, want BadRequestException", input, err)
		}
	}
}

func TestAppSyncCompatibilityAdapterManagesGraphqlAPITags(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateGraphqlApi(ctx, &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey, Tags: map[string]string{"team": "platform"}})
	if err != nil {
		t.Fatal(err)
	}
	tags, err := client.ListTagsForResource(ctx, &appsync.ListTagsForResourceInput{ResourceArn: created.GraphqlApi.Arn})
	if err != nil || tags.Tags["team"] != "platform" {
		t.Fatalf("ListTagsForResource() = %#v, %v; want create-time tag", tags, err)
	}
	if _, err := client.TagResource(ctx, &appsync.TagResourceInput{ResourceArn: created.GraphqlApi.Arn, Tags: map[string]string{"environment": "test"}}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	if _, err := client.UntagResource(ctx, &appsync.UntagResourceInput{ResourceArn: created.GraphqlApi.Arn, TagKeys: []string{"team"}}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	tags, err = client.ListTagsForResource(ctx, &appsync.ListTagsForResourceInput{ResourceArn: created.GraphqlApi.Arn})
	if err != nil || len(tags.Tags) != 1 || tags.Tags["environment"] != "test" {
		t.Fatalf("ListTagsForResource(after update) = %#v, %v; want remaining tag", tags, err)
	}
}

func TestAppSyncCompatibilityAdapterManagesAPIKeys(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	api, err := client.CreateGraphqlApi(ctx, &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey})
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateApiKey(ctx, &appsync.CreateApiKeyInput{ApiId: api.GraphqlApi.ApiId, Description: aws.String("initial"), Expires: 1_900_000_000})
	if err != nil || aws.ToString(created.ApiKey.Id) == "" || aws.ToString(created.ApiKey.Description) != "initial" {
		t.Fatalf("CreateApiKey() = %#v, %v", created, err)
	}
	listed, err := client.ListApiKeys(ctx, &appsync.ListApiKeysInput{ApiId: api.GraphqlApi.ApiId})
	if err != nil || len(listed.ApiKeys) != 1 || aws.ToString(listed.ApiKeys[0].Id) != aws.ToString(created.ApiKey.Id) {
		t.Fatalf("ListApiKeys() = %#v, %v", listed, err)
	}
	updated, err := client.UpdateApiKey(ctx, &appsync.UpdateApiKeyInput{ApiId: api.GraphqlApi.ApiId, Id: created.ApiKey.Id, Description: aws.String("updated"), Expires: 1_900_003_600})
	if err != nil || aws.ToString(updated.ApiKey.Description) != "updated" || updated.ApiKey.Expires != 1_900_003_600 {
		t.Fatalf("UpdateApiKey() = %#v, %v", updated, err)
	}
	if _, err := client.DeleteApiKey(ctx, &appsync.DeleteApiKeyInput{ApiId: api.GraphqlApi.ApiId, Id: created.ApiKey.Id}); err != nil {
		t.Fatalf("DeleteApiKey() error = %v", err)
	}
}

func TestAppSyncCompatibilityAdapterPaginatesAPIKeys(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	api, err := client.CreateGraphqlApi(ctx, &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey})
	if err != nil {
		t.Fatal(err)
	}
	for _, description := range []string{"first", "second"} {
		if _, err := client.CreateApiKey(ctx, &appsync.CreateApiKeyInput{ApiId: api.GraphqlApi.ApiId, Description: aws.String(description), Expires: 1_900_000_000}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := client.ListApiKeys(ctx, &appsync.ListApiKeysInput{ApiId: api.GraphqlApi.ApiId, MaxResults: 1})
	if err != nil || len(first.ApiKeys) != 1 || aws.ToString(first.ApiKeys[0].Description) != "first" || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListApiKeys(first page) = %#v, %v", first, err)
	}
	second, err := client.ListApiKeys(ctx, &appsync.ListApiKeysInput{ApiId: api.GraphqlApi.ApiId, MaxResults: 1, NextToken: first.NextToken})
	if err != nil || len(second.ApiKeys) != 1 || aws.ToString(second.ApiKeys[0].Description) != "second" || second.NextToken != nil {
		t.Fatalf("ListApiKeys(second page) = %#v, %v", second, err)
	}
}

func TestAppSyncCompatibilityAdapterRetainsXRayConfiguration(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateGraphqlApi(ctx, &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey, XrayEnabled: true})
	if err != nil || !created.GraphqlApi.XrayEnabled {
		t.Fatalf("CreateGraphqlApi() = %#v, %v; want X-Ray enabled", created, err)
	}
	updated, err := client.UpdateGraphqlApi(ctx, &appsync.UpdateGraphqlApiInput{ApiId: created.GraphqlApi.ApiId, Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey, XrayEnabled: false})
	if err != nil || updated.GraphqlApi.XrayEnabled {
		t.Fatalf("UpdateGraphqlApi() = %#v, %v; want X-Ray disabled", updated, err)
	}
}

func TestAppSyncCompatibilityAdapterRetainsIntrospectionConfiguration(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateGraphqlApi(ctx, &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey, IntrospectionConfig: types.GraphQLApiIntrospectionConfigDisabled})
	if err != nil || created.GraphqlApi.IntrospectionConfig != types.GraphQLApiIntrospectionConfigDisabled {
		t.Fatalf("CreateGraphqlApi() = %#v, %v; want disabled introspection", created, err)
	}
	updated, err := client.UpdateGraphqlApi(ctx, &appsync.UpdateGraphqlApiInput{ApiId: created.GraphqlApi.ApiId, Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey, IntrospectionConfig: types.GraphQLApiIntrospectionConfigEnabled})
	if err != nil || updated.GraphqlApi.IntrospectionConfig != types.GraphQLApiIntrospectionConfigEnabled {
		t.Fatalf("UpdateGraphqlApi() = %#v, %v; want enabled introspection", updated, err)
	}
}

func TestAppSyncCompatibilityAdapterManagesDataSources(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	api, err := client.CreateGraphqlApi(ctx, &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey})
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateDataSource(ctx, &appsync.CreateDataSourceInput{ApiId: api.GraphqlApi.ApiId, Name: aws.String("orders-source"), Type: types.DataSourceTypeNone, Description: aws.String("initial")})
	if err != nil || aws.ToString(created.DataSource.Name) != "orders-source" || created.DataSource.Type != types.DataSourceTypeNone {
		t.Fatalf("CreateDataSource() = %#v, %v", created, err)
	}
	fetched, err := client.GetDataSource(ctx, &appsync.GetDataSourceInput{ApiId: api.GraphqlApi.ApiId, Name: aws.String("orders-source")})
	if err != nil || aws.ToString(fetched.DataSource.Description) != "initial" {
		t.Fatalf("GetDataSource() = %#v, %v", fetched, err)
	}
	listed, err := client.ListDataSources(ctx, &appsync.ListDataSourcesInput{ApiId: api.GraphqlApi.ApiId})
	if err != nil || len(listed.DataSources) != 1 {
		t.Fatalf("ListDataSources() = %#v, %v", listed, err)
	}
	updated, err := client.UpdateDataSource(ctx, &appsync.UpdateDataSourceInput{ApiId: api.GraphqlApi.ApiId, Name: aws.String("orders-source"), Type: types.DataSourceTypeNone, Description: aws.String("updated")})
	if err != nil || aws.ToString(updated.DataSource.Description) != "updated" {
		t.Fatalf("UpdateDataSource() = %#v, %v", updated, err)
	}
	if _, err := client.DeleteDataSource(ctx, &appsync.DeleteDataSourceInput{ApiId: api.GraphqlApi.ApiId, Name: aws.String("orders-source")}); err != nil {
		t.Fatalf("DeleteDataSource() error = %v", err)
	}
}

func TestAppSyncCompatibilityAdapterPaginatesDataSources(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	api, err := client.CreateGraphqlApi(ctx, &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "bravo"} {
		if _, err := client.CreateDataSource(ctx, &appsync.CreateDataSourceInput{ApiId: api.GraphqlApi.ApiId, Name: aws.String(name), Type: types.DataSourceTypeNone}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := client.ListDataSources(ctx, &appsync.ListDataSourcesInput{ApiId: api.GraphqlApi.ApiId, MaxResults: 1})
	if err != nil || len(first.DataSources) != 1 || aws.ToString(first.DataSources[0].Name) != "alpha" || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListDataSources(first page) = %#v, %v", first, err)
	}
	second, err := client.ListDataSources(ctx, &appsync.ListDataSourcesInput{ApiId: api.GraphqlApi.ApiId, MaxResults: 1, NextToken: first.NextToken})
	if err != nil || len(second.DataSources) != 1 || aws.ToString(second.DataSources[0].Name) != "bravo" || second.NextToken != nil {
		t.Fatalf("ListDataSources(second page) = %#v, %v", second, err)
	}
}

func TestAppSyncCompatibilityAdapterCreatesAndGetsResolverMetadata(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	api, err := client.CreateGraphqlApi(ctx, &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey})
	if err != nil {
		t.Fatal(err)
	}
	created, err := client.CreateResolver(ctx, &appsync.CreateResolverInput{ApiId: api.GraphqlApi.ApiId, TypeName: aws.String("Query"), FieldName: aws.String("order"), Kind: types.ResolverKindUnit})
	if err != nil || aws.ToString(created.Resolver.TypeName) != "Query" || aws.ToString(created.Resolver.FieldName) != "order" {
		t.Fatalf("CreateResolver() = %#v, %v", created, err)
	}
	got, err := client.GetResolver(ctx, &appsync.GetResolverInput{ApiId: api.GraphqlApi.ApiId, TypeName: aws.String("Query"), FieldName: aws.String("order")})
	if err != nil || got.Resolver.Kind != types.ResolverKindUnit {
		t.Fatalf("GetResolver() = %#v, %v", got, err)
	}
}

func TestAppSyncCompatibilityAdapterRejectsInvalidAuthenticationType(t *testing.T) {
	server := httptest.NewServer(compataws.NewAppSyncAdapter())
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.CreateGraphqlApi(context.Background(), &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationType("INVALID")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "BadRequestException" {
		t.Fatalf("CreateGraphqlApi(invalid authentication type) error = %v, want BadRequestException", err)
	}
}

func TestAppSyncCompatibilityAdapterAuthorizesCreation(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewAppSyncAdapter(
		compataws.WithAppSyncAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{Effect: authz.Deny, Actions: []string{"appsync:CreateGraphqlApi"}, Resources: []string{"*"}})),
		compataws.WithAppSyncAuditSink(auditLog.Record),
	))
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.CreateGraphqlApi(context.Background(), &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDeniedException" {
		t.Fatalf("CreateGraphqlApi(denied) error = %v, want AccessDeniedException", err)
	}
	assertDecision(t, auditLog.Decisions(), "appsync:CreateGraphqlApi", false)
}

func TestAppSyncCompatibilityAdapterAuthorizesNamedCreation(t *testing.T) {
	ordersARN := "arn:aws:appsync:us-east-1:000000000000:apis/orders"
	server := httptest.NewServer(compataws.NewAppSyncAdapter(compataws.WithAppSyncAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{Effect: authz.Allow, Actions: []string{"appsync:CreateGraphqlApi"}, Resources: []string{ordersARN}}))))
	defer server.Close()
	client := appsync.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *appsync.Options) { o.BaseEndpoint = aws.String(server.URL) })
	created, err := client.CreateGraphqlApi(context.Background(), &appsync.CreateGraphqlApiInput{Name: aws.String("orders"), AuthenticationType: types.AuthenticationTypeApiKey})
	if err != nil || aws.ToString(created.GraphqlApi.ApiId) != "orders" {
		t.Fatalf("CreateGraphqlApi(orders) = %#v, %v", created, err)
	}
}
