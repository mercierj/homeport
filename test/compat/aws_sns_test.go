package compat_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestSNSCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	topic, err := client.CreateTopic(ctx, &sns.CreateTopicInput{
		Name: aws.String("events"),
		Tags: []snstypes.Tag{{Key: aws.String("env"), Value: aws.String("test")}},
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	assertTags := func(want map[string]string) {
		t.Helper()
		listed, err := client.ListTagsForResource(ctx, &sns.ListTagsForResourceInput{ResourceArn: topic.TopicArn})
		if err != nil {
			t.Fatalf("ListTagsForResource() error = %v", err)
		}
		got := make(map[string]string, len(listed.Tags))
		for _, tag := range listed.Tags {
			got[aws.ToString(tag.Key)] = aws.ToString(tag.Value)
		}
		if !maps.Equal(got, want) {
			t.Fatalf("ListTagsForResource() = %#v, want %#v", got, want)
		}
	}
	assertTags(map[string]string{"env": "test"})
	if _, err := client.TagResource(ctx, &sns.TagResourceInput{
		ResourceArn: topic.TopicArn,
		Tags:        []snstypes.Tag{{Key: aws.String("env"), Value: aws.String("prod")}, {Key: aws.String("owner"), Value: aws.String("platform")}},
	}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	assertTags(map[string]string{"env": "prod", "owner": "platform"})
	if _, err := client.UntagResource(ctx, &sns.UntagResourceInput{
		ResourceArn: topic.TopicArn,
		TagKeys:     []string{"owner"},
	}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	assertTags(map[string]string{"env": "prod"})

	sub, err := client.Subscribe(ctx, &sns.SubscribeInput{
		TopicArn: topic.TopicArn,
		Protocol: aws.String("https"),
		Endpoint: aws.String("https://example.test/hook"),
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if sub.SubscriptionArn == nil || *sub.SubscriptionArn == "" {
		t.Fatalf("SubscriptionArn = %v", sub.SubscriptionArn)
	}
	published, err := client.Publish(ctx, &sns.PublishInput{
		TopicArn: topic.TopicArn,
		Message:  aws.String("hello"),
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.MessageId == nil || *published.MessageId == "" {
		t.Fatalf("MessageId = %v", published.MessageId)
	}
}

func TestSNSCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewSNSAdapter())
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
		TopicARN string `json:"TopicArn"`
	}
	if err := json.Unmarshal(runAWS("sns", "create-topic", "--name", "cli-events"), &created); err != nil {
		t.Fatalf("decode create-topic output: %v", err)
	}
	if created.TopicARN == "" {
		t.Fatal("create-topic returned empty TopicArn")
	}

	var published struct {
		MessageID string `json:"MessageId"`
	}
	if err := json.Unmarshal(runAWS("sns", "publish", "--topic-arn", created.TopicARN, "--message", "payload"), &published); err != nil {
		t.Fatalf("decode publish output: %v", err)
	}
	if published.MessageID == "" {
		t.Fatal("publish returned empty MessageId")
	}

	var listed struct {
		Topics []struct {
			TopicARN string `json:"TopicArn"`
		} `json:"Topics"`
	}
	if err := json.Unmarshal(runAWS("sns", "list-topics"), &listed); err != nil {
		t.Fatalf("decode list-topics output: %v", err)
	}
	if len(listed.Topics) != 1 || listed.Topics[0].TopicARN != created.TopicARN {
		t.Fatalf("list-topics = %#v, want created topic", listed.Topics)
	}

	runAWS("sns", "delete-topic", "--topic-arn", created.TopicARN)
}

func TestSNSCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewSNSAdapter())
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
    sns = %q
  }
}

resource "aws_sns_topic" "events" {
  name         = "tf-events"
  display_name = "Events"
  tags = {
    env = "test"
  }
}

output "topic_arn" {
  value = aws_sns_topic.events.arn
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

	if topicARN := strings.TrimSpace(string(runTerraform("output", "-raw", "topic_arn"))); topicARN == "" {
		t.Fatalf("terraform output topic_arn is empty")
	}
}

func TestSNSCompatibilityAdapterDeliversPublishToHTTPSubscription(t *testing.T) {
	delivered := make(chan string, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		delivered <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer endpoint.Close()

	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	topic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	if _, err := client.Subscribe(context.Background(), &sns.SubscribeInput{
		TopicArn: topic.TopicArn,
		Protocol: aws.String("http"),
		Endpoint: aws.String(endpoint.URL),
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	if _, err := client.Publish(context.Background(), &sns.PublishInput{
		TopicArn: topic.TopicArn,
		Message:  aws.String("hello"),
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	select {
	case body := <-delivered:
		if !strings.Contains(body, "hello") {
			t.Fatalf("delivered body = %q, want hello", body)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for HTTP delivery")
	}
}

func TestSNSCompatibilityAdapterDeduplicatesPublishByMessageDeduplicationID(t *testing.T) {
	delivered := make(chan string, 2)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		delivered <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer endpoint.Close()

	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	topic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	if _, err := client.Subscribe(context.Background(), &sns.SubscribeInput{
		TopicArn: topic.TopicArn,
		Protocol: aws.String("http"),
		Endpoint: aws.String(endpoint.URL),
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	first, err := client.Publish(context.Background(), &sns.PublishInput{
		TopicArn:               topic.TopicArn,
		Message:                aws.String("hello"),
		MessageDeduplicationId: aws.String("dedup-1"),
	})
	if err != nil {
		t.Fatalf("Publish(first) error = %v", err)
	}
	second, err := client.Publish(context.Background(), &sns.PublishInput{
		TopicArn:               topic.TopicArn,
		Message:                aws.String("hello again"),
		MessageDeduplicationId: aws.String("dedup-1"),
	})
	if err != nil {
		t.Fatalf("Publish(second) error = %v", err)
	}
	if *second.MessageId != *first.MessageId {
		t.Fatalf("second MessageId = %q, want %q", *second.MessageId, *first.MessageId)
	}

	select {
	case <-delivered:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first delivery")
	}
	select {
	case body := <-delivered:
		t.Fatalf("duplicate publish delivered %q", body)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSNSCompatibilityAdapterLifecycleWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	topic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	if _, err := client.SetTopicAttributes(context.Background(), &sns.SetTopicAttributesInput{
		TopicArn:       topic.TopicArn,
		AttributeName:  aws.String("DisplayName"),
		AttributeValue: aws.String("Events"),
	}); err != nil {
		t.Fatalf("SetTopicAttributes() error = %v", err)
	}

	attrs, err := client.GetTopicAttributes(context.Background(), &sns.GetTopicAttributesInput{TopicArn: topic.TopicArn})
	if err != nil {
		t.Fatalf("GetTopicAttributes() error = %v", err)
	}
	if attrs.Attributes["DisplayName"] != "Events" {
		t.Fatalf("DisplayName = %q, want Events", attrs.Attributes["DisplayName"])
	}

	listed, err := client.ListTopics(context.Background(), &sns.ListTopicsInput{})
	if err != nil {
		t.Fatalf("ListTopics() error = %v", err)
	}
	if len(listed.Topics) != 1 || *listed.Topics[0].TopicArn != *topic.TopicArn {
		t.Fatalf("ListTopics() = %#v, want %s", listed.Topics, *topic.TopicArn)
	}
	if _, err := client.Subscribe(context.Background(), &sns.SubscribeInput{
		TopicArn: topic.TopicArn,
		Protocol: aws.String("https"),
		Endpoint: aws.String("https://example.test/events"),
	}); err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	if _, err := client.DeleteTopic(context.Background(), &sns.DeleteTopicInput{TopicArn: topic.TopicArn}); err != nil {
		t.Fatalf("DeleteTopic() error = %v", err)
	}
	if _, err := client.DeleteTopic(context.Background(), &sns.DeleteTopicInput{TopicArn: topic.TopicArn}); err != nil {
		t.Fatalf("DeleteTopic(second) error = %v, want idempotent success", err)
	}
	listed, err = client.ListTopics(context.Background(), &sns.ListTopicsInput{})
	if err != nil {
		t.Fatalf("ListTopics(after delete) error = %v", err)
	}
	if len(listed.Topics) != 0 {
		t.Fatalf("ListTopics(after delete) = %#v, want empty", listed.Topics)
	}
	subscriptions, err := client.ListSubscriptions(context.Background(), &sns.ListSubscriptionsInput{})
	if err != nil || len(subscriptions.Subscriptions) != 0 {
		t.Fatalf("ListSubscriptions(after delete) = %#v, %v; want empty", subscriptions, err)
	}
}

func TestSNSCompatibilityAdapterSubscriptionLifecycleWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	topic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	first, err := client.Subscribe(context.Background(), &sns.SubscribeInput{
		TopicArn: topic.TopicArn,
		Protocol: aws.String("https"),
		Endpoint: aws.String("https://example.test/one"),
	})
	if err != nil {
		t.Fatalf("Subscribe(first) error = %v", err)
	}
	if _, err := client.Subscribe(context.Background(), &sns.SubscribeInput{
		TopicArn: topic.TopicArn,
		Protocol: aws.String("https"),
		Endpoint: aws.String("https://example.test/two"),
	}); err != nil {
		t.Fatalf("Subscribe(second) error = %v", err)
	}

	listed, err := client.ListSubscriptionsByTopic(context.Background(), &sns.ListSubscriptionsByTopicInput{TopicArn: topic.TopicArn})
	if err != nil {
		t.Fatalf("ListSubscriptionsByTopic() error = %v", err)
	}
	if len(listed.Subscriptions) != 2 || aws.ToString(listed.Subscriptions[0].SubscriptionArn) == "" {
		t.Fatalf("ListSubscriptionsByTopic() = %#v, want two subscriptions", listed.Subscriptions)
	}

	if _, err := client.Unsubscribe(context.Background(), &sns.UnsubscribeInput{SubscriptionArn: first.SubscriptionArn}); err != nil {
		t.Fatalf("Unsubscribe() error = %v", err)
	}
	listed, err = client.ListSubscriptionsByTopic(context.Background(), &sns.ListSubscriptionsByTopicInput{TopicArn: topic.TopicArn})
	if err != nil {
		t.Fatalf("ListSubscriptionsByTopic(after unsubscribe) error = %v", err)
	}
	if len(listed.Subscriptions) != 1 || aws.ToString(listed.Subscriptions[0].Endpoint) != "https://example.test/two" {
		t.Fatalf("ListSubscriptionsByTopic(after unsubscribe) = %#v, want remaining subscription", listed.Subscriptions)
	}
}

func TestSNSCompatibilityAdapterMakesSubscriptionsIdempotent(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()
	client := sns.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sns.Options) { o.BaseEndpoint = aws.String(server.URL) })
	topic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("idempotent-subscription")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	first, err := client.Subscribe(context.Background(), &sns.SubscribeInput{TopicArn: topic.TopicArn, Protocol: aws.String("https"), Endpoint: aws.String("https://example.test/hooks")})
	if err != nil {
		t.Fatalf("Subscribe(first) error = %v", err)
	}
	second, err := client.Subscribe(context.Background(), &sns.SubscribeInput{TopicArn: topic.TopicArn, Protocol: aws.String("https"), Endpoint: aws.String("https://example.test/hooks")})
	if err != nil || aws.ToString(second.SubscriptionArn) != aws.ToString(first.SubscriptionArn) {
		t.Fatalf("Subscribe(second) = %#v, %v; want first subscription ARN", second, err)
	}
	listed, err := client.ListSubscriptionsByTopic(context.Background(), &sns.ListSubscriptionsByTopicInput{TopicArn: topic.TopicArn})
	if err != nil || len(listed.Subscriptions) != 1 {
		t.Fatalf("ListSubscriptionsByTopic() = %#v, %v; want one subscription", listed, err)
	}
}

func TestSNSCompatibilityAdapterListsSubscriptionsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	firstTopic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic(first) error = %v", err)
	}
	secondTopic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("alerts")})
	if err != nil {
		t.Fatalf("CreateTopic(second) error = %v", err)
	}
	for _, topic := range []*string{firstTopic.TopicArn, secondTopic.TopicArn} {
		if _, err := client.Subscribe(context.Background(), &sns.SubscribeInput{
			TopicArn: topic,
			Protocol: aws.String("https"),
			Endpoint: aws.String("https://example.test/" + strings.TrimPrefix(aws.ToString(topic), "arn:aws:sns:us-east-1:000000000000:")),
		}); err != nil {
			t.Fatalf("Subscribe(%s) error = %v", aws.ToString(topic), err)
		}
	}

	listed, err := client.ListSubscriptions(context.Background(), &sns.ListSubscriptionsInput{})
	if err != nil {
		t.Fatalf("ListSubscriptions() error = %v", err)
	}
	if len(listed.Subscriptions) != 2 {
		t.Fatalf("ListSubscriptions() returned %d subscriptions, want 2", len(listed.Subscriptions))
	}
	got := map[string]bool{}
	for _, sub := range listed.Subscriptions {
		got[aws.ToString(sub.TopicArn)] = true
	}
	for _, topic := range []*string{firstTopic.TopicArn, secondTopic.TopicArn} {
		if !got[aws.ToString(topic)] {
			t.Fatalf("ListSubscriptions() missing topic %s in %#v", aws.ToString(topic), listed.Subscriptions)
		}
	}
}

func TestSNSCompatibilityAdapterListTopicsPaginatesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for i := 0; i < 101; i++ {
		if _, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{
			Name: aws.String(fmt.Sprintf("events-%03d", i)),
		}); err != nil {
			t.Fatalf("CreateTopic(%d) error = %v", i, err)
		}
	}

	first, err := client.ListTopics(context.Background(), &sns.ListTopicsInput{})
	if err != nil {
		t.Fatalf("ListTopics(first) error = %v", err)
	}
	if len(first.Topics) != 100 {
		t.Fatalf("first page topics = %d, want 100", len(first.Topics))
	}
	if first.NextToken == nil || *first.NextToken == "" {
		t.Fatalf("first NextToken = %v, want token", first.NextToken)
	}

	second, err := client.ListTopics(context.Background(), &sns.ListTopicsInput{NextToken: first.NextToken})
	if err != nil {
		t.Fatalf("ListTopics(second) error = %v", err)
	}
	if len(second.Topics) != 1 {
		t.Fatalf("second page topics = %d, want 1", len(second.Topics))
	}
	if second.NextToken != nil && *second.NextToken != "" {
		t.Fatalf("second NextToken = %v, want empty", second.NextToken)
	}
}

func TestSNSCompatibilityAdapterRejectsInvalidPaginationTokens(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	topic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	for name, call := range map[string]func() error{
		"ListTopics": func() error {
			_, err := client.ListTopics(context.Background(), &sns.ListTopicsInput{NextToken: aws.String("not-a-token")})
			return err
		},
		"ListSubscriptionsByTopic": func() error {
			_, err := client.ListSubscriptionsByTopic(context.Background(), &sns.ListSubscriptionsByTopicInput{
				TopicArn:  topic.TopicArn,
				NextToken: aws.String("not-a-token"),
			})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			var apiErr smithy.APIError
			if err := call(); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameter" {
				t.Fatalf("%s invalid token error = %v, want InvalidParameter", name, err)
			}
		})
	}
}

func TestSNSCompatibilityAdapterReturnsNotFoundForMissingTopic(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.GetTopicAttributes(context.Background(), &sns.GetTopicAttributesInput{
		TopicArn: aws.String("arn:aws:sns:us-east-1:000000000000:missing"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "NotFound" {
		t.Fatalf("GetTopicAttributes() error = %v, want NotFound API error", err)
	}
}

func TestSNSCompatibilityAdapterValidatesTopicNames(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, name := range []string{"", "events topic", strings.Repeat("a", 257), "events.fifo.extra"} {
		t.Run(name, func(t *testing.T) {
			_, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String(name)})
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameter" {
				t.Fatalf("CreateTopic(%q) error = %v, want InvalidParameter API error", name, err)
			}
		})
	}
	if _, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events.fifo")}); err != nil {
		t.Fatalf("CreateTopic(valid FIFO name) error = %v", err)
	}
}

func TestSNSCompatibilityAdapterRejectsInvalidSubscriptionFields(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	topic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	_, err = client.Subscribe(context.Background(), &sns.SubscribeInput{
		TopicArn: topic.TopicArn,
		Protocol: aws.String(""),
		Endpoint: aws.String(""),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameter" {
		t.Fatalf("Subscribe(empty protocol/endpoint) error = %v, want InvalidParameter", err)
	}

	listed, err := client.ListSubscriptionsByTopic(context.Background(), &sns.ListSubscriptionsByTopicInput{TopicArn: topic.TopicArn})
	if err != nil {
		t.Fatalf("ListSubscriptionsByTopic() error = %v", err)
	}
	if len(listed.Subscriptions) != 0 {
		t.Fatalf("ListSubscriptionsByTopic() = %#v, want no invalid subscription", listed.Subscriptions)
	}
}

func TestSNSCompatibilityAdapterRejectsUnsupportedSubscriptionProtocol(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter())
	defer server.Close()
	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	topic, err := client.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	_, err = client.Subscribe(ctx, &sns.SubscribeInput{TopicArn: topic.TopicArn, Protocol: aws.String("ftp"), Endpoint: aws.String("ftp://example.test/events")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameter" {
		t.Fatalf("Subscribe(unsupported protocol) error = %v, want InvalidParameter", err)
	}
}

func TestSNSCompatibilityAdapterReturnsThrottledWhenTopicQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter(compataws.WithSNSTopicQuota(1)))
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events-1")}); err != nil {
		t.Fatalf("CreateTopic(first) error = %v", err)
	}
	_, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events-2")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "Throttled" {
		t.Fatalf("CreateTopic(second) error = %v, want Throttled API error", err)
	}
}

func TestSNSCompatibilityAdapterAuthorizesCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter(
		compataws.WithSNSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"sns:CreateTopic"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_age", Values: []string{"10m"}},
			},
		})),
	))
	defer server.Close()

	allowed := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "10m"))
	})
	if _, err := allowed.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")}); err != nil {
		t.Fatalf("CreateTopic(allowed) error = %v", err)
	}

	denied := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "48h"))
	})
	if _, err := denied.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("drafts")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateTopic(denied) error = %v, want AccessDenied", err)
	}
}

func TestSNSCompatibilityAdapterAuthorizesClaimCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter(
		compataws.WithSNSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"sns:CreateTopic"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "claim:mfa", Values: []string{"true"}},
			},
		})),
	))
	defer server.Close()

	allowed := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "true"))
	})
	if _, err := allowed.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")}); err != nil {
		t.Fatalf("CreateTopic(allowed) error = %v", err)
	}

	denied := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "false"))
	})
	if _, err := denied.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("drafts")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateTopic(denied) error = %v, want AccessDenied", err)
	}
}

func TestSNSCompatibilityAdapterAuthorizesExpiredCredentialAndPrincipalAttributeConditions(t *testing.T) {
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
			server := httptest.NewServer(compataws.NewSNSAdapter(
				compataws.WithSNSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
					Effect:     authz.Allow,
					Actions:    []string{"sns:CreateTopic"},
					Resources:  []string{"*"},
					Conditions: []authz.Condition{tc.condition},
				})),
			))
			defer server.Close()

			allowedName, allowedValue, _ := strings.Cut(tc.allowed, ":")
			allowed := sns.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *sns.Options) {
				o.BaseEndpoint = aws.String(server.URL)
				o.APIOptions = append(o.APIOptions, sqsHeader(allowedName, allowedValue))
			})
			if _, err := allowed.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String(tc.name + "-allowed")}); err != nil {
				t.Fatalf("CreateTopic(allowed %s) error = %v", tc.name, err)
			}

			deniedName, deniedValue, _ := strings.Cut(tc.denied, ":")
			denied := sns.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *sns.Options) {
				o.BaseEndpoint = aws.String(server.URL)
				o.APIOptions = append(o.APIOptions, sqsHeader(deniedName, deniedValue))
			})
			if _, err := denied.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String(tc.name + "-denied")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
				t.Fatalf("CreateTopic(denied %s) error = %v, want AccessDenied", tc.name, err)
			}
		})
	}
}

func TestSNSCompatibilityAdapterAuthorizesAndAuditsAWSSDKCalls(t *testing.T) {
	auditLog := authz.NewFileAuditLog(filepath.Join(t.TempDir(), "sns-audit.jsonl"))
	server := httptest.NewServer(compataws.NewSNSAdapter(
		compataws.WithSNSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"sns:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"sns:Publish"}, Resources: []string{"*"}},
		)),
		compataws.WithSNSAuditSink(func(decision authz.Decision) {
			if err := auditLog.Record(decision); err != nil {
				t.Errorf("Record() error = %v", err)
			}
		}),
	))
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	topic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	_, err = client.Publish(context.Background(), &sns.PublishInput{
		TopicArn: topic.TopicArn,
		Message:  aws.String("blocked"),
	})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("Publish() error = %v, want AccessDenied", err)
	}

	decisions, err := auditLog.Decisions()
	if err != nil {
		t.Fatalf("Decisions() error = %v", err)
	}
	assertDecision(t, decisions, "sns:CreateTopic", true)
	assertDecision(t, decisions, "sns:Publish", false)
}

func TestSNSCompatibilityAdapterDeniesAndAuditsTagOperationsWithoutMutation(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewSNSAdapter(
		compataws.WithSNSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Principals: []string{"homeport"}, Actions: []string{"sns:CreateTopic"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Allow, Principals: []string{"inspector"}, Actions: []string{"sns:ListTagsForResource"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Principals: []string{"homeport"}, Actions: []string{"sns:ListTagsForResource", "sns:TagResource", "sns:UntagResource"}, Resources: []string{"*"}},
		)),
		compataws.WithSNSAuditSink(auditLog.Record),
	))
	defer server.Close()

	newClient := func(principal string) *sns.Client {
		return sns.NewFromConfig(aws.Config{
			Region:      "us-east-1",
			Credentials: credentials.NewStaticCredentialsProvider(principal, "homeport", ""),
		}, func(o *sns.Options) { o.BaseEndpoint = aws.String(server.URL) })
	}
	ctx := context.Background()
	client := newClient("homeport")
	topic, err := client.CreateTopic(ctx, &sns.CreateTopicInput{
		Name: aws.String("events"),
		Tags: []snstypes.Tag{{Key: aws.String("env"), Value: aws.String("test")}},
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	for _, tc := range []struct {
		action string
		call   func() error
	}{
		{"sns:ListTagsForResource", func() error {
			_, err := client.ListTagsForResource(ctx, &sns.ListTagsForResourceInput{ResourceArn: topic.TopicArn})
			return err
		}},
		{"sns:TagResource", func() error {
			_, err := client.TagResource(ctx, &sns.TagResourceInput{ResourceArn: topic.TopicArn, Tags: []snstypes.Tag{{Key: aws.String("owner"), Value: aws.String("platform")}}})
			return err
		}},
		{"sns:UntagResource", func() error {
			_, err := client.UntagResource(ctx, &sns.UntagResourceInput{ResourceArn: topic.TopicArn, TagKeys: []string{"env"}})
			return err
		}},
	} {
		var apiErr smithy.APIError
		if err := tc.call(); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
			t.Fatalf("%s error = %v, want AccessDenied", tc.action, err)
		}
		assertDecision(t, auditLog.Decisions(), tc.action, false)
		for _, decision := range auditLog.Decisions() {
			if decision.Request.Action == tc.action && !decision.Allowed && decision.Request.Resource != aws.ToString(topic.TopicArn) {
				t.Fatalf("%s audit resource = %q, want %q", tc.action, decision.Request.Resource, aws.ToString(topic.TopicArn))
			}
		}
	}

	listed, err := newClient("inspector").ListTagsForResource(ctx, &sns.ListTagsForResourceInput{ResourceArn: topic.TopicArn})
	if err != nil {
		t.Fatalf("ListTagsForResource(inspector) error = %v", err)
	}
	if len(listed.Tags) != 1 || aws.ToString(listed.Tags[0].Key) != "env" || aws.ToString(listed.Tags[0].Value) != "test" {
		t.Fatalf("tags after denied operations = %#v, want env=test", listed.Tags)
	}
}

func TestSNSCompatibilityAdapterAuthorizesUnsubscribeSubscriptionResource(t *testing.T) {
	server := httptest.NewServer(compataws.NewSNSAdapter(
		compataws.WithSNSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"sns:CreateTopic", "sns:Subscribe", "sns:ListSubscriptionsByTopic"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"sns:Unsubscribe"},
				Resources: []string{"arn:aws:sns:us-east-1:000000000000:events:*"},
			},
		)),
	))
	defer server.Close()

	client := sns.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	topic, err := client.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	sub, err := client.Subscribe(ctx, &sns.SubscribeInput{
		TopicArn: topic.TopicArn,
		Protocol: aws.String("http"),
		Endpoint: aws.String("http://example.com/hook"),
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	if _, err := client.Unsubscribe(ctx, &sns.UnsubscribeInput{SubscriptionArn: sub.SubscriptionArn}); err != nil {
		t.Fatalf("Unsubscribe(subscription resource) error = %v", err)
	}
	listed, err := client.ListSubscriptionsByTopic(ctx, &sns.ListSubscriptionsByTopicInput{TopicArn: topic.TopicArn})
	if err != nil {
		t.Fatalf("ListSubscriptionsByTopic() error = %v", err)
	}
	if len(listed.Subscriptions) != 0 {
		t.Fatalf("ListSubscriptionsByTopic() = %#v, want no subscriptions", listed.Subscriptions)
	}
}
