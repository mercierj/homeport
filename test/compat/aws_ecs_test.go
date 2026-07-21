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
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestECSCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateService(context.Background(), &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(2),
		LaunchType:     types.LaunchTypeExternal,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if created.Service == nil || aws.ToString(created.Service.ServiceName) != "web" || created.Service.DesiredCount != 2 || aws.ToString(created.Service.Status) != "ACTIVE" {
		t.Fatalf("CreateService() = %#v, want active web service", created.Service)
	}

	described, err := client.DescribeServices(context.Background(), &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || aws.ToString(described.Services[0].ServiceArn) != aws.ToString(created.Service.ServiceArn) {
		t.Fatalf("DescribeServices() = %#v, want created service", described.Services)
	}

	listed, err := client.ListServices(context.Background(), &ecs.ListServicesInput{
		Cluster: aws.String("default"),
	})
	if err != nil {
		t.Fatalf("ListServices() error = %v", err)
	}
	if len(listed.ServiceArns) != 1 || listed.ServiceArns[0] != aws.ToString(created.Service.ServiceArn) {
		t.Fatalf("ListServices() = %#v, want created service ARN", listed.ServiceArns)
	}

	updated, err := client.UpdateService(context.Background(), &ecs.UpdateServiceInput{
		Cluster:      aws.String("default"),
		Service:      aws.String("web"),
		DesiredCount: aws.Int32(3),
	})
	if err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if updated.Service == nil || updated.Service.DesiredCount != 3 {
		t.Fatalf("UpdateService() = %#v, want desired count 3", updated.Service)
	}

	if _, err := client.DeleteService(context.Background(), &ecs.DeleteServiceInput{
		Cluster: aws.String("default"),
		Service: aws.String("web"),
		Force:   aws.Bool(true),
	}); err != nil {
		t.Fatalf("DeleteService() error = %v", err)
	}
	listed, err = client.ListServices(context.Background(), &ecs.ListServicesInput{
		Cluster: aws.String("default"),
	})
	if err != nil {
		t.Fatalf("ListServices(after delete) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after delete) = %#v, want no services", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterManagesTaskDefinitionsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	registered, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:      aws.String("web"),
		Cpu:         aws.String("256"),
		Memory:      aws.String("512"),
		NetworkMode: types.NetworkModeBridge,
		ContainerDefinitions: []types.ContainerDefinition{{
			Name:  aws.String("app"),
			Image: aws.String("nginx:latest"),
			Cpu:   128,
		}},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition() error = %v", err)
	}
	if registered.TaskDefinition == nil || aws.ToString(registered.TaskDefinition.Family) != "web" || registered.TaskDefinition.Revision != 1 || registered.TaskDefinition.Status != types.TaskDefinitionStatusActive {
		t.Fatalf("RegisterTaskDefinition() = %#v, want active web:1", registered.TaskDefinition)
	}
	taskDefinitionArn := aws.ToString(registered.TaskDefinition.TaskDefinitionArn)

	described, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: aws.String("web:1"),
	})
	if err != nil {
		t.Fatalf("DescribeTaskDefinition() error = %v", err)
	}
	if described.TaskDefinition == nil || aws.ToString(described.TaskDefinition.TaskDefinitionArn) != taskDefinitionArn || len(described.TaskDefinition.ContainerDefinitions) != 1 || aws.ToString(described.TaskDefinition.ContainerDefinitions[0].Image) != "nginx:latest" {
		t.Fatalf("DescribeTaskDefinition() = %#v, want registered task definition", described.TaskDefinition)
	}

	listed, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: aws.String("web"),
	})
	if err != nil {
		t.Fatalf("ListTaskDefinitions() error = %v", err)
	}
	if len(listed.TaskDefinitionArns) != 1 || listed.TaskDefinitionArns[0] != taskDefinitionArn {
		t.Fatalf("ListTaskDefinitions() = %#v, want registered task definition", listed.TaskDefinitionArns)
	}

	deregistered, err := client.DeregisterTaskDefinition(ctx, &ecs.DeregisterTaskDefinitionInput{
		TaskDefinition: aws.String(taskDefinitionArn),
	})
	if err != nil {
		t.Fatalf("DeregisterTaskDefinition() error = %v", err)
	}
	if deregistered.TaskDefinition == nil || deregistered.TaskDefinition.Status != types.TaskDefinitionStatusInactive {
		t.Fatalf("DeregisterTaskDefinition() = %#v, want inactive task definition", deregistered.TaskDefinition)
	}
	listed, err = client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: aws.String("web"),
	})
	if err != nil {
		t.Fatalf("ListTaskDefinitions(after deregister) error = %v", err)
	}
	if len(listed.TaskDefinitionArns) != 0 {
		t.Fatalf("ListTaskDefinitions(after deregister) = %#v, want no active task definitions", listed.TaskDefinitionArns)
	}
}

func TestECSCompatibilityAdapterPreservesTaskDefinitionDetailsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	registered, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String("detailed-web"),
		ExecutionRoleArn:        aws.String("arn:aws:iam::123456789012:role/ecsTaskExecutionRole"),
		TaskRoleArn:             aws.String("arn:aws:iam::123456789012:role/appTaskRole"),
		RequiresCompatibilities: []types.Compatibility{types.CompatibilityExternal},
		RuntimePlatform:         &types.RuntimePlatform{CpuArchitecture: types.CPUArchitectureX8664, OperatingSystemFamily: types.OSFamilyLinux},
		Volumes:                 []types.Volume{{Name: aws.String("cache")}},
		ContainerDefinitions:    []types.ContainerDefinition{{Name: aws.String("app"), Image: aws.String("nginx:latest")}},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition() error = %v", err)
	}

	described, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: registered.TaskDefinition.TaskDefinitionArn,
	})
	if err != nil {
		t.Fatalf("DescribeTaskDefinition() error = %v", err)
	}
	taskDefinition := described.TaskDefinition
	if taskDefinition == nil ||
		aws.ToString(taskDefinition.ExecutionRoleArn) != "arn:aws:iam::123456789012:role/ecsTaskExecutionRole" ||
		aws.ToString(taskDefinition.TaskRoleArn) != "arn:aws:iam::123456789012:role/appTaskRole" ||
		len(taskDefinition.RequiresCompatibilities) != 1 || taskDefinition.RequiresCompatibilities[0] != types.CompatibilityExternal ||
		taskDefinition.RuntimePlatform == nil || taskDefinition.RuntimePlatform.CpuArchitecture != types.CPUArchitectureX8664 || taskDefinition.RuntimePlatform.OperatingSystemFamily != types.OSFamilyLinux ||
		len(taskDefinition.Volumes) != 1 || aws.ToString(taskDefinition.Volumes[0].Name) != "cache" {
		t.Fatalf("DescribeTaskDefinition() = %#v, want detailed task definition fields", taskDefinition)
	}
}

func TestECSCompatibilityAdapterRejectsTaskDefinitionWithoutContainers(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family:               aws.String("empty-task"),
		ContainerDefinitions: []types.ContainerDefinition{},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ClientException" {
		t.Fatalf("RegisterTaskDefinition(empty containers) error = %v, want ClientException", err)
	}

	listed, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: aws.String("empty-task"),
	})
	if err != nil {
		t.Fatalf("ListTaskDefinitions(after rejected register) error = %v", err)
	}
	if len(listed.TaskDefinitionArns) != 0 {
		t.Fatalf("ListTaskDefinitions(after rejected register) = %#v, want none", listed.TaskDefinitionArns)
	}
}

func TestECSCompatibilityAdapterRejectsTaskDefinitionContainerWithoutImage(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family: aws.String("missing-image-task"),
		ContainerDefinitions: []types.ContainerDefinition{{
			Name: aws.String("app"),
		}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ClientException" {
		t.Fatalf("RegisterTaskDefinition(missing image) error = %v, want ClientException", err)
	}

	listed, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: aws.String("missing-image-task"),
	})
	if err != nil {
		t.Fatalf("ListTaskDefinitions(after rejected missing image) error = %v", err)
	}
	if len(listed.TaskDefinitionArns) != 0 {
		t.Fatalf("ListTaskDefinitions(after rejected missing image) = %#v, want none", listed.TaskDefinitionArns)
	}
}

func TestECSCompatibilityAdapterRejectsTaskDefinitionContainerWithoutName(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family: aws.String("missing-name-task"),
		ContainerDefinitions: []types.ContainerDefinition{{
			Image: aws.String("nginx:latest"),
		}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ClientException" {
		t.Fatalf("RegisterTaskDefinition(missing name) error = %v, want ClientException", err)
	}

	listed, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: aws.String("missing-name-task"),
	})
	if err != nil {
		t.Fatalf("ListTaskDefinitions(after rejected missing name) error = %v", err)
	}
	if len(listed.TaskDefinitionArns) != 0 {
		t.Fatalf("ListTaskDefinitions(after rejected missing name) = %#v, want none", listed.TaskDefinitionArns)
	}
}

func TestECSCompatibilityAdapterAuthorizesAndAuditsTaskDefinitionOperations(t *testing.T) {
	clientForDeniedAction := func(t *testing.T, action string) (*ecs.Client, *authz.AuditLog, func()) {
		t.Helper()
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewECSAdapter(
			compataws.WithECSAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: []string{"ecs:*"}, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{"ecs:" + action}, Resources: []string{"*"}},
			)),
			compataws.WithECSAuditSink(auditLog.Record),
		))
		client := ecs.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *ecs.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})
		return client, auditLog, server.Close
	}
	registerTaskDefinition := func(t *testing.T, ctx context.Context, client *ecs.Client, family string) string {
		t.Helper()
		registered, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
			Family: aws.String(family),
			ContainerDefinitions: []types.ContainerDefinition{{
				Name:  aws.String("app"),
				Image: aws.String("nginx:latest"),
			}},
		})
		if err != nil {
			t.Fatalf("RegisterTaskDefinition(seed) error = %v", err)
		}
		return aws.ToString(registered.TaskDefinition.TaskDefinitionArn)
	}

	tests := map[string]func(*testing.T, context.Context, *ecs.Client){
		"RegisterTaskDefinition": func(t *testing.T, ctx context.Context, client *ecs.Client) {
			_, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
				Family: aws.String("denied-register"),
				ContainerDefinitions: []types.ContainerDefinition{{
					Name:  aws.String("app"),
					Image: aws.String("nginx:latest"),
				}},
			})
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
				t.Fatalf("RegisterTaskDefinition(denied) error = %v, want AccessDenied", err)
			}
		},
		"DescribeTaskDefinition": func(t *testing.T, ctx context.Context, client *ecs.Client) {
			arn := registerTaskDefinition(t, ctx, client, "denied-describe")
			_, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
				TaskDefinition: aws.String(arn),
			})
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
				t.Fatalf("DescribeTaskDefinition(denied) error = %v, want AccessDenied", err)
			}
		},
		"ListTaskDefinitions": func(t *testing.T, ctx context.Context, client *ecs.Client) {
			registerTaskDefinition(t, ctx, client, "denied-list")
			_, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
				FamilyPrefix: aws.String("denied-list"),
			})
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
				t.Fatalf("ListTaskDefinitions(denied) error = %v, want AccessDenied", err)
			}
		},
		"DeregisterTaskDefinition": func(t *testing.T, ctx context.Context, client *ecs.Client) {
			arn := registerTaskDefinition(t, ctx, client, "denied-deregister")
			_, err := client.DeregisterTaskDefinition(ctx, &ecs.DeregisterTaskDefinitionInput{
				TaskDefinition: aws.String(arn),
			})
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
				t.Fatalf("DeregisterTaskDefinition(denied) error = %v, want AccessDenied", err)
			}
			described, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
				TaskDefinition: aws.String(arn),
			})
			if err != nil {
				t.Fatalf("DescribeTaskDefinition(after denied deregister) error = %v", err)
			}
			if described.TaskDefinition.Status != types.TaskDefinitionStatusActive {
				t.Fatalf("DescribeTaskDefinition(after denied deregister) status = %v, want ACTIVE", described.TaskDefinition.Status)
			}
		},
	}
	for action, run := range tests {
		t.Run(action, func(t *testing.T) {
			client, auditLog, cleanup := clientForDeniedAction(t, action)
			defer cleanup()
			run(t, context.Background(), client)
			assertDecision(t, auditLog.Decisions(), "ecs:"+action, false)
		})
	}
}

func TestECSCompatibilityAdapterPreservesServicePlacementWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	created, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("placed-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(1),
		LaunchType:     types.LaunchTypeExternal,
		PlacementConstraints: []types.PlacementConstraint{{
			Type:       types.PlacementConstraintTypeMemberOf,
			Expression: aws.String("attribute:disk == ssd"),
		}},
		PlacementStrategy: []types.PlacementStrategy{{
			Type:  types.PlacementStrategyTypeSpread,
			Field: aws.String("attribute:ecs.availability-zone"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if len(created.Service.PlacementConstraints) != 1 || created.Service.PlacementConstraints[0].Type != types.PlacementConstraintTypeMemberOf || aws.ToString(created.Service.PlacementConstraints[0].Expression) != "attribute:disk == ssd" {
		t.Fatalf("CreateService() placement constraints = %#v, want memberOf disk constraint", created.Service.PlacementConstraints)
	}
	if len(created.Service.PlacementStrategy) != 1 || created.Service.PlacementStrategy[0].Type != types.PlacementStrategyTypeSpread || aws.ToString(created.Service.PlacementStrategy[0].Field) != "attribute:ecs.availability-zone" {
		t.Fatalf("CreateService() placement strategy = %#v, want AZ spread strategy", created.Service.PlacementStrategy)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"placed-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || len(described.Services[0].PlacementConstraints) != 1 || len(described.Services[0].PlacementStrategy) != 1 {
		t.Fatalf("DescribeServices() placement = constraints %#v strategy %#v, want placement read-back", described.Services[0].PlacementConstraints, described.Services[0].PlacementStrategy)
	}
}

func TestECSCompatibilityAdapterUpdatesServicePlacementWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("updated-placement-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(1),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	updated, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster: aws.String("default"),
		Service: aws.String("updated-placement-web"),
		PlacementConstraints: []types.PlacementConstraint{{
			Type:       types.PlacementConstraintTypeDistinctInstance,
			Expression: aws.String("attribute:stack == blue"),
		}},
		PlacementStrategy: []types.PlacementStrategy{{
			Type:  types.PlacementStrategyTypeBinpack,
			Field: aws.String("cpu"),
		}},
	})
	if err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if len(updated.Service.PlacementConstraints) != 1 || updated.Service.PlacementConstraints[0].Type != types.PlacementConstraintTypeDistinctInstance {
		t.Fatalf("UpdateService() placement constraints = %#v, want distinctInstance", updated.Service.PlacementConstraints)
	}
	if len(updated.Service.PlacementStrategy) != 1 || updated.Service.PlacementStrategy[0].Type != types.PlacementStrategyTypeBinpack || aws.ToString(updated.Service.PlacementStrategy[0].Field) != "cpu" {
		t.Fatalf("UpdateService() placement strategy = %#v, want cpu binpack", updated.Service.PlacementStrategy)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"updated-placement-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || len(described.Services[0].PlacementConstraints) != 1 || len(described.Services[0].PlacementStrategy) != 1 {
		t.Fatalf("DescribeServices() placement = constraints %#v strategy %#v, want updated placement read-back", described.Services[0].PlacementConstraints, described.Services[0].PlacementStrategy)
	}
}

func TestECSCompatibilityAdapterPreservesDeploymentConfigurationWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	created, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("deploy-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(2),
		LaunchType:     types.LaunchTypeExternal,
		DeploymentConfiguration: &types.DeploymentConfiguration{
			MaximumPercent:        aws.Int32(150),
			MinimumHealthyPercent: aws.Int32(75),
			DeploymentCircuitBreaker: &types.DeploymentCircuitBreaker{
				Enable:   true,
				Rollback: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if created.Service.DeploymentConfiguration == nil || aws.ToInt32(created.Service.DeploymentConfiguration.MaximumPercent) != 150 || aws.ToInt32(created.Service.DeploymentConfiguration.MinimumHealthyPercent) != 75 {
		t.Fatalf("CreateService() deployment configuration = %#v, want configured percentages", created.Service.DeploymentConfiguration)
	}
	if created.Service.DeploymentConfiguration.DeploymentCircuitBreaker == nil || !created.Service.DeploymentConfiguration.DeploymentCircuitBreaker.Enable || !created.Service.DeploymentConfiguration.DeploymentCircuitBreaker.Rollback {
		t.Fatalf("CreateService() deployment circuit breaker = %#v, want enabled rollback", created.Service.DeploymentConfiguration.DeploymentCircuitBreaker)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"deploy-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || described.Services[0].DeploymentConfiguration == nil || aws.ToInt32(described.Services[0].DeploymentConfiguration.MaximumPercent) != 150 {
		t.Fatalf("DescribeServices() deployment configuration = %#v, want read-back", described.Services)
	}
}

func TestECSCompatibilityAdapterUpdatesDeploymentConfigurationWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("updated-deploy-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(2),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	updated, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster: aws.String("default"),
		Service: aws.String("updated-deploy-web"),
		DeploymentConfiguration: &types.DeploymentConfiguration{
			MaximumPercent:        aws.Int32(125),
			MinimumHealthyPercent: aws.Int32(50),
			DeploymentCircuitBreaker: &types.DeploymentCircuitBreaker{
				Enable:   true,
				Rollback: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if updated.Service.DeploymentConfiguration == nil || aws.ToInt32(updated.Service.DeploymentConfiguration.MaximumPercent) != 125 || aws.ToInt32(updated.Service.DeploymentConfiguration.MinimumHealthyPercent) != 50 {
		t.Fatalf("UpdateService() deployment configuration = %#v, want updated percentages", updated.Service.DeploymentConfiguration)
	}
	if updated.Service.DeploymentConfiguration.DeploymentCircuitBreaker == nil || !updated.Service.DeploymentConfiguration.DeploymentCircuitBreaker.Enable || !updated.Service.DeploymentConfiguration.DeploymentCircuitBreaker.Rollback {
		t.Fatalf("UpdateService() deployment circuit breaker = %#v, want enabled rollback", updated.Service.DeploymentConfiguration.DeploymentCircuitBreaker)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"updated-deploy-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || described.Services[0].DeploymentConfiguration == nil || aws.ToInt32(described.Services[0].DeploymentConfiguration.MaximumPercent) != 125 {
		t.Fatalf("DescribeServices() deployment configuration = %#v, want updated read-back", described.Services)
	}
}

func TestECSCompatibilityAdapterPreservesServiceDetailsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	created, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:              aws.String("default"),
		ServiceName:          aws.String("detailed-web"),
		TaskDefinition:       aws.String("web:1"),
		LaunchType:           types.LaunchTypeExternal,
		Role:                 aws.String("arn:aws:iam::123456789012:role/ecsServiceRole"),
		PlatformVersion:      aws.String("1.4.0"),
		SchedulingStrategy:   types.SchedulingStrategyDaemon,
		EnableExecuteCommand: true,
		PropagateTags:        types.PropagateTagsService,
		LoadBalancers: []types.LoadBalancer{{
			ContainerName:  aws.String("web"),
			ContainerPort:  aws.Int32(80),
			TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/web/abc"),
		}},
		NetworkConfiguration: &types.NetworkConfiguration{AwsvpcConfiguration: &types.AwsVpcConfiguration{
			Subnets:        []string{"subnet-123"},
			SecurityGroups: []string{"sg-123"},
		}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if created.Service == nil || aws.ToString(created.Service.RoleArn) != "arn:aws:iam::123456789012:role/ecsServiceRole" || aws.ToString(created.Service.PlatformVersion) != "1.4.0" || created.Service.SchedulingStrategy != types.SchedulingStrategyDaemon || !created.Service.EnableExecuteCommand || created.Service.PropagateTags != types.PropagateTagsService || len(created.Service.LoadBalancers) != 1 || created.Service.NetworkConfiguration == nil {
		t.Fatalf("CreateService() = %#v, want detailed service fields", created.Service)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"detailed-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || aws.ToString(described.Services[0].RoleArn) != "arn:aws:iam::123456789012:role/ecsServiceRole" || aws.ToString(described.Services[0].PlatformVersion) != "1.4.0" || described.Services[0].SchedulingStrategy != types.SchedulingStrategyDaemon || !described.Services[0].EnableExecuteCommand || described.Services[0].PropagateTags != types.PropagateTagsService || len(described.Services[0].LoadBalancers) != 1 || described.Services[0].NetworkConfiguration == nil {
		t.Fatalf("DescribeServices() = %#v, want detailed service fields", described.Services)
	}
}

func TestECSCompatibilityAdapterUpdatesServiceDetailsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("updated-detailed-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	updated, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:              aws.String("default"),
		Service:              aws.String("updated-detailed-web"),
		PlatformVersion:      aws.String("1.4.0"),
		EnableExecuteCommand: aws.Bool(true),
		PropagateTags:        types.PropagateTagsService,
		LoadBalancers: []types.LoadBalancer{{
			ContainerName:  aws.String("web"),
			ContainerPort:  aws.Int32(8080),
			TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/web-updated/def"),
		}},
		NetworkConfiguration: &types.NetworkConfiguration{AwsvpcConfiguration: &types.AwsVpcConfiguration{
			Subnets:        []string{"subnet-456"},
			SecurityGroups: []string{"sg-456"},
			AssignPublicIp: types.AssignPublicIpEnabled,
		}},
	})
	if err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if updated.Service == nil || aws.ToString(updated.Service.PlatformVersion) != "1.4.0" || !updated.Service.EnableExecuteCommand || updated.Service.PropagateTags != types.PropagateTagsService || len(updated.Service.LoadBalancers) != 1 || updated.Service.NetworkConfiguration == nil {
		t.Fatalf("UpdateService() = %#v, want updated service detail fields", updated.Service)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"updated-detailed-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || aws.ToString(described.Services[0].PlatformVersion) != "1.4.0" || !described.Services[0].EnableExecuteCommand || described.Services[0].PropagateTags != types.PropagateTagsService || len(described.Services[0].LoadBalancers) != 1 || described.Services[0].NetworkConfiguration == nil {
		t.Fatalf("DescribeServices() = %#v, want updated service detail fields", described.Services)
	}
}

func TestECSCompatibilityAdapterUpdatesDeploymentMetadataWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	created, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("deployment-metadata-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if len(created.Service.Deployments) != 1 || aws.ToString(created.Service.Deployments[0].Id) == "" {
		t.Fatalf("CreateService() deployments = %#v, want initial deployment", created.Service.Deployments)
	}
	initialDeploymentID := aws.ToString(created.Service.Deployments[0].Id)

	updated, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster: aws.String("default"),
		Service: aws.String("deployment-metadata-web"),
		LoadBalancers: []types.LoadBalancer{{
			ContainerName:  aws.String("web"),
			ContainerPort:  aws.Int32(8080),
			TargetGroupArn: aws.String("arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/web-deploy/ghi"),
		}},
	})
	if err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if len(updated.Service.Deployments) != 1 || aws.ToString(updated.Service.Deployments[0].Id) == initialDeploymentID || updated.Service.Deployments[0].RolloutState != types.DeploymentRolloutStateCompleted {
		t.Fatalf("UpdateService() deployments = %#v, want new completed deployment", updated.Service.Deployments)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"deployment-metadata-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || len(described.Services[0].Deployments) != 1 || aws.ToString(described.Services[0].Deployments[0].Id) != aws.ToString(updated.Service.Deployments[0].Id) {
		t.Fatalf("DescribeServices() deployments = %#v, want updated deployment metadata", described.Services)
	}
}

func TestECSCompatibilityAdapterPreservesAdditionalServiceDetailsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	created, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:                     aws.String("default"),
		ServiceName:                 aws.String("additional-detail-web"),
		TaskDefinition:              aws.String("web:1"),
		AvailabilityZoneRebalancing: types.AvailabilityZoneRebalancingEnabled,
		CapacityProviderStrategy: []types.CapacityProviderStrategyItem{{
			CapacityProvider: aws.String("FARGATE"),
			Weight:           1,
		}},
		DeploymentController:          &types.DeploymentController{Type: types.DeploymentControllerTypeCodeDeploy},
		EnableECSManagedTags:          true,
		HealthCheckGracePeriodSeconds: aws.Int32(30),
		ServiceRegistries: []types.ServiceRegistry{{
			RegistryArn: aws.String("arn:aws:servicediscovery:us-east-1:123456789012:service/srv-abc"),
		}},
		VolumeConfigurations: []types.ServiceVolumeConfiguration{{Name: aws.String("data")}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if created.Service == nil || created.Service.AvailabilityZoneRebalancing != types.AvailabilityZoneRebalancingEnabled || len(created.Service.CapacityProviderStrategy) != 1 || created.Service.DeploymentController == nil || created.Service.DeploymentController.Type != types.DeploymentControllerTypeCodeDeploy || !created.Service.EnableECSManagedTags || aws.ToInt32(created.Service.HealthCheckGracePeriodSeconds) != 30 || len(created.Service.ServiceRegistries) != 1 || len(created.Service.Deployments) != 1 || len(created.Service.Deployments[0].VolumeConfigurations) != 1 {
		t.Fatalf("CreateService() = %#v, want additional service fields", created.Service)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"additional-detail-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || described.Services[0].AvailabilityZoneRebalancing != types.AvailabilityZoneRebalancingEnabled || len(described.Services[0].CapacityProviderStrategy) != 1 || described.Services[0].DeploymentController == nil || described.Services[0].DeploymentController.Type != types.DeploymentControllerTypeCodeDeploy || !described.Services[0].EnableECSManagedTags || aws.ToInt32(described.Services[0].HealthCheckGracePeriodSeconds) != 30 || len(described.Services[0].ServiceRegistries) != 1 || len(described.Services[0].Deployments) != 1 || len(described.Services[0].Deployments[0].VolumeConfigurations) != 1 {
		t.Fatalf("DescribeServices() = %#v, want additional service fields", described.Services)
	}
}

func TestECSCompatibilityAdapterUpdatesAdditionalServiceDetailsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("updated-additional-detail-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	updated, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:                     aws.String("default"),
		Service:                     aws.String("updated-additional-detail-web"),
		AvailabilityZoneRebalancing: types.AvailabilityZoneRebalancingEnabled,
		CapacityProviderStrategy: []types.CapacityProviderStrategyItem{{
			CapacityProvider: aws.String("FARGATE"),
			Weight:           1,
		}},
		EnableECSManagedTags:          aws.Bool(true),
		HealthCheckGracePeriodSeconds: aws.Int32(45),
		ServiceRegistries: []types.ServiceRegistry{{
			RegistryArn: aws.String("arn:aws:servicediscovery:us-east-1:123456789012:service/srv-updated"),
		}},
		VolumeConfigurations: []types.ServiceVolumeConfiguration{{Name: aws.String("updated-data")}},
	})
	if err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if updated.Service == nil || updated.Service.AvailabilityZoneRebalancing != types.AvailabilityZoneRebalancingEnabled || len(updated.Service.CapacityProviderStrategy) != 1 || !updated.Service.EnableECSManagedTags || aws.ToInt32(updated.Service.HealthCheckGracePeriodSeconds) != 45 || len(updated.Service.ServiceRegistries) != 1 || len(updated.Service.Deployments) != 1 || len(updated.Service.Deployments[0].VolumeConfigurations) != 1 {
		t.Fatalf("UpdateService() = %#v, want updated additional service fields", updated.Service)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"updated-additional-detail-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || described.Services[0].AvailabilityZoneRebalancing != types.AvailabilityZoneRebalancingEnabled || len(described.Services[0].CapacityProviderStrategy) != 1 || !described.Services[0].EnableECSManagedTags || aws.ToInt32(described.Services[0].HealthCheckGracePeriodSeconds) != 45 || len(described.Services[0].ServiceRegistries) != 1 || len(described.Services[0].Deployments) != 1 || len(described.Services[0].Deployments[0].VolumeConfigurations) != 1 {
		t.Fatalf("DescribeServices() = %#v, want updated additional service fields", described.Services)
	}
}

func TestECSCompatibilityAdapterUpdatesDeploymentControllerWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("updated-controller-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	updated, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:              aws.String("default"),
		Service:              aws.String("updated-controller-web"),
		DeploymentController: &types.DeploymentController{Type: types.DeploymentControllerTypeExternal},
	})
	if err != nil {
		t.Fatalf("UpdateService() error = %v", err)
	}
	if updated.Service.DeploymentController == nil || updated.Service.DeploymentController.Type != types.DeploymentControllerTypeExternal {
		t.Fatalf("UpdateService() deployment controller = %#v, want EXTERNAL", updated.Service.DeploymentController)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"updated-controller-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices() error = %v", err)
	}
	if len(described.Services) != 1 || described.Services[0].DeploymentController == nil || described.Services[0].DeploymentController.Type != types.DeploymentControllerTypeExternal {
		t.Fatalf("DescribeServices() deployment controller = %#v, want EXTERNAL", described.Services)
	}
}

func TestECSCompatibilityAdapterManagesServiceTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	created, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("tagged-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(1),
		LaunchType:     types.LaunchTypeExternal,
		Tags: []types.Tag{{
			Key:   aws.String("env"),
			Value: aws.String("test"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	serviceARN := aws.ToString(created.Service.ServiceArn)

	listed, err := client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
		ResourceArn: aws.String(serviceARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource() error = %v", err)
	}
	if got := ecsTagMap(listed.Tags); got["env"] != "test" {
		t.Fatalf("ListTagsForResource() = %#v, want env=test", got)
	}

	if _, err := client.TagResource(ctx, &ecs.TagResourceInput{
		ResourceArn: aws.String(serviceARN),
		Tags: []types.Tag{{
			Key:   aws.String("owner"),
			Value: aws.String("platform"),
		}},
	}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	listed, err = client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
		ResourceArn: aws.String(serviceARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource(after tag) error = %v", err)
	}
	if got := ecsTagMap(listed.Tags); got["env"] != "test" || got["owner"] != "platform" {
		t.Fatalf("ListTagsForResource(after tag) = %#v, want merged tags", got)
	}

	if _, err := client.UntagResource(ctx, &ecs.UntagResourceInput{
		ResourceArn: aws.String(serviceARN),
		TagKeys:     []string{"env"},
	}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	listed, err = client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
		ResourceArn: aws.String(serviceARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource(after untag) error = %v", err)
	}
	if got := ecsTagMap(listed.Tags); got["env"] != "" || got["owner"] != "platform" {
		t.Fatalf("ListTagsForResource(after untag) = %#v, want owner tag only", got)
	}
}

func TestECSCompatibilityAdapterManagesTaskDefinitionTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	registered, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family: aws.String("tagged-task"),
		ContainerDefinitions: []types.ContainerDefinition{{
			Name:  aws.String("app"),
			Image: aws.String("nginx:latest"),
		}},
		Tags: []types.Tag{{
			Key:   aws.String("env"),
			Value: aws.String("test"),
		}},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition() error = %v", err)
	}
	taskDefinitionARN := aws.ToString(registered.TaskDefinition.TaskDefinitionArn)

	listed, err := client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
		ResourceArn: aws.String(taskDefinitionARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource() error = %v", err)
	}
	if got := ecsTagMap(listed.Tags); got["env"] != "test" {
		t.Fatalf("ListTagsForResource() = %#v, want env=test", got)
	}

	if _, err := client.TagResource(ctx, &ecs.TagResourceInput{
		ResourceArn: aws.String(taskDefinitionARN),
		Tags: []types.Tag{{
			Key:   aws.String("owner"),
			Value: aws.String("platform"),
		}},
	}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	listed, err = client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
		ResourceArn: aws.String(taskDefinitionARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource(after tag) error = %v", err)
	}
	if got := ecsTagMap(listed.Tags); got["env"] != "test" || got["owner"] != "platform" {
		t.Fatalf("ListTagsForResource(after tag) = %#v, want merged tags", got)
	}

	if _, err := client.UntagResource(ctx, &ecs.UntagResourceInput{
		ResourceArn: aws.String(taskDefinitionARN),
		TagKeys:     []string{"env"},
	}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	listed, err = client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
		ResourceArn: aws.String(taskDefinitionARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource(after untag) error = %v", err)
	}
	if got := ecsTagMap(listed.Tags); got["env"] != "" || got["owner"] != "platform" {
		t.Fatalf("ListTagsForResource(after untag) = %#v, want owner tag only", got)
	}
}

func TestECSCompatibilityAdapterDescribesTaskDefinitionTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	registered, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family: aws.String("describe-tagged-task"),
		ContainerDefinitions: []types.ContainerDefinition{{
			Name:  aws.String("app"),
			Image: aws.String("nginx:latest"),
		}},
		Tags: []types.Tag{{
			Key:   aws.String("env"),
			Value: aws.String("test"),
		}},
	})
	if err != nil {
		t.Fatalf("RegisterTaskDefinition() error = %v", err)
	}

	described, err := client.DescribeTaskDefinition(ctx, &ecs.DescribeTaskDefinitionInput{
		TaskDefinition: registered.TaskDefinition.TaskDefinitionArn,
		Include:        []types.TaskDefinitionField{types.TaskDefinitionFieldTags},
	})
	if err != nil {
		t.Fatalf("DescribeTaskDefinition() error = %v", err)
	}
	if got := ecsTagMap(described.Tags); got["env"] != "test" {
		t.Fatalf("DescribeTaskDefinition() tags = %#v, want env=test", got)
	}
}

func TestECSCompatibilityAdapterRejectsReservedTagKeysWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	created, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("reserved-tag-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
		Tags: []types.Tag{{
			Key:   aws.String("env"),
			Value: aws.String("test"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	_, err = client.TagResource(ctx, &ecs.TagResourceInput{
		ResourceArn: created.Service.ServiceArn,
		Tags: []types.Tag{{
			Key:   aws.String("aws:owner"),
			Value: aws.String("platform"),
		}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("TagResource(reserved key) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
		ResourceArn: created.Service.ServiceArn,
	})
	if err != nil {
		t.Fatalf("ListTagsForResource(after rejected reserved key) error = %v", err)
	}
	if got := ecsTagMap(listed.Tags); got["env"] != "test" || got["aws:owner"] != "" {
		t.Fatalf("ListTagsForResource(after rejected reserved key) = %#v, want only env=test", got)
	}
}

func TestECSCompatibilityAdapterRejectsReservedCreateServiceTagKeysWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("reserved-create-tag-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
		Tags: []types.Tag{{
			Key:   aws.String("aws:owner"),
			Value: aws.String("platform"),
		}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(reserved tag key) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{
		Cluster: aws.String("default"),
	})
	if err != nil {
		t.Fatalf("ListServices(after rejected reserved create tag) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected reserved create tag) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsReservedRegisterTaskDefinitionTagKeysWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
		Family: aws.String("reserved-register-tag-web"),
		ContainerDefinitions: []types.ContainerDefinition{{
			Name:  aws.String("web"),
			Image: aws.String("nginx:latest"),
		}},
		Tags: []types.Tag{{
			Key:   aws.String("aws:owner"),
			Value: aws.String("platform"),
		}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("RegisterTaskDefinition(reserved tag key) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: aws.String("reserved-register-tag-web"),
	})
	if err != nil {
		t.Fatalf("ListTaskDefinitions(after rejected reserved register tag) error = %v", err)
	}
	if len(listed.TaskDefinitionArns) != 0 {
		t.Fatalf("ListTaskDefinitions(after rejected reserved register tag) = %#v, want none", listed.TaskDefinitionArns)
	}
}

func ecsTagMap(tags []types.Tag) map[string]string {
	values := map[string]string{}
	for _, tag := range tags {
		values[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
	}
	return values
}

func TestECSCompatibilityAdapterPaginatesListsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	for i := 1; i <= 3; i++ {
		if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
			Cluster:        aws.String("default"),
			ServiceName:    aws.String(fmt.Sprintf("paged-%d", i)),
			TaskDefinition: aws.String("web:1"),
			LaunchType:     types.LaunchTypeExternal,
		}); err != nil {
			t.Fatalf("CreateService(%d) error = %v", i, err)
		}
		if _, err := client.RegisterTaskDefinition(ctx, &ecs.RegisterTaskDefinitionInput{
			Family: aws.String("paged"),
			ContainerDefinitions: []types.ContainerDefinition{{
				Name:  aws.String("app"),
				Image: aws.String("nginx:latest"),
			}},
		}); err != nil {
			t.Fatalf("RegisterTaskDefinition(%d) error = %v", i, err)
		}
	}

	services, err := client.ListServices(ctx, &ecs.ListServicesInput{
		Cluster:    aws.String("default"),
		MaxResults: aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListServices() error = %v", err)
	}
	if len(services.ServiceArns) != 2 || services.NextToken == nil {
		t.Fatalf("ListServices() = %#v next %v, want first page of 2", services.ServiceArns, services.NextToken)
	}
	services, err = client.ListServices(ctx, &ecs.ListServicesInput{
		Cluster:    aws.String("default"),
		MaxResults: aws.Int32(2),
		NextToken:  services.NextToken,
	})
	if err != nil {
		t.Fatalf("ListServices(next) error = %v", err)
	}
	if len(services.ServiceArns) != 1 || services.NextToken != nil {
		t.Fatalf("ListServices(next) = %#v next %v, want final page of 1", services.ServiceArns, services.NextToken)
	}

	taskDefinitions, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: aws.String("paged"),
		MaxResults:   aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListTaskDefinitions() error = %v", err)
	}
	if len(taskDefinitions.TaskDefinitionArns) != 2 || taskDefinitions.NextToken == nil {
		t.Fatalf("ListTaskDefinitions() = %#v next %v, want first page of 2", taskDefinitions.TaskDefinitionArns, taskDefinitions.NextToken)
	}
	taskDefinitions, err = client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{
		FamilyPrefix: aws.String("paged"),
		MaxResults:   aws.Int32(2),
		NextToken:    taskDefinitions.NextToken,
	})
	if err != nil {
		t.Fatalf("ListTaskDefinitions(next) error = %v", err)
	}
	if len(taskDefinitions.TaskDefinitionArns) != 1 || taskDefinitions.NextToken != nil {
		t.Fatalf("ListTaskDefinitions(next) = %#v next %v, want final page of 1", taskDefinitions.TaskDefinitionArns, taskDefinitions.NextToken)
	}
}

func TestECSCompatibilityAdapterRejectsInvalidPaginationWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	cases := []struct {
		name string
		call func() error
	}{
		{
			name: "ListServices invalid max",
			call: func() error {
				_, err := client.ListServices(ctx, &ecs.ListServicesInput{MaxResults: aws.Int32(0)})
				return err
			},
		},
		{
			name: "ListServices invalid token",
			call: func() error {
				_, err := client.ListServices(ctx, &ecs.ListServicesInput{NextToken: aws.String("not-a-token")})
				return err
			},
		},
		{
			name: "ListTaskDefinitions invalid max",
			call: func() error {
				_, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{MaxResults: aws.Int32(101)})
				return err
			},
		},
		{
			name: "ListTaskDefinitions invalid token",
			call: func() error {
				_, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{NextToken: aws.String("not-a-token")})
				return err
			},
		},
		{
			name: "ListTaskDefinitions invalid status",
			call: func() error {
				_, err := client.ListTaskDefinitions(ctx, &ecs.ListTaskDefinitionsInput{Status: types.TaskDefinitionStatus("BROKEN")})
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var apiErr smithy.APIError
			if err := tc.call(); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
				t.Fatalf("%s error = %v, want InvalidParameterException", tc.name, err)
			}
		})
	}
}

func TestECSCompatibilityAdapterReturnsLimitExceededWhenServiceQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter(compataws.WithECSServiceQuota(1)))
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("quota-a"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService(first) error = %v", err)
	}

	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("quota-b"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("CreateService(over quota) error = %v, want LimitExceededException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after quota) error = %v", err)
	}
	if len(listed.ServiceArns) != 1 || !strings.Contains(listed.ServiceArns[0], "/quota-a") {
		t.Fatalf("ListServices(after quota) = %#v, want only quota-a", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsNegativeDesiredCount(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("negative-count-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(-1),
		LaunchType:     types.LaunchTypeExternal,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(negative desired count) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected desired count) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected desired count) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsNegativeHealthCheckGracePeriod(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:                       aws.String("default"),
		ServiceName:                   aws.String("negative-health-grace-web"),
		TaskDefinition:                aws.String("web:1"),
		LaunchType:                    types.LaunchTypeExternal,
		HealthCheckGracePeriodSeconds: aws.Int32(-1),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(negative health-check grace) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected health-check grace) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected health-check grace) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsInvalidCapacityProviderStrategy(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("invalid-capacity-provider-web"),
		TaskDefinition: aws.String("web:1"),
		CapacityProviderStrategy: []types.CapacityProviderStrategyItem{
			{CapacityProvider: aws.String("FARGATE"), Base: -1, Weight: 1},
			{CapacityProvider: aws.String("FARGATE_SPOT"), Weight: -1},
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(invalid capacity provider strategy) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected capacity provider strategy) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected capacity provider strategy) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsInvalidDeploymentConfiguration(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("invalid-deployment-config-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
		DeploymentConfiguration: &types.DeploymentConfiguration{
			MaximumPercent:        aws.Int32(201),
			MinimumHealthyPercent: aws.Int32(101),
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(invalid deployment configuration) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected deployment configuration) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected deployment configuration) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsInvalidPlacementOptions(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("invalid-placement-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
		PlacementConstraints: []types.PlacementConstraint{{
			Type: types.PlacementConstraintType("invalidConstraint"),
		}},
		PlacementStrategy: []types.PlacementStrategy{{
			Type: types.PlacementStrategyType("invalidStrategy"),
		}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(invalid placement options) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected placement options) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected placement options) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsMissingServiceTaskDefinition(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:     aws.String("default"),
		ServiceName: aws.String("missing-task-web"),
		LaunchType:  types.LaunchTypeExternal,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ClientException" {
		t.Fatalf("CreateService(missing task definition) error = %v, want ClientException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after missing task definition) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after missing task definition) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsInvalidLaunchType(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("invalid-launch-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchType("SPACESHIP"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(invalid launch type) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected launch type) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected launch type) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsInvalidSchedulingStrategy(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:            aws.String("default"),
		ServiceName:        aws.String("invalid-scheduling-web"),
		TaskDefinition:     aws.String("web:1"),
		LaunchType:         types.LaunchTypeExternal,
		SchedulingStrategy: types.SchedulingStrategy("SIDEWAYS"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(invalid scheduling strategy) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected scheduling strategy) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected scheduling strategy) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsInvalidDeploymentController(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:              aws.String("default"),
		ServiceName:          aws.String("invalid-controller-web"),
		TaskDefinition:       aws.String("web:1"),
		LaunchType:           types.LaunchTypeExternal,
		DeploymentController: &types.DeploymentController{Type: types.DeploymentControllerType("SPREADSHEET")},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(invalid deployment controller) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected deployment controller) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected deployment controller) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsInvalidAvailabilityZoneRebalancing(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:                     aws.String("default"),
		ServiceName:                 aws.String("invalid-az-rebalancing-web"),
		TaskDefinition:              aws.String("web:1"),
		LaunchType:                  types.LaunchTypeExternal,
		AvailabilityZoneRebalancing: types.AvailabilityZoneRebalancing("MAYBE"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(invalid availability-zone rebalancing) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected availability-zone rebalancing) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected availability-zone rebalancing) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsInvalidPropagateTags(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("invalid-propagate-tags-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
		PropagateTags:  types.PropagateTags("EVERYWHERE"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(invalid propagate tags) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected propagate tags) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected propagate tags) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsInvalidAssignPublicIP(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("invalid-assign-public-ip-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
		NetworkConfiguration: &types.NetworkConfiguration{AwsvpcConfiguration: &types.AwsVpcConfiguration{
			Subnets:        []string{"subnet-123"},
			AssignPublicIp: types.AssignPublicIp("MAYBE"),
		}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(invalid assign public IP) error = %v, want InvalidParameterException", err)
	}

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after rejected assign public IP) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after rejected assign public IP) = %#v, want none", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterRejectsNegativeUpdateDesiredCount(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("negative-update-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(2),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	_, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:      aws.String("default"),
		Service:      aws.String("negative-update-web"),
		DesiredCount: aws.Int32(-1),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("UpdateService(negative desired count) error = %v, want InvalidParameterException", err)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"negative-update-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices(after rejected update) error = %v", err)
	}
	if len(described.Services) != 1 || described.Services[0].DesiredCount != 2 {
		t.Fatalf("DescribeServices(after rejected update) = %#v, want desired count 2", described.Services)
	}
}

func TestECSCompatibilityAdapterRejectsUnknownUpdateTaskDefinition(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("unknown-task-update-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(2),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	_, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:        aws.String("default"),
		Service:        aws.String("unknown-task-update-web"),
		TaskDefinition: aws.String("missing:1"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ClientException" {
		t.Fatalf("UpdateService(unknown task definition) error = %v, want ClientException", err)
	}

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"unknown-task-update-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices(after rejected task update) error = %v", err)
	}
	if len(described.Services) != 1 || !strings.HasSuffix(aws.ToString(described.Services[0].TaskDefinition), "/web:1") {
		t.Fatalf("DescribeServices(after rejected task update) = %#v, want task definition web:1", described.Services)
	}
}

func TestECSCompatibilityAdapterReplaysIdempotentCreateService(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	input := &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("idempotent-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(2),
		LaunchType:     types.LaunchTypeExternal,
		ClientToken:    aws.String("create-service-token"),
	}
	created, err := client.CreateService(ctx, input)
	if err != nil {
		t.Fatalf("CreateService(first) error = %v", err)
	}
	replayed, err := client.CreateService(ctx, input)
	if err != nil {
		t.Fatalf("CreateService(replay) error = %v", err)
	}
	if aws.ToString(replayed.Service.ServiceArn) != aws.ToString(created.Service.ServiceArn) || replayed.Service.DesiredCount != 2 {
		t.Fatalf("CreateService(replay) = %#v, want original service", replayed.Service)
	}
}

func TestECSCompatibilityAdapterRejectsMismatchedCreateServiceClientToken(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter())
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("idempotent-mismatch-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(2),
		LaunchType:     types.LaunchTypeExternal,
		ClientToken:    aws.String("mismatched-create-service-token"),
	}); err != nil {
		t.Fatalf("CreateService(first) error = %v", err)
	}

	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("idempotent-mismatch-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(3),
		LaunchType:     types.LaunchTypeExternal,
		ClientToken:    aws.String("mismatched-create-service-token"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("CreateService(mismatched token) error = %v, want InvalidParameterException", err)
	}
}

func TestECSCompatibilityAdapterAuthorizesAndAuditsCreateService(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewECSAdapter(
		compataws.WithECSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ecs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"ecs:CreateService"}, Resources: []string{"*"}},
		)),
		compataws.WithECSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	_, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("denied-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("CreateService(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ecs:CreateService", false)

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after denied create) error = %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("ListServices(after denied create) = %#v, want no services", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterAuthorizesAndAuditsDeleteService(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewECSAdapter(
		compataws.WithECSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ecs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"ecs:DeleteService"}, Resources: []string{"*"}},
		)),
		compataws.WithECSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("kept-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	_, err := client.DeleteService(ctx, &ecs.DeleteServiceInput{
		Cluster: aws.String("default"),
		Service: aws.String("kept-web"),
		Force:   aws.Bool(true),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("DeleteService(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ecs:DeleteService", false)

	listed, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	if err != nil {
		t.Fatalf("ListServices(after denied delete) error = %v", err)
	}
	if len(listed.ServiceArns) != 1 || !strings.Contains(listed.ServiceArns[0], "/kept-web") {
		t.Fatalf("ListServices(after denied delete) = %#v, want kept-web", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterAuthorizesAndAuditsUpdateService(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewECSAdapter(
		compataws.WithECSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ecs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"ecs:UpdateService"}, Resources: []string{"*"}},
		)),
		compataws.WithECSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("steady-web"),
		TaskDefinition: aws.String("web:1"),
		DesiredCount:   aws.Int32(2),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	_, err := client.UpdateService(ctx, &ecs.UpdateServiceInput{
		Cluster:      aws.String("default"),
		Service:      aws.String("steady-web"),
		DesiredCount: aws.Int32(5),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("UpdateService(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ecs:UpdateService", false)

	described, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"steady-web"},
	})
	if err != nil {
		t.Fatalf("DescribeServices(after denied update) error = %v", err)
	}
	if len(described.Services) != 1 || described.Services[0].DesiredCount != 2 {
		t.Fatalf("DescribeServices(after denied update) = %#v, want desired count 2", described.Services)
	}
}

func TestECSCompatibilityAdapterAuthorizesAndAuditsTagResource(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewECSAdapter(
		compataws.WithECSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ecs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"ecs:TagResource"}, Resources: []string{"*"}},
		)),
		compataws.WithECSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	created, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("tag-authz-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
		Tags: []types.Tag{{
			Key:   aws.String("env"),
			Value: aws.String("test"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	serviceARN := aws.ToString(created.Service.ServiceArn)

	_, err = client.TagResource(ctx, &ecs.TagResourceInput{
		ResourceArn: aws.String(serviceARN),
		Tags: []types.Tag{{
			Key:   aws.String("owner"),
			Value: aws.String("platform"),
		}},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("TagResource(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ecs:TagResource", false)

	listed, err := client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
		ResourceArn: aws.String(serviceARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource(after denied tag) error = %v", err)
	}
	if got := ecsTagMap(listed.Tags); got["env"] != "test" || got["owner"] != "" {
		t.Fatalf("ListTagsForResource(after denied tag) = %#v, want only env=test", got)
	}
}

func TestECSCompatibilityAdapterAuthorizesAndAuditsUntagResource(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewECSAdapter(
		compataws.WithECSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ecs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"ecs:UntagResource"}, Resources: []string{"*"}},
		)),
		compataws.WithECSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	created, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("untag-authz-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
		Tags: []types.Tag{{
			Key:   aws.String("env"),
			Value: aws.String("test"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	serviceARN := aws.ToString(created.Service.ServiceArn)

	_, err = client.UntagResource(ctx, &ecs.UntagResourceInput{
		ResourceArn: aws.String(serviceARN),
		TagKeys:     []string{"env"},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("UntagResource(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ecs:UntagResource", false)

	listed, err := client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
		ResourceArn: aws.String(serviceARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource(after denied untag) error = %v", err)
	}
	if got := ecsTagMap(listed.Tags); got["env"] != "test" {
		t.Fatalf("ListTagsForResource(after denied untag) = %#v, want env=test", got)
	}
}

func TestECSCompatibilityAdapterAuthorizesAndAuditsListTagsForResource(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewECSAdapter(
		compataws.WithECSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ecs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"ecs:ListTagsForResource"}, Resources: []string{"*"}},
		)),
		compataws.WithECSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	created, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("list-tags-authz-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
		Tags: []types.Tag{{
			Key:   aws.String("env"),
			Value: aws.String("test"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	_, err = client.ListTagsForResource(ctx, &ecs.ListTagsForResourceInput{
		ResourceArn: created.Service.ServiceArn,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("ListTagsForResource(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ecs:ListTagsForResource", false)
}

func TestECSCompatibilityAdapterAuthorizesAndAuditsListServices(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewECSAdapter(
		compataws.WithECSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ecs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"ecs:ListServices"}, Resources: []string{"*"}},
		)),
		compataws.WithECSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("list-authz-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	_, err := client.ListServices(ctx, &ecs.ListServicesInput{Cluster: aws.String("default")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("ListServices(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ecs:ListServices", false)
}

func TestECSCompatibilityAdapterAuthorizesAndAuditsDescribeServices(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewECSAdapter(
		compataws.WithECSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"ecs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"ecs:DescribeServices"}, Resources: []string{"*"}},
		)),
		compataws.WithECSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := ecs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *ecs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateService(ctx, &ecs.CreateServiceInput{
		Cluster:        aws.String("default"),
		ServiceName:    aws.String("describe-authz-web"),
		TaskDefinition: aws.String("web:1"),
		LaunchType:     types.LaunchTypeExternal,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	_, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String("default"),
		Services: []string{"describe-authz-web"},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("DescribeServices(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "ecs:DescribeServices", false)
}

func TestECSCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewECSAdapter())
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
		Service struct {
			ServiceArn   string `json:"serviceArn"`
			ServiceName  string `json:"serviceName"`
			Status       string `json:"status"`
			DesiredCount int    `json:"desiredCount"`
		} `json:"service"`
	}
	if err := json.Unmarshal(runAWS("ecs", "create-service",
		"--cluster", "default",
		"--service-name", "cli-web",
		"--task-definition", "web:1",
		"--desired-count", "2",
		"--launch-type", "EXTERNAL",
	), &created); err != nil {
		t.Fatalf("decode create-service output: %v", err)
	}
	if created.Service.ServiceArn == "" || created.Service.ServiceName != "cli-web" || created.Service.Status != "ACTIVE" || created.Service.DesiredCount != 2 {
		t.Fatalf("create-service = %#v, want active cli-web service", created.Service)
	}

	var described struct {
		Services []struct {
			ServiceArn string `json:"serviceArn"`
		} `json:"services"`
	}
	if err := json.Unmarshal(runAWS("ecs", "describe-services", "--cluster", "default", "--services", "cli-web"), &described); err != nil {
		t.Fatalf("decode describe-services output: %v", err)
	}
	if len(described.Services) != 1 || described.Services[0].ServiceArn != created.Service.ServiceArn {
		t.Fatalf("describe-services = %#v, want created service", described.Services)
	}

	var listed struct {
		ServiceArns []string `json:"serviceArns"`
	}
	if err := json.Unmarshal(runAWS("ecs", "list-services", "--cluster", "default"), &listed); err != nil {
		t.Fatalf("decode list-services output: %v", err)
	}
	if len(listed.ServiceArns) != 1 || listed.ServiceArns[0] != created.Service.ServiceArn {
		t.Fatalf("list-services = %#v, want created service ARN", listed.ServiceArns)
	}

	var updated struct {
		Service struct {
			DesiredCount int `json:"desiredCount"`
		} `json:"service"`
	}
	if err := json.Unmarshal(runAWS("ecs", "update-service",
		"--cluster", "default",
		"--service", "cli-web",
		"--desired-count", "3",
	), &updated); err != nil {
		t.Fatalf("decode update-service output: %v", err)
	}
	if updated.Service.DesiredCount != 3 {
		t.Fatalf("update-service = %#v, want desired count 3", updated.Service)
	}

	runAWS("ecs", "delete-service", "--cluster", "default", "--service", "cli-web", "--force")
	if err := json.Unmarshal(runAWS("ecs", "list-services", "--cluster", "default"), &listed); err != nil {
		t.Fatalf("decode list-services after delete output: %v", err)
	}
	if len(listed.ServiceArns) != 0 {
		t.Fatalf("list-services after delete = %#v, want no services", listed.ServiceArns)
	}
}

func TestECSCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewECSAdapter())
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
    ecs = %q
  }
}

resource "aws_ecs_service" "deploy" {
  name                = "terraform-web"
  cluster             = "default"
  task_definition     = "web:1"
  desired_count       = 2
  launch_type         = "EXTERNAL"
  scheduling_strategy = "REPLICA"
}

output "service_name" {
  value = aws_ecs_service.deploy.name
}
`, server.URL)
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(config), 0o600); err != nil {
		t.Fatalf("write Terraform config: %v", err)
	}

	runTerraform := func(args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "terraform", args...)
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

	out := runTerraform("output", "-raw", "service_name")
	if strings.TrimSpace(string(out)) != "terraform-web" {
		t.Fatalf("terraform output service_name = %q, want terraform-web", strings.TrimSpace(string(out)))
	}
}

func TestECSCompatibilityAdapterShapesAuthorizerFailure(t *testing.T) {
	server := httptest.NewServer(compataws.NewECSAdapter(compataws.WithECSAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
		return authz.Decision{}, errors.New("authorizer unavailable")
	}))))
	defer server.Close()
	client := ecs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *ecs.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListServices(context.Background(), &ecs.ListServicesInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ServerException" {
		t.Fatalf("ListServices(authorizer failure) error = %v, want ServerException", err)
	}
}
