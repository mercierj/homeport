package compat_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestALBCompatibilityAdapterLifecycleWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewALBAdapter())
	defer server.Close()

	client := elbv2.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(options *elbv2.Options) {
		options.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()

	created, err := client.CreateLoadBalancer(ctx, &elbv2.CreateLoadBalancerInput{
		Name:    aws.String("edge"),
		Subnets: []string{"subnet-a", "subnet-b"},
		Type:    types.LoadBalancerTypeEnumApplication,
	})
	if err != nil || len(created.LoadBalancers) != 1 {
		t.Fatalf("CreateLoadBalancer() = %#v, %v; want one load balancer", created, err)
	}
	loadBalancer := created.LoadBalancers[0]
	if aws.ToString(loadBalancer.LoadBalancerName) != "edge" || aws.ToString(loadBalancer.LoadBalancerArn) == "" || aws.ToString(loadBalancer.DNSName) == "" {
		t.Fatalf("CreateLoadBalancer() = %#v, want named ARN-backed load balancer", loadBalancer)
	}

	described, err := client.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{LoadBalancerArns: []string{aws.ToString(loadBalancer.LoadBalancerArn)}})
	if err != nil || len(described.LoadBalancers) != 1 || aws.ToString(described.LoadBalancers[0].LoadBalancerArn) != aws.ToString(loadBalancer.LoadBalancerArn) {
		t.Fatalf("DescribeLoadBalancers() = %#v, %v; want created load balancer", described, err)
	}

	updated, err := client.ModifyLoadBalancerAttributes(ctx, &elbv2.ModifyLoadBalancerAttributesInput{
		LoadBalancerArn: loadBalancer.LoadBalancerArn,
		Attributes:      []types.LoadBalancerAttribute{{Key: aws.String("deletion_protection.enabled"), Value: aws.String("true")}},
	})
	if err != nil || len(updated.Attributes) != 1 || aws.ToString(updated.Attributes[0].Value) != "true" {
		t.Fatalf("ModifyLoadBalancerAttributes() = %#v, %v; want updated attribute", updated, err)
	}

	if _, err := client.DeleteLoadBalancer(ctx, &elbv2.DeleteLoadBalancerInput{LoadBalancerArn: loadBalancer.LoadBalancerArn}); err != nil {
		t.Fatalf("DeleteLoadBalancer() error = %v", err)
	}
}

func TestALBCompatibilityAdapterPaginatesLoadBalancersWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewALBAdapter())
	defer server.Close()
	client := elbv2.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(options *elbv2.Options) {
		options.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	for _, name := range []string{"alpha", "bravo"} {
		if _, err := client.CreateLoadBalancer(ctx, &elbv2.CreateLoadBalancerInput{Name: aws.String(name), Subnets: []string{"subnet-a", "subnet-b"}}); err != nil {
			t.Fatalf("CreateLoadBalancer(%s) error = %v", name, err)
		}
	}

	first, err := client.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{PageSize: aws.Int32(1)})
	if err != nil || len(first.LoadBalancers) != 1 || aws.ToString(first.LoadBalancers[0].LoadBalancerName) != "alpha" || aws.ToString(first.NextMarker) == "" {
		t.Fatalf("DescribeLoadBalancers(first page) = %#v, %v; want alpha and marker", first, err)
	}
	second, err := client.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{PageSize: aws.Int32(1), Marker: first.NextMarker})
	if err != nil || len(second.LoadBalancers) != 1 || aws.ToString(second.LoadBalancers[0].LoadBalancerName) != "bravo" || second.NextMarker != nil {
		t.Fatalf("DescribeLoadBalancers(second page) = %#v, %v; want bravo without marker", second, err)
	}
}

func TestALBCompatibilityAdapterAuthorizesAndAuditsCreate(t *testing.T) {
	decisions := make([]authz.Decision, 0)
	server := httptest.NewServer(compataws.NewALBAdapter(
		compataws.WithALBAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{Effect: authz.Deny, Actions: []string{"elasticloadbalancing:CreateLoadBalancer"}, Resources: []string{"*"}})),
		compataws.WithALBAuditSink(func(decision authz.Decision) { decisions = append(decisions, decision) }),
	))
	defer server.Close()
	client := elbv2.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(options *elbv2.Options) {
		options.BaseEndpoint = aws.String(server.URL)
	})
	_, err := client.CreateLoadBalancer(context.Background(), &elbv2.CreateLoadBalancerInput{Name: aws.String("denied"), Subnets: []string{"subnet-a", "subnet-b"}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("CreateLoadBalancer(denied) error = %v, want AccessDenied", err)
	}
	if len(decisions) != 1 || decisions[0].Allowed {
		t.Fatalf("audit decisions = %#v, want one denied decision", decisions)
	}
}

func TestALBCompatibilityAdapterRejectsQuota(t *testing.T) {
	server := httptest.NewServer(compataws.NewALBAdapter(compataws.WithALBQuota(1)))
	defer server.Close()
	client := elbv2.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(options *elbv2.Options) {
		options.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	for _, name := range []string{"first", "second"} {
		_, err := client.CreateLoadBalancer(ctx, &elbv2.CreateLoadBalancerInput{Name: aws.String(name), Subnets: []string{"subnet-a", "subnet-b"}})
		if name == "first" && err != nil {
			t.Fatalf("CreateLoadBalancer(first) error = %v", err)
		}
		if name == "second" {
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "TooManyLoadBalancers" {
				t.Fatalf("CreateLoadBalancer(second) error = %v, want TooManyLoadBalancers", err)
			}
		}
	}
}

func TestALBCompatibilityAdapterAuthorizesDescribeForRequestedARN(t *testing.T) {
	arn := "arn:aws:elasticloadbalancing:us-east-1:000000000000:loadbalancer/app/scoped/homeport-1"
	server := httptest.NewServer(compataws.NewALBAdapter(compataws.WithALBAuthorizer(authz.NewPolicyAuthorizer(
		authz.Rule{Effect: authz.Allow, Actions: []string{"elasticloadbalancing:CreateLoadBalancer"}, Resources: []string{"*"}},
		authz.Rule{Effect: authz.Allow, Actions: []string{"elasticloadbalancing:DescribeLoadBalancers"}, Resources: []string{arn}},
	))))
	defer server.Close()
	client := elbv2.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(options *elbv2.Options) {
		options.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	created, err := client.CreateLoadBalancer(ctx, &elbv2.CreateLoadBalancerInput{Name: aws.String("scoped"), Subnets: []string{"subnet-a", "subnet-b"}})
	if err != nil {
		t.Fatalf("CreateLoadBalancer() error = %v", err)
	}
	if aws.ToString(created.LoadBalancers[0].LoadBalancerArn) != arn {
		t.Fatalf("CreateLoadBalancer() ARN = %q, want %q", aws.ToString(created.LoadBalancers[0].LoadBalancerArn), arn)
	}
	described, err := client.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{LoadBalancerArns: []string{arn}})
	if err != nil || len(described.LoadBalancers) != 1 {
		t.Fatalf("DescribeLoadBalancers(scoped ARN) = %#v, %v; want authorized result", described, err)
	}
}

func TestALBCompatibilityAdapterRejectsNonPostRequests(t *testing.T) {
	server := httptest.NewServer(compataws.NewALBAdapter())
	defer server.Close()
	client := elbv2.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(options *elbv2.Options) {
		options.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	created, err := client.CreateLoadBalancer(ctx, &elbv2.CreateLoadBalancerInput{Name: aws.String("method-check"), Subnets: []string{"subnet-a", "subnet-b"}})
	if err != nil {
		t.Fatalf("CreateLoadBalancer() error = %v", err)
	}
	response, err := http.Get(server.URL + "?Action=DeleteLoadBalancer&LoadBalancerArn=" + url.QueryEscape(aws.ToString(created.LoadBalancers[0].LoadBalancerArn)))
	if err != nil {
		t.Fatalf("GET DeleteLoadBalancer request error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET DeleteLoadBalancer status = %d, want %d", response.StatusCode, http.StatusMethodNotAllowed)
	}
	described, err := client.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{LoadBalancerArns: []string{aws.ToString(created.LoadBalancers[0].LoadBalancerArn)}})
	if err != nil || len(described.LoadBalancers) != 1 {
		t.Fatalf("DescribeLoadBalancers(after GET) = %#v, %v; want existing load balancer", described, err)
	}
}

func TestALBCompatibilityAdapterRejectsInvalidCreateInputs(t *testing.T) {
	for _, test := range []struct {
		name  string
		input *elbv2.CreateLoadBalancerInput
	}{
		{name: "missing subnets", input: &elbv2.CreateLoadBalancerInput{Name: aws.String("no-subnets")}},
		{name: "invalid scheme", input: &elbv2.CreateLoadBalancerInput{Name: aws.String("bad-scheme"), Subnets: []string{"subnet-a", "subnet-b"}, Scheme: types.LoadBalancerSchemeEnum("bogus")}},
		{name: "invalid type", input: &elbv2.CreateLoadBalancerInput{Name: aws.String("bad-type"), Subnets: []string{"subnet-a", "subnet-b"}, Type: types.LoadBalancerTypeEnum("bogus")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(compataws.NewALBAdapter())
			defer server.Close()
			client := elbv2.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(options *elbv2.Options) {
				options.BaseEndpoint = aws.String(server.URL)
			})
			_, err := client.CreateLoadBalancer(context.Background(), test.input)
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationError" {
				t.Fatalf("CreateLoadBalancer() error = %v, want ValidationError", err)
			}
		})
	}
}
