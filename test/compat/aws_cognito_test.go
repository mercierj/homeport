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
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestCognitoCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()

	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateUserPool(context.Background(), &cognitoidentityprovider.CreateUserPoolInput{
		PoolName: aws.String("customers"),
	})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	if created.UserPool == nil || aws.ToString(created.UserPool.Id) == "" || aws.ToString(created.UserPool.Name) != "customers" {
		t.Fatalf("CreateUserPool() = %#v, want customers user pool", created.UserPool)
	}
	poolID := aws.ToString(created.UserPool.Id)

	described, err := client.DescribeUserPool(context.Background(), &cognitoidentityprovider.DescribeUserPoolInput{
		UserPoolId: aws.String(poolID),
	})
	if err != nil {
		t.Fatalf("DescribeUserPool() error = %v", err)
	}
	if described.UserPool == nil || aws.ToString(described.UserPool.Name) != "customers" {
		t.Fatalf("DescribeUserPool() = %#v, want customers user pool", described.UserPool)
	}

	listed, err := client.ListUserPools(context.Background(), &cognitoidentityprovider.ListUserPoolsInput{
		MaxResults: aws.Int32(10),
	})
	if err != nil {
		t.Fatalf("ListUserPools() error = %v", err)
	}
	if len(listed.UserPools) != 1 || aws.ToString(listed.UserPools[0].Id) != poolID {
		t.Fatalf("ListUserPools() = %#v, want created pool", listed.UserPools)
	}

	if _, err := client.UpdateUserPool(context.Background(), &cognitoidentityprovider.UpdateUserPoolInput{
		UserPoolId:       aws.String(poolID),
		MfaConfiguration: types.UserPoolMfaTypeOptional,
	}); err != nil {
		t.Fatalf("UpdateUserPool() error = %v", err)
	}
	described, err = client.DescribeUserPool(context.Background(), &cognitoidentityprovider.DescribeUserPoolInput{
		UserPoolId: aws.String(poolID),
	})
	if err != nil {
		t.Fatalf("DescribeUserPool(after update) error = %v", err)
	}
	if described.UserPool == nil || described.UserPool.MfaConfiguration != types.UserPoolMfaTypeOptional {
		t.Fatalf("DescribeUserPool(after update) = %#v, want optional MFA", described.UserPool)
	}

	if _, err := client.DeleteUserPool(context.Background(), &cognitoidentityprovider.DeleteUserPoolInput{
		UserPoolId: aws.String(poolID),
	}); err != nil {
		t.Fatalf("DeleteUserPool() error = %v", err)
	}
	_, err = client.DescribeUserPool(context.Background(), &cognitoidentityprovider.DescribeUserPoolInput{
		UserPoolId: aws.String(poolID),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("DescribeUserPool(after delete) error = %v, want ResourceNotFoundException", err)
	}
}

func TestCognitoCompatibilityAdapterRejectsInvalidMFAConfiguration(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *cognitoidentityprovider.Options) { o.BaseEndpoint = aws.String(server.URL) })
	pool, err := client.CreateUserPool(context.Background(), &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	_, err = client.UpdateUserPool(context.Background(), &cognitoidentityprovider.UpdateUserPoolInput{UserPoolId: pool.UserPool.Id, MfaConfiguration: types.UserPoolMfaType("INVALID")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("UpdateUserPool(invalid MFA) error = %v, want InvalidParameterException", err)
	}
	described, err := client.DescribeUserPool(context.Background(), &cognitoidentityprovider.DescribeUserPoolInput{UserPoolId: pool.UserPool.Id})
	if err != nil || described.UserPool.MfaConfiguration != types.UserPoolMfaTypeOff {
		t.Fatalf("DescribeUserPool(after invalid update) = %#v, %v; want MFA OFF", described, err)
	}
}

func TestCognitoCompatibilityAdapterAuthorizesAndAuditsCreateUserPool(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewCognitoAdapter(
		compataws.WithCognitoAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"cognito-idp:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"cognito-idp:CreateUserPool"}, Resources: []string{"*"}},
		)),
		compataws.WithCognitoAuditSink(auditLog.Record),
	))
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	_, err := client.CreateUserPool(context.Background(), &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NotAuthorizedException" {
		t.Fatalf("CreateUserPool(denied) error = %v, want NotAuthorizedException", err)
	}
	assertDecision(t, auditLog.Decisions(), "cognito-idp:CreateUserPool", false)
	listed, err := client.ListUserPools(context.Background(), &cognitoidentityprovider.ListUserPoolsInput{MaxResults: aws.Int32(10)})
	if err != nil {
		t.Fatalf("ListUserPools(after denied create) error = %v", err)
	}
	if len(listed.UserPools) != 0 {
		t.Fatalf("ListUserPools(after denied create) = %#v, want no pools", listed.UserPools)
	}
}

func TestCognitoCompatibilityAdapterEnforcesUserPoolQuota(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter(compataws.WithCognitoUserPoolQuota(1)))
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	if _, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("first")}); err != nil {
		t.Fatalf("CreateUserPool(first) error = %v", err)
	}
	_, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("second")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("CreateUserPool(over quota) error = %v, want LimitExceededException", err)
	}
}

func TestCognitoCompatibilityAdapterManagesUserPoolTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *cognitoidentityprovider.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	pool, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.TagResource(ctx, &cognitoidentityprovider.TagResourceInput{ResourceArn: pool.UserPool.Arn, Tags: map[string]string{"env": "prod"}}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	tags, err := client.ListTagsForResource(ctx, &cognitoidentityprovider.ListTagsForResourceInput{ResourceArn: pool.UserPool.Arn})
	if err != nil || tags.Tags["env"] != "prod" {
		t.Fatalf("ListTagsForResource() = %#v, %v", tags, err)
	}
	if _, err := client.UntagResource(ctx, &cognitoidentityprovider.UntagResourceInput{ResourceArn: pool.UserPool.Arn, TagKeys: []string{"env"}}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
}

func TestCognitoCompatibilityAdapterAuthorizesTagsByResourceARN(t *testing.T) {
	var protectedARN string
	server := httptest.NewServer(compataws.NewCognitoAdapter(compataws.WithCognitoAuthorizer(authz.AuthorizerFunc(func(_ context.Context, req authz.Request) (authz.Decision, error) {
		return authz.Decision{Request: req, Allowed: !(req.Action == "cognito-idp:TagResource" && req.Resource == protectedARN)}, nil
	}))))
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *cognitoidentityprovider.Options) { o.BaseEndpoint = aws.String(server.URL) })
	pool, err := client.CreateUserPool(context.Background(), &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatal(err)
	}
	protectedARN = aws.ToString(pool.UserPool.Arn)
	_, err = client.TagResource(context.Background(), &cognitoidentityprovider.TagResourceInput{ResourceArn: pool.UserPool.Arn, Tags: map[string]string{"env": "prod"}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NotAuthorizedException" {
		t.Fatalf("TagResource(denied) error = %v, want NotAuthorizedException", err)
	}
}

func TestCognitoCompatibilityAdapterPaginatesUserPoolsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	for _, name := range []string{"first", "second"} {
		if _, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String(name)}); err != nil {
			t.Fatalf("CreateUserPool(%s) error = %v", name, err)
		}
	}
	first, err := client.ListUserPools(ctx, &cognitoidentityprovider.ListUserPoolsInput{MaxResults: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListUserPools(first page) error = %v", err)
	}
	if len(first.UserPools) != 1 || aws.ToString(first.UserPools[0].Name) != "first" || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListUserPools(first page) = %#v, want first pool and next token", first)
	}
	second, err := client.ListUserPools(ctx, &cognitoidentityprovider.ListUserPoolsInput{MaxResults: aws.Int32(1), NextToken: first.NextToken})
	if err != nil {
		t.Fatalf("ListUserPools(second page) error = %v", err)
	}
	if len(second.UserPools) != 1 || aws.ToString(second.UserPools[0].Name) != "second" || second.NextToken != nil {
		t.Fatalf("ListUserPools(second page) = %#v, want second pool and no next token", second)
	}
}

func TestCognitoCompatibilityAdapterPaginatesUserPoolClientsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	pool, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	poolID := pool.UserPool.Id
	for _, name := range []string{"first", "second"} {
		if _, err := client.CreateUserPoolClient(ctx, &cognitoidentityprovider.CreateUserPoolClientInput{UserPoolId: poolID, ClientName: aws.String(name)}); err != nil {
			t.Fatalf("CreateUserPoolClient(%s) error = %v", name, err)
		}
	}
	first, err := client.ListUserPoolClients(ctx, &cognitoidentityprovider.ListUserPoolClientsInput{UserPoolId: poolID, MaxResults: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListUserPoolClients(first page) error = %v", err)
	}
	if len(first.UserPoolClients) != 1 || aws.ToString(first.UserPoolClients[0].ClientName) != "first" || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListUserPoolClients(first page) = %#v, want first client and next token", first)
	}
	second, err := client.ListUserPoolClients(ctx, &cognitoidentityprovider.ListUserPoolClientsInput{UserPoolId: poolID, MaxResults: aws.Int32(1), NextToken: first.NextToken})
	if err != nil {
		t.Fatalf("ListUserPoolClients(second page) error = %v", err)
	}
	if len(second.UserPoolClients) != 1 || aws.ToString(second.UserPoolClients[0].ClientName) != "second" || second.NextToken != nil {
		t.Fatalf("ListUserPoolClients(second page) = %#v, want second client and no next token", second)
	}
}

func TestCognitoCompatibilityAdapterPaginatesUsersWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	pool, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	poolID := pool.UserPool.Id
	for _, username := range []string{"ada", "zoe"} {
		if _, err := client.AdminCreateUser(ctx, &cognitoidentityprovider.AdminCreateUserInput{UserPoolId: poolID, Username: aws.String(username)}); err != nil {
			t.Fatalf("AdminCreateUser(%s) error = %v", username, err)
		}
	}
	first, err := client.ListUsers(ctx, &cognitoidentityprovider.ListUsersInput{UserPoolId: poolID, Limit: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListUsers(first page) error = %v", err)
	}
	if len(first.Users) != 1 || aws.ToString(first.Users[0].Username) != "ada" || aws.ToString(first.PaginationToken) == "" {
		t.Fatalf("ListUsers(first page) = %#v, want ada and pagination token", first)
	}
	second, err := client.ListUsers(ctx, &cognitoidentityprovider.ListUsersInput{UserPoolId: poolID, Limit: aws.Int32(1), PaginationToken: first.PaginationToken})
	if err != nil {
		t.Fatalf("ListUsers(second page) error = %v", err)
	}
	if len(second.Users) != 1 || aws.ToString(second.Users[0].Username) != "zoe" || second.PaginationToken != nil {
		t.Fatalf("ListUsers(second page) = %#v, want zoe and no pagination token", second)
	}
}

func TestCognitoCompatibilityAdapterPaginatesGroupsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	pool, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	poolID := pool.UserPool.Id
	for _, name := range []string{"admins", "readers"} {
		if _, err := client.CreateGroup(ctx, &cognitoidentityprovider.CreateGroupInput{UserPoolId: poolID, GroupName: aws.String(name)}); err != nil {
			t.Fatalf("CreateGroup(%s) error = %v", name, err)
		}
	}
	first, err := client.ListGroups(ctx, &cognitoidentityprovider.ListGroupsInput{UserPoolId: poolID, Limit: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListGroups(first page) error = %v", err)
	}
	if len(first.Groups) != 1 || aws.ToString(first.Groups[0].GroupName) != "admins" || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListGroups(first page) = %#v, want admins and next token", first)
	}
	second, err := client.ListGroups(ctx, &cognitoidentityprovider.ListGroupsInput{UserPoolId: poolID, Limit: aws.Int32(1), NextToken: first.NextToken})
	if err != nil {
		t.Fatalf("ListGroups(second page) error = %v", err)
	}
	if len(second.Groups) != 1 || aws.ToString(second.Groups[0].GroupName) != "readers" || second.NextToken != nil {
		t.Fatalf("ListGroups(second page) = %#v, want readers and no next token", second)
	}
}

func TestCognitoCompatibilityAdapterPaginatesGroupsForUserWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	pool, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	poolID := pool.UserPool.Id
	if _, err := client.AdminCreateUser(ctx, &cognitoidentityprovider.AdminCreateUserInput{UserPoolId: poolID, Username: aws.String("ada")}); err != nil {
		t.Fatalf("AdminCreateUser() error = %v", err)
	}
	for _, name := range []string{"admins", "readers"} {
		if _, err := client.CreateGroup(ctx, &cognitoidentityprovider.CreateGroupInput{UserPoolId: poolID, GroupName: aws.String(name)}); err != nil {
			t.Fatalf("CreateGroup(%s) error = %v", name, err)
		}
		if _, err := client.AdminAddUserToGroup(ctx, &cognitoidentityprovider.AdminAddUserToGroupInput{UserPoolId: poolID, GroupName: aws.String(name), Username: aws.String("ada")}); err != nil {
			t.Fatalf("AdminAddUserToGroup(%s) error = %v", name, err)
		}
	}
	first, err := client.AdminListGroupsForUser(ctx, &cognitoidentityprovider.AdminListGroupsForUserInput{UserPoolId: poolID, Username: aws.String("ada"), Limit: aws.Int32(1)})
	if err != nil {
		t.Fatalf("AdminListGroupsForUser(first page) error = %v", err)
	}
	if len(first.Groups) != 1 || aws.ToString(first.Groups[0].GroupName) != "admins" || aws.ToString(first.NextToken) == "" {
		t.Fatalf("AdminListGroupsForUser(first page) = %#v, want admins and next token", first)
	}
	second, err := client.AdminListGroupsForUser(ctx, &cognitoidentityprovider.AdminListGroupsForUserInput{UserPoolId: poolID, Username: aws.String("ada"), Limit: aws.Int32(1), NextToken: first.NextToken})
	if err != nil {
		t.Fatalf("AdminListGroupsForUser(second page) error = %v", err)
	}
	if len(second.Groups) != 1 || aws.ToString(second.Groups[0].GroupName) != "readers" || second.NextToken != nil {
		t.Fatalf("AdminListGroupsForUser(second page) = %#v, want readers and no next token", second)
	}
}

func TestCognitoCompatibilityAdapterRejectsDuplicateUserAndGroupWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()
	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	pool, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	poolID := pool.UserPool.Id
	if _, err := client.AdminCreateUser(ctx, &cognitoidentityprovider.AdminCreateUserInput{UserPoolId: poolID, Username: aws.String("ada")}); err != nil {
		t.Fatalf("AdminCreateUser() error = %v", err)
	}
	_, err = client.AdminCreateUser(ctx, &cognitoidentityprovider.AdminCreateUserInput{UserPoolId: poolID, Username: aws.String("ada")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "UsernameExistsException" {
		t.Fatalf("AdminCreateUser(duplicate) error = %v, want UsernameExistsException", err)
	}
	if _, err := client.CreateGroup(ctx, &cognitoidentityprovider.CreateGroupInput{UserPoolId: poolID, GroupName: aws.String("admins")}); err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	_, err = client.CreateGroup(ctx, &cognitoidentityprovider.CreateGroupInput{UserPoolId: poolID, GroupName: aws.String("admins")})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "GroupExistsException" {
		t.Fatalf("CreateGroup(duplicate) error = %v, want GroupExistsException", err)
	}
}

func TestCognitoCompatibilityAdapterManagesUserPoolClientsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()

	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	pool, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	poolID := aws.ToString(pool.UserPool.Id)

	created, err := client.CreateUserPoolClient(ctx, &cognitoidentityprovider.CreateUserPoolClientInput{
		UserPoolId: aws.String(poolID),
		ClientName: aws.String("web"),
	})
	if err != nil {
		t.Fatalf("CreateUserPoolClient() error = %v", err)
	}
	clientID := aws.ToString(created.UserPoolClient.ClientId)
	if clientID == "" || aws.ToString(created.UserPoolClient.ClientName) != "web" || aws.ToString(created.UserPoolClient.UserPoolId) != poolID {
		t.Fatalf("CreateUserPoolClient() = %#v, want web client in pool", created.UserPoolClient)
	}
	updated, err := client.UpdateUserPoolClient(ctx, &cognitoidentityprovider.UpdateUserPoolClientInput{
		UserPoolId: aws.String(poolID),
		ClientId:   aws.String(clientID),
		ClientName: aws.String("web-v2"),
	})
	if err != nil || updated.UserPoolClient == nil || aws.ToString(updated.UserPoolClient.ClientName) != "web-v2" {
		t.Fatalf("UpdateUserPoolClient() = %#v, %v; want renamed client", updated, err)
	}

	described, err := client.DescribeUserPoolClient(ctx, &cognitoidentityprovider.DescribeUserPoolClientInput{
		UserPoolId: aws.String(poolID),
		ClientId:   aws.String(clientID),
	})
	if err != nil {
		t.Fatalf("DescribeUserPoolClient() error = %v", err)
	}
	if described.UserPoolClient == nil || aws.ToString(described.UserPoolClient.ClientName) != "web-v2" {
		t.Fatalf("DescribeUserPoolClient() = %#v, want renamed client", described.UserPoolClient)
	}

	listed, err := client.ListUserPoolClients(ctx, &cognitoidentityprovider.ListUserPoolClientsInput{
		UserPoolId: aws.String(poolID),
		MaxResults: aws.Int32(10),
	})
	if err != nil {
		t.Fatalf("ListUserPoolClients() error = %v", err)
	}
	if len(listed.UserPoolClients) != 1 || aws.ToString(listed.UserPoolClients[0].ClientId) != clientID {
		t.Fatalf("ListUserPoolClients() = %#v, want created client", listed.UserPoolClients)
	}

	if _, err := client.DeleteUserPoolClient(ctx, &cognitoidentityprovider.DeleteUserPoolClientInput{
		UserPoolId: aws.String(poolID),
		ClientId:   aws.String(clientID),
	}); err != nil {
		t.Fatalf("DeleteUserPoolClient() error = %v", err)
	}
	_, err = client.DescribeUserPoolClient(ctx, &cognitoidentityprovider.DescribeUserPoolClientInput{
		UserPoolId: aws.String(poolID),
		ClientId:   aws.String(clientID),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("DescribeUserPoolClient(after delete) error = %v, want ResourceNotFoundException", err)
	}
}

func TestCognitoCompatibilityAdapterManagesUserPoolDomainsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()

	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	pool, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	poolID := aws.ToString(pool.UserPool.Id)

	if _, err := client.CreateUserPoolDomain(ctx, &cognitoidentityprovider.CreateUserPoolDomainInput{
		UserPoolId: aws.String(poolID),
		Domain:     aws.String("homeport-customers"),
	}); err != nil {
		t.Fatalf("CreateUserPoolDomain() error = %v", err)
	}

	described, err := client.DescribeUserPoolDomain(ctx, &cognitoidentityprovider.DescribeUserPoolDomainInput{
		Domain: aws.String("homeport-customers"),
	})
	if err != nil {
		t.Fatalf("DescribeUserPoolDomain() error = %v", err)
	}
	if described.DomainDescription == nil || aws.ToString(described.DomainDescription.Domain) != "homeport-customers" || aws.ToString(described.DomainDescription.UserPoolId) != poolID || described.DomainDescription.Status != types.DomainStatusTypeActive {
		t.Fatalf("DescribeUserPoolDomain() = %#v, want active domain on pool", described.DomainDescription)
	}

	if _, err := client.DeleteUserPoolDomain(ctx, &cognitoidentityprovider.DeleteUserPoolDomainInput{
		UserPoolId: aws.String(poolID),
		Domain:     aws.String("homeport-customers"),
	}); err != nil {
		t.Fatalf("DeleteUserPoolDomain() error = %v", err)
	}
	_, err = client.DescribeUserPoolDomain(ctx, &cognitoidentityprovider.DescribeUserPoolDomainInput{
		Domain: aws.String("homeport-customers"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("DescribeUserPoolDomain(after delete) error = %v, want ResourceNotFoundException", err)
	}
}

func TestCognitoCompatibilityAdapterManagesUsersWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()

	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	pool, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	poolID := aws.ToString(pool.UserPool.Id)

	created, err := client.AdminCreateUser(ctx, &cognitoidentityprovider.AdminCreateUserInput{
		UserPoolId: aws.String(poolID),
		Username:   aws.String("ada"),
		UserAttributes: []types.AttributeType{{
			Name:  aws.String("email"),
			Value: aws.String("ada@example.com"),
		}},
	})
	if err != nil {
		t.Fatalf("AdminCreateUser() error = %v", err)
	}
	if created.User == nil || aws.ToString(created.User.Username) != "ada" || created.User.UserStatus != types.UserStatusTypeForceChangePassword {
		t.Fatalf("AdminCreateUser() = %#v, want ada force-change-password user", created.User)
	}

	got, err := client.AdminGetUser(ctx, &cognitoidentityprovider.AdminGetUserInput{
		UserPoolId: aws.String(poolID),
		Username:   aws.String("ada"),
	})
	if err != nil {
		t.Fatalf("AdminGetUser() error = %v", err)
	}
	if aws.ToString(got.Username) != "ada" || cognitoAttribute(got.UserAttributes, "email") != "ada@example.com" {
		t.Fatalf("AdminGetUser() = %#v, want ada email", got)
	}

	listed, err := client.ListUsers(ctx, &cognitoidentityprovider.ListUsersInput{
		UserPoolId: aws.String(poolID),
		Limit:      aws.Int32(10),
	})
	if err != nil {
		t.Fatalf("ListUsers() error = %v", err)
	}
	if len(listed.Users) != 1 || aws.ToString(listed.Users[0].Username) != "ada" {
		t.Fatalf("ListUsers() = %#v, want ada", listed.Users)
	}

	if _, err := client.AdminDeleteUser(ctx, &cognitoidentityprovider.AdminDeleteUserInput{
		UserPoolId: aws.String(poolID),
		Username:   aws.String("ada"),
	}); err != nil {
		t.Fatalf("AdminDeleteUser() error = %v", err)
	}
	_, err = client.AdminGetUser(ctx, &cognitoidentityprovider.AdminGetUserInput{
		UserPoolId: aws.String(poolID),
		Username:   aws.String("ada"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("AdminGetUser(after delete) error = %v, want ResourceNotFoundException", err)
	}
}

func TestCognitoCompatibilityAdapterManagesGroupsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCognitoAdapter())
	defer server.Close()

	client := cognitoidentityprovider.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cognitoidentityprovider.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	pool, err := client.CreateUserPool(ctx, &cognitoidentityprovider.CreateUserPoolInput{PoolName: aws.String("customers")})
	if err != nil {
		t.Fatalf("CreateUserPool() error = %v", err)
	}
	poolID := aws.ToString(pool.UserPool.Id)

	created, err := client.CreateGroup(ctx, &cognitoidentityprovider.CreateGroupInput{
		UserPoolId:  aws.String(poolID),
		GroupName:   aws.String("admins"),
		Description: aws.String("Administrators"),
		Precedence:  aws.Int32(1),
	})
	if err != nil {
		t.Fatalf("CreateGroup() error = %v", err)
	}
	if created.Group == nil || aws.ToString(created.Group.GroupName) != "admins" || aws.ToString(created.Group.Description) != "Administrators" || aws.ToInt32(created.Group.Precedence) != 1 {
		t.Fatalf("CreateGroup() = %#v, want admins group", created.Group)
	}
	updated, err := client.UpdateGroup(ctx, &cognitoidentityprovider.UpdateGroupInput{
		UserPoolId:  aws.String(poolID),
		GroupName:   aws.String("admins"),
		Description: aws.String("Platform administrators"),
		Precedence:  aws.Int32(2),
	})
	if err != nil || updated.Group == nil || aws.ToString(updated.Group.Description) != "Platform administrators" || aws.ToInt32(updated.Group.Precedence) != 2 {
		t.Fatalf("UpdateGroup() = %#v, %v; want updated group", updated, err)
	}

	got, err := client.GetGroup(ctx, &cognitoidentityprovider.GetGroupInput{
		UserPoolId: aws.String(poolID),
		GroupName:  aws.String("admins"),
	})
	if err != nil {
		t.Fatalf("GetGroup() error = %v", err)
	}
	if got.Group == nil || aws.ToString(got.Group.GroupName) != "admins" {
		t.Fatalf("GetGroup() = %#v, want admins group", got.Group)
	}

	listed, err := client.ListGroups(ctx, &cognitoidentityprovider.ListGroupsInput{
		UserPoolId: aws.String(poolID),
		Limit:      aws.Int32(10),
	})
	if err != nil {
		t.Fatalf("ListGroups() error = %v", err)
	}
	if len(listed.Groups) != 1 || aws.ToString(listed.Groups[0].GroupName) != "admins" {
		t.Fatalf("ListGroups() = %#v, want admins", listed.Groups)
	}

	if _, err := client.AdminCreateUser(ctx, &cognitoidentityprovider.AdminCreateUserInput{
		UserPoolId: aws.String(poolID),
		Username:   aws.String("ada"),
	}); err != nil {
		t.Fatalf("AdminCreateUser() error = %v", err)
	}
	if _, err := client.AdminAddUserToGroup(ctx, &cognitoidentityprovider.AdminAddUserToGroupInput{
		UserPoolId: aws.String(poolID),
		GroupName:  aws.String("admins"),
		Username:   aws.String("ada"),
	}); err != nil {
		t.Fatalf("AdminAddUserToGroup() error = %v", err)
	}
	userGroups, err := client.AdminListGroupsForUser(ctx, &cognitoidentityprovider.AdminListGroupsForUserInput{
		UserPoolId: aws.String(poolID),
		Username:   aws.String("ada"),
		Limit:      aws.Int32(10),
	})
	if err != nil {
		t.Fatalf("AdminListGroupsForUser() error = %v", err)
	}
	if len(userGroups.Groups) != 1 || aws.ToString(userGroups.Groups[0].GroupName) != "admins" {
		t.Fatalf("AdminListGroupsForUser() = %#v, want admins", userGroups.Groups)
	}

	if _, err := client.AdminRemoveUserFromGroup(ctx, &cognitoidentityprovider.AdminRemoveUserFromGroupInput{
		UserPoolId: aws.String(poolID),
		GroupName:  aws.String("admins"),
		Username:   aws.String("ada"),
	}); err != nil {
		t.Fatalf("AdminRemoveUserFromGroup() error = %v", err)
	}
	userGroups, err = client.AdminListGroupsForUser(ctx, &cognitoidentityprovider.AdminListGroupsForUserInput{
		UserPoolId: aws.String(poolID),
		Username:   aws.String("ada"),
		Limit:      aws.Int32(10),
	})
	if err != nil {
		t.Fatalf("AdminListGroupsForUser(after remove) error = %v", err)
	}
	if len(userGroups.Groups) != 0 {
		t.Fatalf("AdminListGroupsForUser(after remove) = %#v, want no groups", userGroups.Groups)
	}

	if _, err := client.DeleteGroup(ctx, &cognitoidentityprovider.DeleteGroupInput{
		UserPoolId: aws.String(poolID),
		GroupName:  aws.String("admins"),
	}); err != nil {
		t.Fatalf("DeleteGroup() error = %v", err)
	}
	_, err = client.GetGroup(ctx, &cognitoidentityprovider.GetGroupInput{
		UserPoolId: aws.String(poolID),
		GroupName:  aws.String("admins"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("GetGroup(after delete) error = %v, want ResourceNotFoundException", err)
	}
}

func cognitoAttribute(attributes []types.AttributeType, name string) string {
	for _, attribute := range attributes {
		if aws.ToString(attribute.Name) == name {
			return aws.ToString(attribute.Value)
		}
	}
	return ""
}

func TestCognitoCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewCognitoAdapter())
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
		UserPool struct {
			ID   string `json:"Id"`
			Name string `json:"Name"`
		} `json:"UserPool"`
	}
	if err := json.Unmarshal(runAWS("cognito-idp", "create-user-pool", "--pool-name", "cli-customers"), &created); err != nil {
		t.Fatalf("decode create-user-pool output: %v", err)
	}
	if created.UserPool.ID == "" || created.UserPool.Name != "cli-customers" {
		t.Fatalf("create-user-pool = %#v, want cli-customers pool", created.UserPool)
	}

	var described struct {
		UserPool struct {
			ID               string `json:"Id"`
			Name             string `json:"Name"`
			MFAConfiguration string `json:"MfaConfiguration"`
		} `json:"UserPool"`
	}
	if err := json.Unmarshal(runAWS("cognito-idp", "describe-user-pool", "--user-pool-id", created.UserPool.ID), &described); err != nil {
		t.Fatalf("decode describe-user-pool output: %v", err)
	}
	if described.UserPool.ID != created.UserPool.ID || described.UserPool.Name != "cli-customers" {
		t.Fatalf("describe-user-pool = %#v, want created pool", described.UserPool)
	}

	var listed struct {
		UserPools []struct {
			ID string `json:"Id"`
		} `json:"UserPools"`
	}
	if err := json.Unmarshal(runAWS("cognito-idp", "list-user-pools", "--max-results", "10"), &listed); err != nil {
		t.Fatalf("decode list-user-pools output: %v", err)
	}
	if len(listed.UserPools) != 1 || listed.UserPools[0].ID != created.UserPool.ID {
		t.Fatalf("list-user-pools = %#v, want created pool", listed.UserPools)
	}

	runAWS("cognito-idp", "update-user-pool", "--user-pool-id", created.UserPool.ID, "--mfa-configuration", "OPTIONAL")
	if err := json.Unmarshal(runAWS("cognito-idp", "describe-user-pool", "--user-pool-id", created.UserPool.ID), &described); err != nil {
		t.Fatalf("decode describe-user-pool after update output: %v", err)
	}
	if described.UserPool.MFAConfiguration != "OPTIONAL" {
		t.Fatalf("describe-user-pool after update = %#v, want optional MFA", described.UserPool)
	}

	runAWS("cognito-idp", "delete-user-pool", "--user-pool-id", created.UserPool.ID)
}

func TestCognitoCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewCognitoAdapter())
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
    cognitoidp = %q
  }
}

resource "aws_cognito_user_pool" "deploy" {
  name = "terraform-customers"
  tags = {
    env = "test"
  }
}

output "user_pool_id" {
  value = aws_cognito_user_pool.deploy.id
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

	out := runTerraform("output", "-raw", "user_pool_id")
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("terraform output user_pool_id is empty")
	}
}
