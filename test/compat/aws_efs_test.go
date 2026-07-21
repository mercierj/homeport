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
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestEFSCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()

	client := efs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *efs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{
		CreationToken: aws.String("orders-fs"),
		Tags: []types.Tag{{
			Key:   aws.String("Name"),
			Value: aws.String("orders"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateFileSystem() error = %v", err)
	}
	if aws.ToString(created.FileSystemId) == "" || aws.ToString(created.CreationToken) != "orders-fs" || created.LifeCycleState != types.LifeCycleStateAvailable {
		t.Fatalf("CreateFileSystem() = %#v, want available orders-fs", created)
	}

	described, err := client.DescribeFileSystems(context.Background(), &efs.DescribeFileSystemsInput{
		FileSystemId: created.FileSystemId,
	})
	if err != nil {
		t.Fatalf("DescribeFileSystems() error = %v", err)
	}
	if len(described.FileSystems) != 1 || aws.ToString(described.FileSystems[0].FileSystemId) != aws.ToString(created.FileSystemId) {
		t.Fatalf("DescribeFileSystems() = %#v, want created file system", described.FileSystems)
	}

	updated, err := client.UpdateFileSystem(context.Background(), &efs.UpdateFileSystemInput{
		FileSystemId:   created.FileSystemId,
		ThroughputMode: types.ThroughputModeElastic,
	})
	if err != nil {
		t.Fatalf("UpdateFileSystem() error = %v", err)
	}
	if updated.ThroughputMode != types.ThroughputModeElastic {
		t.Fatalf("UpdateFileSystem() = %#v, want elastic throughput mode", updated)
	}

	if _, err := client.DeleteFileSystem(context.Background(), &efs.DeleteFileSystemInput{FileSystemId: created.FileSystemId}); err != nil {
		t.Fatalf("DeleteFileSystem() error = %v", err)
	}
	described, err = client.DescribeFileSystems(context.Background(), &efs.DescribeFileSystemsInput{})
	if err != nil {
		t.Fatalf("DescribeFileSystems(after delete) error = %v", err)
	}
	if len(described.FileSystems) != 0 {
		t.Fatalf("DescribeFileSystems(after delete) = %#v, want no file systems", described.FileSystems)
	}
}

func TestEFSCompatibilityAdapterPaginatesDescribeFileSystemsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()

	client := efs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *efs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, token := range []string{"alpha", "bravo", "charlie"} {
		if _, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{CreationToken: aws.String(token)}); err != nil {
			t.Fatalf("CreateFileSystem(%s) error = %v", token, err)
		}
	}

	first, err := client.DescribeFileSystems(context.Background(), &efs.DescribeFileSystemsInput{MaxItems: aws.Int32(2)})
	if err != nil {
		t.Fatalf("DescribeFileSystems(first) error = %v", err)
	}
	if len(first.FileSystems) != 2 || first.NextMarker == nil {
		t.Fatalf("DescribeFileSystems(first) = %#v marker=%v, want two file systems and marker", first.FileSystems, first.NextMarker)
	}

	second, err := client.DescribeFileSystems(context.Background(), &efs.DescribeFileSystemsInput{
		MaxItems: aws.Int32(2),
		Marker:   first.NextMarker,
	})
	if err != nil {
		t.Fatalf("DescribeFileSystems(second) error = %v", err)
	}
	if len(second.FileSystems) != 1 || second.NextMarker != nil {
		t.Fatalf("DescribeFileSystems(second) = %#v marker=%v, want final file system and no marker", second.FileSystems, second.NextMarker)
	}
}

func TestEFSCompatibilityAdapterReplaysCreateFileSystemCreationTokenWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()

	client := efs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *efs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	first, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{CreationToken: aws.String("orders-token")})
	if err != nil {
		t.Fatalf("CreateFileSystem(first) error = %v", err)
	}
	second, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{CreationToken: aws.String("orders-token")})
	if err != nil {
		t.Fatalf("CreateFileSystem(second) error = %v", err)
	}
	if aws.ToString(second.FileSystemId) != aws.ToString(first.FileSystemId) {
		t.Fatalf("CreateFileSystem(second) FileSystemId = %q, want replay of %q", aws.ToString(second.FileSystemId), aws.ToString(first.FileSystemId))
	}

	described, err := client.DescribeFileSystems(context.Background(), &efs.DescribeFileSystemsInput{})
	if err != nil {
		t.Fatalf("DescribeFileSystems() error = %v", err)
	}
	if len(described.FileSystems) != 1 {
		t.Fatalf("DescribeFileSystems() = %#v, want one file system after token replay", described.FileSystems)
	}
}

func TestEFSCompatibilityAdapterAuthorizesAndAuditsFileSystemOperationsWithAWSSDK(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewEFSAdapter(
		compataws.WithEFSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"elasticfilesystem:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"elasticfilesystem:UpdateFileSystem"}, Resources: []string{"*"}},
		)),
		compataws.WithEFSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := efs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *efs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{CreationToken: aws.String("authz-token")})
	if err != nil {
		t.Fatalf("CreateFileSystem() error = %v", err)
	}
	_, err = client.UpdateFileSystem(context.Background(), &efs.UpdateFileSystemInput{
		FileSystemId:   created.FileSystemId,
		ThroughputMode: types.ThroughputModeElastic,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("UpdateFileSystem(denied) error = %v, want AccessDenied", err)
	}

	described, err := client.DescribeFileSystems(context.Background(), &efs.DescribeFileSystemsInput{FileSystemId: created.FileSystemId})
	if err != nil {
		t.Fatalf("DescribeFileSystems() error = %v", err)
	}
	if described.FileSystems[0].ThroughputMode != types.ThroughputModeBursting {
		t.Fatalf("DescribeFileSystems() throughput = %s, want denied update to preserve bursting", described.FileSystems[0].ThroughputMode)
	}
	assertDecision(t, auditLog.Decisions(), "elasticfilesystem:CreateFileSystem", true)
	assertDecision(t, auditLog.Decisions(), "elasticfilesystem:UpdateFileSystem", false)
}

func TestEFSCompatibilityAdapterManagesMountTargetsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()

	client := efs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *efs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	fs, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{CreationToken: aws.String("mount-target-fs")})
	if err != nil {
		t.Fatalf("CreateFileSystem() error = %v", err)
	}
	target, err := client.CreateMountTarget(context.Background(), &efs.CreateMountTargetInput{
		FileSystemId: fs.FileSystemId,
		SubnetId:     aws.String("subnet-12345678"),
	})
	if err != nil {
		t.Fatalf("CreateMountTarget() error = %v", err)
	}
	if aws.ToString(target.MountTargetId) == "" || aws.ToString(target.FileSystemId) != aws.ToString(fs.FileSystemId) || target.LifeCycleState != types.LifeCycleStateAvailable {
		t.Fatalf("CreateMountTarget() = %#v, want available target for file system", target)
	}
	describedFS, err := client.DescribeFileSystems(context.Background(), &efs.DescribeFileSystemsInput{FileSystemId: fs.FileSystemId})
	if err != nil || len(describedFS.FileSystems) != 1 || describedFS.FileSystems[0].NumberOfMountTargets != 1 {
		t.Fatalf("DescribeFileSystems(after mount target) = %#v, %v; want one mount target", describedFS, err)
	}

	byFileSystem, err := client.DescribeMountTargets(context.Background(), &efs.DescribeMountTargetsInput{FileSystemId: fs.FileSystemId})
	if err != nil {
		t.Fatalf("DescribeMountTargets(file system) error = %v", err)
	}
	if len(byFileSystem.MountTargets) != 1 || aws.ToString(byFileSystem.MountTargets[0].MountTargetId) != aws.ToString(target.MountTargetId) {
		t.Fatalf("DescribeMountTargets(file system) = %#v, want created mount target", byFileSystem.MountTargets)
	}

	byID, err := client.DescribeMountTargets(context.Background(), &efs.DescribeMountTargetsInput{MountTargetId: target.MountTargetId})
	if err != nil {
		t.Fatalf("DescribeMountTargets(id) error = %v", err)
	}
	if len(byID.MountTargets) != 1 || aws.ToString(byID.MountTargets[0].SubnetId) != "subnet-12345678" {
		t.Fatalf("DescribeMountTargets(id) = %#v, want subnet read-back", byID.MountTargets)
	}

	if _, err := client.DeleteMountTarget(context.Background(), &efs.DeleteMountTargetInput{MountTargetId: target.MountTargetId}); err != nil {
		t.Fatalf("DeleteMountTarget() error = %v", err)
	}
	afterDelete, err := client.DescribeMountTargets(context.Background(), &efs.DescribeMountTargetsInput{FileSystemId: fs.FileSystemId})
	if err != nil {
		t.Fatalf("DescribeMountTargets(after delete) error = %v", err)
	}
	if len(afterDelete.MountTargets) != 0 {
		t.Fatalf("DescribeMountTargets(after delete) = %#v, want no mount targets", afterDelete.MountTargets)
	}
	describedFS, err = client.DescribeFileSystems(context.Background(), &efs.DescribeFileSystemsInput{FileSystemId: fs.FileSystemId})
	if err != nil || describedFS.FileSystems[0].NumberOfMountTargets != 0 {
		t.Fatalf("DescribeFileSystems(after target delete) = %#v, %v; want no mount targets", describedFS, err)
	}
}

func TestEFSCompatibilityAdapterPaginatesMountTargetsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()

	client := efs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *efs.Options) { o.BaseEndpoint = aws.String(server.URL) })
	fs, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{CreationToken: aws.String("paged-mount-targets")})
	if err != nil {
		t.Fatalf("CreateFileSystem() error = %v", err)
	}
	for _, subnetID := range []string{"subnet-a", "subnet-b"} {
		if _, err := client.CreateMountTarget(context.Background(), &efs.CreateMountTargetInput{FileSystemId: fs.FileSystemId, SubnetId: aws.String(subnetID)}); err != nil {
			t.Fatalf("CreateMountTarget(%s) error = %v", subnetID, err)
		}
	}
	first, err := client.DescribeMountTargets(context.Background(), &efs.DescribeMountTargetsInput{FileSystemId: fs.FileSystemId, MaxItems: aws.Int32(1)})
	if err != nil || len(first.MountTargets) != 1 || first.NextMarker == nil {
		t.Fatalf("DescribeMountTargets(first) = %#v, %v; want one target and marker", first, err)
	}
	second, err := client.DescribeMountTargets(context.Background(), &efs.DescribeMountTargetsInput{FileSystemId: fs.FileSystemId, MaxItems: aws.Int32(1), Marker: first.NextMarker})
	if err != nil || len(second.MountTargets) != 1 || second.NextMarker != nil {
		t.Fatalf("DescribeMountTargets(second) = %#v, %v; want final target", second, err)
	}
}

func TestEFSCompatibilityAdapterRejectsDeletionWhileMountTargetsExist(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()
	client := efs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *efs.Options) { o.BaseEndpoint = aws.String(server.URL) })
	fs, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{CreationToken: aws.String("in-use-fs")})
	if err != nil {
		t.Fatalf("CreateFileSystem() error = %v", err)
	}
	target, err := client.CreateMountTarget(context.Background(), &efs.CreateMountTargetInput{FileSystemId: fs.FileSystemId, SubnetId: aws.String("subnet-in-use")})
	if err != nil {
		t.Fatalf("CreateMountTarget() error = %v", err)
	}
	_, err = client.DeleteFileSystem(context.Background(), &efs.DeleteFileSystemInput{FileSystemId: fs.FileSystemId})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "FileSystemInUse" {
		t.Fatalf("DeleteFileSystem(in use) error = %v, want FileSystemInUse", err)
	}
	if _, err := client.DeleteMountTarget(context.Background(), &efs.DeleteMountTargetInput{MountTargetId: target.MountTargetId}); err != nil {
		t.Fatalf("DeleteMountTarget() error = %v", err)
	}
	if _, err := client.DeleteFileSystem(context.Background(), &efs.DeleteFileSystemInput{FileSystemId: fs.FileSystemId}); err != nil {
		t.Fatalf("DeleteFileSystem(after dependencies) error = %v", err)
	}
}

func TestEFSCompatibilityAdapterManagesFileSystemPolicyWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()

	client := efs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *efs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	fs, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{CreationToken: aws.String("policy-fs")})
	if err != nil {
		t.Fatalf("CreateFileSystem() error = %v", err)
	}
	policy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":"*","Action":"elasticfilesystem:ClientMount","Resource":"*"}]}`
	put, err := client.PutFileSystemPolicy(context.Background(), &efs.PutFileSystemPolicyInput{
		FileSystemId: fs.FileSystemId,
		Policy:       aws.String(policy),
	})
	if err != nil {
		t.Fatalf("PutFileSystemPolicy() error = %v", err)
	}
	if aws.ToString(put.Policy) != policy || aws.ToString(put.FileSystemId) != aws.ToString(fs.FileSystemId) {
		t.Fatalf("PutFileSystemPolicy() = %#v, want policy read-back", put)
	}

	described, err := client.DescribeFileSystemPolicy(context.Background(), &efs.DescribeFileSystemPolicyInput{FileSystemId: fs.FileSystemId})
	if err != nil {
		t.Fatalf("DescribeFileSystemPolicy() error = %v", err)
	}
	if aws.ToString(described.Policy) != policy {
		t.Fatalf("DescribeFileSystemPolicy() policy = %q, want %q", aws.ToString(described.Policy), policy)
	}

	if _, err := client.DeleteFileSystemPolicy(context.Background(), &efs.DeleteFileSystemPolicyInput{FileSystemId: fs.FileSystemId}); err != nil {
		t.Fatalf("DeleteFileSystemPolicy() error = %v", err)
	}
	_, err = client.DescribeFileSystemPolicy(context.Background(), &efs.DescribeFileSystemPolicyInput{FileSystemId: fs.FileSystemId})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "PolicyNotFound" {
		t.Fatalf("DescribeFileSystemPolicy(after delete) error = %v, want PolicyNotFound", err)
	}
}

func TestEFSCompatibilityAdapterManagesAccessPointsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()

	client := efs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *efs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	fs, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{CreationToken: aws.String("access-point-fs")})
	if err != nil {
		t.Fatalf("CreateFileSystem() error = %v", err)
	}
	accessPoint, err := client.CreateAccessPoint(context.Background(), &efs.CreateAccessPointInput{
		ClientToken:  aws.String("orders-ap"),
		FileSystemId: fs.FileSystemId,
		PosixUser: &types.PosixUser{
			Uid: aws.Int64(1000),
			Gid: aws.Int64(1000),
		},
		RootDirectory: &types.RootDirectory{Path: aws.String("/orders")},
		Tags: []types.Tag{{
			Key:   aws.String("Name"),
			Value: aws.String("orders"),
		}},
	})
	if err != nil {
		t.Fatalf("CreateAccessPoint() error = %v", err)
	}
	if aws.ToString(accessPoint.AccessPointId) == "" ||
		aws.ToString(accessPoint.FileSystemId) != aws.ToString(fs.FileSystemId) ||
		accessPoint.LifeCycleState != types.LifeCycleStateAvailable ||
		accessPoint.RootDirectory == nil ||
		aws.ToString(accessPoint.RootDirectory.Path) != "/orders" {
		t.Fatalf("CreateAccessPoint() = %#v, want available access point for file system", accessPoint)
	}

	byFileSystem, err := client.DescribeAccessPoints(context.Background(), &efs.DescribeAccessPointsInput{FileSystemId: fs.FileSystemId})
	if err != nil {
		t.Fatalf("DescribeAccessPoints(file system) error = %v", err)
	}
	if len(byFileSystem.AccessPoints) != 1 || aws.ToString(byFileSystem.AccessPoints[0].AccessPointId) != aws.ToString(accessPoint.AccessPointId) {
		t.Fatalf("DescribeAccessPoints(file system) = %#v, want created access point", byFileSystem.AccessPoints)
	}

	byID, err := client.DescribeAccessPoints(context.Background(), &efs.DescribeAccessPointsInput{AccessPointId: accessPoint.AccessPointId})
	if err != nil {
		t.Fatalf("DescribeAccessPoints(id) error = %v", err)
	}
	if len(byID.AccessPoints) != 1 ||
		byID.AccessPoints[0].RootDirectory == nil ||
		aws.ToString(byID.AccessPoints[0].RootDirectory.Path) != "/orders" {
		t.Fatalf("DescribeAccessPoints(id) = %#v, want root directory read-back", byID.AccessPoints)
	}

	if _, err := client.DeleteAccessPoint(context.Background(), &efs.DeleteAccessPointInput{AccessPointId: accessPoint.AccessPointId}); err != nil {
		t.Fatalf("DeleteAccessPoint() error = %v", err)
	}
	afterDelete, err := client.DescribeAccessPoints(context.Background(), &efs.DescribeAccessPointsInput{FileSystemId: fs.FileSystemId})
	if err != nil {
		t.Fatalf("DescribeAccessPoints(after delete) error = %v", err)
	}
	if len(afterDelete.AccessPoints) != 0 {
		t.Fatalf("DescribeAccessPoints(after delete) = %#v, want no access points", afterDelete.AccessPoints)
	}
}

func TestEFSCompatibilityAdapterPaginatesAccessPointsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()
	client := efs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *efs.Options) { o.BaseEndpoint = aws.String(server.URL) })
	fs, err := client.CreateFileSystem(context.Background(), &efs.CreateFileSystemInput{CreationToken: aws.String("paged-access-points")})
	if err != nil {
		t.Fatalf("CreateFileSystem() error = %v", err)
	}
	for _, token := range []string{"ap-one", "ap-two"} {
		if _, err := client.CreateAccessPoint(context.Background(), &efs.CreateAccessPointInput{ClientToken: aws.String(token), FileSystemId: fs.FileSystemId}); err != nil {
			t.Fatalf("CreateAccessPoint(%s) error = %v", token, err)
		}
	}
	first, err := client.DescribeAccessPoints(context.Background(), &efs.DescribeAccessPointsInput{FileSystemId: fs.FileSystemId, MaxResults: aws.Int32(1)})
	if err != nil || len(first.AccessPoints) != 1 || first.NextToken == nil {
		t.Fatalf("DescribeAccessPoints(first) = %#v, %v; want one access point and token", first, err)
	}
	second, err := client.DescribeAccessPoints(context.Background(), &efs.DescribeAccessPointsInput{FileSystemId: fs.FileSystemId, MaxResults: aws.Int32(1), NextToken: first.NextToken})
	if err != nil || len(second.AccessPoints) != 1 || second.NextToken != nil {
		t.Fatalf("DescribeAccessPoints(second) = %#v, %v; want final access point", second, err)
	}
}

func TestEFSCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewEFSAdapter())
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
		FileSystemID  string `json:"FileSystemId"`
		CreationToken string `json:"CreationToken"`
		LifeCycle     string `json:"LifeCycleState"`
	}
	if err := json.Unmarshal(runAWS("efs", "create-file-system", "--creation-token", "cli-orders-fs"), &created); err != nil {
		t.Fatalf("decode create-file-system output: %v", err)
	}
	if created.FileSystemID == "" || created.CreationToken != "cli-orders-fs" || created.LifeCycle != "available" {
		t.Fatalf("create-file-system = %#v, want available cli-orders-fs", created)
	}

	var described struct {
		FileSystems []struct {
			FileSystemID string `json:"FileSystemId"`
		} `json:"FileSystems"`
	}
	if err := json.Unmarshal(runAWS("efs", "describe-file-systems", "--file-system-id", created.FileSystemID), &described); err != nil {
		t.Fatalf("decode describe-file-systems output: %v", err)
	}
	if len(described.FileSystems) != 1 || described.FileSystems[0].FileSystemID != created.FileSystemID {
		t.Fatalf("describe-file-systems = %#v, want created file system", described.FileSystems)
	}

	var updated struct {
		ThroughputMode string `json:"ThroughputMode"`
	}
	if err := json.Unmarshal(runAWS("efs", "update-file-system", "--file-system-id", created.FileSystemID, "--throughput-mode", "elastic"), &updated); err != nil {
		t.Fatalf("decode update-file-system output: %v", err)
	}
	if updated.ThroughputMode != "elastic" {
		t.Fatalf("update-file-system = %#v, want elastic throughput mode", updated)
	}

	runAWS("efs", "delete-file-system", "--file-system-id", created.FileSystemID)
	if err := json.Unmarshal(runAWS("efs", "describe-file-systems"), &described); err != nil {
		t.Fatalf("decode describe-file-systems after delete output: %v", err)
	}
	if len(described.FileSystems) != 0 {
		t.Fatalf("describe-file-systems after delete = %#v, want no file systems", described.FileSystems)
	}
}

func TestEFSCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewEFSAdapter())
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
    efs = %q
  }
}

resource "aws_efs_file_system" "deploy" {
  creation_token  = "terraform-orders-fs"
  throughput_mode = "bursting"
  tags = {
    Name = "terraform-orders"
  }
}

output "file_system_id" {
  value = aws_efs_file_system.deploy.id
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

	if id := strings.TrimSpace(string(runTerraform("output", "-raw", "file_system_id"))); id == "" {
		t.Fatalf("terraform output file_system_id is empty")
	}
}

func TestEFSCompatibilityAdapterShapesAuthorizerFailure(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter(compataws.WithEFSAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
		return authz.Decision{}, errors.New("authorizer unavailable")
	}))))
	defer server.Close()
	client := efs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *efs.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.DescribeFileSystems(context.Background(), &efs.DescribeFileSystemsInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InternalServerError" {
		t.Fatalf("DescribeFileSystems(authorizer failure) error = %v, want InternalServerError", err)
	}
}

func TestEFSCompatibilityAdapterRejectsExpiredCredential(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter(compataws.WithEFSAuthorizer(authz.NewPolicyAuthorizer(
		authz.Rule{Effect: authz.Allow, Actions: []string{"elasticfilesystem:*"}, Resources: []string{"*"}},
		authz.Rule{
			Effect:    authz.Deny,
			Actions:   []string{"elasticfilesystem:DescribeFileSystems"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_expired", Values: []string{"true"}},
			},
		},
	))))
	defer server.Close()
	client := efs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *efs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Expired", "true"))
	})
	_, err := client.DescribeFileSystems(context.Background(), &efs.DescribeFileSystemsInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("DescribeFileSystems(expired credential) error = %v, want AccessDenied", err)
	}
}

func TestEFSCompatibilityAdapterRejectsMalformedCreateFileSystem(t *testing.T) {
	server := httptest.NewServer(compataws.NewEFSAdapter())
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/2015-02-01/file-systems", strings.NewReader("{"))
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
	if resp.StatusCode != http.StatusBadRequest || body["__type"] != "BadRequest" {
		t.Fatalf("malformed CreateFileSystem = status %d body %#v, want 400 BadRequest", resp.StatusCode, body)
	}
}
