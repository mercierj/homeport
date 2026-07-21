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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestCloudWatchLogsCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String("/homeport/app"),
	}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
	}); err != nil {
		t.Fatalf("CreateLogStream() error = %v", err)
	}
	put, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
		LogEvents: []types.InputLogEvent{
			{Message: aws.String("started"), Timestamp: aws.Int64(time.Now().UnixMilli())},
		},
	})
	if err != nil {
		t.Fatalf("PutLogEvents() error = %v", err)
	}
	if put.NextSequenceToken == nil || *put.NextSequenceToken == "" {
		t.Fatalf("NextSequenceToken = %v", put.NextSequenceToken)
	}

	streams, err := client.DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String("/homeport/app"),
	})
	if err != nil {
		t.Fatalf("DescribeLogStreams() error = %v", err)
	}
	if len(streams.LogStreams) != 1 || *streams.LogStreams[0].LogStreamName != "web" {
		t.Fatalf("DescribeLogStreams() = %#v", streams.LogStreams)
	}
}

func TestCloudWatchLogsCompatibilityAdapterRoundTripsTagsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	group := aws.String("/homeport/tags")
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: group, Tags: map[string]string{"env": "test"}}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	list := func() map[string]string {
		t.Helper()
		out, err := client.ListTagsLogGroup(ctx, &cloudwatchlogs.ListTagsLogGroupInput{LogGroupName: group})
		if err != nil {
			t.Fatalf("ListTagsLogGroup() error = %v", err)
		}
		return out.Tags
	}

	if tags := list(); len(tags) != 1 || tags["env"] != "test" {
		t.Fatalf("initial tags = %#v, want env=test", tags)
	}
	if _, err := client.TagLogGroup(ctx, &cloudwatchlogs.TagLogGroupInput{LogGroupName: group, Tags: map[string]string{"env": "prod", "owner": "platform"}}); err != nil {
		t.Fatalf("TagLogGroup() error = %v", err)
	}
	if tags := list(); len(tags) != 2 || tags["env"] != "prod" || tags["owner"] != "platform" {
		t.Fatalf("tags after tag = %#v, want env=prod and owner=platform", tags)
	}
	if _, err := client.UntagLogGroup(ctx, &cloudwatchlogs.UntagLogGroupInput{LogGroupName: group, Tags: []string{"env"}}); err != nil {
		t.Fatalf("UntagLogGroup() error = %v", err)
	}
	if tags := list(); len(tags) != 1 || tags["owner"] != "platform" {
		t.Fatalf("tags after untag = %#v, want owner=platform", tags)
	}
}

func TestCloudWatchLogsCompatibilityAdapterRoundTripsResourceTagsWithAWSSDK(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter(
		compataws.WithCloudWatchLogsAuthorizer(authz.AuthorizerFunc(func(_ context.Context, req authz.Request) (authz.Decision, error) {
			return authz.Decision{Request: req, Allowed: true}, nil
		})),
		compataws.WithCloudWatchLogsAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	group := aws.String("/homeport/resource-tags")
	resourceARN := aws.String("arn:aws:logs:us-east-1:homeport:log-group:/homeport/resource-tags")
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: group, Tags: map[string]string{"env": "test"}}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	list := func() map[string]string {
		t.Helper()
		out, err := client.ListTagsForResource(ctx, &cloudwatchlogs.ListTagsForResourceInput{ResourceArn: resourceARN})
		if err != nil {
			t.Fatalf("ListTagsForResource() error = %v", err)
		}
		return out.Tags
	}

	if tags := list(); len(tags) != 1 || tags["env"] != "test" {
		t.Fatalf("initial tags = %#v, want env=test", tags)
	}
	if _, err := client.TagResource(ctx, &cloudwatchlogs.TagResourceInput{ResourceArn: resourceARN, Tags: map[string]string{"env": "prod", "owner": "platform"}}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	if tags := list(); len(tags) != 2 || tags["env"] != "prod" || tags["owner"] != "platform" {
		t.Fatalf("tags after tag = %#v, want env=prod and owner=platform", tags)
	}
	if _, err := client.UntagResource(ctx, &cloudwatchlogs.UntagResourceInput{ResourceArn: resourceARN, TagKeys: []string{"env"}}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	if tags := list(); len(tags) != 1 || tags["owner"] != "platform" {
		t.Fatalf("tags after untag = %#v, want owner=platform", tags)
	}

	for _, action := range []string{"logs:ListTagsForResource", "logs:TagResource", "logs:UntagResource"} {
		found := false
		for _, decision := range auditLog.Decisions() {
			if decision.Request.Action == action && decision.Request.Resource == *resourceARN {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("audit decisions missing allowed %s on %s: %#v", action, *resourceARN, auditLog.Decisions())
		}
	}
}

func TestCloudWatchLogsCompatibilityAdapterRejectsTooManyResourceTagsWithoutMutation(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	resourceARN := aws.String("arn:aws:logs:us-east-1:homeport:log-group:/homeport/tag-limit")
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String("/homeport/tag-limit"),
		Tags:         map[string]string{"env": "test"},
	}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}

	tags := map[string]string{"env": "prod"}
	for i := range 49 {
		tags["tag-"+strconv.Itoa(i)] = "value"
	}
	if _, err := client.TagResource(ctx, &cloudwatchlogs.TagResourceInput{ResourceArn: resourceARN, Tags: tags}); err != nil {
		t.Fatalf("TagResource(50-tag union) error = %v", err)
	}
	tags["overflow"] = "blocked"
	_, err := client.TagResource(ctx, &cloudwatchlogs.TagResourceInput{ResourceArn: resourceARN, Tags: tags})
	var tooMany *types.TooManyTagsException
	if err == nil || !errors.As(err, &tooMany) {
		t.Fatalf("TagResource(51-tag union) error = %v, want TooManyTagsException", err)
	}

	got, err := client.ListTagsForResource(ctx, &cloudwatchlogs.ListTagsForResourceInput{ResourceArn: resourceARN})
	if err != nil {
		t.Fatalf("ListTagsForResource() error = %v", err)
	}
	if len(got.Tags) != 50 || got.Tags["env"] != "prod" || got.Tags["overflow"] != "" {
		t.Fatalf("tags after rejected mutation = %#v, want unchanged 50-tag set", got.Tags)
	}
}

func TestCloudWatchLogsCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
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

	runAWS("logs", "create-log-group", "--log-group-name", "/homeport/cli")
	runAWS("logs", "create-log-stream", "--log-group-name", "/homeport/cli", "--log-stream-name", "web")

	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	var put struct {
		NextSequenceToken string `json:"nextSequenceToken"`
	}
	if err := json.Unmarshal(runAWS("logs", "put-log-events",
		"--log-group-name", "/homeport/cli",
		"--log-stream-name", "web",
		"--log-events", "timestamp="+timestamp+",message=started",
	), &put); err != nil {
		t.Fatalf("decode put-log-events output: %v", err)
	}
	if put.NextSequenceToken == "" {
		t.Fatal("put-log-events returned empty nextSequenceToken")
	}

	var got struct {
		Events []struct {
			Message string `json:"message"`
		} `json:"events"`
	}
	if err := json.Unmarshal(runAWS("logs", "get-log-events",
		"--log-group-name", "/homeport/cli",
		"--log-stream-name", "web",
	), &got); err != nil {
		t.Fatalf("decode get-log-events output: %v", err)
	}
	if len(got.Events) != 1 || got.Events[0].Message != "started" {
		t.Fatalf("get-log-events = %#v, want started event", got.Events)
	}

	runAWS("logs", "delete-log-group", "--log-group-name", "/homeport/cli")
}

func TestCloudWatchLogsCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
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
    cloudwatchlogs = %q
  }
}

resource "aws_cloudwatch_log_group" "app" {
  name              = "/homeport/tf"
  retention_in_days = 7
  tags = {
    env = "test"
  }
}

output "log_group_name" {
  value = aws_cloudwatch_log_group.app.name
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

	if name := strings.TrimSpace(string(runTerraform("output", "-raw", "log_group_name"))); name != "/homeport/tf" {
		t.Fatalf("terraform output log_group_name = %q, want /homeport/tf", name)
	}
}

func TestCloudWatchLogsCompatibilityAdapterGetsLogEvents(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
	}); err != nil {
		t.Fatalf("CreateLogStream() error = %v", err)
	}
	if _, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
		LogEvents: []types.InputLogEvent{
			{Message: aws.String("started"), Timestamp: aws.Int64(time.Now().UnixMilli())},
			{Message: aws.String("ready"), Timestamp: aws.Int64(time.Now().UnixMilli())},
		},
	}); err != nil {
		t.Fatalf("PutLogEvents() error = %v", err)
	}

	events, err := client.GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
	})
	if err != nil {
		t.Fatalf("GetLogEvents() error = %v", err)
	}
	if len(events.Events) != 2 || aws.ToString(events.Events[0].Message) != "started" || aws.ToString(events.Events[1].Message) != "ready" {
		t.Fatalf("GetLogEvents() = %#v, want stored messages", events.Events)
	}
}

func TestCloudWatchLogsCompatibilityAdapterPaginatesGetLogEvents(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
	}); err != nil {
		t.Fatalf("CreateLogStream() error = %v", err)
	}
	if _, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
		LogEvents: []types.InputLogEvent{
			{Message: aws.String("started"), Timestamp: aws.Int64(time.Now().UnixMilli())},
			{Message: aws.String("ready"), Timestamp: aws.Int64(time.Now().UnixMilli())},
		},
	}); err != nil {
		t.Fatalf("PutLogEvents() error = %v", err)
	}

	first, err := client.GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
		Limit:         aws.Int32(1),
	})
	if err != nil {
		t.Fatalf("GetLogEvents(first) error = %v", err)
	}
	if len(first.Events) != 1 || aws.ToString(first.Events[0].Message) != "started" || first.NextForwardToken == nil {
		t.Fatalf("GetLogEvents(first) = %#v, want first event and token", first)
	}

	second, err := client.GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
		Limit:         aws.Int32(1),
		NextToken:     first.NextForwardToken,
	})
	if err != nil {
		t.Fatalf("GetLogEvents(second) error = %v", err)
	}
	if len(second.Events) != 1 || aws.ToString(second.Events[0].Message) != "ready" || second.NextForwardToken != nil {
		t.Fatalf("GetLogEvents(second) = %#v, want final event without token", second)
	}

	_, err = client.GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
		NextToken:     aws.String("not-a-token"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("GetLogEvents(invalid token) error = %v, want InvalidParameterException", err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterDeletesLogStreamAndGroup(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	for _, name := range []string{"api", "worker"} {
		if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
			LogGroupName:  aws.String("/homeport/app"),
			LogStreamName: aws.String(name),
		}); err != nil {
			t.Fatalf("CreateLogStream(%s) error = %v", name, err)
		}
	}

	if _, err := client.DeleteLogStream(ctx, &cloudwatchlogs.DeleteLogStreamInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("api"),
	}); err != nil {
		t.Fatalf("DeleteLogStream() error = %v", err)
	}
	streams, err := client.DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{LogGroupName: aws.String("/homeport/app")})
	if err != nil {
		t.Fatalf("DescribeLogStreams() error = %v", err)
	}
	if len(streams.LogStreams) != 1 || aws.ToString(streams.LogStreams[0].LogStreamName) != "worker" {
		t.Fatalf("DescribeLogStreams() = %#v, want only worker", streams.LogStreams)
	}

	if _, err := client.DeleteLogGroup(ctx, &cloudwatchlogs.DeleteLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("DeleteLogGroup() error = %v", err)
	}
	groups, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{LogGroupNamePrefix: aws.String("/homeport/")})
	if err != nil {
		t.Fatalf("DescribeLogGroups() error = %v", err)
	}
	if len(groups.LogGroups) != 0 {
		t.Fatalf("DescribeLogGroups() = %#v, want no groups", groups.LogGroups)
	}
}

func TestCloudWatchLogsCompatibilityAdapterPaginatesDescribeLogStreams(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	for _, name := range []string{"api", "worker"} {
		if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
			LogGroupName:  aws.String("/homeport/app"),
			LogStreamName: aws.String(name),
		}); err != nil {
			t.Fatalf("CreateLogStream(%s) error = %v", name, err)
		}
	}

	first, err := client.DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String("/homeport/app"),
		Limit:        aws.Int32(1),
	})
	if err != nil {
		t.Fatalf("DescribeLogStreams(first) error = %v", err)
	}
	if len(first.LogStreams) != 1 || aws.ToString(first.LogStreams[0].LogStreamName) != "api" || first.NextToken == nil {
		t.Fatalf("DescribeLogStreams(first) = %#v, want first stream and token", first)
	}

	second, err := client.DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String("/homeport/app"),
		Limit:        aws.Int32(1),
		NextToken:    first.NextToken,
	})
	if err != nil {
		t.Fatalf("DescribeLogStreams(second) error = %v", err)
	}
	if len(second.LogStreams) != 1 || aws.ToString(second.LogStreams[0].LogStreamName) != "worker" || second.NextToken != nil {
		t.Fatalf("DescribeLogStreams(second) = %#v, want final stream without token", second)
	}
}

func TestCloudWatchLogsCompatibilityAdapterPaginatesDescribeLogGroups(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	for _, name := range []string{"/homeport/api", "/homeport/worker"} {
		if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String(name)}); err != nil {
			t.Fatalf("CreateLogGroup(%s) error = %v", name, err)
		}
	}

	first, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{Limit: aws.Int32(1)})
	if err != nil {
		t.Fatalf("DescribeLogGroups(first) error = %v", err)
	}
	if len(first.LogGroups) != 1 || aws.ToString(first.LogGroups[0].LogGroupName) != "/homeport/api" || first.NextToken == nil {
		t.Fatalf("DescribeLogGroups(first) = %#v, want first group and token", first)
	}

	second, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
		Limit:     aws.Int32(1),
		NextToken: first.NextToken,
	})
	if err != nil {
		t.Fatalf("DescribeLogGroups(second) error = %v", err)
	}
	if len(second.LogGroups) != 1 || aws.ToString(second.LogGroups[0].LogGroupName) != "/homeport/worker" || second.NextToken != nil {
		t.Fatalf("DescribeLogGroups(second) = %#v, want final group without token", second)
	}
}

func TestCloudWatchLogsCompatibilityAdapterRejectsInvalidDescribeLogGroupsToken(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}

	_, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
		NextToken: aws.String("not-a-token"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("DescribeLogGroups(invalid token) error = %v, want InvalidParameterException", err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterReturnsResourceNotFoundForMissingResources(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{
			name: "create stream missing group",
			call: func() error {
				_, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
					LogGroupName:  aws.String("/homeport/missing"),
					LogStreamName: aws.String("web"),
				})
				return err
			},
		},
		{
			name: "put retention missing group",
			call: func() error {
				_, err := client.PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
					LogGroupName:    aws.String("/homeport/missing"),
					RetentionInDays: aws.Int32(7),
				})
				return err
			},
		},
		{
			name: "put events missing stream",
			call: func() error {
				_, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
					LogGroupName:  aws.String("/homeport/app"),
					LogStreamName: aws.String("missing"),
					LogEvents:     []types.InputLogEvent{{Message: aws.String("miss"), Timestamp: aws.Int64(time.Now().UnixMilli())}},
				})
				return err
			},
		},
		{
			name: "describe streams missing group",
			call: func() error {
				_, err := client.DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
					LogGroupName: aws.String("/homeport/missing"),
				})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var apiErr smithy.APIError
			if err := tc.call(); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
				t.Fatalf("%s error = %v, want ResourceNotFoundException", tc.name, err)
			}
		})
	}
}

func TestCloudWatchLogsCompatibilityAdapterReturnsResourceAlreadyExistsForDuplicateCreates(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup(first) error = %v", err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
	}); err != nil {
		t.Fatalf("CreateLogStream(first) error = %v", err)
	}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{
			name: "duplicate group",
			call: func() error {
				_, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")})
				return err
			},
		},
		{
			name: "duplicate stream",
			call: func() error {
				_, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
					LogGroupName:  aws.String("/homeport/app"),
					LogStreamName: aws.String("web"),
				})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var apiErr smithy.APIError
			if err := tc.call(); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceAlreadyExistsException" {
				t.Fatalf("%s error = %v, want ResourceAlreadyExistsException", tc.name, err)
			}
		})
	}
}

func TestCloudWatchLogsCompatibilityAdapterRejectsMissingRequiredNames(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup(valid) error = %v", err)
	}

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{
			name: "create group empty name",
			call: func() error {
				_, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("")})
				return err
			},
		},
		{
			name: "create stream empty stream",
			call: func() error {
				_, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
					LogGroupName:  aws.String("/homeport/app"),
					LogStreamName: aws.String(""),
				})
				return err
			},
		},
		{
			name: "put retention empty group",
			call: func() error {
				_, err := client.PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
					LogGroupName:    aws.String(""),
					RetentionInDays: aws.Int32(7),
				})
				return err
			},
		},
		{
			name: "put events empty stream",
			call: func() error {
				_, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
					LogGroupName:  aws.String("/homeport/app"),
					LogStreamName: aws.String(""),
					LogEvents:     []types.InputLogEvent{{Message: aws.String("invalid"), Timestamp: aws.Int64(time.Now().UnixMilli())}},
				})
				return err
			},
		},
		{
			name: "describe streams empty group",
			call: func() error {
				_, err := client.DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{LogGroupName: aws.String("")})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var apiErr smithy.APIError
			if err := tc.call(); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
				t.Fatalf("%s error = %v, want InvalidParameterException", tc.name, err)
			}
		})
	}
}

func TestCloudWatchLogsCompatibilityAdapterRejectsInvalidDescribeLogStreamsToken(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}

	_, err := client.DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName: aws.String("/homeport/app"),
		NextToken:    aws.String("not-a-token"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("DescribeLogStreams(invalid token) error = %v, want InvalidParameterException", err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterRejectsInvalidPaginationLimits(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
	}); err != nil {
		t.Fatalf("CreateLogStream() error = %v", err)
	}

	cases := map[string]func() error{
		"DescribeLogGroups": func() error {
			_, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{Limit: aws.Int32(51)})
			return err
		},
		"DescribeLogStreams": func() error {
			_, err := client.DescribeLogStreams(ctx, &cloudwatchlogs.DescribeLogStreamsInput{
				LogGroupName: aws.String("/homeport/app"),
				Limit:        aws.Int32(51),
			})
			return err
		},
		"GetLogEvents": func() error {
			_, err := client.GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{
				LogGroupName:  aws.String("/homeport/app"),
				LogStreamName: aws.String("web"),
				Limit:         aws.Int32(10001),
			})
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			var apiErr smithy.APIError
			if err := call(); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
				t.Fatalf("%s invalid limit error = %v, want InvalidParameterException", name, err)
			}
		})
	}
}

func TestCloudWatchLogsCompatibilityAdapterReturnsLimitExceededWhenGroupQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter(compataws.WithCloudWatchLogsGroupQuota(1)))
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/one")}); err != nil {
		t.Fatalf("CreateLogGroup(first) error = %v", err)
	}
	_, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/two")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("CreateLogGroup(over quota) error = %v, want LimitExceededException", err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterStoresRetentionPolicy(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    aws.String("/homeport/app"),
		RetentionInDays: aws.Int32(7),
	}); err != nil {
		t.Fatalf("PutRetentionPolicy() error = %v", err)
	}

	groups, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
		LogGroupNamePrefix: aws.String("/homeport/"),
	})
	if err != nil {
		t.Fatalf("DescribeLogGroups() error = %v", err)
	}
	if len(groups.LogGroups) != 1 || aws.ToInt32(groups.LogGroups[0].RetentionInDays) != 7 {
		t.Fatalf("DescribeLogGroups() = %#v, want retention 7", groups.LogGroups)
	}
}

func TestCloudWatchLogsCompatibilityAdapterDeletesRetentionPolicy(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    aws.String("/homeport/app"),
		RetentionInDays: aws.Int32(7),
	}); err != nil {
		t.Fatalf("PutRetentionPolicy() error = %v", err)
	}
	if _, err := client.DeleteRetentionPolicy(ctx, &cloudwatchlogs.DeleteRetentionPolicyInput{
		LogGroupName: aws.String("/homeport/app"),
	}); err != nil {
		t.Fatalf("DeleteRetentionPolicy() error = %v", err)
	}

	groups, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{
		LogGroupNamePrefix: aws.String("/homeport/"),
	})
	if err != nil {
		t.Fatalf("DescribeLogGroups() error = %v", err)
	}
	if len(groups.LogGroups) != 1 || groups.LogGroups[0].RetentionInDays != nil {
		t.Fatalf("DescribeLogGroups() = %#v, want no retention", groups.LogGroups)
	}
}

func TestCloudWatchLogsCompatibilityAdapterRejectsInvalidRetentionPolicy(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}

	_, err := client.PutRetentionPolicy(ctx, &cloudwatchlogs.PutRetentionPolicyInput{
		LogGroupName:    aws.String("/homeport/app"),
		RetentionInDays: aws.Int32(2),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("PutRetentionPolicy(invalid retention) error = %v, want InvalidParameterException", err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterIgnoresStaleSequenceToken(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
	}); err != nil {
		t.Fatalf("CreateLogStream() error = %v", err)
	}

	first, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
		LogEvents: []types.InputLogEvent{
			{Message: aws.String("first"), Timestamp: aws.Int64(time.Now().UnixMilli())},
		},
	})
	if err != nil {
		t.Fatalf("PutLogEvents(first) error = %v", err)
	}
	second, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
		SequenceToken: first.NextSequenceToken,
		LogEvents:     []types.InputLogEvent{{Message: aws.String("second"), Timestamp: aws.Int64(time.Now().UnixMilli())}},
	})
	if err != nil {
		t.Fatalf("PutLogEvents(second) error = %v", err)
	}
	if aws.ToString(second.NextSequenceToken) == aws.ToString(first.NextSequenceToken) {
		t.Fatalf("second NextSequenceToken = %q, want advanced token", aws.ToString(second.NextSequenceToken))
	}

	stale, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
		SequenceToken: first.NextSequenceToken,
		LogEvents:     []types.InputLogEvent{{Message: aws.String("stale"), Timestamp: aws.Int64(time.Now().UnixMilli())}},
	})
	if err != nil {
		t.Fatalf("PutLogEvents(stale token) error = %v, want accepted request", err)
	}
	if aws.ToString(stale.NextSequenceToken) == "" {
		t.Fatal("PutLogEvents(stale token) returned an empty next sequence token")
	}
}

func TestCloudWatchLogsCompatibilityAdapterRejectsEmptyLogEventBatch(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()
	client := cloudwatchlogs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *cloudwatchlogs.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{LogGroupName: aws.String("/homeport/app"), LogStreamName: aws.String("web")}); err != nil {
		t.Fatalf("CreateLogStream() error = %v", err)
	}
	_, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{LogGroupName: aws.String("/homeport/app"), LogStreamName: aws.String("web"), LogEvents: []types.InputLogEvent{}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("PutLogEvents(empty batch) error = %v, want InvalidParameterException", err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterValidatesLogEventShapeAndOrdering(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter())
	defer server.Close()
	client := cloudwatchlogs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *cloudwatchlogs.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{LogGroupName: aws.String("/homeport/app"), LogStreamName: aws.String("web")}); err != nil {
		t.Fatalf("CreateLogStream() error = %v", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(`{"logGroupName":"/homeport/app","logStreamName":"web","logEvents":[{"message":"missing timestamp"}]}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/x-amz-json-1.1")
	request.Header.Set("X-Amz-Target", "Logs_20140328.PutLogEvents")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.StatusCode != http.StatusBadRequest || body["__type"] != "InvalidParameterException" {
		t.Fatalf("PutLogEvents(missing timestamp) = status %d body %#v, want InvalidParameterException", response.StatusCode, body)
	}
	overflowRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(`{"logGroupName":"/homeport/app","logStreamName":"web","logEvents":[{"message":"overflow","timestamp":9223372036854775808}]}`))
	if err != nil {
		t.Fatalf("NewRequest(overflow) error = %v", err)
	}
	overflowRequest.Header.Set("Content-Type", "application/x-amz-json-1.1")
	overflowRequest.Header.Set("X-Amz-Target", "Logs_20140328.PutLogEvents")
	overflowResponse, err := http.DefaultClient.Do(overflowRequest)
	if err != nil {
		t.Fatalf("Do(overflow) error = %v", err)
	}
	defer overflowResponse.Body.Close()
	var overflowBody map[string]any
	if err := json.NewDecoder(overflowResponse.Body).Decode(&overflowBody); err != nil {
		t.Fatalf("Decode(overflow) error = %v", err)
	}
	if overflowResponse.StatusCode != http.StatusBadRequest || overflowBody["__type"] != "InvalidParameterException" {
		t.Fatalf("PutLogEvents(overflow timestamp) = status %d body %#v, want InvalidParameterException", overflowResponse.StatusCode, overflowBody)
	}
	_, err = client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{LogGroupName: aws.String("/homeport/app"), LogStreamName: aws.String("web"), LogEvents: []types.InputLogEvent{
		{Message: aws.String("later"), Timestamp: aws.Int64(2)},
		{Message: aws.String("earlier"), Timestamp: aws.Int64(1)},
	}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterException" {
		t.Fatalf("PutLogEvents(out of order) error = %v, want InvalidParameterException", err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterRejectsOutOfWindowEventsButKeepsValidSiblings(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter(compataws.WithCloudWatchLogsClock(func() time.Time { return now })))
	defer server.Close()
	client := cloudwatchlogs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *cloudwatchlogs.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{LogGroupName: aws.String("/homeport/app"), LogStreamName: aws.String("web")}); err != nil {
		t.Fatalf("CreateLogStream() error = %v", err)
	}
	old, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{LogGroupName: aws.String("/homeport/app"), LogStreamName: aws.String("web"), LogEvents: []types.InputLogEvent{{Message: aws.String("too old"), Timestamp: aws.Int64(now.Add(-14*24*time.Hour - time.Millisecond).UnixMilli())}}})
	if err != nil {
		t.Fatalf("PutLogEvents(too old) error = %v", err)
	}
	if old.RejectedLogEventsInfo == nil || aws.ToInt32(old.RejectedLogEventsInfo.TooOldLogEventEndIndex) != 1 {
		t.Fatalf("PutLogEvents(too old) rejected events = %#v, want exclusive old index 1", old.RejectedLogEventsInfo)
	}
	result, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{LogGroupName: aws.String("/homeport/app"), LogStreamName: aws.String("web"), LogEvents: []types.InputLogEvent{
		{Message: aws.String("valid"), Timestamp: aws.Int64(now.UnixMilli())},
		{Message: aws.String("too new"), Timestamp: aws.Int64(now.Add(2*time.Hour + time.Millisecond).UnixMilli())},
	}})
	if err != nil {
		t.Fatalf("PutLogEvents(valid and too new) error = %v", err)
	}
	if result.RejectedLogEventsInfo == nil || aws.ToInt32(result.RejectedLogEventsInfo.TooNewLogEventStartIndex) != 1 {
		t.Fatalf("PutLogEvents(valid and too new) rejected events = %#v, want new index 1", result.RejectedLogEventsInfo)
	}
	read, err := client.GetLogEvents(ctx, &cloudwatchlogs.GetLogEventsInput{LogGroupName: aws.String("/homeport/app"), LogStreamName: aws.String("web")})
	if err != nil || len(read.Events) != 1 || aws.ToString(read.Events[0].Message) != "valid" {
		t.Fatalf("GetLogEvents() = %#v, %v; want only valid event", read.Events, err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterAuthorizesCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter(
		compataws.WithCloudWatchLogsAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"logs:CreateLogGroup"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_age", Values: []string{"10m"}},
			},
		})),
	))
	defer server.Close()

	allowed := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "10m"))
	})
	if _, err := allowed.CreateLogGroup(context.Background(), &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/allowed")}); err != nil {
		t.Fatalf("CreateLogGroup(allowed) error = %v", err)
	}

	denied := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "48h"))
	})
	if _, err := denied.CreateLogGroup(context.Background(), &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/denied")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateLogGroup(denied) error = %v, want AccessDenied", err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterAuthorizesClaimCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter(
		compataws.WithCloudWatchLogsAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"logs:CreateLogGroup"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "claim:mfa", Values: []string{"true"}},
			},
		})),
	))
	defer server.Close()

	allowed := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "true"))
	})
	if _, err := allowed.CreateLogGroup(context.Background(), &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/allowed")}); err != nil {
		t.Fatalf("CreateLogGroup(allowed) error = %v", err)
	}

	denied := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "false"))
	})
	if _, err := denied.CreateLogGroup(context.Background(), &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/denied")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateLogGroup(denied) error = %v, want AccessDenied", err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterAuthorizesExpiredCredentialAndPrincipalAttributeConditions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		condition authz.Condition
		allowed   string
		denied    string
	}{
		{
			name:      "expired-credential",
			condition: authz.Condition{Key: "credential_expired", Values: []string{"false"}},
			allowed:   "X-Homeport-Credential-Expired:false",
			denied:    "X-Homeport-Credential-Expired:true",
		},
		{
			name:      "principal-attribute",
			condition: authz.Condition{Key: "principal:department", Values: []string{"finance"}},
			allowed:   "X-Homeport-Principal-Attribute-Department:finance",
			denied:    "X-Homeport-Principal-Attribute-Department:engineering",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter(
				compataws.WithCloudWatchLogsAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
					Effect:     authz.Allow,
					Actions:    []string{"logs:CreateLogGroup"},
					Resources:  []string{"*"},
					Conditions: []authz.Condition{tc.condition},
				})),
			))
			defer server.Close()

			allowedName, allowedValue, _ := strings.Cut(tc.allowed, ":")
			allowed := cloudwatchlogs.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *cloudwatchlogs.Options) {
				o.BaseEndpoint = aws.String(server.URL)
				o.APIOptions = append(o.APIOptions, sqsHeader(allowedName, allowedValue))
			})
			if _, err := allowed.CreateLogGroup(context.Background(), &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/" + tc.name + "/allowed")}); err != nil {
				t.Fatalf("CreateLogGroup(allowed %s) error = %v", tc.name, err)
			}

			deniedName, deniedValue, _ := strings.Cut(tc.denied, ":")
			denied := cloudwatchlogs.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *cloudwatchlogs.Options) {
				o.BaseEndpoint = aws.String(server.URL)
				o.APIOptions = append(o.APIOptions, sqsHeader(deniedName, deniedValue))
			})
			if _, err := denied.CreateLogGroup(context.Background(), &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String("/homeport/" + tc.name + "/denied")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
				t.Fatalf("CreateLogGroup(denied %s) error = %v, want AccessDenied", tc.name, err)
			}
		})
	}
}

func TestCloudWatchLogsCompatibilityAdapterAuthorizesAndAuditsAWSSDKCalls(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter(
		compataws.WithCloudWatchLogsAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"logs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"logs:PutLogEvents"}, Resources: []string{"*"}},
		)),
		compataws.WithCloudWatchLogsAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String("/homeport/app"),
	}); err != nil {
		t.Fatalf("CreateLogGroup() error = %v", err)
	}
	if _, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
	}); err != nil {
		t.Fatalf("CreateLogStream() error = %v", err)
	}

	_, err := client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogGroupName:  aws.String("/homeport/app"),
		LogStreamName: aws.String("web"),
		LogEvents: []types.InputLogEvent{
			{Message: aws.String("blocked"), Timestamp: aws.Int64(time.Now().UnixMilli())},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("PutLogEvents() error = %v, want AccessDenied", err)
	}

	assertDecision(t, auditLog.Decisions(), "logs:CreateLogGroup", true)
	assertDecision(t, auditLog.Decisions(), "logs:PutLogEvents", false)
}

func TestCloudWatchLogsCompatibilityAdapterAuthorizesDescribeLogGroupsResourcePrefix(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter(
		compataws.WithCloudWatchLogsAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"logs:CreateLogGroup"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"logs:DescribeLogGroups"},
				Resources: []string{"arn:aws:logs:us-east-1:homeport:log-group:/homeport/app*"},
			},
		)),
	))
	defer server.Close()

	client := cloudwatchlogs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *cloudwatchlogs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	for _, group := range []string{"/homeport/app", "/homeport/other"} {
		if _, err := client.CreateLogGroup(ctx, &cloudwatchlogs.CreateLogGroupInput{LogGroupName: aws.String(group)}); err != nil {
			t.Fatalf("CreateLogGroup(%s) error = %v", group, err)
		}
	}

	if _, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{LogGroupNamePrefix: aws.String("/homeport/app")}); err != nil {
		t.Fatalf("DescribeLogGroups(allowed prefix) error = %v", err)
	}
	_, err := client.DescribeLogGroups(ctx, &cloudwatchlogs.DescribeLogGroupsInput{LogGroupNamePrefix: aws.String("/homeport/other")})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("DescribeLogGroups(denied prefix) error = %v, want AccessDenied", err)
	}
}

func TestCloudWatchLogsCompatibilityAdapterShapesAuthorizerFailure(t *testing.T) {
	server := httptest.NewServer(compataws.NewCloudWatchLogsAdapter(compataws.WithCloudWatchLogsAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
		return authz.Decision{}, errors.New("authorizer unavailable")
	}))))
	defer server.Close()
	client := cloudwatchlogs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *cloudwatchlogs.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.DescribeLogGroups(context.Background(), &cloudwatchlogs.DescribeLogGroupsInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ServiceUnavailableException" {
		t.Fatalf("DescribeLogGroups(authorizer failure) error = %v, want ServiceUnavailableException", err)
	}
}
