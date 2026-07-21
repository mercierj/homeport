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
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestDynamoDBCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()

	client := dynamodb.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateTable(context.Background(), &dynamodb.CreateTableInput{
		TableName: aws.String("things"),
		AttributeDefinitions: []types.AttributeDefinition{{
			AttributeName: aws.String("id"),
			AttributeType: types.ScalarAttributeTypeS,
		}},
		KeySchema: []types.KeySchemaElement{{
			AttributeName: aws.String("id"),
			KeyType:       types.KeyTypeHash,
		}},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	if created.TableDescription == nil || aws.ToString(created.TableDescription.TableName) != "things" || created.TableDescription.TableStatus != types.TableStatusActive {
		t.Fatalf("CreateTable() = %#v, want active things table", created.TableDescription)
	}

	described, err := client.DescribeTable(context.Background(), &dynamodb.DescribeTableInput{TableName: aws.String("things")})
	if err != nil {
		t.Fatalf("DescribeTable() error = %v", err)
	}
	if described.Table == nil || aws.ToString(described.Table.TableName) != "things" {
		t.Fatalf("DescribeTable() = %#v, want things table", described.Table)
	}

	if _, err := client.PutItem(context.Background(), &dynamodb.PutItemInput{
		TableName: aws.String("things"),
		Item: map[string]types.AttributeValue{
			"id":    &types.AttributeValueMemberS{Value: "1"},
			"value": &types.AttributeValueMemberS{Value: "hello"},
		},
	}); err != nil {
		t.Fatalf("PutItem() error = %v", err)
	}

	got, err := client.GetItem(context.Background(), &dynamodb.GetItemInput{
		TableName: aws.String("things"),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: "1"},
		},
	})
	if err != nil {
		t.Fatalf("GetItem() error = %v", err)
	}
	if got.Item["value"].(*types.AttributeValueMemberS).Value != "hello" {
		t.Fatalf("GetItem() value = %#v, want hello", got.Item["value"])
	}

	queried, err := client.Query(context.Background(), &dynamodb.QueryInput{
		TableName:              aws.String("things"),
		KeyConditionExpression: aws.String("id = :id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":id": &types.AttributeValueMemberS{Value: "1"},
		},
	})
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(queried.Items) != 1 || queried.Items[0]["value"].(*types.AttributeValueMemberS).Value != "hello" {
		t.Fatalf("Query() items = %#v, want stored item", queried.Items)
	}

	if _, err := client.DeleteTable(context.Background(), &dynamodb.DeleteTableInput{TableName: aws.String("things")}); err != nil {
		t.Fatalf("DeleteTable() error = %v", err)
	}
	_, err = client.DescribeTable(context.Background(), &dynamodb.DescribeTableInput{TableName: aws.String("things")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("DescribeTable(after delete) error = %v, want ResourceNotFoundException", err)
	}
}

func TestDynamoDBCompatibilityAdapterSupportsGlobalSecondaryIndexQueries(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()

	client := dynamoClient(server.URL)
	ctx := context.Background()
	input := dynamoCreateTableInput("indexed-things", func(input *dynamodb.CreateTableInput) {
		input.AttributeDefinitions = append(input.AttributeDefinitions, types.AttributeDefinition{
			AttributeName: aws.String("email"),
			AttributeType: types.ScalarAttributeTypeS,
		})
		input.GlobalSecondaryIndexes = []types.GlobalSecondaryIndex{{
			IndexName: aws.String("email-index"),
			KeySchema: []types.KeySchemaElement{{
				AttributeName: aws.String("email"),
				KeyType:       types.KeyTypeHash,
			}},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		}}
	})
	if _, err := client.CreateTable(ctx, input); err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}

	described, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String("indexed-things")})
	if err != nil {
		t.Fatalf("DescribeTable() error = %v", err)
	}
	if got := described.Table.GlobalSecondaryIndexes; len(got) != 1 || aws.ToString(got[0].IndexName) != "email-index" || got[0].IndexStatus != types.IndexStatusActive {
		t.Fatalf("GlobalSecondaryIndexes = %#v, want active email-index", got)
	}

	if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String("indexed-things"),
		Item: map[string]types.AttributeValue{
			"id":    &types.AttributeValueMemberS{Value: "1"},
			"email": &types.AttributeValueMemberS{Value: "ada@example.com"},
		},
	}); err != nil {
		t.Fatalf("PutItem() error = %v", err)
	}

	queried, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String("indexed-things"),
		IndexName:              aws.String("email-index"),
		KeyConditionExpression: aws.String("email = :email"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":email": &types.AttributeValueMemberS{Value: "ada@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("Query(index) error = %v", err)
	}
	if len(queried.Items) != 1 || queried.Items[0]["id"].(*types.AttributeValueMemberS).Value != "1" {
		t.Fatalf("Query(index) items = %#v, want indexed item", queried.Items)
	}
}

func TestDynamoDBCompatibilityAdapterPaginatesSecondaryIndexQueries(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()
	client := dynamoClient(server.URL)
	ctx := context.Background()
	input := dynamoCreateTableInput("paged-index", func(input *dynamodb.CreateTableInput) {
		input.AttributeDefinitions = append(input.AttributeDefinitions, types.AttributeDefinition{AttributeName: aws.String("email"), AttributeType: types.ScalarAttributeTypeS})
		input.GlobalSecondaryIndexes = []types.GlobalSecondaryIndex{{IndexName: aws.String("email-index"), KeySchema: []types.KeySchemaElement{{AttributeName: aws.String("email"), KeyType: types.KeyTypeHash}}, Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll}}}
	})
	if _, err := client.CreateTable(ctx, input); err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	for _, id := range []string{"1", "2"} {
		if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("paged-index"), Item: map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: id}, "email": &types.AttributeValueMemberS{Value: "ada@example.com"}}}); err != nil {
			t.Fatalf("PutItem(%s) error = %v", id, err)
		}
	}
	query := &dynamodb.QueryInput{TableName: aws.String("paged-index"), IndexName: aws.String("email-index"), KeyConditionExpression: aws.String("email = :email"), ExpressionAttributeValues: map[string]types.AttributeValue{":email": &types.AttributeValueMemberS{Value: "ada@example.com"}}, Limit: aws.Int32(1)}
	first, err := client.Query(ctx, query)
	if err != nil || len(first.Items) != 1 || first.LastEvaluatedKey == nil || first.Items[0]["id"].(*types.AttributeValueMemberS).Value != "1" {
		t.Fatalf("Query(first) = %#v, %v; want id 1 and last key", first, err)
	}
	query.ExclusiveStartKey = first.LastEvaluatedKey
	second, err := client.Query(ctx, query)
	if err != nil || len(second.Items) != 1 || second.LastEvaluatedKey != nil || second.Items[0]["id"].(*types.AttributeValueMemberS).Value != "2" {
		t.Fatalf("Query(second) = %#v, %v; want final id 2", second, err)
	}
	query.ExclusiveStartKey = map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: "missing"}}
	_, err = client.Query(ctx, query)
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
		t.Fatalf("Query(invalid start key) error = %v, want ValidationException", err)
	}
}

func TestDynamoDBCompatibilityAdapterRejectsNonKeyQueryCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()
	client := dynamoClient(server.URL)
	ctx := context.Background()
	if _, err := client.CreateTable(ctx, dynamoCreateTableInput("query-things")); err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	_, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String("query-things"),
		KeyConditionExpression: aws.String("category = :category"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":category": &types.AttributeValueMemberS{Value: "news"},
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
		t.Fatalf("Query(non-key condition) error = %v, want ValidationException", err)
	}
}

func TestDynamoDBCompatibilityAdapterDescribesCreateTimeStreamSpecification(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()

	client := dynamoClient(server.URL)
	ctx := context.Background()
	if _, err := client.CreateTable(ctx, dynamoCreateTableInput("streamed-things", func(input *dynamodb.CreateTableInput) {
		input.StreamSpecification = &types.StreamSpecification{
			StreamEnabled:  aws.Bool(true),
			StreamViewType: types.StreamViewTypeNewAndOldImages,
		}
	})); err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}

	described, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String("streamed-things")})
	if err != nil {
		t.Fatalf("DescribeTable() error = %v", err)
	}
	if described.Table.StreamSpecification == nil || !aws.ToBool(described.Table.StreamSpecification.StreamEnabled) || described.Table.StreamSpecification.StreamViewType != types.StreamViewTypeNewAndOldImages {
		t.Fatalf("StreamSpecification = %#v, want enabled NEW_AND_OLD_IMAGES", described.Table.StreamSpecification)
	}
	if aws.ToString(described.Table.LatestStreamArn) == "" || aws.ToString(described.Table.LatestStreamLabel) == "" {
		t.Fatalf("LatestStreamArn/LatestStreamLabel = %q/%q, want stream identifiers", aws.ToString(described.Table.LatestStreamArn), aws.ToString(described.Table.LatestStreamLabel))
	}
}

func TestDynamoDBCompatibilityAdapterAuthorizesAndAuditsAWSSDKCalls(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewDynamoDBAdapter(
		compataws.WithDynamoDBAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"dynamodb:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"dynamodb:PutItem"}, Resources: []string{"*"}},
		)),
		compataws.WithDynamoDBAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := dynamodb.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String("things"),
		AttributeDefinitions: []types.AttributeDefinition{{
			AttributeName: aws.String("id"),
			AttributeType: types.ScalarAttributeTypeS,
		}},
		KeySchema: []types.KeySchemaElement{{
			AttributeName: aws.String("id"),
			KeyType:       types.KeyTypeHash,
		}},
		BillingMode: types.BillingModePayPerRequest,
	}); err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}

	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String("things"),
		Item: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: "1"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("PutItem() error = %v, want AccessDenied", err)
	}

	got, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String("things"),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: "1"},
		},
	})
	if err != nil {
		t.Fatalf("GetItem() error = %v", err)
	}
	if len(got.Item) != 0 {
		t.Fatalf("GetItem() = %#v, want denied PutItem to leave no item", got.Item)
	}

	assertDecision(t, auditLog.Decisions(), "dynamodb:CreateTable", true)
	assertDecision(t, auditLog.Decisions(), "dynamodb:PutItem", false)
	assertDecision(t, auditLog.Decisions(), "dynamodb:GetItem", true)
}

func TestDynamoDBCompatibilityAdapterAuthorizesAndAuditsSupportedActions(t *testing.T) {
	cases := []struct {
		action string
		call   func(context.Context, *dynamodb.Client) error
	}{
		{
			action: "CreateTable",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.CreateTable(ctx, dynamoCreateTableInput("things"))
				return err
			},
		},
		{
			action: "DescribeTable",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String("things")})
				return err
			},
		},
		{
			action: "ListTables",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.ListTables(ctx, &dynamodb.ListTablesInput{})
				return err
			},
		},
		{
			action: "PutItem",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
					TableName: aws.String("things"),
					Item: map[string]types.AttributeValue{
						"id": &types.AttributeValueMemberS{Value: "1"},
					},
				})
				return err
			},
		},
		{
			action: "GetItem",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.GetItem(ctx, &dynamodb.GetItemInput{
					TableName: aws.String("things"),
					Key: map[string]types.AttributeValue{
						"id": &types.AttributeValueMemberS{Value: "1"},
					},
				})
				return err
			},
		},
		{
			action: "Query",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.Query(ctx, &dynamodb.QueryInput{
					TableName:              aws.String("things"),
					KeyConditionExpression: aws.String("id = :id"),
					ExpressionAttributeValues: map[string]types.AttributeValue{
						":id": &types.AttributeValueMemberS{Value: "1"},
					},
				})
				return err
			},
		},
		{
			action: "Scan",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.Scan(ctx, &dynamodb.ScanInput{TableName: aws.String("things")})
				return err
			},
		},
		{
			action: "DescribeTimeToLive",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{TableName: aws.String("things")})
				return err
			},
		},
		{
			action: "ListTagsOfResource",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.ListTagsOfResource(ctx, &dynamodb.ListTagsOfResourceInput{
					ResourceArn: aws.String("arn:aws:dynamodb:us-east-1:000000000000:table/things"),
				})
				return err
			},
		},
		{
			action: "TagResource",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.TagResource(ctx, &dynamodb.TagResourceInput{
					ResourceArn: aws.String("arn:aws:dynamodb:us-east-1:000000000000:table/things"),
					Tags:        []types.Tag{{Key: aws.String("env"), Value: aws.String("test")}},
				})
				return err
			},
		},
		{
			action: "UntagResource",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.UntagResource(ctx, &dynamodb.UntagResourceInput{
					ResourceArn: aws.String("arn:aws:dynamodb:us-east-1:000000000000:table/things"),
					TagKeys:     []string{"env"},
				})
				return err
			},
		},
		{
			action: "DeleteTable",
			call: func(ctx context.Context, client *dynamodb.Client) error {
				_, err := client.DeleteTable(ctx, &dynamodb.DeleteTableInput{TableName: aws.String("things")})
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			auditLog := authz.NewAuditLog()
			server := httptest.NewServer(compataws.NewDynamoDBAdapter(
				compataws.WithDynamoDBAuthorizer(authz.NewPolicyAuthorizer(
					authz.Rule{Effect: authz.Allow, Actions: []string{"dynamodb:*"}, Resources: []string{"*"}},
					authz.Rule{Effect: authz.Deny, Actions: []string{"dynamodb:" + tc.action}, Resources: []string{"*"}},
				)),
				compataws.WithDynamoDBAuditSink(auditLog.Record),
			))
			defer server.Close()

			client := dynamoClient(server.URL)
			ctx := context.Background()
			if tc.action != "CreateTable" {
				if _, err := client.CreateTable(ctx, dynamoCreateTableInput("things")); err != nil {
					t.Fatalf("CreateTable(setup) error = %v", err)
				}
			}

			err := tc.call(ctx, client)
			if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
				t.Fatalf("%s() error = %v, want AccessDenied", tc.action, err)
			}

			assertDecision(t, auditLog.Decisions(), "dynamodb:"+tc.action, false)
		})
	}
}

func TestDynamoDBCompatibilityAdapterManagesTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()

	client := dynamoClient(server.URL)
	ctx := context.Background()
	input := dynamoCreateTableInput("tagged-things", func(input *dynamodb.CreateTableInput) {
		input.Tags = []types.Tag{{Key: aws.String("env"), Value: aws.String("test")}}
	})
	created, err := client.CreateTable(ctx, input)
	if err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	arn := aws.ToString(created.TableDescription.TableArn)

	tags, err := client.ListTagsOfResource(ctx, &dynamodb.ListTagsOfResourceInput{ResourceArn: aws.String(arn)})
	if err != nil {
		t.Fatalf("ListTagsOfResource() error = %v", err)
	}
	if got := dynamoTagMap(tags.Tags); got["env"] != "test" {
		t.Fatalf("ListTagsOfResource() = %#v, want env=test", got)
	}

	if _, err := client.TagResource(ctx, &dynamodb.TagResourceInput{
		ResourceArn: aws.String(arn),
		Tags:        []types.Tag{{Key: aws.String("owner"), Value: aws.String("platform")}},
	}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	tags, err = client.ListTagsOfResource(ctx, &dynamodb.ListTagsOfResourceInput{ResourceArn: aws.String(arn)})
	if err != nil {
		t.Fatalf("ListTagsOfResource(after tag) error = %v", err)
	}
	if got := dynamoTagMap(tags.Tags); got["env"] != "test" || got["owner"] != "platform" {
		t.Fatalf("ListTagsOfResource(after tag) = %#v, want env/owner tags", got)
	}

	if _, err := client.UntagResource(ctx, &dynamodb.UntagResourceInput{ResourceArn: aws.String(arn), TagKeys: []string{"env"}}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	tags, err = client.ListTagsOfResource(ctx, &dynamodb.ListTagsOfResourceInput{ResourceArn: aws.String(arn)})
	if err != nil {
		t.Fatalf("ListTagsOfResource(after untag) error = %v", err)
	}
	if got := dynamoTagMap(tags.Tags); got["env"] != "" || got["owner"] != "platform" {
		t.Fatalf("ListTagsOfResource(after untag) = %#v, want owner only", got)
	}
}

func TestDynamoDBCompatibilityAdapterReturnsLimitExceededWhenTableQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter(compataws.WithDynamoDBTableQuota(1)))
	defer server.Close()

	client := dynamoClient(server.URL)
	ctx := context.Background()
	if _, err := client.CreateTable(ctx, dynamoCreateTableInput("things-a")); err != nil {
		t.Fatalf("CreateTable(first) error = %v", err)
	}

	_, err := client.CreateTable(ctx, dynamoCreateTableInput("things-b"))
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("CreateTable(over quota) error = %v, want LimitExceededException", err)
	}

	_, err = client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String("things-b")})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("DescribeTable(rejected table) error = %v, want ResourceNotFoundException", err)
	}
}

func TestDynamoDBCompatibilityAdapterRejectsInvalidCreateTableName(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()

	client := dynamoClient(server.URL)
	ctx := context.Background()
	for _, name := range []string{"ab", "bad/name"} {
		_, err := client.CreateTable(ctx, dynamoCreateTableInput(name))
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
			t.Fatalf("CreateTable(%q) error = %v, want ValidationException", name, err)
		}
	}

	out, err := client.ListTables(ctx, &dynamodb.ListTablesInput{})
	if err != nil {
		t.Fatalf("ListTables error = %v", err)
	}
	if len(out.TableNames) != 0 {
		t.Fatalf("ListTables after rejected creates = %v, want no tables", out.TableNames)
	}
}

func TestDynamoDBCompatibilityAdapterPaginatesListTablesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()

	client := dynamoClient(server.URL)
	ctx := context.Background()
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		if _, err := client.CreateTable(ctx, dynamoCreateTableInput(name)); err != nil {
			t.Fatalf("CreateTable(%s) error = %v", name, err)
		}
	}

	first, err := client.ListTables(ctx, &dynamodb.ListTablesInput{Limit: aws.Int32(2)})
	if err != nil {
		t.Fatalf("ListTables(first) error = %v", err)
	}
	if got := first.TableNames; len(got) != 2 || got[0] != "alpha" || got[1] != "bravo" || aws.ToString(first.LastEvaluatedTableName) != "bravo" {
		t.Fatalf("ListTables(first) = names %v last %q, want alpha/bravo last bravo", got, aws.ToString(first.LastEvaluatedTableName))
	}

	second, err := client.ListTables(ctx, &dynamodb.ListTablesInput{ExclusiveStartTableName: first.LastEvaluatedTableName, Limit: aws.Int32(2)})
	if err != nil {
		t.Fatalf("ListTables(second) error = %v", err)
	}
	if got := second.TableNames; len(got) != 1 || got[0] != "charlie" || second.LastEvaluatedTableName != nil {
		t.Fatalf("ListTables(second) = names %v last %v, want charlie and no last table", got, second.LastEvaluatedTableName)
	}
}

func TestDynamoDBCompatibilityAdapterScansItemsWithPagination(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()

	client := dynamoClient(server.URL)
	ctx := context.Background()
	if _, err := client.CreateTable(ctx, dynamoCreateTableInput("scanned-things")); err != nil {
		t.Fatalf("CreateTable() error = %v", err)
	}
	for _, id := range []string{"1", "2"} {
		if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("scanned-things"), Item: map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: id}}}); err != nil {
			t.Fatalf("PutItem(%q) error = %v", id, err)
		}
	}

	first, err := client.Scan(ctx, &dynamodb.ScanInput{TableName: aws.String("scanned-things"), Limit: aws.Int32(1)})
	if err != nil || first.Count != 1 || len(first.Items) != 1 || first.LastEvaluatedKey == nil {
		t.Fatalf("Scan(first) = %#v, %v; want one item and a last key", first, err)
	}
	second, err := client.Scan(ctx, &dynamodb.ScanInput{TableName: aws.String("scanned-things"), Limit: aws.Int32(1), ExclusiveStartKey: first.LastEvaluatedKey})
	if err != nil || second.Count != 1 || len(second.Items) != 1 || second.LastEvaluatedKey != nil {
		t.Fatalf("Scan(second) = %#v, %v; want final item", second, err)
	}
}

func TestDynamoDBCompatibilityAdapterRejectsInvalidListTablesLimit(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()

	client := dynamoClient(server.URL)
	for _, limit := range []int32{0, 101} {
		_, err := client.ListTables(context.Background(), &dynamodb.ListTablesInput{Limit: aws.Int32(limit)})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
			t.Fatalf("ListTables(Limit=%d) error = %v, want ValidationException", limit, err)
		}
	}
}

func TestDynamoDBCompatibilityAdapterRejectsMalformedListTablesStartName(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
	defer server.Close()

	client := dynamoClient(server.URL)
	_, err := client.ListTables(context.Background(), &dynamodb.ListTablesInput{ExclusiveStartTableName: aws.String("bad/name")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
		t.Fatalf("ListTables(malformed ExclusiveStartTableName) error = %v, want ValidationException", err)
	}
}

func dynamoClient(endpoint string) *dynamodb.Client {
	return dynamodb.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
}

func dynamoCreateTableInput(name string, options ...func(*dynamodb.CreateTableInput)) *dynamodb.CreateTableInput {
	input := &dynamodb.CreateTableInput{
		TableName: aws.String(name),
		AttributeDefinitions: []types.AttributeDefinition{{
			AttributeName: aws.String("id"),
			AttributeType: types.ScalarAttributeTypeS,
		}},
		KeySchema: []types.KeySchemaElement{{
			AttributeName: aws.String("id"),
			KeyType:       types.KeyTypeHash,
		}},
		BillingMode: types.BillingModePayPerRequest,
	}
	for _, option := range options {
		option(input)
	}
	return input
}

func dynamoTagMap(tags []types.Tag) map[string]string {
	out := map[string]string{}
	for _, tag := range tags {
		out[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return out
}

func TestDynamoDBCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
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
		TableDescription struct {
			TableName   string `json:"TableName"`
			TableStatus string `json:"TableStatus"`
		} `json:"TableDescription"`
	}
	if err := json.Unmarshal(runAWS(
		"dynamodb", "create-table",
		"--table-name", "cli-things",
		"--attribute-definitions", "AttributeName=id,AttributeType=S",
		"--key-schema", "AttributeName=id,KeyType=HASH",
		"--billing-mode", "PAY_PER_REQUEST",
	), &created); err != nil {
		t.Fatalf("decode create-table output: %v", err)
	}
	if created.TableDescription.TableName != "cli-things" || created.TableDescription.TableStatus != "ACTIVE" {
		t.Fatalf("create-table = %#v, want active cli-things table", created.TableDescription)
	}

	var described struct {
		Table struct {
			TableName string `json:"TableName"`
		} `json:"Table"`
	}
	if err := json.Unmarshal(runAWS("dynamodb", "describe-table", "--table-name", "cli-things"), &described); err != nil {
		t.Fatalf("decode describe-table output: %v", err)
	}
	if described.Table.TableName != "cli-things" {
		t.Fatalf("describe-table = %#v, want cli-things", described.Table)
	}

	runAWS("dynamodb", "put-item",
		"--table-name", "cli-things",
		"--item", `{"id":{"S":"1"},"value":{"S":"hello"}}`,
	)

	var got struct {
		Item map[string]map[string]string `json:"Item"`
	}
	if err := json.Unmarshal(runAWS("dynamodb", "get-item",
		"--table-name", "cli-things",
		"--key", `{"id":{"S":"1"}}`,
	), &got); err != nil {
		t.Fatalf("decode get-item output: %v", err)
	}
	if got.Item["value"]["S"] != "hello" {
		t.Fatalf("get-item = %#v, want hello value", got.Item)
	}

	var queried struct {
		Items []map[string]map[string]string `json:"Items"`
	}
	if err := json.Unmarshal(runAWS("dynamodb", "query",
		"--table-name", "cli-things",
		"--key-condition-expression", "id = :id",
		"--expression-attribute-values", `{":id":{"S":"1"}}`,
	), &queried); err != nil {
		t.Fatalf("decode query output: %v", err)
	}
	if len(queried.Items) != 1 || queried.Items[0]["value"]["S"] != "hello" {
		t.Fatalf("query = %#v, want stored item", queried.Items)
	}

	runAWS("dynamodb", "delete-table", "--table-name", "cli-things")
}

func TestDynamoDBCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewDynamoDBAdapter())
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
    dynamodb = %q
  }
}

resource "aws_dynamodb_table" "deploy" {
  name         = "terraform-things"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "id"

  attribute {
    name = "id"
    type = "S"
  }
}

output "table_arn" {
  value = aws_dynamodb_table.deploy.arn
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

	out := runTerraform("output", "-raw", "table_arn")
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("terraform output table_arn is empty")
	}
}

func TestDynamoDBCompatibilityAdapterShapesAuthorizerFailure(t *testing.T) {
	server := httptest.NewServer(compataws.NewDynamoDBAdapter(compataws.WithDynamoDBAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
		return authz.Decision{}, errors.New("authorizer unavailable")
	}))))
	defer server.Close()

	client := dynamodb.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *dynamodb.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListTables(context.Background(), &dynamodb.ListTablesInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InternalServerError" {
		t.Fatalf("ListTables(authorizer failure) error = %v, want InternalServerError", err)
	}
}
