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
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestIAMCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("homeport-orders"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`),
		Description:              aws.String("initial role"),
		Tags: []types.Tag{{
			Key:   aws.String("env"),
			Value: aws.String("test"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if created.Role == nil || aws.ToString(created.Role.RoleName) != "homeport-orders" || aws.ToString(created.Role.Arn) == "" {
		t.Fatalf("CreateRole() = %#v, want created role", created.Role)
	}

	got, err := client.GetRole(context.Background(), &iam.GetRoleInput{RoleName: aws.String("homeport-orders")})
	if err != nil {
		t.Fatalf("GetRole() error = %v", err)
	}
	if got.Role == nil || aws.ToString(got.Role.Arn) != aws.ToString(created.Role.Arn) {
		t.Fatalf("GetRole() = %#v, want created role", got.Role)
	}

	listed, err := client.ListRoles(context.Background(), &iam.ListRolesInput{})
	if err != nil {
		t.Fatalf("ListRoles() error = %v", err)
	}
	if len(listed.Roles) != 1 || aws.ToString(listed.Roles[0].RoleName) != "homeport-orders" {
		t.Fatalf("ListRoles() = %#v, want homeport-orders", listed.Roles)
	}

	if _, err := client.UpdateRole(context.Background(), &iam.UpdateRoleInput{
		RoleName:    aws.String("homeport-orders"),
		Description: aws.String("updated role"),
	}); err != nil {
		t.Fatalf("UpdateRole() error = %v", err)
	}

	updated, err := client.GetRole(context.Background(), &iam.GetRoleInput{RoleName: aws.String("homeport-orders")})
	if err != nil {
		t.Fatalf("GetRole(after update) error = %v", err)
	}
	if aws.ToString(updated.Role.Description) != "updated role" {
		t.Fatalf("GetRole(after update) description = %q, want updated role", aws.ToString(updated.Role.Description))
	}

	if _, err := client.DeleteRole(context.Background(), &iam.DeleteRoleInput{RoleName: aws.String("homeport-orders")}); err != nil {
		t.Fatalf("DeleteRole() error = %v", err)
	}
	listed, err = client.ListRoles(context.Background(), &iam.ListRolesInput{})
	if err != nil {
		t.Fatalf("ListRoles(after delete) error = %v", err)
	}
	if len(listed.Roles) != 0 {
		t.Fatalf("ListRoles(after delete) = %#v, want no roles", listed.Roles)
	}
}

func TestIAMCompatibilityAdapterPaginatesListRolesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, name := range []string{"page-alpha", "page-bravo", "page-charlie"} {
		if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
			RoleName:                 aws.String(name),
			AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
		}); err != nil {
			t.Fatalf("CreateRole(%s) error = %v", name, err)
		}
	}

	first, err := client.ListRoles(context.Background(), &iam.ListRolesInput{MaxItems: aws.Int32(2)})
	if err != nil {
		t.Fatalf("ListRoles(first) error = %v", err)
	}
	if len(first.Roles) != 2 || !first.IsTruncated || first.Marker == nil {
		t.Fatalf("ListRoles(first) = roles:%#v truncated:%v marker:%v, want two roles and marker", first.Roles, first.IsTruncated, first.Marker)
	}

	second, err := client.ListRoles(context.Background(), &iam.ListRolesInput{
		MaxItems: aws.Int32(2),
		Marker:   first.Marker,
	})
	if err != nil {
		t.Fatalf("ListRoles(second) error = %v", err)
	}
	if len(second.Roles) != 1 || second.IsTruncated || second.Marker != nil {
		t.Fatalf("ListRoles(second) = roles:%#v truncated:%v marker:%v, want final role and no marker", second.Roles, second.IsTruncated, second.Marker)
	}
}

func TestIAMCompatibilityAdapterRejectsInvalidListRolesPaginationWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	cases := map[string]*iam.ListRolesInput{
		"bad marker": {Marker: aws.String("not-a-marker")},
		"zero max":   {MaxItems: aws.Int32(0)},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := client.ListRoles(context.Background(), input)
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidInput" {
				t.Fatalf("ListRoles() error = %v, want InvalidInput", err)
			}
		})
	}
}

func TestIAMCompatibilityAdapterPreservesRolePathWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("orders-worker"),
		Path:                     aws.String("/service-role/"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
	}); err != nil {
		t.Fatalf("CreateRole(path) error = %v", err)
	}
	if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("orders-admin"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
	}); err != nil {
		t.Fatalf("CreateRole(default path) error = %v", err)
	}

	got, err := client.GetRole(context.Background(), &iam.GetRoleInput{RoleName: aws.String("orders-worker")})
	if err != nil {
		t.Fatalf("GetRole(path) error = %v", err)
	}
	if got.Role == nil || aws.ToString(got.Role.Path) != "/service-role/" || !strings.HasSuffix(aws.ToString(got.Role.Arn), ":role/service-role/orders-worker") {
		t.Fatalf("GetRole(path) = %#v, want service-role path and ARN", got.Role)
	}

	listed, err := client.ListRoles(context.Background(), &iam.ListRolesInput{PathPrefix: aws.String("/service-role/")})
	if err != nil {
		t.Fatalf("ListRoles(path prefix) error = %v", err)
	}
	if len(listed.Roles) != 1 || aws.ToString(listed.Roles[0].RoleName) != "orders-worker" || aws.ToString(listed.Roles[0].Path) != "/service-role/" {
		t.Fatalf("ListRoles(path prefix) = %#v, want only service-role role", listed.Roles)
	}
}

func TestIAMCompatibilityAdapterManagesInlineRolePoliciesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("policy-homeport-orders"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
	}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:ListBucket","Resource":"*"}]}`
	if _, err := client.PutRolePolicy(context.Background(), &iam.PutRolePolicyInput{
		RoleName:       aws.String("policy-homeport-orders"),
		PolicyName:     aws.String("orders-inline"),
		PolicyDocument: aws.String(policy),
	}); err != nil {
		t.Fatalf("PutRolePolicy() error = %v", err)
	}

	got, err := client.GetRolePolicy(context.Background(), &iam.GetRolePolicyInput{
		RoleName:   aws.String("policy-homeport-orders"),
		PolicyName: aws.String("orders-inline"),
	})
	if err != nil {
		t.Fatalf("GetRolePolicy() error = %v", err)
	}
	if aws.ToString(got.RoleName) != "policy-homeport-orders" || aws.ToString(got.PolicyName) != "orders-inline" || aws.ToString(got.PolicyDocument) != policy {
		t.Fatalf("GetRolePolicy() = %#v, want policy read-back", got)
	}

	listed, err := client.ListRolePolicies(context.Background(), &iam.ListRolePoliciesInput{RoleName: aws.String("policy-homeport-orders")})
	if err != nil {
		t.Fatalf("ListRolePolicies() error = %v", err)
	}
	if len(listed.PolicyNames) != 1 || listed.PolicyNames[0] != "orders-inline" {
		t.Fatalf("ListRolePolicies() = %#v, want orders-inline", listed.PolicyNames)
	}

	if _, err := client.DeleteRolePolicy(context.Background(), &iam.DeleteRolePolicyInput{
		RoleName:   aws.String("policy-homeport-orders"),
		PolicyName: aws.String("orders-inline"),
	}); err != nil {
		t.Fatalf("DeleteRolePolicy() error = %v", err)
	}
	_, err = client.GetRolePolicy(context.Background(), &iam.GetRolePolicyInput{
		RoleName:   aws.String("policy-homeport-orders"),
		PolicyName: aws.String("orders-inline"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NoSuchEntity" {
		t.Fatalf("GetRolePolicy(after delete) error = %v, want NoSuchEntity", err)
	}
}

func TestIAMCompatibilityAdapterPaginatesInlineRolePoliciesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("paged-policy-homeport-orders"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
	}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		if _, err := client.PutRolePolicy(context.Background(), &iam.PutRolePolicyInput{
			RoleName:       aws.String("paged-policy-homeport-orders"),
			PolicyName:     aws.String(name),
			PolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
		}); err != nil {
			t.Fatalf("PutRolePolicy(%s) error = %v", name, err)
		}
	}

	first, err := client.ListRolePolicies(context.Background(), &iam.ListRolePoliciesInput{
		RoleName: aws.String("paged-policy-homeport-orders"),
		MaxItems: aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListRolePolicies(first) error = %v", err)
	}
	if len(first.PolicyNames) != 2 || !first.IsTruncated || first.Marker == nil {
		t.Fatalf("ListRolePolicies(first) = names:%#v truncated:%v marker:%v, want two policies and marker", first.PolicyNames, first.IsTruncated, first.Marker)
	}

	second, err := client.ListRolePolicies(context.Background(), &iam.ListRolePoliciesInput{
		RoleName: aws.String("paged-policy-homeport-orders"),
		MaxItems: aws.Int32(2),
		Marker:   first.Marker,
	})
	if err != nil {
		t.Fatalf("ListRolePolicies(second) error = %v", err)
	}
	if len(second.PolicyNames) != 1 || second.IsTruncated || second.Marker != nil {
		t.Fatalf("ListRolePolicies(second) = names:%#v truncated:%v marker:%v, want final policy and no marker", second.PolicyNames, second.IsTruncated, second.Marker)
	}

	cases := map[string]*iam.ListRolePoliciesInput{
		"bad marker": {RoleName: aws.String("paged-policy-homeport-orders"), Marker: aws.String("bad")},
		"zero max":   {RoleName: aws.String("paged-policy-homeport-orders"), MaxItems: aws.Int32(0)},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := client.ListRolePolicies(context.Background(), input)
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidInput" {
				t.Fatalf("ListRolePolicies() error = %v, want InvalidInput", err)
			}
		})
	}
}

func TestIAMCompatibilityAdapterManagesAttachedRolePoliciesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("attached-policy-homeport-orders"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
	}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	policyARN := "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
	if _, err := client.AttachRolePolicy(context.Background(), &iam.AttachRolePolicyInput{
		RoleName:  aws.String("attached-policy-homeport-orders"),
		PolicyArn: aws.String(policyARN),
	}); err != nil {
		t.Fatalf("AttachRolePolicy() error = %v", err)
	}

	listed, err := client.ListAttachedRolePolicies(context.Background(), &iam.ListAttachedRolePoliciesInput{RoleName: aws.String("attached-policy-homeport-orders")})
	if err != nil {
		t.Fatalf("ListAttachedRolePolicies() error = %v", err)
	}
	if len(listed.AttachedPolicies) != 1 ||
		aws.ToString(listed.AttachedPolicies[0].PolicyArn) != policyARN ||
		aws.ToString(listed.AttachedPolicies[0].PolicyName) != "AWSLambdaBasicExecutionRole" {
		t.Fatalf("ListAttachedRolePolicies() = %#v, want attached AWSLambdaBasicExecutionRole", listed.AttachedPolicies)
	}

	if _, err := client.DetachRolePolicy(context.Background(), &iam.DetachRolePolicyInput{
		RoleName:  aws.String("attached-policy-homeport-orders"),
		PolicyArn: aws.String(policyARN),
	}); err != nil {
		t.Fatalf("DetachRolePolicy() error = %v", err)
	}
	listed, err = client.ListAttachedRolePolicies(context.Background(), &iam.ListAttachedRolePoliciesInput{RoleName: aws.String("attached-policy-homeport-orders")})
	if err != nil {
		t.Fatalf("ListAttachedRolePolicies(after detach) error = %v", err)
	}
	if len(listed.AttachedPolicies) != 0 {
		t.Fatalf("ListAttachedRolePolicies(after detach) = %#v, want no attached policies", listed.AttachedPolicies)
	}
}

func TestIAMCompatibilityAdapterRejectsAttachingMissingCustomerPolicy(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()
	client := iam.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *iam.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateRole(ctx, &iam.CreateRoleInput{RoleName: aws.String("missing-policy-role"), AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`)}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	_, err := client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{RoleName: aws.String("missing-policy-role"), PolicyArn: aws.String("arn:aws:iam::000000000000:policy/missing")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NoSuchEntity" {
		t.Fatalf("AttachRolePolicy(missing policy) error = %v, want NoSuchEntity", err)
	}
}

func TestIAMCompatibilityAdapterManagesPoliciesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreatePolicy(context.Background(), &iam.CreatePolicyInput{
		PolicyName:     aws.String("orders-managed"),
		Path:           aws.String("/service-role/"),
		Description:    aws.String("orders access"),
		PolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
	})
	if err != nil {
		t.Fatalf("CreatePolicy() error = %v", err)
	}
	if created.Policy == nil ||
		aws.ToString(created.Policy.PolicyName) != "orders-managed" ||
		aws.ToString(created.Policy.Path) != "/service-role/" ||
		!strings.HasSuffix(aws.ToString(created.Policy.Arn), ":policy/service-role/orders-managed") ||
		aws.ToString(created.Policy.DefaultVersionId) != "v1" ||
		!created.Policy.IsAttachable {
		t.Fatalf("CreatePolicy() = %#v, want managed policy metadata", created.Policy)
	}
	policyARN := aws.ToString(created.Policy.Arn)

	if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("managed-policy-homeport-orders"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
	}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if _, err := client.AttachRolePolicy(context.Background(), &iam.AttachRolePolicyInput{
		RoleName:  aws.String("managed-policy-homeport-orders"),
		PolicyArn: aws.String(policyARN),
	}); err != nil {
		t.Fatalf("AttachRolePolicy() error = %v", err)
	}

	got, err := client.GetPolicy(context.Background(), &iam.GetPolicyInput{PolicyArn: aws.String(policyARN)})
	if err != nil {
		t.Fatalf("GetPolicy() error = %v", err)
	}
	if got.Policy == nil ||
		aws.ToString(got.Policy.Description) != "orders access" ||
		aws.ToInt32(got.Policy.AttachmentCount) != 1 {
		t.Fatalf("GetPolicy() = %#v, want description and attachment count", got.Policy)
	}

	listed, err := client.ListPolicies(context.Background(), &iam.ListPoliciesInput{PathPrefix: aws.String("/service-role/")})
	if err != nil {
		t.Fatalf("ListPolicies() error = %v", err)
	}
	if len(listed.Policies) != 1 || aws.ToString(listed.Policies[0].Arn) != policyARN {
		t.Fatalf("ListPolicies() = %#v, want created policy", listed.Policies)
	}

	if _, err := client.DetachRolePolicy(context.Background(), &iam.DetachRolePolicyInput{
		RoleName:  aws.String("managed-policy-homeport-orders"),
		PolicyArn: aws.String(policyARN),
	}); err != nil {
		t.Fatalf("DetachRolePolicy() error = %v", err)
	}
	got, err = client.GetPolicy(context.Background(), &iam.GetPolicyInput{PolicyArn: aws.String(policyARN)})
	if err != nil {
		t.Fatalf("GetPolicy(after detach) error = %v", err)
	}
	if aws.ToInt32(got.Policy.AttachmentCount) != 0 {
		t.Fatalf("GetPolicy(after detach) attachment count = %d, want 0", aws.ToInt32(got.Policy.AttachmentCount))
	}

	if _, err := client.DeletePolicy(context.Background(), &iam.DeletePolicyInput{PolicyArn: aws.String(policyARN)}); err != nil {
		t.Fatalf("DeletePolicy() error = %v", err)
	}
	_, err = client.GetPolicy(context.Background(), &iam.GetPolicyInput{PolicyArn: aws.String(policyARN)})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NoSuchEntity" {
		t.Fatalf("GetPolicy(after delete) error = %v, want NoSuchEntity", err)
	}
}

func TestIAMCompatibilityAdapterPaginatesPoliciesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, name := range []string{"alpha-managed", "bravo-managed", "charlie-managed"} {
		if _, err := client.CreatePolicy(context.Background(), &iam.CreatePolicyInput{
			PolicyName:     aws.String(name),
			Path:           aws.String("/service-role/"),
			PolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
		}); err != nil {
			t.Fatalf("CreatePolicy(%s) error = %v", name, err)
		}
	}

	first, err := client.ListPolicies(context.Background(), &iam.ListPoliciesInput{
		PathPrefix: aws.String("/service-role/"),
		MaxItems:   aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListPolicies(first) error = %v", err)
	}
	if len(first.Policies) != 2 || !first.IsTruncated || first.Marker == nil {
		t.Fatalf("ListPolicies(first) = policies:%#v truncated:%v marker:%v, want two policies and marker", first.Policies, first.IsTruncated, first.Marker)
	}

	second, err := client.ListPolicies(context.Background(), &iam.ListPoliciesInput{
		PathPrefix: aws.String("/service-role/"),
		MaxItems:   aws.Int32(2),
		Marker:     first.Marker,
	})
	if err != nil {
		t.Fatalf("ListPolicies(second) error = %v", err)
	}
	if len(second.Policies) != 1 || second.IsTruncated || second.Marker != nil {
		t.Fatalf("ListPolicies(second) = policies:%#v truncated:%v marker:%v, want final policy and no marker", second.Policies, second.IsTruncated, second.Marker)
	}

	cases := map[string]*iam.ListPoliciesInput{
		"bad marker": {PathPrefix: aws.String("/service-role/"), Marker: aws.String("bad")},
		"zero max":   {PathPrefix: aws.String("/service-role/"), MaxItems: aws.Int32(0)},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := client.ListPolicies(context.Background(), input)
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidInput" {
				t.Fatalf("ListPolicies() error = %v, want InvalidInput", err)
			}
		})
	}
}

func TestIAMCompatibilityAdapterManagesPolicyVersionsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	v1Document := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:ListBucket","Resource":"*"}]}`
	created, err := client.CreatePolicy(context.Background(), &iam.CreatePolicyInput{
		PolicyName:     aws.String("versioned-managed"),
		PolicyDocument: aws.String(v1Document),
	})
	if err != nil {
		t.Fatalf("CreatePolicy() error = %v", err)
	}
	policyARN := aws.ToString(created.Policy.Arn)

	v1, err := client.GetPolicyVersion(context.Background(), &iam.GetPolicyVersionInput{
		PolicyArn: aws.String(policyARN),
		VersionId: aws.String("v1"),
	})
	if err != nil {
		t.Fatalf("GetPolicyVersion(v1) error = %v", err)
	}
	if v1.PolicyVersion == nil ||
		aws.ToString(v1.PolicyVersion.VersionId) != "v1" ||
		!v1.PolicyVersion.IsDefaultVersion ||
		aws.ToString(v1.PolicyVersion.Document) != v1Document {
		t.Fatalf("GetPolicyVersion(v1) = %#v, want default v1 document", v1.PolicyVersion)
	}

	v2Document := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:*","Resource":"*"}]}`
	v2, err := client.CreatePolicyVersion(context.Background(), &iam.CreatePolicyVersionInput{
		PolicyArn:      aws.String(policyARN),
		PolicyDocument: aws.String(v2Document),
		SetAsDefault:   false,
	})
	if err != nil {
		t.Fatalf("CreatePolicyVersion(v2) error = %v", err)
	}
	if v2.PolicyVersion == nil || aws.ToString(v2.PolicyVersion.VersionId) != "v2" || v2.PolicyVersion.IsDefaultVersion {
		t.Fatalf("CreatePolicyVersion(v2) = %#v, want non-default v2", v2.PolicyVersion)
	}

	if _, err := client.SetDefaultPolicyVersion(context.Background(), &iam.SetDefaultPolicyVersionInput{
		PolicyArn: aws.String(policyARN),
		VersionId: aws.String("v2"),
	}); err != nil {
		t.Fatalf("SetDefaultPolicyVersion(v2) error = %v", err)
	}
	got, err := client.GetPolicy(context.Background(), &iam.GetPolicyInput{PolicyArn: aws.String(policyARN)})
	if err != nil {
		t.Fatalf("GetPolicy(after default) error = %v", err)
	}
	if aws.ToString(got.Policy.DefaultVersionId) != "v2" {
		t.Fatalf("GetPolicy(after default) default version = %q, want v2", aws.ToString(got.Policy.DefaultVersionId))
	}

	listed, err := client.ListPolicyVersions(context.Background(), &iam.ListPolicyVersionsInput{PolicyArn: aws.String(policyARN)})
	if err != nil {
		t.Fatalf("ListPolicyVersions() error = %v", err)
	}
	if len(listed.Versions) != 2 || aws.ToString(listed.Versions[1].VersionId) != "v2" || !listed.Versions[1].IsDefaultVersion {
		t.Fatalf("ListPolicyVersions() = %#v, want v1 and default v2", listed.Versions)
	}

	if _, err := client.DeletePolicyVersion(context.Background(), &iam.DeletePolicyVersionInput{
		PolicyArn: aws.String(policyARN),
		VersionId: aws.String("v1"),
	}); err != nil {
		t.Fatalf("DeletePolicyVersion(v1) error = %v", err)
	}
	_, err = client.GetPolicyVersion(context.Background(), &iam.GetPolicyVersionInput{
		PolicyArn: aws.String(policyARN),
		VersionId: aws.String("v1"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NoSuchEntity" {
		t.Fatalf("GetPolicyVersion(v1 after delete) error = %v, want NoSuchEntity", err)
	}
}

func TestIAMCompatibilityAdapterPaginatesAttachedRolePoliciesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("paged-attached-policy-homeport-orders"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
	}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	for _, arn := range []string{
		"arn:aws:iam::aws:policy/AmazonS3ReadOnlyAccess",
		"arn:aws:iam::aws:policy/CloudWatchReadOnlyAccess",
		"arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole",
	} {
		if _, err := client.AttachRolePolicy(context.Background(), &iam.AttachRolePolicyInput{
			RoleName:  aws.String("paged-attached-policy-homeport-orders"),
			PolicyArn: aws.String(arn),
		}); err != nil {
			t.Fatalf("AttachRolePolicy(%s) error = %v", arn, err)
		}
	}

	first, err := client.ListAttachedRolePolicies(context.Background(), &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String("paged-attached-policy-homeport-orders"),
		MaxItems: aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListAttachedRolePolicies(first) error = %v", err)
	}
	if len(first.AttachedPolicies) != 2 || !first.IsTruncated || first.Marker == nil {
		t.Fatalf("ListAttachedRolePolicies(first) = policies:%#v truncated:%v marker:%v, want two policies and marker", first.AttachedPolicies, first.IsTruncated, first.Marker)
	}

	second, err := client.ListAttachedRolePolicies(context.Background(), &iam.ListAttachedRolePoliciesInput{
		RoleName: aws.String("paged-attached-policy-homeport-orders"),
		MaxItems: aws.Int32(2),
		Marker:   first.Marker,
	})
	if err != nil {
		t.Fatalf("ListAttachedRolePolicies(second) error = %v", err)
	}
	if len(second.AttachedPolicies) != 1 || second.IsTruncated || second.Marker != nil {
		t.Fatalf("ListAttachedRolePolicies(second) = policies:%#v truncated:%v marker:%v, want final policy and no marker", second.AttachedPolicies, second.IsTruncated, second.Marker)
	}

	cases := map[string]*iam.ListAttachedRolePoliciesInput{
		"bad marker": {RoleName: aws.String("paged-attached-policy-homeport-orders"), Marker: aws.String("bad")},
		"zero max":   {RoleName: aws.String("paged-attached-policy-homeport-orders"), MaxItems: aws.Int32(0)},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := client.ListAttachedRolePolicies(context.Background(), input)
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidInput" {
				t.Fatalf("ListAttachedRolePolicies() error = %v, want InvalidInput", err)
			}
		})
	}
}

func TestIAMCompatibilityAdapterManagesInstanceProfilesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("profile-homeport-orders"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
	}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}

	created, err := client.CreateInstanceProfile(context.Background(), &iam.CreateInstanceProfileInput{
		InstanceProfileName: aws.String("orders-profile"),
		Path:                aws.String("/service-role/"),
	})
	if err != nil {
		t.Fatalf("CreateInstanceProfile() error = %v", err)
	}
	if created.InstanceProfile == nil ||
		aws.ToString(created.InstanceProfile.InstanceProfileName) != "orders-profile" ||
		aws.ToString(created.InstanceProfile.Path) != "/service-role/" ||
		!strings.HasSuffix(aws.ToString(created.InstanceProfile.Arn), ":instance-profile/service-role/orders-profile") {
		t.Fatalf("CreateInstanceProfile() = %#v, want path-qualified profile", created.InstanceProfile)
	}

	if _, err := client.AddRoleToInstanceProfile(context.Background(), &iam.AddRoleToInstanceProfileInput{
		InstanceProfileName: aws.String("orders-profile"),
		RoleName:            aws.String("profile-homeport-orders"),
	}); err != nil {
		t.Fatalf("AddRoleToInstanceProfile() error = %v", err)
	}

	got, err := client.GetInstanceProfile(context.Background(), &iam.GetInstanceProfileInput{InstanceProfileName: aws.String("orders-profile")})
	if err != nil {
		t.Fatalf("GetInstanceProfile() error = %v", err)
	}
	if got.InstanceProfile == nil || len(got.InstanceProfile.Roles) != 1 || aws.ToString(got.InstanceProfile.Roles[0].RoleName) != "profile-homeport-orders" {
		t.Fatalf("GetInstanceProfile() = %#v, want attached role", got.InstanceProfile)
	}

	listed, err := client.ListInstanceProfilesForRole(context.Background(), &iam.ListInstanceProfilesForRoleInput{RoleName: aws.String("profile-homeport-orders")})
	if err != nil {
		t.Fatalf("ListInstanceProfilesForRole() error = %v", err)
	}
	if len(listed.InstanceProfiles) != 1 || aws.ToString(listed.InstanceProfiles[0].InstanceProfileName) != "orders-profile" {
		t.Fatalf("ListInstanceProfilesForRole() = %#v, want orders-profile", listed.InstanceProfiles)
	}

	if _, err := client.RemoveRoleFromInstanceProfile(context.Background(), &iam.RemoveRoleFromInstanceProfileInput{
		InstanceProfileName: aws.String("orders-profile"),
		RoleName:            aws.String("profile-homeport-orders"),
	}); err != nil {
		t.Fatalf("RemoveRoleFromInstanceProfile() error = %v", err)
	}
	got, err = client.GetInstanceProfile(context.Background(), &iam.GetInstanceProfileInput{InstanceProfileName: aws.String("orders-profile")})
	if err != nil {
		t.Fatalf("GetInstanceProfile(after remove) error = %v", err)
	}
	if got.InstanceProfile == nil || len(got.InstanceProfile.Roles) != 0 {
		t.Fatalf("GetInstanceProfile(after remove) = %#v, want no roles", got.InstanceProfile)
	}

	if _, err := client.DeleteInstanceProfile(context.Background(), &iam.DeleteInstanceProfileInput{InstanceProfileName: aws.String("orders-profile")}); err != nil {
		t.Fatalf("DeleteInstanceProfile() error = %v", err)
	}
	_, err = client.GetInstanceProfile(context.Background(), &iam.GetInstanceProfileInput{InstanceProfileName: aws.String("orders-profile")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NoSuchEntity" {
		t.Fatalf("GetInstanceProfile(after delete) error = %v, want NoSuchEntity", err)
	}
}

func TestIAMCompatibilityAdapterPaginatesInstanceProfilesForRoleWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("paged-profile-homeport-orders"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
	}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	for _, name := range []string{"alpha-profile", "bravo-profile", "charlie-profile"} {
		if _, err := client.CreateInstanceProfile(context.Background(), &iam.CreateInstanceProfileInput{InstanceProfileName: aws.String(name)}); err != nil {
			t.Fatalf("CreateInstanceProfile(%s) error = %v", name, err)
		}
		if _, err := client.AddRoleToInstanceProfile(context.Background(), &iam.AddRoleToInstanceProfileInput{
			InstanceProfileName: aws.String(name),
			RoleName:            aws.String("paged-profile-homeport-orders"),
		}); err != nil {
			t.Fatalf("AddRoleToInstanceProfile(%s) error = %v", name, err)
		}
	}

	first, err := client.ListInstanceProfilesForRole(context.Background(), &iam.ListInstanceProfilesForRoleInput{
		RoleName: aws.String("paged-profile-homeport-orders"),
		MaxItems: aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListInstanceProfilesForRole(first) error = %v", err)
	}
	if len(first.InstanceProfiles) != 2 || !first.IsTruncated || first.Marker == nil {
		t.Fatalf("ListInstanceProfilesForRole(first) = profiles:%#v truncated:%v marker:%v, want two profiles and marker", first.InstanceProfiles, first.IsTruncated, first.Marker)
	}

	second, err := client.ListInstanceProfilesForRole(context.Background(), &iam.ListInstanceProfilesForRoleInput{
		RoleName: aws.String("paged-profile-homeport-orders"),
		MaxItems: aws.Int32(2),
		Marker:   first.Marker,
	})
	if err != nil {
		t.Fatalf("ListInstanceProfilesForRole(second) error = %v", err)
	}
	if len(second.InstanceProfiles) != 1 || second.IsTruncated || second.Marker != nil {
		t.Fatalf("ListInstanceProfilesForRole(second) = profiles:%#v truncated:%v marker:%v, want final profile and no marker", second.InstanceProfiles, second.IsTruncated, second.Marker)
	}

	cases := map[string]*iam.ListInstanceProfilesForRoleInput{
		"bad marker": {RoleName: aws.String("paged-profile-homeport-orders"), Marker: aws.String("bad")},
		"zero max":   {RoleName: aws.String("paged-profile-homeport-orders"), MaxItems: aws.Int32(0)},
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := client.ListInstanceProfilesForRole(context.Background(), input)
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidInput" {
				t.Fatalf("ListInstanceProfilesForRole() error = %v, want InvalidInput", err)
			}
		})
	}
}

func TestIAMCompatibilityAdapterRejectsDeletingRoleAttachedToInstanceProfile(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()
	client := iam.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *iam.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateRole(ctx, &iam.CreateRoleInput{RoleName: aws.String("in-use-role"), AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17"}`)}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if _, err := client.CreateInstanceProfile(ctx, &iam.CreateInstanceProfileInput{InstanceProfileName: aws.String("in-use-profile")}); err != nil {
		t.Fatalf("CreateInstanceProfile() error = %v", err)
	}
	if _, err := client.AddRoleToInstanceProfile(ctx, &iam.AddRoleToInstanceProfileInput{InstanceProfileName: aws.String("in-use-profile"), RoleName: aws.String("in-use-role")}); err != nil {
		t.Fatalf("AddRoleToInstanceProfile() error = %v", err)
	}
	_, err := client.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: aws.String("in-use-role")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "DeleteConflict" {
		t.Fatalf("DeleteRole(attached) error = %v, want DeleteConflict", err)
	}
	if _, err := client.RemoveRoleFromInstanceProfile(ctx, &iam.RemoveRoleFromInstanceProfileInput{InstanceProfileName: aws.String("in-use-profile"), RoleName: aws.String("in-use-role")}); err != nil {
		t.Fatalf("RemoveRoleFromInstanceProfile() error = %v", err)
	}
	if _, err := client.DeleteRole(ctx, &iam.DeleteRoleInput{RoleName: aws.String("in-use-role")}); err != nil {
		t.Fatalf("DeleteRole(after remove) error = %v", err)
	}
}

func TestIAMCompatibilityAdapterRejectsDeletingAttachedManagedPolicy(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter())
	defer server.Close()
	client := iam.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *iam.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateRole(ctx, &iam.CreateRoleInput{RoleName: aws.String("policy-role"), AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17"}`)}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	policy, err := client.CreatePolicy(ctx, &iam.CreatePolicyInput{PolicyName: aws.String("in-use-policy"), PolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`)})
	if err != nil {
		t.Fatalf("CreatePolicy() error = %v", err)
	}
	if _, err := client.AttachRolePolicy(ctx, &iam.AttachRolePolicyInput{RoleName: aws.String("policy-role"), PolicyArn: policy.Policy.Arn}); err != nil {
		t.Fatalf("AttachRolePolicy() error = %v", err)
	}
	_, err = client.DeletePolicy(ctx, &iam.DeletePolicyInput{PolicyArn: policy.Policy.Arn})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "DeleteConflict" {
		t.Fatalf("DeletePolicy(attached) error = %v, want DeleteConflict", err)
	}
	if _, err := client.DetachRolePolicy(ctx, &iam.DetachRolePolicyInput{RoleName: aws.String("policy-role"), PolicyArn: policy.Policy.Arn}); err != nil {
		t.Fatalf("DetachRolePolicy() error = %v", err)
	}
	if _, err := client.DeletePolicy(ctx, &iam.DeletePolicyInput{PolicyArn: policy.Policy.Arn}); err != nil {
		t.Fatalf("DeletePolicy(after detach) error = %v", err)
	}
}

func TestIAMCompatibilityAdapterAuthorizesAndAuditsRoleOperationsWithAWSSDK(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewIAMAdapter(
		compataws.WithIAMAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"iam:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"iam:UpdateRole"}, Resources: []string{"*"}},
		)),
		compataws.WithIAMAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := iam.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateRole(context.Background(), &iam.CreateRoleInput{
		RoleName:                 aws.String("authz-homeport-orders"),
		AssumeRolePolicyDocument: aws.String(`{"Version":"2012-10-17","Statement":[]}`),
		Description:              aws.String("initial role"),
	}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	_, err := client.UpdateRole(context.Background(), &iam.UpdateRoleInput{
		RoleName:    aws.String("authz-homeport-orders"),
		Description: aws.String("denied role"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("UpdateRole(denied) error = %v, want AccessDenied", err)
	}

	got, err := client.GetRole(context.Background(), &iam.GetRoleInput{RoleName: aws.String("authz-homeport-orders")})
	if err != nil {
		t.Fatalf("GetRole() error = %v", err)
	}
	if aws.ToString(got.Role.Description) != "initial role" {
		t.Fatalf("GetRole() description = %q, want denied update to preserve initial role", aws.ToString(got.Role.Description))
	}
	assertDecision(t, auditLog.Decisions(), "iam:CreateRole", true)
	assertDecision(t, auditLog.Decisions(), "iam:UpdateRole", false)
}

func TestIAMCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewIAMAdapter())
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

	policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
	var created struct {
		Role struct {
			RoleName string `json:"RoleName"`
			Arn      string `json:"Arn"`
		} `json:"Role"`
	}
	if err := json.Unmarshal(runAWS("iam", "create-role", "--role-name", "cli-homeport-orders", "--assume-role-policy-document", policy, "--description", "initial role"), &created); err != nil {
		t.Fatalf("decode create-role output: %v", err)
	}
	if created.Role.RoleName != "cli-homeport-orders" || created.Role.Arn == "" {
		t.Fatalf("create-role = %#v, want cli-homeport-orders role", created.Role)
	}

	var got struct {
		Role struct {
			Arn         string `json:"Arn"`
			Description string `json:"Description"`
		} `json:"Role"`
	}
	if err := json.Unmarshal(runAWS("iam", "get-role", "--role-name", "cli-homeport-orders"), &got); err != nil {
		t.Fatalf("decode get-role output: %v", err)
	}
	if got.Role.Arn != created.Role.Arn {
		t.Fatalf("get-role = %#v, want created role", got.Role)
	}

	var listed struct {
		Roles []struct {
			RoleName string `json:"RoleName"`
		} `json:"Roles"`
	}
	if err := json.Unmarshal(runAWS("iam", "list-roles"), &listed); err != nil {
		t.Fatalf("decode list-roles output: %v", err)
	}
	if len(listed.Roles) != 1 || listed.Roles[0].RoleName != "cli-homeport-orders" {
		t.Fatalf("list-roles = %#v, want cli-homeport-orders", listed.Roles)
	}

	runAWS("iam", "update-role", "--role-name", "cli-homeport-orders", "--description", "updated role")
	if err := json.Unmarshal(runAWS("iam", "get-role", "--role-name", "cli-homeport-orders"), &got); err != nil {
		t.Fatalf("decode get-role after update output: %v", err)
	}
	if got.Role.Description != "updated role" {
		t.Fatalf("get-role after update description = %q, want updated role", got.Role.Description)
	}

	runAWS("iam", "delete-role", "--role-name", "cli-homeport-orders")
	if err := json.Unmarshal(runAWS("iam", "list-roles"), &listed); err != nil {
		t.Fatalf("decode list-roles after delete output: %v", err)
	}
	if len(listed.Roles) != 0 {
		t.Fatalf("list-roles after delete = %#v, want no roles", listed.Roles)
	}
}

func TestIAMCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewIAMAdapter())
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
    iam = %q
  }
}

resource "aws_iam_role" "deploy" {
  name = "terraform-homeport-orders"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action = "sts:AssumeRole"
    }]
  })
  description = "Terraform role"
  tags = {
    env = "test"
  }
}

output "role_arn" {
  value = aws_iam_role.deploy.arn
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

	if arn := strings.TrimSpace(string(runTerraform("output", "-raw", "role_arn"))); arn == "" {
		t.Fatalf("terraform output role_arn is empty")
	}
}

func TestIAMCompatibilityAdapterRejectsExpiredCredential(t *testing.T) {
	server := httptest.NewServer(compataws.NewIAMAdapter(compataws.WithIAMAuthorizer(authz.NewPolicyAuthorizer(
		authz.Rule{Effect: authz.Allow, Actions: []string{"iam:*"}, Resources: []string{"*"}},
		authz.Rule{
			Effect:    authz.Deny,
			Actions:   []string{"iam:ListRoles"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_expired", Values: []string{"true"}},
			},
		},
	))))
	defer server.Close()
	client := iam.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *iam.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Expired", "true"))
	})
	_, err := client.ListRoles(context.Background(), &iam.ListRolesInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("ListRoles(expired credential) error = %v, want AccessDenied", err)
	}
}
