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
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestEKSCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
		Tags: map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	if created.Cluster == nil || aws.ToString(created.Cluster.Name) != "orders" || created.Cluster.Status != types.ClusterStatusActive {
		t.Fatalf("CreateCluster() = %#v, want active orders cluster", created.Cluster)
	}

	described, err := client.DescribeCluster(context.Background(), &eks.DescribeClusterInput{Name: aws.String("orders")})
	if err != nil {
		t.Fatalf("DescribeCluster() error = %v", err)
	}
	if described.Cluster == nil || aws.ToString(described.Cluster.Arn) != aws.ToString(created.Cluster.Arn) {
		t.Fatalf("DescribeCluster() = %#v, want created cluster", described.Cluster)
	}

	listed, err := client.ListClusters(context.Background(), &eks.ListClustersInput{})
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if len(listed.Clusters) != 1 || listed.Clusters[0] != "orders" {
		t.Fatalf("ListClusters() = %#v, want orders", listed.Clusters)
	}

	updated, err := client.UpdateClusterConfig(context.Background(), &eks.UpdateClusterConfigInput{
		Name:               aws.String("orders"),
		DeletionProtection: aws.Bool(true),
	})
	if err != nil {
		t.Fatalf("UpdateClusterConfig() error = %v", err)
	}
	if updated.Update == nil || updated.Update.Status != types.UpdateStatusSuccessful {
		t.Fatalf("UpdateClusterConfig() = %#v, want successful update", updated.Update)
	}

	deleted, err := client.DeleteCluster(context.Background(), &eks.DeleteClusterInput{Name: aws.String("orders")})
	if err != nil {
		t.Fatalf("DeleteCluster() error = %v", err)
	}
	if deleted.Cluster == nil || aws.ToString(deleted.Cluster.Name) != "orders" {
		t.Fatalf("DeleteCluster() = %#v, want deleted orders cluster", deleted.Cluster)
	}
	listed, err = client.ListClusters(context.Background(), &eks.ListClustersInput{})
	if err != nil {
		t.Fatalf("ListClusters(after delete) error = %v", err)
	}
	if len(listed.Clusters) != 0 {
		t.Fatalf("ListClusters(after delete) = %#v, want no clusters", listed.Clusters)
	}
}

func TestEKSCompatibilityAdapterAuthorizesAndAuditsCreateClusterWithAWSSDK(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewEKSAdapter(
		compataws.WithEKSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"eks:ListClusters"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"eks:CreateCluster"}, Resources: []string{"*"}},
		)),
		compataws.WithEKSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("denied"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("CreateCluster(denied) error = %v, want AccessDenied", err)
	}
	assertDecision(t, auditLog.Decisions(), "eks:CreateCluster", false)

	listed, err := client.ListClusters(context.Background(), &eks.ListClustersInput{})
	if err != nil {
		t.Fatalf("ListClusters() error = %v", err)
	}
	if len(listed.Clusters) != 0 {
		t.Fatalf("ListClusters() = %#v, want no clusters after denied create", listed.Clusters)
	}
}

func TestEKSCompatibilityAdapterReturnsResourceLimitExceededWhenClusterQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter(compataws.WithEKSClusterQuota(1)))
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, name := range []string{"quota-a", "quota-b"} {
		_, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
			Name:    aws.String(name),
			RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
			ResourcesVpcConfig: &types.VpcConfigRequest{
				SubnetIds: []string{"subnet-a", "subnet-b"},
			},
		})
		if name == "quota-a" && err != nil {
			t.Fatalf("CreateCluster(first) error = %v", err)
		}
		if name == "quota-b" {
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceLimitExceededException" {
				t.Fatalf("CreateCluster(over quota) error = %v, want ResourceLimitExceededException", err)
			}
		}
	}

	listed, err := client.ListClusters(context.Background(), &eks.ListClustersInput{})
	if err != nil {
		t.Fatalf("ListClusters(after quota) error = %v", err)
	}
	if !slices.Equal(listed.Clusters, []string{"quota-a"}) {
		t.Fatalf("ListClusters(after quota) = %#v, want only quota-a", listed.Clusters)
	}
}

func TestEKSCompatibilityAdapterAuthorizesAndAuditsClusterOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, extraAllowed ...string) (*eks.Client, *authz.AuditLog) {
		t.Helper()
		allowed := append([]string{"eks:CreateCluster"}, extraAllowed...)
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewEKSAdapter(
			compataws.WithEKSAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithEKSAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := eks.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *eks.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
			Name:    aws.String("orders"),
			RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
			ResourcesVpcConfig: &types.VpcConfigRequest{
				SubnetIds: []string{"subnet-a", "subnet-b"},
			},
		}); err != nil {
			t.Fatalf("CreateCluster() error = %v", err)
		}
		return client, auditLog
	}

	t.Run("ListClusters", func(t *testing.T) {
		client, auditLog := setup(t, "eks:ListClusters")
		_, err := client.ListClusters(context.Background(), &eks.ListClustersInput{})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("ListClusters(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "eks:ListClusters", false)
	})

	t.Run("DescribeCluster", func(t *testing.T) {
		client, auditLog := setup(t, "eks:DescribeCluster")
		_, err := client.DescribeCluster(context.Background(), &eks.DescribeClusterInput{Name: aws.String("orders")})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("DescribeCluster(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "eks:DescribeCluster", false)
	})

	t.Run("UpdateClusterConfig", func(t *testing.T) {
		client, auditLog := setup(t, "eks:UpdateClusterConfig", "eks:DescribeCluster")
		_, err := client.UpdateClusterConfig(context.Background(), &eks.UpdateClusterConfigInput{
			Name:               aws.String("orders"),
			DeletionProtection: aws.Bool(true),
		})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("UpdateClusterConfig(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "eks:UpdateClusterConfig", false)

		described, err := client.DescribeCluster(context.Background(), &eks.DescribeClusterInput{Name: aws.String("orders")})
		if err != nil {
			t.Fatalf("DescribeCluster(after denied update) error = %v", err)
		}
		if described.Cluster == nil || aws.ToBool(described.Cluster.DeletionProtection) {
			t.Fatalf("DescribeCluster(after denied update) = %#v, want deletion protection unchanged", described.Cluster)
		}
	})

	t.Run("DeleteCluster", func(t *testing.T) {
		client, auditLog := setup(t, "eks:DeleteCluster", "eks:DescribeCluster")
		_, err := client.DeleteCluster(context.Background(), &eks.DeleteClusterInput{Name: aws.String("orders")})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("DeleteCluster(denied) error = %v, want AccessDenied", err)
		}
		assertDecision(t, auditLog.Decisions(), "eks:DeleteCluster", false)

		described, err := client.DescribeCluster(context.Background(), &eks.DescribeClusterInput{Name: aws.String("orders")})
		if err != nil {
			t.Fatalf("DescribeCluster(after denied delete) error = %v", err)
		}
		if described.Cluster == nil || aws.ToString(described.Cluster.Name) != "orders" {
			t.Fatalf("DescribeCluster(after denied delete) = %#v, want orders preserved", described.Cluster)
		}
	})
}

func TestEKSCompatibilityAdapterAuthorizesAndAuditsNodegroupOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, seedNodegroup bool) (*eks.Client, *authz.AuditLog) {
		t.Helper()
		allowed := []string{"eks:CreateCluster", "eks:CreateNodegroup", "eks:ListNodegroups", "eks:DescribeNodegroup"}
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewEKSAdapter(
			compataws.WithEKSAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithEKSAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := eks.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *eks.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
			Name:    aws.String("orders"),
			RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
			ResourcesVpcConfig: &types.VpcConfigRequest{
				SubnetIds: []string{"subnet-a", "subnet-b"},
			},
		}); err != nil {
			t.Fatalf("CreateCluster() error = %v", err)
		}
		if seedNodegroup {
			if _, err := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
				ClusterName:   aws.String("orders"),
				NodegroupName: aws.String("workers"),
				NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
				Subnets:       []string{"subnet-a", "subnet-b"},
				ScalingConfig: &types.NodegroupScalingConfig{DesiredSize: aws.Int32(2), MaxSize: aws.Int32(3), MinSize: aws.Int32(1)},
			}); err != nil {
				t.Fatalf("CreateNodegroup() error = %v", err)
			}
		}
		return client, auditLog
	}
	expectAccessDenied := func(t *testing.T, err error, action string) {
		t.Helper()
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("%s(denied) error = %v, want AccessDenied", action, err)
		}
	}

	t.Run("CreateNodegroup", func(t *testing.T) {
		client, auditLog := setup(t, "eks:CreateNodegroup", false)
		_, err := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
			ClusterName:   aws.String("orders"),
			NodegroupName: aws.String("workers"),
			NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
			Subnets:       []string{"subnet-a", "subnet-b"},
		})
		expectAccessDenied(t, err, "CreateNodegroup")
		assertDecision(t, auditLog.Decisions(), "eks:CreateNodegroup", false)
		listed, err := client.ListNodegroups(context.Background(), &eks.ListNodegroupsInput{ClusterName: aws.String("orders")})
		if err != nil {
			t.Fatalf("ListNodegroups(after denied create) error = %v", err)
		}
		if len(listed.Nodegroups) != 0 {
			t.Fatalf("ListNodegroups(after denied create) = %#v, want no nodegroups", listed.Nodegroups)
		}
	})

	t.Run("ListNodegroups", func(t *testing.T) {
		client, auditLog := setup(t, "eks:ListNodegroups", true)
		_, err := client.ListNodegroups(context.Background(), &eks.ListNodegroupsInput{ClusterName: aws.String("orders")})
		expectAccessDenied(t, err, "ListNodegroups")
		assertDecision(t, auditLog.Decisions(), "eks:ListNodegroups", false)
	})

	t.Run("DescribeNodegroup", func(t *testing.T) {
		client, auditLog := setup(t, "eks:DescribeNodegroup", true)
		_, err := client.DescribeNodegroup(context.Background(), &eks.DescribeNodegroupInput{
			ClusterName:   aws.String("orders"),
			NodegroupName: aws.String("workers"),
		})
		expectAccessDenied(t, err, "DescribeNodegroup")
		assertDecision(t, auditLog.Decisions(), "eks:DescribeNodegroup", false)
	})

	t.Run("UpdateNodegroupConfig", func(t *testing.T) {
		client, auditLog := setup(t, "eks:UpdateNodegroupConfig", true)
		_, err := client.UpdateNodegroupConfig(context.Background(), &eks.UpdateNodegroupConfigInput{
			ClusterName:   aws.String("orders"),
			NodegroupName: aws.String("workers"),
			ScalingConfig: &types.NodegroupScalingConfig{DesiredSize: aws.Int32(5), MaxSize: aws.Int32(6), MinSize: aws.Int32(4)},
		})
		expectAccessDenied(t, err, "UpdateNodegroupConfig")
		assertDecision(t, auditLog.Decisions(), "eks:UpdateNodegroupConfig", false)
		described, err := client.DescribeNodegroup(context.Background(), &eks.DescribeNodegroupInput{ClusterName: aws.String("orders"), NodegroupName: aws.String("workers")})
		if err != nil {
			t.Fatalf("DescribeNodegroup(after denied config update) error = %v", err)
		}
		if aws.ToInt32(described.Nodegroup.ScalingConfig.DesiredSize) != 2 {
			t.Fatalf("DescribeNodegroup(after denied config update) scaling = %#v, want unchanged desired size 2", described.Nodegroup.ScalingConfig)
		}
	})

	t.Run("UpdateNodegroupVersion", func(t *testing.T) {
		client, auditLog := setup(t, "eks:UpdateNodegroupVersion", true)
		_, err := client.UpdateNodegroupVersion(context.Background(), &eks.UpdateNodegroupVersionInput{
			ClusterName:    aws.String("orders"),
			NodegroupName:  aws.String("workers"),
			Version:        aws.String("1.31"),
			ReleaseVersion: aws.String("1.31.1-20260709"),
		})
		expectAccessDenied(t, err, "UpdateNodegroupVersion")
		assertDecision(t, auditLog.Decisions(), "eks:UpdateNodegroupVersion", false)
		described, err := client.DescribeNodegroup(context.Background(), &eks.DescribeNodegroupInput{ClusterName: aws.String("orders"), NodegroupName: aws.String("workers")})
		if err != nil {
			t.Fatalf("DescribeNodegroup(after denied version update) error = %v", err)
		}
		if aws.ToString(described.Nodegroup.Version) != "1.30" {
			t.Fatalf("DescribeNodegroup(after denied version update) version = %q, want unchanged 1.30", aws.ToString(described.Nodegroup.Version))
		}
	})

	t.Run("DeleteNodegroup", func(t *testing.T) {
		client, auditLog := setup(t, "eks:DeleteNodegroup", true)
		_, err := client.DeleteNodegroup(context.Background(), &eks.DeleteNodegroupInput{
			ClusterName:   aws.String("orders"),
			NodegroupName: aws.String("workers"),
		})
		expectAccessDenied(t, err, "DeleteNodegroup")
		assertDecision(t, auditLog.Decisions(), "eks:DeleteNodegroup", false)
		if _, err := client.DescribeNodegroup(context.Background(), &eks.DescribeNodegroupInput{ClusterName: aws.String("orders"), NodegroupName: aws.String("workers")}); err != nil {
			t.Fatalf("DescribeNodegroup(after denied delete) error = %v", err)
		}
	})
}

func TestEKSCompatibilityAdapterAuthorizesAndAuditsAddonOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, seedAddon bool) (*eks.Client, *authz.AuditLog) {
		t.Helper()
		allowed := []string{"eks:CreateCluster", "eks:CreateAddon", "eks:ListAddons", "eks:DescribeAddon"}
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewEKSAdapter(
			compataws.WithEKSAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithEKSAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := eks.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *eks.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
			Name:    aws.String("orders"),
			RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
			ResourcesVpcConfig: &types.VpcConfigRequest{
				SubnetIds: []string{"subnet-a", "subnet-b"},
			},
		}); err != nil {
			t.Fatalf("CreateCluster() error = %v", err)
		}
		if seedAddon {
			if _, err := client.CreateAddon(context.Background(), &eks.CreateAddonInput{
				ClusterName:  aws.String("orders"),
				AddonName:    aws.String("vpc-cni"),
				AddonVersion: aws.String("v1.18.0-eksbuild.1"),
			}); err != nil {
				t.Fatalf("CreateAddon() error = %v", err)
			}
		}
		return client, auditLog
	}
	expectAccessDenied := func(t *testing.T, err error, action string) {
		t.Helper()
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("%s(denied) error = %v, want AccessDenied", action, err)
		}
	}

	t.Run("CreateAddon", func(t *testing.T) {
		client, auditLog := setup(t, "eks:CreateAddon", false)
		_, err := client.CreateAddon(context.Background(), &eks.CreateAddonInput{
			ClusterName:  aws.String("orders"),
			AddonName:    aws.String("vpc-cni"),
			AddonVersion: aws.String("v1.18.0-eksbuild.1"),
		})
		expectAccessDenied(t, err, "CreateAddon")
		assertDecision(t, auditLog.Decisions(), "eks:CreateAddon", false)
		listed, err := client.ListAddons(context.Background(), &eks.ListAddonsInput{ClusterName: aws.String("orders")})
		if err != nil {
			t.Fatalf("ListAddons(after denied create) error = %v", err)
		}
		if len(listed.Addons) != 0 {
			t.Fatalf("ListAddons(after denied create) = %#v, want no add-ons", listed.Addons)
		}
	})

	t.Run("ListAddons", func(t *testing.T) {
		client, auditLog := setup(t, "eks:ListAddons", true)
		_, err := client.ListAddons(context.Background(), &eks.ListAddonsInput{ClusterName: aws.String("orders")})
		expectAccessDenied(t, err, "ListAddons")
		assertDecision(t, auditLog.Decisions(), "eks:ListAddons", false)
	})

	t.Run("DescribeAddon", func(t *testing.T) {
		client, auditLog := setup(t, "eks:DescribeAddon", true)
		_, err := client.DescribeAddon(context.Background(), &eks.DescribeAddonInput{
			ClusterName: aws.String("orders"),
			AddonName:   aws.String("vpc-cni"),
		})
		expectAccessDenied(t, err, "DescribeAddon")
		assertDecision(t, auditLog.Decisions(), "eks:DescribeAddon", false)
	})

	t.Run("UpdateAddon", func(t *testing.T) {
		client, auditLog := setup(t, "eks:UpdateAddon", true)
		_, err := client.UpdateAddon(context.Background(), &eks.UpdateAddonInput{
			ClusterName:  aws.String("orders"),
			AddonName:    aws.String("vpc-cni"),
			AddonVersion: aws.String("v1.19.0-eksbuild.1"),
		})
		expectAccessDenied(t, err, "UpdateAddon")
		assertDecision(t, auditLog.Decisions(), "eks:UpdateAddon", false)
		described, err := client.DescribeAddon(context.Background(), &eks.DescribeAddonInput{ClusterName: aws.String("orders"), AddonName: aws.String("vpc-cni")})
		if err != nil {
			t.Fatalf("DescribeAddon(after denied update) error = %v", err)
		}
		if aws.ToString(described.Addon.AddonVersion) != "v1.18.0-eksbuild.1" {
			t.Fatalf("DescribeAddon(after denied update) version = %q, want unchanged v1.18.0-eksbuild.1", aws.ToString(described.Addon.AddonVersion))
		}
	})

	t.Run("DeleteAddon", func(t *testing.T) {
		client, auditLog := setup(t, "eks:DeleteAddon", true)
		_, err := client.DeleteAddon(context.Background(), &eks.DeleteAddonInput{
			ClusterName: aws.String("orders"),
			AddonName:   aws.String("vpc-cni"),
		})
		expectAccessDenied(t, err, "DeleteAddon")
		assertDecision(t, auditLog.Decisions(), "eks:DeleteAddon", false)
		if _, err := client.DescribeAddon(context.Background(), &eks.DescribeAddonInput{ClusterName: aws.String("orders"), AddonName: aws.String("vpc-cni")}); err != nil {
			t.Fatalf("DescribeAddon(after denied delete) error = %v", err)
		}
	})
}

func TestEKSCompatibilityAdapterAuthorizesAndAuditsAccessEntryOperationsWithAWSSDK(t *testing.T) {
	const principal = "arn:aws:iam::000000000000:root"
	const policyARN = "arn:aws:eks::aws:cluster-access-policy/AmazonEKSViewPolicy"

	setup := func(t *testing.T, deniedAction string, seedEntry bool, seedPolicy bool) (*eks.Client, *authz.AuditLog) {
		t.Helper()
		allowed := []string{
			"eks:CreateCluster",
			"eks:CreateAccessEntry",
			"eks:ListAccessEntries",
			"eks:DescribeAccessEntry",
			"eks:AssociateAccessPolicy",
			"eks:ListAssociatedAccessPolicies",
		}
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewEKSAdapter(
			compataws.WithEKSAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithEKSAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := eks.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *eks.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
			Name:    aws.String("orders"),
			RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
			ResourcesVpcConfig: &types.VpcConfigRequest{
				SubnetIds: []string{"subnet-a", "subnet-b"},
			},
		}); err != nil {
			t.Fatalf("CreateCluster() error = %v", err)
		}
		if seedEntry {
			if _, err := client.CreateAccessEntry(context.Background(), &eks.CreateAccessEntryInput{
				ClusterName:      aws.String("orders"),
				PrincipalArn:     aws.String(principal),
				KubernetesGroups: []string{"viewers"},
				Username:         aws.String("homeport-root"),
			}); err != nil {
				t.Fatalf("CreateAccessEntry() error = %v", err)
			}
		}
		if seedPolicy {
			if _, err := client.AssociateAccessPolicy(context.Background(), &eks.AssociateAccessPolicyInput{
				ClusterName:  aws.String("orders"),
				PrincipalArn: aws.String(principal),
				PolicyArn:    aws.String(policyARN),
				AccessScope:  &types.AccessScope{Type: types.AccessScopeTypeCluster},
			}); err != nil {
				t.Fatalf("AssociateAccessPolicy() error = %v", err)
			}
		}
		return client, auditLog
	}
	expectAccessDenied := func(t *testing.T, err error, action string) {
		t.Helper()
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("%s(denied) error = %v, want AccessDenied", action, err)
		}
	}

	t.Run("CreateAccessEntry", func(t *testing.T) {
		client, auditLog := setup(t, "eks:CreateAccessEntry", false, false)
		_, err := client.CreateAccessEntry(context.Background(), &eks.CreateAccessEntryInput{
			ClusterName:  aws.String("orders"),
			PrincipalArn: aws.String(principal),
		})
		expectAccessDenied(t, err, "CreateAccessEntry")
		assertDecision(t, auditLog.Decisions(), "eks:CreateAccessEntry", false)
		listed, err := client.ListAccessEntries(context.Background(), &eks.ListAccessEntriesInput{ClusterName: aws.String("orders")})
		if err != nil {
			t.Fatalf("ListAccessEntries(after denied create) error = %v", err)
		}
		if len(listed.AccessEntries) != 0 {
			t.Fatalf("ListAccessEntries(after denied create) = %#v, want no access entries", listed.AccessEntries)
		}
	})

	t.Run("ListAccessEntries", func(t *testing.T) {
		client, auditLog := setup(t, "eks:ListAccessEntries", true, false)
		_, err := client.ListAccessEntries(context.Background(), &eks.ListAccessEntriesInput{ClusterName: aws.String("orders")})
		expectAccessDenied(t, err, "ListAccessEntries")
		assertDecision(t, auditLog.Decisions(), "eks:ListAccessEntries", false)
	})

	t.Run("DescribeAccessEntry", func(t *testing.T) {
		client, auditLog := setup(t, "eks:DescribeAccessEntry", true, false)
		_, err := client.DescribeAccessEntry(context.Background(), &eks.DescribeAccessEntryInput{
			ClusterName:  aws.String("orders"),
			PrincipalArn: aws.String(principal),
		})
		expectAccessDenied(t, err, "DescribeAccessEntry")
		assertDecision(t, auditLog.Decisions(), "eks:DescribeAccessEntry", false)
	})

	t.Run("UpdateAccessEntry", func(t *testing.T) {
		client, auditLog := setup(t, "eks:UpdateAccessEntry", true, false)
		_, err := client.UpdateAccessEntry(context.Background(), &eks.UpdateAccessEntryInput{
			ClusterName:      aws.String("orders"),
			PrincipalArn:     aws.String(principal),
			Username:         aws.String("denied"),
			KubernetesGroups: []string{"admins"},
		})
		expectAccessDenied(t, err, "UpdateAccessEntry")
		assertDecision(t, auditLog.Decisions(), "eks:UpdateAccessEntry", false)
		described, err := client.DescribeAccessEntry(context.Background(), &eks.DescribeAccessEntryInput{ClusterName: aws.String("orders"), PrincipalArn: aws.String(principal)})
		if err != nil {
			t.Fatalf("DescribeAccessEntry(after denied update) error = %v", err)
		}
		if aws.ToString(described.AccessEntry.Username) != "homeport-root" || !slices.Equal(described.AccessEntry.KubernetesGroups, []string{"viewers"}) {
			t.Fatalf("DescribeAccessEntry(after denied update) = %#v, want unchanged username/groups", described.AccessEntry)
		}
	})

	t.Run("DeleteAccessEntry", func(t *testing.T) {
		client, auditLog := setup(t, "eks:DeleteAccessEntry", true, false)
		_, err := client.DeleteAccessEntry(context.Background(), &eks.DeleteAccessEntryInput{
			ClusterName:  aws.String("orders"),
			PrincipalArn: aws.String(principal),
		})
		expectAccessDenied(t, err, "DeleteAccessEntry")
		assertDecision(t, auditLog.Decisions(), "eks:DeleteAccessEntry", false)
		if _, err := client.DescribeAccessEntry(context.Background(), &eks.DescribeAccessEntryInput{ClusterName: aws.String("orders"), PrincipalArn: aws.String(principal)}); err != nil {
			t.Fatalf("DescribeAccessEntry(after denied delete) error = %v", err)
		}
	})

	t.Run("AssociateAccessPolicy", func(t *testing.T) {
		client, auditLog := setup(t, "eks:AssociateAccessPolicy", true, false)
		_, err := client.AssociateAccessPolicy(context.Background(), &eks.AssociateAccessPolicyInput{
			ClusterName:  aws.String("orders"),
			PrincipalArn: aws.String(principal),
			PolicyArn:    aws.String(policyARN),
			AccessScope:  &types.AccessScope{Type: types.AccessScopeTypeCluster},
		})
		expectAccessDenied(t, err, "AssociateAccessPolicy")
		assertDecision(t, auditLog.Decisions(), "eks:AssociateAccessPolicy", false)
		listed, err := client.ListAssociatedAccessPolicies(context.Background(), &eks.ListAssociatedAccessPoliciesInput{ClusterName: aws.String("orders"), PrincipalArn: aws.String(principal)})
		if err != nil {
			t.Fatalf("ListAssociatedAccessPolicies(after denied associate) error = %v", err)
		}
		if len(listed.AssociatedAccessPolicies) != 0 {
			t.Fatalf("ListAssociatedAccessPolicies(after denied associate) = %#v, want no policies", listed.AssociatedAccessPolicies)
		}
	})

	t.Run("ListAssociatedAccessPolicies", func(t *testing.T) {
		client, auditLog := setup(t, "eks:ListAssociatedAccessPolicies", true, true)
		_, err := client.ListAssociatedAccessPolicies(context.Background(), &eks.ListAssociatedAccessPoliciesInput{
			ClusterName:  aws.String("orders"),
			PrincipalArn: aws.String(principal),
		})
		expectAccessDenied(t, err, "ListAssociatedAccessPolicies")
		assertDecision(t, auditLog.Decisions(), "eks:ListAssociatedAccessPolicies", false)
	})

	t.Run("DisassociateAccessPolicy", func(t *testing.T) {
		client, auditLog := setup(t, "eks:DisassociateAccessPolicy", true, true)
		_, err := client.DisassociateAccessPolicy(context.Background(), &eks.DisassociateAccessPolicyInput{
			ClusterName:  aws.String("orders"),
			PrincipalArn: aws.String(principal),
			PolicyArn:    aws.String(policyARN),
		})
		expectAccessDenied(t, err, "DisassociateAccessPolicy")
		assertDecision(t, auditLog.Decisions(), "eks:DisassociateAccessPolicy", false)
		listed, err := client.ListAssociatedAccessPolicies(context.Background(), &eks.ListAssociatedAccessPoliciesInput{ClusterName: aws.String("orders"), PrincipalArn: aws.String(principal)})
		if err != nil {
			t.Fatalf("ListAssociatedAccessPolicies(after denied disassociate) error = %v", err)
		}
		if len(listed.AssociatedAccessPolicies) != 1 || aws.ToString(listed.AssociatedAccessPolicies[0].PolicyArn) != policyARN {
			t.Fatalf("ListAssociatedAccessPolicies(after denied disassociate) = %#v, want policy preserved", listed.AssociatedAccessPolicies)
		}
	})
}

func TestEKSCompatibilityAdapterPaginatesListClustersWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, name := range []string{"billing", "orders", "warehouse"} {
		if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
			Name:    aws.String(name),
			RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
			ResourcesVpcConfig: &types.VpcConfigRequest{
				SubnetIds: []string{"subnet-a", "subnet-b"},
			},
		}); err != nil {
			t.Fatalf("CreateCluster(%s) error = %v", name, err)
		}
	}

	first, err := client.ListClusters(context.Background(), &eks.ListClustersInput{MaxResults: aws.Int32(2)})
	if err != nil {
		t.Fatalf("ListClusters(first page) error = %v", err)
	}
	if got, want := first.Clusters, []string{"billing", "orders"}; !slices.Equal(got, want) {
		t.Fatalf("ListClusters(first page) = %#v, want %#v", got, want)
	}
	if aws.ToString(first.NextToken) == "" {
		t.Fatal("ListClusters(first page) NextToken empty, want token")
	}

	second, err := client.ListClusters(context.Background(), &eks.ListClustersInput{
		MaxResults: aws.Int32(2),
		NextToken:  first.NextToken,
	})
	if err != nil {
		t.Fatalf("ListClusters(second page) error = %v", err)
	}
	if got, want := second.Clusters, []string{"warehouse"}; !slices.Equal(got, want) {
		t.Fatalf("ListClusters(second page) = %#v, want %#v", got, want)
	}
	if second.NextToken != nil {
		t.Fatalf("ListClusters(second page) NextToken = %q, want nil", aws.ToString(second.NextToken))
	}
}

func TestEKSCompatibilityAdapterManagesClusterTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
		Tags: map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	clusterARN := aws.ToString(created.Cluster.Arn)

	listed, err := client.ListTagsForResource(context.Background(), &eks.ListTagsForResourceInput{
		ResourceArn: aws.String(clusterARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource() error = %v", err)
	}
	if listed.Tags["env"] != "test" {
		t.Fatalf("ListTagsForResource() = %#v, want env=test", listed.Tags)
	}

	if _, err := client.TagResource(context.Background(), &eks.TagResourceInput{
		ResourceArn: aws.String(clusterARN),
		Tags:        map[string]string{"owner": "platform"},
	}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	listed, err = client.ListTagsForResource(context.Background(), &eks.ListTagsForResourceInput{
		ResourceArn: aws.String(clusterARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource(after tag) error = %v", err)
	}
	if listed.Tags["env"] != "test" || listed.Tags["owner"] != "platform" {
		t.Fatalf("ListTagsForResource(after tag) = %#v, want merged tags", listed.Tags)
	}

	if _, err := client.UntagResource(context.Background(), &eks.UntagResourceInput{
		ResourceArn: aws.String(clusterARN),
		TagKeys:     []string{"env"},
	}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	listed, err = client.ListTagsForResource(context.Background(), &eks.ListTagsForResourceInput{
		ResourceArn: aws.String(clusterARN),
	})
	if err != nil {
		t.Fatalf("ListTagsForResource(after untag) error = %v", err)
	}
	if _, ok := listed.Tags["env"]; ok || listed.Tags["owner"] != "platform" {
		t.Fatalf("ListTagsForResource(after untag) = %#v, want owner tag only", listed.Tags)
	}
}

func TestEKSCompatibilityAdapterManagesNonClusterResourceTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	nodegroup, err := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
		NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
		Subnets:       []string{"subnet-a", "subnet-b"},
		Tags:          map[string]string{"env": "node"},
	})
	if err != nil {
		t.Fatalf("CreateNodegroup() error = %v", err)
	}
	addon, err := client.CreateAddon(context.Background(), &eks.CreateAddonInput{
		ClusterName:  aws.String("orders"),
		AddonName:    aws.String("vpc-cni"),
		AddonVersion: aws.String("v1.18.0-eksbuild.1"),
		Tags:         map[string]string{"env": "addon"},
	})
	if err != nil {
		t.Fatalf("CreateAddon() error = %v", err)
	}
	entry, err := client.CreateAccessEntry(context.Background(), &eks.CreateAccessEntryInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String("arn:aws:iam::000000000000:root"),
		Tags:         map[string]string{"env": "access"},
	})
	if err != nil {
		t.Fatalf("CreateAccessEntry() error = %v", err)
	}

	cases := []struct {
		name        string
		resourceARN string
		env         string
	}{
		{name: "nodegroup", resourceARN: aws.ToString(nodegroup.Nodegroup.NodegroupArn), env: "node"},
		{name: "addon", resourceARN: aws.ToString(addon.Addon.AddonArn), env: "addon"},
		{name: "access entry", resourceARN: aws.ToString(entry.AccessEntry.AccessEntryArn), env: "access"},
	}
	for _, tc := range cases {
		listed, err := client.ListTagsForResource(context.Background(), &eks.ListTagsForResourceInput{
			ResourceArn: aws.String(tc.resourceARN),
		})
		if err != nil {
			t.Fatalf("ListTagsForResource(%s) error = %v", tc.name, err)
		}
		if listed.Tags["env"] != tc.env {
			t.Fatalf("ListTagsForResource(%s) = %#v, want env=%s", tc.name, listed.Tags, tc.env)
		}

		if _, err := client.TagResource(context.Background(), &eks.TagResourceInput{
			ResourceArn: aws.String(tc.resourceARN),
			Tags:        map[string]string{"owner": "platform"},
		}); err != nil {
			t.Fatalf("TagResource(%s) error = %v", tc.name, err)
		}
		listed, err = client.ListTagsForResource(context.Background(), &eks.ListTagsForResourceInput{
			ResourceArn: aws.String(tc.resourceARN),
		})
		if err != nil {
			t.Fatalf("ListTagsForResource(%s after tag) error = %v", tc.name, err)
		}
		if listed.Tags["env"] != tc.env || listed.Tags["owner"] != "platform" {
			t.Fatalf("ListTagsForResource(%s after tag) = %#v, want merged tags", tc.name, listed.Tags)
		}

		if _, err := client.UntagResource(context.Background(), &eks.UntagResourceInput{
			ResourceArn: aws.String(tc.resourceARN),
			TagKeys:     []string{"env"},
		}); err != nil {
			t.Fatalf("UntagResource(%s) error = %v", tc.name, err)
		}
		listed, err = client.ListTagsForResource(context.Background(), &eks.ListTagsForResourceInput{
			ResourceArn: aws.String(tc.resourceARN),
		})
		if err != nil {
			t.Fatalf("ListTagsForResource(%s after untag) error = %v", tc.name, err)
		}
		if _, ok := listed.Tags["env"]; ok || listed.Tags["owner"] != "platform" {
			t.Fatalf("ListTagsForResource(%s after untag) = %#v, want owner tag only", tc.name, listed.Tags)
		}
	}
}

func TestEKSCompatibilityAdapterAuthorizesAndAuditsTagOperationsWithAWSSDK(t *testing.T) {
	setup := func(t *testing.T, deniedAction string, extraAllowed ...string) (*eks.Client, *authz.AuditLog, string) {
		t.Helper()
		allowed := append([]string{"eks:CreateCluster", "eks:CreateNodegroup"}, extraAllowed...)
		auditLog := authz.NewAuditLog()
		server := httptest.NewServer(compataws.NewEKSAdapter(
			compataws.WithEKSAuthorizer(authz.NewPolicyAuthorizer(
				authz.Rule{Effect: authz.Allow, Actions: allowed, Resources: []string{"*"}},
				authz.Rule{Effect: authz.Deny, Actions: []string{deniedAction}, Resources: []string{"*"}},
			)),
			compataws.WithEKSAuditSink(auditLog.Record),
		))
		t.Cleanup(server.Close)

		client := eks.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
		}, func(o *eks.Options) {
			o.BaseEndpoint = aws.String(server.URL)
		})

		if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
			Name:    aws.String("orders"),
			RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
			ResourcesVpcConfig: &types.VpcConfigRequest{
				SubnetIds: []string{"subnet-a", "subnet-b"},
			},
		}); err != nil {
			t.Fatalf("CreateCluster() error = %v", err)
		}
		nodegroup, err := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
			ClusterName:   aws.String("orders"),
			NodegroupName: aws.String("workers"),
			NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
			Subnets:       []string{"subnet-a", "subnet-b"},
			Tags:          map[string]string{"env": "test"},
		})
		if err != nil {
			t.Fatalf("CreateNodegroup() error = %v", err)
		}
		return client, auditLog, aws.ToString(nodegroup.Nodegroup.NodegroupArn)
	}
	expectAccessDenied := func(t *testing.T, err error, action string) {
		t.Helper()
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("%s(denied) error = %v, want AccessDenied", action, err)
		}
	}

	t.Run("ListTagsForResource", func(t *testing.T) {
		client, auditLog, resourceARN := setup(t, "eks:ListTagsForResource")
		_, err := client.ListTagsForResource(context.Background(), &eks.ListTagsForResourceInput{ResourceArn: aws.String(resourceARN)})
		expectAccessDenied(t, err, "ListTagsForResource")
		assertDecision(t, auditLog.Decisions(), "eks:ListTagsForResource", false)
	})

	t.Run("TagResource", func(t *testing.T) {
		client, auditLog, resourceARN := setup(t, "eks:TagResource", "eks:ListTagsForResource")
		_, err := client.TagResource(context.Background(), &eks.TagResourceInput{
			ResourceArn: aws.String(resourceARN),
			Tags:        map[string]string{"owner": "platform"},
		})
		expectAccessDenied(t, err, "TagResource")
		assertDecision(t, auditLog.Decisions(), "eks:TagResource", false)
		listed, err := client.ListTagsForResource(context.Background(), &eks.ListTagsForResourceInput{ResourceArn: aws.String(resourceARN)})
		if err != nil {
			t.Fatalf("ListTagsForResource(after denied tag) error = %v", err)
		}
		if listed.Tags["env"] != "test" || listed.Tags["owner"] != "" {
			t.Fatalf("ListTagsForResource(after denied tag) = %#v, want original tags", listed.Tags)
		}
	})

	t.Run("UntagResource", func(t *testing.T) {
		client, auditLog, resourceARN := setup(t, "eks:UntagResource", "eks:ListTagsForResource")
		_, err := client.UntagResource(context.Background(), &eks.UntagResourceInput{
			ResourceArn: aws.String(resourceARN),
			TagKeys:     []string{"env"},
		})
		expectAccessDenied(t, err, "UntagResource")
		assertDecision(t, auditLog.Decisions(), "eks:UntagResource", false)
		listed, err := client.ListTagsForResource(context.Background(), &eks.ListTagsForResourceInput{ResourceArn: aws.String(resourceARN)})
		if err != nil {
			t.Fatalf("ListTagsForResource(after denied untag) error = %v", err)
		}
		if listed.Tags["env"] != "test" {
			t.Fatalf("ListTagsForResource(after denied untag) = %#v, want env preserved", listed.Tags)
		}
	})
}

func TestEKSCompatibilityAdapterReplaysCreateClusterClientRequestTokenWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	input := &eks.CreateClusterInput{
		Name:               aws.String("idempotent-orders"),
		RoleArn:            aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ClientRequestToken: aws.String("create-cluster-token"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}
	created, err := client.CreateCluster(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateCluster(first) error = %v", err)
	}
	replayed, err := client.CreateCluster(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateCluster(replay) error = %v", err)
	}
	if aws.ToString(replayed.Cluster.Arn) != aws.ToString(created.Cluster.Arn) {
		t.Fatalf("CreateCluster(replay) = %#v, want original cluster", replayed.Cluster)
	}
}

func TestEKSCompatibilityAdapterReplaysCreateNodegroupClientRequestTokenWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	input := &eks.CreateNodegroupInput{
		ClusterName:        aws.String("orders"),
		NodegroupName:      aws.String("idempotent-workers"),
		NodeRole:           aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
		Subnets:            []string{"subnet-a", "subnet-b"},
		ClientRequestToken: aws.String("create-nodegroup-token"),
	}
	created, err := client.CreateNodegroup(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateNodegroup(first) error = %v", err)
	}
	replayed, err := client.CreateNodegroup(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateNodegroup(replay) error = %v", err)
	}
	if aws.ToString(replayed.Nodegroup.NodegroupArn) != aws.ToString(created.Nodegroup.NodegroupArn) {
		t.Fatalf("CreateNodegroup(replay) = %#v, want original nodegroup", replayed.Nodegroup)
	}
}

func TestEKSCompatibilityAdapterRejectsMismatchedClientRequestTokensWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	expectInvalidToken := func(t *testing.T, err error, action string) {
		t.Helper()
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
			t.Fatalf("%s(mismatched token) error = %v, want InvalidParameterException", action, err)
		}
	}

	clusterInput := &eks.CreateClusterInput{
		Name:               aws.String("idempotent-a"),
		RoleArn:            aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ClientRequestToken: aws.String("mismatch-create-cluster"),
		ResourcesVpcConfig: &types.VpcConfigRequest{SubnetIds: []string{"subnet-a", "subnet-b"}},
	}
	if _, err := client.CreateCluster(context.Background(), clusterInput); err != nil {
		t.Fatalf("CreateCluster(seed token) error = %v", err)
	}
	clusterInput.Name = aws.String("idempotent-b")
	_, err := client.CreateCluster(context.Background(), clusterInput)
	expectInvalidToken(t, err, "CreateCluster")

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:               aws.String("orders"),
		RoleArn:            aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{SubnetIds: []string{"subnet-a", "subnet-b"}},
	}); err != nil {
		t.Fatalf("CreateCluster(orders) error = %v", err)
	}

	nodeInput := &eks.CreateNodegroupInput{
		ClusterName:        aws.String("orders"),
		NodegroupName:      aws.String("workers"),
		NodeRole:           aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
		Subnets:            []string{"subnet-a", "subnet-b"},
		ClientRequestToken: aws.String("mismatch-create-nodegroup"),
	}
	if _, err := client.CreateNodegroup(context.Background(), nodeInput); err != nil {
		t.Fatalf("CreateNodegroup(seed token) error = %v", err)
	}
	nodeInput.NodegroupName = aws.String("workers-2")
	_, err = client.CreateNodegroup(context.Background(), nodeInput)
	expectInvalidToken(t, err, "CreateNodegroup")

	addonInput := &eks.CreateAddonInput{
		ClusterName:        aws.String("orders"),
		AddonName:          aws.String("vpc-cni"),
		AddonVersion:       aws.String("v1.18.0-eksbuild.1"),
		ClientRequestToken: aws.String("mismatch-create-addon"),
	}
	if _, err := client.CreateAddon(context.Background(), addonInput); err != nil {
		t.Fatalf("CreateAddon(seed token) error = %v", err)
	}
	addonInput.AddonName = aws.String("coredns")
	_, err = client.CreateAddon(context.Background(), addonInput)
	expectInvalidToken(t, err, "CreateAddon")

	principal := "arn:aws:iam::000000000000:root"
	accessInput := &eks.CreateAccessEntryInput{
		ClusterName:        aws.String("orders"),
		PrincipalArn:       aws.String(principal),
		ClientRequestToken: aws.String("mismatch-create-access-entry"),
	}
	if _, err := client.CreateAccessEntry(context.Background(), accessInput); err != nil {
		t.Fatalf("CreateAccessEntry(seed token) error = %v", err)
	}
	accessInput.PrincipalArn = aws.String("arn:aws:iam::000000000000:role/other")
	_, err = client.CreateAccessEntry(context.Background(), accessInput)
	expectInvalidToken(t, err, "CreateAccessEntry")

	updateAddonInput := &eks.UpdateAddonInput{
		ClusterName:        aws.String("orders"),
		AddonName:          aws.String("vpc-cni"),
		AddonVersion:       aws.String("v1.19.0-eksbuild.1"),
		ClientRequestToken: aws.String("mismatch-update-addon"),
	}
	if _, err := client.UpdateAddon(context.Background(), updateAddonInput); err != nil {
		t.Fatalf("UpdateAddon(seed token) error = %v", err)
	}
	updateAddonInput.AddonVersion = aws.String("v1.20.0-eksbuild.1")
	_, err = client.UpdateAddon(context.Background(), updateAddonInput)
	expectInvalidToken(t, err, "UpdateAddon")

	updateAccessInput := &eks.UpdateAccessEntryInput{
		ClusterName:        aws.String("orders"),
		PrincipalArn:       aws.String(principal),
		Username:           aws.String("first-user"),
		ClientRequestToken: aws.String("mismatch-update-access-entry"),
	}
	if _, err := client.UpdateAccessEntry(context.Background(), updateAccessInput); err != nil {
		t.Fatalf("UpdateAccessEntry(seed token) error = %v", err)
	}
	updateAccessInput.Username = aws.String("second-user")
	_, err = client.UpdateAccessEntry(context.Background(), updateAccessInput)
	expectInvalidToken(t, err, "UpdateAccessEntry")

	updateNodeConfigInput := &eks.UpdateNodegroupConfigInput{
		ClusterName:        aws.String("orders"),
		NodegroupName:      aws.String("workers"),
		ScalingConfig:      &types.NodegroupScalingConfig{DesiredSize: aws.Int32(2), MaxSize: aws.Int32(3), MinSize: aws.Int32(1)},
		ClientRequestToken: aws.String("mismatch-update-nodegroup-config"),
	}
	if _, err := client.UpdateNodegroupConfig(context.Background(), updateNodeConfigInput); err != nil {
		t.Fatalf("UpdateNodegroupConfig(seed token) error = %v", err)
	}
	updateNodeConfigInput.ScalingConfig.DesiredSize = aws.Int32(3)
	_, err = client.UpdateNodegroupConfig(context.Background(), updateNodeConfigInput)
	expectInvalidToken(t, err, "UpdateNodegroupConfig")

	updateNodeVersionInput := &eks.UpdateNodegroupVersionInput{
		ClusterName:        aws.String("orders"),
		NodegroupName:      aws.String("workers"),
		Version:            aws.String("1.31"),
		ReleaseVersion:     aws.String("1.31.1-20260709"),
		ClientRequestToken: aws.String("mismatch-update-nodegroup-version"),
	}
	if _, err := client.UpdateNodegroupVersion(context.Background(), updateNodeVersionInput); err != nil {
		t.Fatalf("UpdateNodegroupVersion(seed token) error = %v", err)
	}
	updateNodeVersionInput.Version = aws.String("1.32")
	_, err = client.UpdateNodegroupVersion(context.Background(), updateNodeVersionInput)
	expectInvalidToken(t, err, "UpdateNodegroupVersion")
}

func TestEKSCompatibilityAdapterRejectsInvalidCreateNodegroupRequiredFieldsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	cases := []struct {
		name  string
		input *eks.CreateNodegroupInput
	}{
		{
			name: "empty-node-role",
			input: &eks.CreateNodegroupInput{
				ClusterName:   aws.String("orders"),
				NodegroupName: aws.String("empty-node-role"),
				NodeRole:      aws.String(""),
				Subnets:       []string{"subnet-a"},
			},
		},
		{
			name: "empty-subnets",
			input: &eks.CreateNodegroupInput{
				ClusterName:   aws.String("orders"),
				NodegroupName: aws.String("empty-subnets"),
				NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
				Subnets:       []string{},
			},
		},
	}
	for _, tc := range cases {
		_, err := client.CreateNodegroup(context.Background(), tc.input)
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
			t.Fatalf("CreateNodegroup(%s) error = %v, want InvalidParameterException", tc.name, err)
		}
	}

	listed, err := client.ListNodegroups(context.Background(), &eks.ListNodegroupsInput{ClusterName: aws.String("orders")})
	if err != nil {
		t.Fatalf("ListNodegroups(after invalid creates) error = %v", err)
	}
	if len(listed.Nodegroups) != 0 {
		t.Fatalf("ListNodegroups(after invalid creates) = %#v, want no nodegroups", listed.Nodegroups)
	}
}

func TestEKSCompatibilityAdapterManagesNodegroupsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	created, err := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
		NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
		Subnets:       []string{"subnet-a", "subnet-b"},
		ScalingConfig: &types.NodegroupScalingConfig{
			DesiredSize: aws.Int32(2),
			MaxSize:     aws.Int32(3),
			MinSize:     aws.Int32(1),
		},
		Tags: map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("CreateNodegroup() error = %v", err)
	}
	if created.Nodegroup == nil || aws.ToString(created.Nodegroup.NodegroupName) != "workers" || created.Nodegroup.Status != types.NodegroupStatusActive {
		t.Fatalf("CreateNodegroup() = %#v, want active workers nodegroup", created.Nodegroup)
	}

	described, err := client.DescribeNodegroup(context.Background(), &eks.DescribeNodegroupInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
	})
	if err != nil {
		t.Fatalf("DescribeNodegroup() error = %v", err)
	}
	if described.Nodegroup == nil || aws.ToString(described.Nodegroup.NodegroupArn) != aws.ToString(created.Nodegroup.NodegroupArn) || described.Nodegroup.Tags["env"] != "test" {
		t.Fatalf("DescribeNodegroup() = %#v, want created nodegroup with tags", described.Nodegroup)
	}

	listed, err := client.ListNodegroups(context.Background(), &eks.ListNodegroupsInput{ClusterName: aws.String("orders")})
	if err != nil {
		t.Fatalf("ListNodegroups() error = %v", err)
	}
	if got, want := listed.Nodegroups, []string{"workers"}; !slices.Equal(got, want) {
		t.Fatalf("ListNodegroups() = %#v, want %#v", got, want)
	}

	deleted, err := client.DeleteNodegroup(context.Background(), &eks.DeleteNodegroupInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
	})
	if err != nil {
		t.Fatalf("DeleteNodegroup() error = %v", err)
	}
	if deleted.Nodegroup == nil || aws.ToString(deleted.Nodegroup.NodegroupName) != "workers" {
		t.Fatalf("DeleteNodegroup() = %#v, want deleted workers nodegroup", deleted.Nodegroup)
	}

	listed, err = client.ListNodegroups(context.Background(), &eks.ListNodegroupsInput{ClusterName: aws.String("orders")})
	if err != nil {
		t.Fatalf("ListNodegroups(after delete) error = %v", err)
	}
	if len(listed.Nodegroups) != 0 {
		t.Fatalf("ListNodegroups(after delete) = %#v, want no nodegroups", listed.Nodegroups)
	}
}

func TestEKSCompatibilityAdapterManagesAddonsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	created, err := client.CreateAddon(context.Background(), &eks.CreateAddonInput{
		ClusterName:  aws.String("orders"),
		AddonName:    aws.String("vpc-cni"),
		AddonVersion: aws.String("v1.18.0-eksbuild.1"),
		Tags:         map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("CreateAddon() error = %v", err)
	}
	if created.Addon == nil || aws.ToString(created.Addon.AddonName) != "vpc-cni" || created.Addon.Status != types.AddonStatusActive {
		t.Fatalf("CreateAddon() = %#v, want active vpc-cni addon", created.Addon)
	}

	described, err := client.DescribeAddon(context.Background(), &eks.DescribeAddonInput{
		ClusterName: aws.String("orders"),
		AddonName:   aws.String("vpc-cni"),
	})
	if err != nil {
		t.Fatalf("DescribeAddon() error = %v", err)
	}
	if described.Addon == nil ||
		aws.ToString(described.Addon.AddonArn) != aws.ToString(created.Addon.AddonArn) ||
		aws.ToString(described.Addon.AddonVersion) != "v1.18.0-eksbuild.1" ||
		described.Addon.Tags["env"] != "test" {
		t.Fatalf("DescribeAddon() = %#v, want created addon with version and tags", described.Addon)
	}

	listed, err := client.ListAddons(context.Background(), &eks.ListAddonsInput{ClusterName: aws.String("orders")})
	if err != nil {
		t.Fatalf("ListAddons() error = %v", err)
	}
	if got, want := listed.Addons, []string{"vpc-cni"}; !slices.Equal(got, want) {
		t.Fatalf("ListAddons() = %#v, want %#v", got, want)
	}

	deleted, err := client.DeleteAddon(context.Background(), &eks.DeleteAddonInput{
		ClusterName: aws.String("orders"),
		AddonName:   aws.String("vpc-cni"),
	})
	if err != nil {
		t.Fatalf("DeleteAddon() error = %v", err)
	}
	if deleted.Addon == nil || aws.ToString(deleted.Addon.AddonName) != "vpc-cni" {
		t.Fatalf("DeleteAddon() = %#v, want deleted vpc-cni addon", deleted.Addon)
	}

	listed, err = client.ListAddons(context.Background(), &eks.ListAddonsInput{ClusterName: aws.String("orders")})
	if err != nil {
		t.Fatalf("ListAddons(after delete) error = %v", err)
	}
	if len(listed.Addons) != 0 {
		t.Fatalf("ListAddons(after delete) = %#v, want no addons", listed.Addons)
	}
}

func TestEKSCompatibilityAdapterReplaysCreateAddonClientRequestTokenWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	input := &eks.CreateAddonInput{
		ClusterName:        aws.String("orders"),
		AddonName:          aws.String("idempotent-vpc-cni"),
		AddonVersion:       aws.String("v1.18.0-eksbuild.1"),
		ClientRequestToken: aws.String("create-addon-token"),
	}
	created, err := client.CreateAddon(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateAddon(first) error = %v", err)
	}
	replayed, err := client.CreateAddon(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateAddon(replay) error = %v", err)
	}
	if aws.ToString(replayed.Addon.AddonArn) != aws.ToString(created.Addon.AddonArn) {
		t.Fatalf("CreateAddon(replay) = %#v, want original addon", replayed.Addon)
	}
}

func TestEKSCompatibilityAdapterReplaysUpdateAddonClientRequestTokenWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	if _, err := client.CreateAddon(context.Background(), &eks.CreateAddonInput{
		ClusterName:  aws.String("orders"),
		AddonName:    aws.String("vpc-cni"),
		AddonVersion: aws.String("v1.18.0-eksbuild.1"),
	}); err != nil {
		t.Fatalf("CreateAddon() error = %v", err)
	}

	input := &eks.UpdateAddonInput{
		ClusterName:        aws.String("orders"),
		AddonName:          aws.String("vpc-cni"),
		AddonVersion:       aws.String("v1.19.0-eksbuild.1"),
		ClientRequestToken: aws.String("update-vpc-cni"),
	}
	updated, err := client.UpdateAddon(context.Background(), input)
	if err != nil {
		t.Fatalf("UpdateAddon(first) error = %v", err)
	}
	if updated.Update == nil || updated.Update.Status != types.UpdateStatusSuccessful {
		t.Fatalf("UpdateAddon(first) = %#v, want successful update", updated.Update)
	}
	replayed, err := client.UpdateAddon(context.Background(), input)
	if err != nil {
		t.Fatalf("UpdateAddon(replay) error = %v", err)
	}
	if aws.ToString(replayed.Update.Id) != aws.ToString(updated.Update.Id) {
		t.Fatalf("UpdateAddon(replay) = %#v, want original update id %q", replayed.Update, aws.ToString(updated.Update.Id))
	}

	described, err := client.DescribeAddon(context.Background(), &eks.DescribeAddonInput{
		ClusterName: aws.String("orders"),
		AddonName:   aws.String("vpc-cni"),
	})
	if err != nil {
		t.Fatalf("DescribeAddon(after update) error = %v", err)
	}
	if aws.ToString(described.Addon.AddonVersion) != "v1.19.0-eksbuild.1" {
		t.Fatalf("DescribeAddon(after update) version = %q, want updated addon version", aws.ToString(described.Addon.AddonVersion))
	}
}

func TestEKSCompatibilityAdapterPreservesAddonFieldsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	if _, err := client.CreateAddon(context.Background(), &eks.CreateAddonInput{
		ClusterName:           aws.String("orders"),
		AddonName:             aws.String("coredns"),
		AddonVersion:          aws.String("v1.11.1-eksbuild.4"),
		ConfigurationValues:   aws.String(`{"replicaCount":2}`),
		ServiceAccountRoleArn: aws.String("arn:aws:iam::000000000000:role/coredns-addon"),
	}); err != nil {
		t.Fatalf("CreateAddon() error = %v", err)
	}

	created, err := client.DescribeAddon(context.Background(), &eks.DescribeAddonInput{
		ClusterName: aws.String("orders"),
		AddonName:   aws.String("coredns"),
	})
	if err != nil {
		t.Fatalf("DescribeAddon(created fields) error = %v", err)
	}
	if aws.ToString(created.Addon.ConfigurationValues) != `{"replicaCount":2}` ||
		aws.ToString(created.Addon.ServiceAccountRoleArn) != "arn:aws:iam::000000000000:role/coredns-addon" {
		t.Fatalf("DescribeAddon(created fields) config = %q role = %q, want create-time values", aws.ToString(created.Addon.ConfigurationValues), aws.ToString(created.Addon.ServiceAccountRoleArn))
	}

	if _, err := client.UpdateAddon(context.Background(), &eks.UpdateAddonInput{
		ClusterName:           aws.String("orders"),
		AddonName:             aws.String("coredns"),
		ConfigurationValues:   aws.String(`{"replicaCount":3}`),
		ServiceAccountRoleArn: aws.String("arn:aws:iam::000000000000:role/coredns-addon-v2"),
	}); err != nil {
		t.Fatalf("UpdateAddon(fields) error = %v", err)
	}

	updated, err := client.DescribeAddon(context.Background(), &eks.DescribeAddonInput{
		ClusterName: aws.String("orders"),
		AddonName:   aws.String("coredns"),
	})
	if err != nil {
		t.Fatalf("DescribeAddon(updated fields) error = %v", err)
	}
	if aws.ToString(updated.Addon.ConfigurationValues) != `{"replicaCount":3}` ||
		aws.ToString(updated.Addon.ServiceAccountRoleArn) != "arn:aws:iam::000000000000:role/coredns-addon-v2" {
		t.Fatalf("DescribeAddon(updated fields) config = %q role = %q, want update-time values", aws.ToString(updated.Addon.ConfigurationValues), aws.ToString(updated.Addon.ServiceAccountRoleArn))
	}
}

func TestEKSCompatibilityAdapterPreservesAddonNamespaceAndPodIdentityWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	if _, err := client.CreateAddon(context.Background(), &eks.CreateAddonInput{
		ClusterName:  aws.String("orders"),
		AddonName:    aws.String("coredns"),
		AddonVersion: aws.String("v1.11.1-eksbuild.4"),
		NamespaceConfig: &types.AddonNamespaceConfigRequest{
			Namespace: aws.String("kube-system"),
		},
		PodIdentityAssociations: []types.AddonPodIdentityAssociations{{
			RoleArn:        aws.String("arn:aws:iam::000000000000:role/coredns-addon"),
			ServiceAccount: aws.String("coredns"),
		}},
	}); err != nil {
		t.Fatalf("CreateAddon() error = %v", err)
	}

	created, err := client.DescribeAddon(context.Background(), &eks.DescribeAddonInput{
		ClusterName: aws.String("orders"),
		AddonName:   aws.String("coredns"),
	})
	if err != nil {
		t.Fatalf("DescribeAddon(created namespace) error = %v", err)
	}
	if created.Addon.NamespaceConfig == nil || aws.ToString(created.Addon.NamespaceConfig.Namespace) != "kube-system" {
		t.Fatalf("DescribeAddon(created namespace) = %#v, want kube-system", created.Addon.NamespaceConfig)
	}
	if got, want := created.Addon.PodIdentityAssociations, []string{"coredns=arn:aws:iam::000000000000:role/coredns-addon"}; !slices.Equal(got, want) {
		t.Fatalf("DescribeAddon(created pod identities) = %#v, want %#v", got, want)
	}

	if _, err := client.UpdateAddon(context.Background(), &eks.UpdateAddonInput{
		ClusterName: aws.String("orders"),
		AddonName:   aws.String("coredns"),
		PodIdentityAssociations: []types.AddonPodIdentityAssociations{{
			RoleArn:        aws.String("arn:aws:iam::000000000000:role/coredns-addon-v2"),
			ServiceAccount: aws.String("coredns-v2"),
		}},
	}); err != nil {
		t.Fatalf("UpdateAddon(namespace) error = %v", err)
	}

	updated, err := client.DescribeAddon(context.Background(), &eks.DescribeAddonInput{
		ClusterName: aws.String("orders"),
		AddonName:   aws.String("coredns"),
	})
	if err != nil {
		t.Fatalf("DescribeAddon(updated namespace) error = %v", err)
	}
	if updated.Addon.NamespaceConfig == nil || aws.ToString(updated.Addon.NamespaceConfig.Namespace) != "kube-system" {
		t.Fatalf("DescribeAddon(updated namespace) = %#v, want kube-system", updated.Addon.NamespaceConfig)
	}
	if got, want := updated.Addon.PodIdentityAssociations, []string{"coredns-v2=arn:aws:iam::000000000000:role/coredns-addon-v2"}; !slices.Equal(got, want) {
		t.Fatalf("DescribeAddon(updated pod identities) = %#v, want %#v", got, want)
	}
}

func TestEKSCompatibilityAdapterManagesAccessEntriesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	principal := "arn:aws:iam::000000000000:root"
	created, err := client.CreateAccessEntry(context.Background(), &eks.CreateAccessEntryInput{
		ClusterName:      aws.String("orders"),
		PrincipalArn:     aws.String(principal),
		KubernetesGroups: []string{"viewers"},
		Tags:             map[string]string{"env": "test"},
		Type:             aws.String("STANDARD"),
		Username:         aws.String("homeport-root"),
	})
	if err != nil {
		t.Fatalf("CreateAccessEntry() error = %v", err)
	}
	if created.AccessEntry == nil || aws.ToString(created.AccessEntry.PrincipalArn) != principal {
		t.Fatalf("CreateAccessEntry() = %#v, want principal %s", created.AccessEntry, principal)
	}

	described, err := client.DescribeAccessEntry(context.Background(), &eks.DescribeAccessEntryInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
	})
	if err != nil {
		t.Fatalf("DescribeAccessEntry() error = %v", err)
	}
	if described.AccessEntry == nil ||
		aws.ToString(described.AccessEntry.AccessEntryArn) != aws.ToString(created.AccessEntry.AccessEntryArn) ||
		aws.ToString(described.AccessEntry.Username) != "homeport-root" ||
		described.AccessEntry.Tags["env"] != "test" ||
		!slices.Equal(described.AccessEntry.KubernetesGroups, []string{"viewers"}) {
		t.Fatalf("DescribeAccessEntry() = %#v, want created access entry", described.AccessEntry)
	}

	listed, err := client.ListAccessEntries(context.Background(), &eks.ListAccessEntriesInput{ClusterName: aws.String("orders")})
	if err != nil {
		t.Fatalf("ListAccessEntries() error = %v", err)
	}
	if got, want := listed.AccessEntries, []string{principal}; !slices.Equal(got, want) {
		t.Fatalf("ListAccessEntries() = %#v, want %#v", got, want)
	}

	if _, err := client.DeleteAccessEntry(context.Background(), &eks.DeleteAccessEntryInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
	}); err != nil {
		t.Fatalf("DeleteAccessEntry() error = %v", err)
	}

	listed, err = client.ListAccessEntries(context.Background(), &eks.ListAccessEntriesInput{ClusterName: aws.String("orders")})
	if err != nil {
		t.Fatalf("ListAccessEntries(after delete) error = %v", err)
	}
	if len(listed.AccessEntries) != 0 {
		t.Fatalf("ListAccessEntries(after delete) = %#v, want no access entries", listed.AccessEntries)
	}
}

func TestEKSCompatibilityAdapterReplaysCreateAccessEntryClientRequestTokenWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	input := &eks.CreateAccessEntryInput{
		ClusterName:        aws.String("orders"),
		PrincipalArn:       aws.String("arn:aws:iam::000000000000:root"),
		ClientRequestToken: aws.String("create-access-entry-token"),
		KubernetesGroups:   []string{"viewers"},
		Type:               aws.String("STANDARD"),
		Username:           aws.String("homeport-root"),
	}
	created, err := client.CreateAccessEntry(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateAccessEntry(first) error = %v", err)
	}
	replayed, err := client.CreateAccessEntry(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateAccessEntry(replay) error = %v", err)
	}
	if aws.ToString(replayed.AccessEntry.AccessEntryArn) != aws.ToString(created.AccessEntry.AccessEntryArn) {
		t.Fatalf("CreateAccessEntry(replay) = %#v, want original access entry", replayed.AccessEntry)
	}
}

func TestEKSCompatibilityAdapterManagesAccessPolicyAssociationsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	principal := "arn:aws:iam::000000000000:root"
	if _, err := client.CreateAccessEntry(context.Background(), &eks.CreateAccessEntryInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
	}); err != nil {
		t.Fatalf("CreateAccessEntry() error = %v", err)
	}

	policyARN := "arn:aws:eks::aws:cluster-access-policy/AmazonEKSViewPolicy"
	associated, err := client.AssociateAccessPolicy(context.Background(), &eks.AssociateAccessPolicyInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
		PolicyArn:    aws.String(policyARN),
		AccessScope:  &types.AccessScope{Type: types.AccessScopeTypeCluster},
	})
	if err != nil {
		t.Fatalf("AssociateAccessPolicy() error = %v", err)
	}
	if associated.AssociatedAccessPolicy == nil ||
		aws.ToString(associated.AssociatedAccessPolicy.PolicyArn) != policyARN ||
		associated.AssociatedAccessPolicy.AccessScope == nil ||
		associated.AssociatedAccessPolicy.AccessScope.Type != types.AccessScopeTypeCluster {
		t.Fatalf("AssociateAccessPolicy() = %#v, want cluster-scoped view policy", associated.AssociatedAccessPolicy)
	}

	listed, err := client.ListAssociatedAccessPolicies(context.Background(), &eks.ListAssociatedAccessPoliciesInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
	})
	if err != nil {
		t.Fatalf("ListAssociatedAccessPolicies() error = %v", err)
	}
	if len(listed.AssociatedAccessPolicies) != 1 || aws.ToString(listed.AssociatedAccessPolicies[0].PolicyArn) != policyARN {
		t.Fatalf("ListAssociatedAccessPolicies() = %#v, want associated policy", listed.AssociatedAccessPolicies)
	}

	if _, err := client.DisassociateAccessPolicy(context.Background(), &eks.DisassociateAccessPolicyInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
		PolicyArn:    aws.String(policyARN),
	}); err != nil {
		t.Fatalf("DisassociateAccessPolicy() error = %v", err)
	}

	listed, err = client.ListAssociatedAccessPolicies(context.Background(), &eks.ListAssociatedAccessPoliciesInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
	})
	if err != nil {
		t.Fatalf("ListAssociatedAccessPolicies(after disassociate) error = %v", err)
	}
	if len(listed.AssociatedAccessPolicies) != 0 {
		t.Fatalf("ListAssociatedAccessPolicies(after disassociate) = %#v, want no associated policies", listed.AssociatedAccessPolicies)
	}
}

func TestEKSCompatibilityAdapterReplaysUpdateAccessEntryClientRequestTokenWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	principal := "arn:aws:iam::000000000000:root"
	if _, err := client.CreateAccessEntry(context.Background(), &eks.CreateAccessEntryInput{
		ClusterName:      aws.String("orders"),
		PrincipalArn:     aws.String(principal),
		KubernetesGroups: []string{"viewers"},
		Username:         aws.String("old-user"),
	}); err != nil {
		t.Fatalf("CreateAccessEntry() error = %v", err)
	}

	input := &eks.UpdateAccessEntryInput{
		ClusterName:        aws.String("orders"),
		PrincipalArn:       aws.String(principal),
		KubernetesGroups:   []string{"admins", "operators"},
		Username:           aws.String("homeport-admin"),
		ClientRequestToken: aws.String("update-root-entry"),
	}
	updated, err := client.UpdateAccessEntry(context.Background(), input)
	if err != nil {
		t.Fatalf("UpdateAccessEntry(first) error = %v", err)
	}
	if updated.AccessEntry == nil ||
		aws.ToString(updated.AccessEntry.Username) != "homeport-admin" ||
		!slices.Equal(updated.AccessEntry.KubernetesGroups, []string{"admins", "operators"}) {
		t.Fatalf("UpdateAccessEntry(first) = %#v, want updated username and groups", updated.AccessEntry)
	}

	replayed, err := client.UpdateAccessEntry(context.Background(), input)
	if err != nil {
		t.Fatalf("UpdateAccessEntry(replay) error = %v", err)
	}
	if aws.ToString(replayed.AccessEntry.AccessEntryArn) != aws.ToString(updated.AccessEntry.AccessEntryArn) {
		t.Fatalf("UpdateAccessEntry(replay) = %#v, want original access entry ARN %q", replayed.AccessEntry, aws.ToString(updated.AccessEntry.AccessEntryArn))
	}

	described, err := client.DescribeAccessEntry(context.Background(), &eks.DescribeAccessEntryInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
	})
	if err != nil {
		t.Fatalf("DescribeAccessEntry(after update) error = %v", err)
	}
	if aws.ToString(described.AccessEntry.Username) != "homeport-admin" ||
		!slices.Equal(described.AccessEntry.KubernetesGroups, []string{"admins", "operators"}) {
		t.Fatalf("DescribeAccessEntry(after update) = %#v, want updated username and groups", described.AccessEntry)
	}
}

func TestEKSCompatibilityAdapterHandlesRoleARNAccessEntryPathsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	principal := "arn:aws:iam::000000000000:role/team/platform-admin"
	if _, err := client.CreateAccessEntry(context.Background(), &eks.CreateAccessEntryInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
		Username:     aws.String("platform-admin"),
	}); err != nil {
		t.Fatalf("CreateAccessEntry() error = %v", err)
	}

	if _, err := client.UpdateAccessEntry(context.Background(), &eks.UpdateAccessEntryInput{
		ClusterName:      aws.String("orders"),
		PrincipalArn:     aws.String(principal),
		KubernetesGroups: []string{"platform"},
		Username:         aws.String("platform-admin-updated"),
	}); err != nil {
		t.Fatalf("UpdateAccessEntry(role ARN) error = %v", err)
	}

	described, err := client.DescribeAccessEntry(context.Background(), &eks.DescribeAccessEntryInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
	})
	if err != nil {
		t.Fatalf("DescribeAccessEntry(role ARN) error = %v", err)
	}
	if aws.ToString(described.AccessEntry.Username) != "platform-admin-updated" ||
		!slices.Equal(described.AccessEntry.KubernetesGroups, []string{"platform"}) {
		t.Fatalf("DescribeAccessEntry(role ARN) = %#v, want updated role entry", described.AccessEntry)
	}

	policyARN := "arn:aws:eks::aws:cluster-access-policy/AmazonEKSViewPolicy"
	if _, err := client.AssociateAccessPolicy(context.Background(), &eks.AssociateAccessPolicyInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
		PolicyArn:    aws.String(policyARN),
		AccessScope:  &types.AccessScope{Type: types.AccessScopeTypeCluster},
	}); err != nil {
		t.Fatalf("AssociateAccessPolicy(role ARN) error = %v", err)
	}
	associated, err := client.ListAssociatedAccessPolicies(context.Background(), &eks.ListAssociatedAccessPoliciesInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
	})
	if err != nil {
		t.Fatalf("ListAssociatedAccessPolicies(role ARN) error = %v", err)
	}
	if len(associated.AssociatedAccessPolicies) != 1 || aws.ToString(associated.AssociatedAccessPolicies[0].PolicyArn) != policyARN {
		t.Fatalf("ListAssociatedAccessPolicies(role ARN) = %#v, want associated policy", associated.AssociatedAccessPolicies)
	}
	if _, err := client.DisassociateAccessPolicy(context.Background(), &eks.DisassociateAccessPolicyInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
		PolicyArn:    aws.String(policyARN),
	}); err != nil {
		t.Fatalf("DisassociateAccessPolicy(role ARN) error = %v", err)
	}

	if _, err := client.DeleteAccessEntry(context.Background(), &eks.DeleteAccessEntryInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
	}); err != nil {
		t.Fatalf("DeleteAccessEntry(role ARN) error = %v", err)
	}
}

func TestEKSCompatibilityAdapterPaginatesListNodegroupsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	for _, name := range []string{"blue", "green", "red"} {
		if _, err := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
			ClusterName:   aws.String("orders"),
			NodegroupName: aws.String(name),
			NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
			Subnets:       []string{"subnet-a", "subnet-b"},
		}); err != nil {
			t.Fatalf("CreateNodegroup(%s) error = %v", name, err)
		}
	}

	first, err := client.ListNodegroups(context.Background(), &eks.ListNodegroupsInput{
		ClusterName: aws.String("orders"),
		MaxResults:  aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListNodegroups(first page) error = %v", err)
	}
	if got, want := first.Nodegroups, []string{"blue", "green"}; !slices.Equal(got, want) {
		t.Fatalf("ListNodegroups(first page) = %#v, want %#v", got, want)
	}
	if aws.ToString(first.NextToken) == "" {
		t.Fatal("ListNodegroups(first page) NextToken empty, want token")
	}

	second, err := client.ListNodegroups(context.Background(), &eks.ListNodegroupsInput{
		ClusterName: aws.String("orders"),
		MaxResults:  aws.Int32(2),
		NextToken:   first.NextToken,
	})
	if err != nil {
		t.Fatalf("ListNodegroups(second page) error = %v", err)
	}
	if got, want := second.Nodegroups, []string{"red"}; !slices.Equal(got, want) {
		t.Fatalf("ListNodegroups(second page) = %#v, want %#v", got, want)
	}
	if second.NextToken != nil {
		t.Fatalf("ListNodegroups(second page) NextToken = %q, want nil", aws.ToString(second.NextToken))
	}
}

func TestEKSCompatibilityAdapterPaginatesAddonAndAccessEntryListsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	for _, name := range []string{"vpc-cni", "coredns", "kube-proxy"} {
		if _, err := client.CreateAddon(context.Background(), &eks.CreateAddonInput{
			ClusterName:  aws.String("orders"),
			AddonName:    aws.String(name),
			AddonVersion: aws.String("v1.0.0"),
		}); err != nil {
			t.Fatalf("CreateAddon(%s) error = %v", name, err)
		}
	}
	addons, err := client.ListAddons(context.Background(), &eks.ListAddonsInput{
		ClusterName: aws.String("orders"),
		MaxResults:  aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListAddons(first page) error = %v", err)
	}
	if got, want := addons.Addons, []string{"coredns", "kube-proxy"}; !slices.Equal(got, want) {
		t.Fatalf("ListAddons(first page) = %#v, want %#v", got, want)
	}
	if aws.ToString(addons.NextToken) == "" {
		t.Fatal("ListAddons(first page) NextToken empty, want token")
	}
	addons, err = client.ListAddons(context.Background(), &eks.ListAddonsInput{
		ClusterName: aws.String("orders"),
		MaxResults:  aws.Int32(2),
		NextToken:   addons.NextToken,
	})
	if err != nil {
		t.Fatalf("ListAddons(second page) error = %v", err)
	}
	if got, want := addons.Addons, []string{"vpc-cni"}; !slices.Equal(got, want) || addons.NextToken != nil {
		t.Fatalf("ListAddons(second page) = %#v token=%q, want %#v and no token", got, aws.ToString(addons.NextToken), want)
	}

	principals := []string{
		"arn:aws:iam::000000000000:role/viewer",
		"arn:aws:iam::000000000000:role/admin",
		"arn:aws:iam::000000000000:role/dev",
	}
	for _, principal := range principals {
		if _, err := client.CreateAccessEntry(context.Background(), &eks.CreateAccessEntryInput{
			ClusterName:  aws.String("orders"),
			PrincipalArn: aws.String(principal),
		}); err != nil {
			t.Fatalf("CreateAccessEntry(%s) error = %v", principal, err)
		}
	}
	entries, err := client.ListAccessEntries(context.Background(), &eks.ListAccessEntriesInput{
		ClusterName: aws.String("orders"),
		MaxResults:  aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListAccessEntries(first page) error = %v", err)
	}
	if got, want := entries.AccessEntries, []string{
		"arn:aws:iam::000000000000:role/admin",
		"arn:aws:iam::000000000000:role/dev",
	}; !slices.Equal(got, want) {
		t.Fatalf("ListAccessEntries(first page) = %#v, want %#v", got, want)
	}
	if aws.ToString(entries.NextToken) == "" {
		t.Fatal("ListAccessEntries(first page) NextToken empty, want token")
	}
	entries, err = client.ListAccessEntries(context.Background(), &eks.ListAccessEntriesInput{
		ClusterName: aws.String("orders"),
		MaxResults:  aws.Int32(2),
		NextToken:   entries.NextToken,
	})
	if err != nil {
		t.Fatalf("ListAccessEntries(second page) error = %v", err)
	}
	if got, want := entries.AccessEntries, []string{"arn:aws:iam::000000000000:role/viewer"}; !slices.Equal(got, want) || entries.NextToken != nil {
		t.Fatalf("ListAccessEntries(second page) = %#v token=%q, want %#v and no token", got, aws.ToString(entries.NextToken), want)
	}

	for _, policyARN := range []string{
		"arn:aws:eks::aws:cluster-access-policy/AmazonEKSViewPolicy",
		"arn:aws:eks::aws:cluster-access-policy/AmazonEKSAdminPolicy",
		"arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy",
	} {
		if _, err := client.AssociateAccessPolicy(context.Background(), &eks.AssociateAccessPolicyInput{
			ClusterName:  aws.String("orders"),
			PrincipalArn: aws.String("arn:aws:iam::000000000000:role/admin"),
			PolicyArn:    aws.String(policyARN),
			AccessScope:  &types.AccessScope{Type: types.AccessScopeTypeCluster},
		}); err != nil {
			t.Fatalf("AssociateAccessPolicy(%s) error = %v", policyARN, err)
		}
	}
	policies, err := client.ListAssociatedAccessPolicies(context.Background(), &eks.ListAssociatedAccessPoliciesInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String("arn:aws:iam::000000000000:role/admin"),
		MaxResults:   aws.Int32(2),
	})
	if err != nil {
		t.Fatalf("ListAssociatedAccessPolicies(first page) error = %v", err)
	}
	gotPolicies := []string{aws.ToString(policies.AssociatedAccessPolicies[0].PolicyArn), aws.ToString(policies.AssociatedAccessPolicies[1].PolicyArn)}
	if want := []string{
		"arn:aws:eks::aws:cluster-access-policy/AmazonEKSAdminPolicy",
		"arn:aws:eks::aws:cluster-access-policy/AmazonEKSClusterAdminPolicy",
	}; !slices.Equal(gotPolicies, want) {
		t.Fatalf("ListAssociatedAccessPolicies(first page) = %#v, want %#v", gotPolicies, want)
	}
	if aws.ToString(policies.NextToken) == "" {
		t.Fatal("ListAssociatedAccessPolicies(first page) NextToken empty, want token")
	}
	policies, err = client.ListAssociatedAccessPolicies(context.Background(), &eks.ListAssociatedAccessPoliciesInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String("arn:aws:iam::000000000000:role/admin"),
		MaxResults:   aws.Int32(2),
		NextToken:    policies.NextToken,
	})
	if err != nil {
		t.Fatalf("ListAssociatedAccessPolicies(second page) error = %v", err)
	}
	gotPolicies = []string{aws.ToString(policies.AssociatedAccessPolicies[0].PolicyArn)}
	if want := []string{"arn:aws:eks::aws:cluster-access-policy/AmazonEKSViewPolicy"}; !slices.Equal(gotPolicies, want) || policies.NextToken != nil {
		t.Fatalf("ListAssociatedAccessPolicies(second page) = %#v token=%q, want %#v and no token", gotPolicies, aws.ToString(policies.NextToken), want)
	}
}

func TestEKSCompatibilityAdapterRejectsInvalidListPaginationWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	principal := "arn:aws:iam::000000000000:root"
	if _, err := client.CreateAccessEntry(context.Background(), &eks.CreateAccessEntryInput{
		ClusterName:  aws.String("orders"),
		PrincipalArn: aws.String(principal),
	}); err != nil {
		t.Fatalf("CreateAccessEntry() error = %v", err)
	}

	cases := []struct {
		name string
		call func(context.Context) error
	}{
		{
			name: "ListClusters invalid max",
			call: func(ctx context.Context) error {
				_, err := client.ListClusters(ctx, &eks.ListClustersInput{MaxResults: aws.Int32(0)})
				return err
			},
		},
		{
			name: "ListClusters malformed token",
			call: func(ctx context.Context) error {
				_, err := client.ListClusters(ctx, &eks.ListClustersInput{NextToken: aws.String("bad-token")})
				return err
			},
		},
		{
			name: "ListNodegroups invalid max",
			call: func(ctx context.Context) error {
				_, err := client.ListNodegroups(ctx, &eks.ListNodegroupsInput{ClusterName: aws.String("orders"), MaxResults: aws.Int32(0)})
				return err
			},
		},
		{
			name: "ListNodegroups malformed token",
			call: func(ctx context.Context) error {
				_, err := client.ListNodegroups(ctx, &eks.ListNodegroupsInput{ClusterName: aws.String("orders"), NextToken: aws.String("bad-token")})
				return err
			},
		},
		{
			name: "ListAddons invalid max",
			call: func(ctx context.Context) error {
				_, err := client.ListAddons(ctx, &eks.ListAddonsInput{ClusterName: aws.String("orders"), MaxResults: aws.Int32(0)})
				return err
			},
		},
		{
			name: "ListAddons malformed token",
			call: func(ctx context.Context) error {
				_, err := client.ListAddons(ctx, &eks.ListAddonsInput{ClusterName: aws.String("orders"), NextToken: aws.String("bad-token")})
				return err
			},
		},
		{
			name: "ListAccessEntries invalid max",
			call: func(ctx context.Context) error {
				_, err := client.ListAccessEntries(ctx, &eks.ListAccessEntriesInput{ClusterName: aws.String("orders"), MaxResults: aws.Int32(0)})
				return err
			},
		},
		{
			name: "ListAccessEntries malformed token",
			call: func(ctx context.Context) error {
				_, err := client.ListAccessEntries(ctx, &eks.ListAccessEntriesInput{ClusterName: aws.String("orders"), NextToken: aws.String("bad-token")})
				return err
			},
		},
		{
			name: "ListAssociatedAccessPolicies invalid max",
			call: func(ctx context.Context) error {
				_, err := client.ListAssociatedAccessPolicies(ctx, &eks.ListAssociatedAccessPoliciesInput{ClusterName: aws.String("orders"), PrincipalArn: aws.String(principal), MaxResults: aws.Int32(0)})
				return err
			},
		},
		{
			name: "ListAssociatedAccessPolicies malformed token",
			call: func(ctx context.Context) error {
				_, err := client.ListAssociatedAccessPolicies(ctx, &eks.ListAssociatedAccessPoliciesInput{ClusterName: aws.String("orders"), PrincipalArn: aws.String(principal), NextToken: aws.String("bad-token")})
				return err
			},
		},
	}
	for _, tc := range cases {
		err := tc.call(context.Background())
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
			t.Fatalf("%s error = %v, want InvalidParameterException", tc.name, err)
		}
	}
}

func TestEKSCompatibilityAdapterUpdatesNodegroupConfigWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	if _, err := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
		NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
		Subnets:       []string{"subnet-a", "subnet-b"},
		Labels:        map[string]string{"tier": "old", "keep": "yes"},
		ScalingConfig: &types.NodegroupScalingConfig{
			DesiredSize: aws.Int32(2),
			MaxSize:     aws.Int32(3),
			MinSize:     aws.Int32(1),
		},
	}); err != nil {
		t.Fatalf("CreateNodegroup() error = %v", err)
	}

	updated, err := client.UpdateNodegroupConfig(context.Background(), &eks.UpdateNodegroupConfigInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
		Labels: &types.UpdateLabelsPayload{
			AddOrUpdateLabels: map[string]string{"tier": "api"},
			RemoveLabels:      []string{"keep"},
		},
		ScalingConfig: &types.NodegroupScalingConfig{
			DesiredSize: aws.Int32(4),
			MaxSize:     aws.Int32(5),
			MinSize:     aws.Int32(2),
		},
	})
	if err != nil {
		t.Fatalf("UpdateNodegroupConfig() error = %v", err)
	}
	if updated.Update == nil || updated.Update.Status != types.UpdateStatusSuccessful {
		t.Fatalf("UpdateNodegroupConfig() = %#v, want successful update", updated.Update)
	}

	described, err := client.DescribeNodegroup(context.Background(), &eks.DescribeNodegroupInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
	})
	if err != nil {
		t.Fatalf("DescribeNodegroup(after update) error = %v", err)
	}
	if described.Nodegroup.Labels["tier"] != "api" {
		t.Fatalf("DescribeNodegroup(after update) labels = %#v, want tier=api", described.Nodegroup.Labels)
	}
	if _, ok := described.Nodegroup.Labels["keep"]; ok {
		t.Fatalf("DescribeNodegroup(after update) labels = %#v, want keep removed", described.Nodegroup.Labels)
	}
	if described.Nodegroup.ScalingConfig == nil ||
		aws.ToInt32(described.Nodegroup.ScalingConfig.DesiredSize) != 4 ||
		aws.ToInt32(described.Nodegroup.ScalingConfig.MaxSize) != 5 ||
		aws.ToInt32(described.Nodegroup.ScalingConfig.MinSize) != 2 {
		t.Fatalf("DescribeNodegroup(after update) scaling = %#v, want 2/4/5", described.Nodegroup.ScalingConfig)
	}
}

func TestEKSCompatibilityAdapterReplaysUpdateNodegroupConfigClientRequestTokenWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	if _, err := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
		NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
		Subnets:       []string{"subnet-a", "subnet-b"},
	}); err != nil {
		t.Fatalf("CreateNodegroup() error = %v", err)
	}

	input := &eks.UpdateNodegroupConfigInput{
		ClusterName:        aws.String("orders"),
		NodegroupName:      aws.String("workers"),
		ClientRequestToken: aws.String("update-nodegroup-config-token"),
		ScalingConfig: &types.NodegroupScalingConfig{
			DesiredSize: aws.Int32(4),
			MaxSize:     aws.Int32(5),
			MinSize:     aws.Int32(2),
		},
	}
	updated, err := client.UpdateNodegroupConfig(context.Background(), input)
	if err != nil {
		t.Fatalf("UpdateNodegroupConfig(first) error = %v", err)
	}
	replayed, err := client.UpdateNodegroupConfig(context.Background(), input)
	if err != nil {
		t.Fatalf("UpdateNodegroupConfig(replay) error = %v", err)
	}
	if aws.ToString(replayed.Update.Id) != aws.ToString(updated.Update.Id) {
		t.Fatalf("UpdateNodegroupConfig(replay) = %#v, want original update id %q", replayed.Update, aws.ToString(updated.Update.Id))
	}
}

func TestEKSCompatibilityAdapterUpdatesNodegroupVersionWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	if _, err := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
		NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
		Subnets:       []string{"subnet-a", "subnet-b"},
	}); err != nil {
		t.Fatalf("CreateNodegroup() error = %v", err)
	}

	updated, err := client.UpdateNodegroupVersion(context.Background(), &eks.UpdateNodegroupVersionInput{
		ClusterName:    aws.String("orders"),
		NodegroupName:  aws.String("workers"),
		Version:        aws.String("1.31"),
		ReleaseVersion: aws.String("1.31.1-20260709"),
	})
	if err != nil {
		t.Fatalf("UpdateNodegroupVersion() error = %v", err)
	}
	if updated.Update == nil || updated.Update.Status != types.UpdateStatusSuccessful {
		t.Fatalf("UpdateNodegroupVersion() = %#v, want successful update", updated.Update)
	}

	described, err := client.DescribeNodegroup(context.Background(), &eks.DescribeNodegroupInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
	})
	if err != nil {
		t.Fatalf("DescribeNodegroup(after version update) error = %v", err)
	}
	if aws.ToString(described.Nodegroup.Version) != "1.31" || aws.ToString(described.Nodegroup.ReleaseVersion) != "1.31.1-20260709" {
		t.Fatalf("DescribeNodegroup(after version update) version = %q release = %q, want updated values", aws.ToString(described.Nodegroup.Version), aws.ToString(described.Nodegroup.ReleaseVersion))
	}
}

func TestEKSCompatibilityAdapterReplaysUpdateNodegroupVersionClientRequestTokenWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()

	client := eks.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{
		Name:    aws.String("orders"),
		RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"),
		ResourcesVpcConfig: &types.VpcConfigRequest{
			SubnetIds: []string{"subnet-a", "subnet-b"},
		},
	}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	if _, err := client.CreateNodegroup(context.Background(), &eks.CreateNodegroupInput{
		ClusterName:   aws.String("orders"),
		NodegroupName: aws.String("workers"),
		NodeRole:      aws.String("arn:aws:iam::000000000000:role/homeport-eks-node"),
		Subnets:       []string{"subnet-a", "subnet-b"},
	}); err != nil {
		t.Fatalf("CreateNodegroup() error = %v", err)
	}

	input := &eks.UpdateNodegroupVersionInput{
		ClusterName:        aws.String("orders"),
		NodegroupName:      aws.String("workers"),
		ClientRequestToken: aws.String("update-nodegroup-version-token"),
		Version:            aws.String("1.31"),
		ReleaseVersion:     aws.String("1.31.1-20260709"),
	}
	updated, err := client.UpdateNodegroupVersion(context.Background(), input)
	if err != nil {
		t.Fatalf("UpdateNodegroupVersion(first) error = %v", err)
	}
	replayed, err := client.UpdateNodegroupVersion(context.Background(), input)
	if err != nil {
		t.Fatalf("UpdateNodegroupVersion(replay) error = %v", err)
	}
	if aws.ToString(replayed.Update.Id) != aws.ToString(updated.Update.Id) {
		t.Fatalf("UpdateNodegroupVersion(replay) = %#v, want original update id %q", replayed.Update, aws.ToString(updated.Update.Id))
	}
}

func TestEKSCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewEKSAdapter())
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
		Cluster struct {
			Name   string `json:"name"`
			Arn    string `json:"arn"`
			Status string `json:"status"`
		} `json:"cluster"`
	}
	if err := json.Unmarshal(runAWS("eks", "create-cluster",
		"--name", "cli-orders",
		"--role-arn", "arn:aws:iam::000000000000:role/homeport-eks",
		"--resources-vpc-config", "subnetIds=subnet-a,subnet-b",
	), &created); err != nil {
		t.Fatalf("decode create-cluster output: %v", err)
	}
	if created.Cluster.Name != "cli-orders" || created.Cluster.Arn == "" || created.Cluster.Status != "ACTIVE" {
		t.Fatalf("create-cluster = %#v, want active cli-orders cluster", created.Cluster)
	}

	var described struct {
		Cluster struct {
			Arn string `json:"arn"`
		} `json:"cluster"`
	}
	if err := json.Unmarshal(runAWS("eks", "describe-cluster", "--name", "cli-orders"), &described); err != nil {
		t.Fatalf("decode describe-cluster output: %v", err)
	}
	if described.Cluster.Arn != created.Cluster.Arn {
		t.Fatalf("describe-cluster = %#v, want created cluster", described.Cluster)
	}

	var listed struct {
		Clusters []string `json:"clusters"`
	}
	if err := json.Unmarshal(runAWS("eks", "list-clusters"), &listed); err != nil {
		t.Fatalf("decode list-clusters output: %v", err)
	}
	if len(listed.Clusters) != 1 || listed.Clusters[0] != "cli-orders" {
		t.Fatalf("list-clusters = %#v, want cli-orders", listed.Clusters)
	}

	var updated struct {
		Update struct {
			Status string `json:"status"`
		} `json:"update"`
	}
	if err := json.Unmarshal(runAWS("eks", "update-cluster-config", "--name", "cli-orders", "--resources-vpc-config", "endpointPublicAccess=false,endpointPrivateAccess=true"), &updated); err != nil {
		t.Fatalf("decode update-cluster-config output: %v", err)
	}
	if updated.Update.Status != "Successful" {
		t.Fatalf("update-cluster-config = %#v, want successful update", updated.Update)
	}

	runAWS("eks", "delete-cluster", "--name", "cli-orders")
	if err := json.Unmarshal(runAWS("eks", "list-clusters"), &listed); err != nil {
		t.Fatalf("decode list-clusters after delete output: %v", err)
	}
	if len(listed.Clusters) != 0 {
		t.Fatalf("list-clusters after delete = %#v, want no clusters", listed.Clusters)
	}
}

func TestEKSCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewEKSAdapter())
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
    eks = %q
  }
}

resource "aws_eks_cluster" "deploy" {
  name     = "terraform-orders"
  role_arn = "arn:aws:iam::000000000000:role/homeport-eks"

  vpc_config {
    subnet_ids = ["subnet-a", "subnet-b"]
  }

  tags = {
    env = "test"
  }
}

output "cluster_arn" {
  value = aws_eks_cluster.deploy.arn
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

	out := runTerraform("output", "-raw", "cluster_arn")
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("terraform output cluster_arn is empty")
	}
}

func TestEKSCompatibilityAdapterRejectsExpiredCredential(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter(compataws.WithEKSAuthorizer(authz.NewPolicyAuthorizer(
		authz.Rule{Effect: authz.Allow, Actions: []string{"eks:*"}, Resources: []string{"*"}},
		authz.Rule{
			Effect:    authz.Deny,
			Actions:   []string{"eks:ListClusters"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_expired", Values: []string{"true"}},
			},
		},
	))))
	defer server.Close()
	client := eks.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *eks.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Expired", "true"))
	})
	_, err := client.ListClusters(context.Background(), &eks.ListClustersInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("ListClusters(expired credential) error = %v, want AccessDenied", err)
	}
}

func TestEKSCompatibilityAdapterRejectsMalformedCreateCluster(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/clusters", strings.NewReader("{"))
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
	if resp.StatusCode != http.StatusBadRequest || body["__type"] != "InvalidParameterException" {
		t.Fatalf("malformed CreateCluster = status %d body %#v, want 400 InvalidParameterException", resp.StatusCode, body)
	}
}

func TestEKSCompatibilityAdapterRejectsMalformedUpdateClusterConfig(t *testing.T) {
	server := httptest.NewServer(compataws.NewEKSAdapter())
	defer server.Close()
	client := eks.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *eks.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateCluster(context.Background(), &eks.CreateClusterInput{Name: aws.String("orders"), RoleArn: aws.String("arn:aws:iam::000000000000:role/homeport-eks"), ResourcesVpcConfig: &types.VpcConfigRequest{SubnetIds: []string{"subnet-a"}}}); err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, server.URL+"/clusters/orders/update-config", strings.NewReader("{"))
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
	if resp.StatusCode != http.StatusBadRequest || body["__type"] != "InvalidParameterException" {
		t.Fatalf("malformed UpdateClusterConfig = status %d body %#v, want 400 InvalidParameterException", resp.StatusCode, body)
	}
}
