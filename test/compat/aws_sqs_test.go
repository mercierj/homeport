package compat_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestSQSCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameVisibilityTimeout): "5",
			string(types.QueueAttributeNameDelaySeconds):      "0",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	url, err := client.GetQueueUrl(context.Background(), &sqs.GetQueueUrlInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("GetQueueUrl() error = %v", err)
	}
	if *url.QueueUrl != *created.QueueUrl {
		t.Fatalf("QueueUrl = %q, want %q", *url.QueueUrl, *created.QueueUrl)
	}

	if _, err := client.SetQueueAttributes(context.Background(), &sqs.SetQueueAttributesInput{
		QueueUrl: url.QueueUrl,
		Attributes: map[string]string{
			string(types.QueueAttributeNameMessageRetentionPeriod): "1209600",
		},
	}); err != nil {
		t.Fatalf("SetQueueAttributes() error = %v", err)
	}

	attrs, err := client.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl:       url.QueueUrl,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll},
	})
	if err != nil {
		t.Fatalf("GetQueueAttributes() error = %v", err)
	}
	if attrs.Attributes[string(types.QueueAttributeNameVisibilityTimeout)] != "5" {
		t.Fatalf("VisibilityTimeout = %q, want 5", attrs.Attributes[string(types.QueueAttributeNameVisibilityTimeout)])
	}

	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:     url.QueueUrl,
		MessageBody:  aws.String("payload"),
		DelaySeconds: 0,
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            url.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   1,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 || *received.Messages[0].Body != "payload" {
		t.Fatalf("ReceiveMessage() = %#v, want payload", received.Messages)
	}
	receipt := received.Messages[0].ReceiptHandle

	if _, err := client.ChangeMessageVisibility(context.Background(), &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          url.QueueUrl,
		ReceiptHandle:     receipt,
		VisibilityTimeout: 0,
	}); err != nil {
		t.Fatalf("ChangeMessageVisibility() error = %v", err)
	}

	receivedAgain, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            url.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after visibility change) error = %v", err)
	}
	if len(receivedAgain.Messages) != 1 || *receivedAgain.Messages[0].Body != "payload" {
		t.Fatalf("ReceiveMessage(after visibility change) = %#v, want payload", receivedAgain.Messages)
	}
	receipt = receivedAgain.Messages[0].ReceiptHandle

	if _, err := client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
		QueueUrl:      url.QueueUrl,
		ReceiptHandle: receipt,
	}); err != nil {
		t.Fatalf("DeleteMessage() error = %v", err)
	}

	time.Sleep(1100 * time.Millisecond)
	empty, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            url.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(empty) error = %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Fatalf("ReceiveMessage(empty) returned %d messages", len(empty.Messages))
	}
}

func TestSQSCompatibilityAdapterCreateQueueIsIdempotentForMatchingAttributes(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	first, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameVisibilityTimeout): "5",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue(first) error = %v", err)
	}
	second, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameVisibilityTimeout): "5",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue(same attributes) error = %v", err)
	}
	if aws.ToString(second.QueueUrl) != aws.ToString(first.QueueUrl) {
		t.Fatalf("CreateQueue(same attributes) URL = %q, want %q", aws.ToString(second.QueueUrl), aws.ToString(first.QueueUrl))
	}

	_, err = client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameVisibilityTimeout): "10",
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "QueueNameExists" {
		t.Fatalf("CreateQueue(conflicting attributes) error = %v, want QueueNameExists", err)
	}

	attrs, err := client.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl: first.QueueUrl,
		AttributeNames: []types.QueueAttributeName{
			types.QueueAttributeNameVisibilityTimeout,
		},
	})
	if err != nil {
		t.Fatalf("GetQueueAttributes() error = %v", err)
	}
	if attrs.Attributes[string(types.QueueAttributeNameVisibilityTimeout)] != "5" {
		t.Fatalf("VisibilityTimeout = %q, want original value 5", attrs.Attributes[string(types.QueueAttributeNameVisibilityTimeout)])
	}
	if len(attrs.Attributes) != 1 {
		t.Fatalf("GetQueueAttributes(VisibilityTimeout) returned %#v, want only requested attribute", attrs.Attributes)
	}

	_, err = client.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl:       first.QueueUrl,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeName("NotAnAttribute")},
	})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidAttributeName" {
		t.Fatalf("GetQueueAttributes(invalid attribute) error = %v, want InvalidAttributeName", err)
	}
}

func TestSQSCompatibilityAdapterReplaysIdempotentSendMessage(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Idempotency-Key", "send-message"))
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	first, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("first"),
	})
	if err != nil {
		t.Fatalf("SendMessage(first) error = %v", err)
	}
	second, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("second"),
	})
	if err != nil {
		t.Fatalf("SendMessage(second) error = %v", err)
	}
	if aws.ToString(second.MessageId) != aws.ToString(first.MessageId) {
		t.Fatalf("SendMessage(second) MessageId = %q, want replayed %q", aws.ToString(second.MessageId), aws.ToString(first.MessageId))
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(first) error = %v", err)
	}
	if len(received.Messages) != 1 || aws.ToString(received.Messages[0].Body) != "first" {
		t.Fatalf("ReceiveMessage(first) = %#v, want only first message", received.Messages)
	}
	if _, err := client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
		QueueUrl:      created.QueueUrl,
		ReceiptHandle: received.Messages[0].ReceiptHandle,
	}); err != nil {
		t.Fatalf("DeleteMessage(first) error = %v", err)
	}
	empty, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(empty) error = %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Fatalf("ReceiveMessage(empty) = %#v, want no replay duplicate", empty.Messages)
	}
}

func TestSQSCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewSQSAdapter())
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
		QueueURL string `json:"QueueUrl"`
	}
	if err := json.Unmarshal(runAWS("sqs", "create-queue", "--queue-name", "cli-jobs"), &created); err != nil {
		t.Fatalf("decode create-queue output: %v", err)
	}
	if created.QueueURL == "" {
		t.Fatal("create-queue returned empty QueueUrl")
	}

	runAWS("sqs", "send-message", "--queue-url", created.QueueURL, "--message-body", "payload")

	var received struct {
		Messages []struct {
			Body string `json:"Body"`
		} `json:"Messages"`
	}
	if err := json.Unmarshal(runAWS("sqs", "receive-message", "--queue-url", created.QueueURL, "--max-number-of-messages", "1"), &received); err != nil {
		t.Fatalf("decode receive-message output: %v", err)
	}
	if len(received.Messages) != 1 || received.Messages[0].Body != "payload" {
		t.Fatalf("receive-message = %#v, want one payload message", received.Messages)
	}

	runAWS("sqs", "delete-queue", "--queue-url", created.QueueURL)
}

func TestSQSCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewSQSAdapter())
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
    sqs = %q
  }
}

resource "aws_sqs_queue" "jobs" {
  name                       = "tf-jobs"
  visibility_timeout_seconds = 5
  tags = {
    env = "test"
  }
}

output "queue_url" {
  value = aws_sqs_queue.jobs.url
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

	if queueURL := strings.TrimSpace(string(runTerraform("output", "-raw", "queue_url"))); queueURL == "" {
		t.Fatalf("terraform output queue_url is empty")
	}
}

func TestSQSCompatibilityAdapterReceivesRequestedMessageBatch(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for _, body := range []string{"first", "second", "third"} {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:    created.QueueUrl,
			MessageBody: aws.String(body),
		}); err != nil {
			t.Fatalf("SendMessage(%s) error = %v", body, err)
		}
	}

	firstBatch, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(first batch) error = %v", err)
	}
	if len(firstBatch.Messages) != 2 {
		t.Fatalf("ReceiveMessage(first batch) returned %d messages, want 2", len(firstBatch.Messages))
	}
	if aws.ToString(firstBatch.Messages[0].Body) != "first" || aws.ToString(firstBatch.Messages[1].Body) != "second" {
		t.Fatalf("ReceiveMessage(first batch) = %#v, want first and second", firstBatch.Messages)
	}

	secondBatch, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(second batch) error = %v", err)
	}
	if len(secondBatch.Messages) != 1 || aws.ToString(secondBatch.Messages[0].Body) != "third" {
		t.Fatalf("ReceiveMessage(second batch) = %#v, want remaining third message", secondBatch.Messages)
	}
}

func TestSQSCompatibilityAdapterSendsMessageBatch(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	sent, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{Id: aws.String("first"), MessageBody: aws.String("one")},
			{Id: aws.String("second"), MessageBody: aws.String("two")},
		},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(sent.Failed) != 0 || len(sent.Successful) != 2 {
		t.Fatalf("SendMessageBatch() = successful %#v failed %#v, want 2 successful", sent.Successful, sent.Failed)
	}
	if aws.ToString(sent.Successful[0].Id) != "first" || aws.ToString(sent.Successful[1].Id) != "second" {
		t.Fatalf("SendMessageBatch() successful entries = %#v, want matching entry IDs", sent.Successful)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 2 ||
		aws.ToString(received.Messages[0].Body) != "one" ||
		aws.ToString(received.Messages[1].Body) != "two" {
		t.Fatalf("ReceiveMessage() = %#v, want batched messages in order", received.Messages)
	}
}

func TestSQSCompatibilityAdapterReturnsFailedFIFOMessageBatchEntries(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	sent, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{Id: aws.String("missing-group"), MessageBody: aws.String("one"), MessageDeduplicationId: aws.String("dedup-a")},
			{Id: aws.String("missing-dedup"), MessageBody: aws.String("two"), MessageGroupId: aws.String("group-a")},
			{Id: aws.String("entry-delay"), MessageBody: aws.String("three"), MessageGroupId: aws.String("group-a"), MessageDeduplicationId: aws.String("dedup-b"), DelaySeconds: 1},
		},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(sent.Successful) != 0 || len(sent.Failed) != 3 {
		t.Fatalf("SendMessageBatch() = successful %#v failed %#v, want 3 failed entries", sent.Successful, sent.Failed)
	}
	for _, failed := range sent.Failed {
		if aws.ToString(failed.Code) != "InvalidParameterValue" || !failed.SenderFault {
			t.Fatalf("SendMessageBatch() failed entry = %#v, want sender InvalidParameterValue", failed)
		}
	}

	time.Sleep(1100 * time.Millisecond)
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 3,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage() = %#v, want no invalid FIFO batch messages enqueued", received.Messages)
	}
}

func TestSQSCompatibilityAdapterDeduplicatesFIFOMessageBatchID(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	sent, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{Id: aws.String("first"), MessageBody: aws.String("one"), MessageGroupId: aws.String("group-a"), MessageDeduplicationId: aws.String("dedup-a")},
			{Id: aws.String("duplicate"), MessageBody: aws.String("two"), MessageGroupId: aws.String("group-a"), MessageDeduplicationId: aws.String("dedup-a")},
		},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(sent.Failed) != 0 || len(sent.Successful) != 2 {
		t.Fatalf("SendMessageBatch() = successful %#v failed %#v, want 2 accepted entries", sent.Successful, sent.Failed)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 || aws.ToString(received.Messages[0].Body) != "one" {
		t.Fatalf("ReceiveMessage() = %#v, want only first FIFO batch message delivered", received.Messages)
	}
}

func TestSQSCompatibilityAdapterDeletesMessageBatch(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for _, body := range []string{"one", "two"} {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:    created.QueueUrl,
			MessageBody: aws.String(body),
		}); err != nil {
			t.Fatalf("SendMessage(%s) error = %v", body, err)
		}
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   60,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 2 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 2", len(received.Messages))
	}
	deleted, err := client.DeleteMessageBatch(context.Background(), &sqs.DeleteMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.DeleteMessageBatchRequestEntry{
			{Id: aws.String("first"), ReceiptHandle: received.Messages[0].ReceiptHandle},
			{Id: aws.String("second"), ReceiptHandle: received.Messages[1].ReceiptHandle},
		},
	})
	if err != nil {
		t.Fatalf("DeleteMessageBatch() error = %v", err)
	}
	if len(deleted.Failed) != 0 || len(deleted.Successful) != 2 {
		t.Fatalf("DeleteMessageBatch() = successful %#v failed %#v, want 2 successful", deleted.Successful, deleted.Failed)
	}
	if aws.ToString(deleted.Successful[0].Id) != "first" || aws.ToString(deleted.Successful[1].Id) != "second" {
		t.Fatalf("DeleteMessageBatch() successful entries = %#v, want matching entry IDs", deleted.Successful)
	}

	empty, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after delete batch) error = %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Fatalf("ReceiveMessage(after delete batch) = %#v, want no messages", empty.Messages)
	}
}

func TestSQSCompatibilityAdapterChangesMessageVisibilityBatch(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for _, body := range []string{"one", "two"} {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:    created.QueueUrl,
			MessageBody: aws.String(body),
		}); err != nil {
			t.Fatalf("SendMessage(%s) error = %v", body, err)
		}
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   60,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 2 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 2", len(received.Messages))
	}
	changed, err := client.ChangeMessageVisibilityBatch(context.Background(), &sqs.ChangeMessageVisibilityBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.ChangeMessageVisibilityBatchRequestEntry{
			{Id: aws.String("first"), ReceiptHandle: received.Messages[0].ReceiptHandle, VisibilityTimeout: 0},
			{Id: aws.String("second"), ReceiptHandle: received.Messages[1].ReceiptHandle, VisibilityTimeout: 0},
		},
	})
	if err != nil {
		t.Fatalf("ChangeMessageVisibilityBatch() error = %v", err)
	}
	if len(changed.Failed) != 0 || len(changed.Successful) != 2 {
		t.Fatalf("ChangeMessageVisibilityBatch() = successful %#v failed %#v, want 2 successful", changed.Successful, changed.Failed)
	}

	visible, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after visibility batch) error = %v", err)
	}
	if len(visible.Messages) != 2 ||
		aws.ToString(visible.Messages[0].Body) != "one" ||
		aws.ToString(visible.Messages[1].Body) != "two" {
		t.Fatalf("ReceiveMessage(after visibility batch) = %#v, want both messages visible", visible.Messages)
	}
}

func TestSQSCompatibilityAdapterRejectsDuplicateBatchEntryIDs(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	_, err = client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{Id: aws.String("same"), MessageBody: aws.String("one")},
			{Id: aws.String("same"), MessageBody: aws.String("two")},
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "BatchEntryIdsNotDistinct" {
		t.Fatalf("SendMessageBatch(duplicate IDs) error = %v, want BatchEntryIdsNotDistinct", err)
	}
	empty, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after duplicate send batch) error = %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Fatalf("ReceiveMessage(after duplicate send batch) = %#v, want no messages", empty.Messages)
	}

	for _, body := range []string{"delete-one", "delete-two"} {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:    created.QueueUrl,
			MessageBody: aws.String(body),
		}); err != nil {
			t.Fatalf("SendMessage(%s) error = %v", body, err)
		}
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   60,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(delete setup) error = %v", err)
	}
	if len(received.Messages) != 2 {
		t.Fatalf("ReceiveMessage(delete setup) returned %d messages, want 2", len(received.Messages))
	}
	_, err = client.DeleteMessageBatch(context.Background(), &sqs.DeleteMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.DeleteMessageBatchRequestEntry{
			{Id: aws.String("same"), ReceiptHandle: received.Messages[0].ReceiptHandle},
			{Id: aws.String("same"), ReceiptHandle: received.Messages[1].ReceiptHandle},
		},
	})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "BatchEntryIdsNotDistinct" {
		t.Fatalf("DeleteMessageBatch(duplicate IDs) error = %v, want BatchEntryIdsNotDistinct", err)
	}
	for _, msg := range received.Messages {
		if _, err := client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
			QueueUrl:      created.QueueUrl,
			ReceiptHandle: msg.ReceiptHandle,
		}); err != nil {
			t.Fatalf("DeleteMessage(after duplicate delete batch) error = %v", err)
		}
	}

	for _, body := range []string{"visibility-one", "visibility-two"} {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:    created.QueueUrl,
			MessageBody: aws.String(body),
		}); err != nil {
			t.Fatalf("SendMessage(%s) error = %v", body, err)
		}
	}
	received, err = client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   60,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(visibility setup) error = %v", err)
	}
	if len(received.Messages) != 2 {
		t.Fatalf("ReceiveMessage(visibility setup) returned %d messages, want 2", len(received.Messages))
	}
	_, err = client.ChangeMessageVisibilityBatch(context.Background(), &sqs.ChangeMessageVisibilityBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.ChangeMessageVisibilityBatchRequestEntry{
			{Id: aws.String("same"), ReceiptHandle: received.Messages[0].ReceiptHandle, VisibilityTimeout: 0},
			{Id: aws.String("same"), ReceiptHandle: received.Messages[1].ReceiptHandle, VisibilityTimeout: 0},
		},
	})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "BatchEntryIdsNotDistinct" {
		t.Fatalf("ChangeMessageVisibilityBatch(duplicate IDs) error = %v, want BatchEntryIdsNotDistinct", err)
	}
	empty, err = client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after duplicate visibility batch) error = %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Fatalf("ReceiveMessage(after duplicate visibility batch) = %#v, want messages still inflight", empty.Messages)
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidBatchEntryIDs(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	_, err = client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{Id: aws.String("bad id"), MessageBody: aws.String("one")},
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidBatchEntryId" {
		t.Fatalf("SendMessageBatch(invalid ID) error = %v, want InvalidBatchEntryId", err)
	}
	empty, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after invalid send batch) error = %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Fatalf("ReceiveMessage(after invalid send batch) = %#v, want no messages", empty.Messages)
	}

	for _, body := range []string{"delete-one", "delete-two"} {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:    created.QueueUrl,
			MessageBody: aws.String(body),
		}); err != nil {
			t.Fatalf("SendMessage(%s) error = %v", body, err)
		}
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   60,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(delete setup) error = %v", err)
	}
	if len(received.Messages) != 2 {
		t.Fatalf("ReceiveMessage(delete setup) returned %d messages, want 2", len(received.Messages))
	}
	_, err = client.DeleteMessageBatch(context.Background(), &sqs.DeleteMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.DeleteMessageBatchRequestEntry{
			{Id: aws.String("bad id"), ReceiptHandle: received.Messages[0].ReceiptHandle},
		},
	})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidBatchEntryId" {
		t.Fatalf("DeleteMessageBatch(invalid ID) error = %v, want InvalidBatchEntryId", err)
	}
	for _, msg := range received.Messages {
		if _, err := client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
			QueueUrl:      created.QueueUrl,
			ReceiptHandle: msg.ReceiptHandle,
		}); err != nil {
			t.Fatalf("DeleteMessage(after invalid delete batch) error = %v", err)
		}
	}

	for _, body := range []string{"visibility-one", "visibility-two"} {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:    created.QueueUrl,
			MessageBody: aws.String(body),
		}); err != nil {
			t.Fatalf("SendMessage(%s) error = %v", body, err)
		}
	}
	received, err = client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   60,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(visibility setup) error = %v", err)
	}
	if len(received.Messages) != 2 {
		t.Fatalf("ReceiveMessage(visibility setup) returned %d messages, want 2", len(received.Messages))
	}
	_, err = client.ChangeMessageVisibilityBatch(context.Background(), &sqs.ChangeMessageVisibilityBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.ChangeMessageVisibilityBatchRequestEntry{
			{Id: aws.String("bad id"), ReceiptHandle: received.Messages[0].ReceiptHandle, VisibilityTimeout: 0},
		},
	})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidBatchEntryId" {
		t.Fatalf("ChangeMessageVisibilityBatch(invalid ID) error = %v, want InvalidBatchEntryId", err)
	}
	empty, err = client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after invalid visibility batch) error = %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Fatalf("ReceiveMessage(after invalid visibility batch) = %#v, want messages still inflight", empty.Messages)
	}
}

func TestSQSCompatibilityAdapterRejectsSendMessageBatchTooLong(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	_, err = client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{Id: aws.String("first"), MessageBody: aws.String(strings.Repeat("x", 600000))},
			{Id: aws.String("second"), MessageBody: aws.String(strings.Repeat("y", 600000))},
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "BatchRequestTooLong" {
		t.Fatalf("SendMessageBatch(too long) error = %v, want BatchRequestTooLong", err)
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage() = %#v, want no messages from rejected batch", received.Messages)
	}
}

func TestSQSCompatibilityAdapterCountsAttributesInBatchRequestSize(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("batch-size")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	attributes := map[string]types.MessageAttributeValue{
		"kind": {DataType: aws.String("String"), StringValue: aws.String("event")},
	}
	_, err = client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{Id: aws.String("first"), MessageBody: aws.String(strings.Repeat("x", 524280)), MessageAttributes: attributes},
			{Id: aws.String("second"), MessageBody: aws.String(strings.Repeat("y", 524280)), MessageAttributes: attributes},
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "BatchRequestTooLong" {
		t.Fatalf("SendMessageBatch(attribute oversized) error = %v, want BatchRequestTooLong", err)
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidReceiveMessageMaxNumber(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("payload"),
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	_, err = client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 11,
		WaitTimeSeconds:     0,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("ReceiveMessage(invalid MaxNumberOfMessages) error = %v, want InvalidParameterValue", err)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after invalid max) error = %v", err)
	}
	if len(received.Messages) != 1 || aws.ToString(received.Messages[0].Body) != "payload" {
		t.Fatalf("ReceiveMessage(after invalid max) = %#v, want queued payload", received.Messages)
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidReceiveMessageWaitTime(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("payload"),
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	_, err = client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     21,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("ReceiveMessage(invalid WaitTimeSeconds) error = %v, want InvalidParameterValue", err)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after invalid wait) error = %v", err)
	}
	if len(received.Messages) != 1 || aws.ToString(received.Messages[0].Body) != "payload" {
		t.Fatalf("ReceiveMessage(after invalid wait) = %#v, want queued payload", received.Messages)
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidReceiveMessageVisibilityTimeout(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("payload"),
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}

	_, err = client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		VisibilityTimeout:   43201,
		WaitTimeSeconds:     0,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("ReceiveMessage(invalid VisibilityTimeout) error = %v, want InvalidParameterValue", err)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after invalid visibility) error = %v", err)
	}
	if len(received.Messages) != 1 || aws.ToString(received.Messages[0].Body) != "payload" {
		t.Fatalf("ReceiveMessage(after invalid visibility) = %#v, want queued payload", received.Messages)
	}
}

func TestSQSCompatibilityAdapterReturnsQueueDoesNotExistForMissingQueues(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	missingURL := aws.String(server.URL + "/missing")

	for _, tc := range []struct {
		name string
		call func(context.Context) error
	}{
		{
			name: "GetQueueUrl",
			call: func(ctx context.Context) error {
				_, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String("missing")})
				return err
			},
		},
		{
			name: "SetQueueAttributes",
			call: func(ctx context.Context) error {
				_, err := client.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
					QueueUrl:   missingURL,
					Attributes: map[string]string{string(types.QueueAttributeNameVisibilityTimeout): "5"},
				})
				return err
			},
		},
		{
			name: "GetQueueAttributes",
			call: func(ctx context.Context) error {
				_, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
					QueueUrl:       missingURL,
					AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll},
				})
				return err
			},
		},
		{
			name: "ListQueueTags",
			call: func(ctx context.Context) error {
				_, err := client.ListQueueTags(ctx, &sqs.ListQueueTagsInput{QueueUrl: missingURL})
				return err
			},
		},
		{
			name: "TagQueue",
			call: func(ctx context.Context) error {
				_, err := client.TagQueue(ctx, &sqs.TagQueueInput{QueueUrl: missingURL, Tags: map[string]string{"env": "dev"}})
				return err
			},
		},
		{
			name: "UntagQueue",
			call: func(ctx context.Context) error {
				_, err := client.UntagQueue(ctx, &sqs.UntagQueueInput{QueueUrl: missingURL, TagKeys: []string{"env"}})
				return err
			},
		},
		{
			name: "SendMessage",
			call: func(ctx context.Context) error {
				_, err := client.SendMessage(ctx, &sqs.SendMessageInput{QueueUrl: missingURL, MessageBody: aws.String("payload")})
				return err
			},
		},
		{
			name: "ReceiveMessage",
			call: func(ctx context.Context) error {
				_, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: missingURL})
				return err
			},
		},
		{
			name: "DeleteMessage",
			call: func(ctx context.Context) error {
				_, err := client.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: missingURL, ReceiptHandle: aws.String("receipt")})
				return err
			},
		},
		{
			name: "DeleteQueue",
			call: func(ctx context.Context) error {
				_, err := client.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: missingURL})
				return err
			},
		},
		{
			name: "ChangeMessageVisibility",
			call: func(ctx context.Context) error {
				_, err := client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
					QueueUrl:          missingURL,
					ReceiptHandle:     aws.String("receipt"),
					VisibilityTimeout: 1,
				})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(context.Background())
			var missing *types.QueueDoesNotExist
			if err == nil || !errors.As(err, &missing) {
				t.Fatalf("%s() error = %v, want QueueDoesNotExist", tc.name, err)
			}
		})
	}
}

func TestSQSCompatibilityAdapterReturnsReceiptHandleIsInvalid(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for _, tc := range []struct {
		name string
		call func(context.Context) error
	}{
		{
			name: "DeleteMessage",
			call: func(ctx context.Context) error {
				_, err := client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      created.QueueUrl,
					ReceiptHandle: aws.String("not-a-receipt"),
				})
				return err
			},
		},
		{
			name: "ChangeMessageVisibility",
			call: func(ctx context.Context) error {
				_, err := client.ChangeMessageVisibility(ctx, &sqs.ChangeMessageVisibilityInput{
					QueueUrl:          created.QueueUrl,
					ReceiptHandle:     aws.String("not-a-receipt"),
					VisibilityTimeout: 1,
				})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(context.Background())
			var invalid *types.ReceiptHandleIsInvalid
			if err == nil || !errors.As(err, &invalid) {
				t.Fatalf("%s() error = %v, want ReceiptHandleIsInvalid", tc.name, err)
			}
		})
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidChangeMessageVisibilityTimeout(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("payload"),
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 {
		t.Fatalf("ReceiveMessage() = %#v, want one message", received.Messages)
	}
	receipt := received.Messages[0].ReceiptHandle

	_, err = client.ChangeMessageVisibility(context.Background(), &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          created.QueueUrl,
		ReceiptHandle:     receipt,
		VisibilityTimeout: 43201,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("ChangeMessageVisibility(invalid timeout) error = %v, want InvalidParameterValue", err)
	}
	if _, err := client.DeleteMessage(context.Background(), &sqs.DeleteMessageInput{
		QueueUrl:      created.QueueUrl,
		ReceiptHandle: receipt,
	}); err != nil {
		t.Fatalf("DeleteMessage(after invalid visibility) error = %v", err)
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidAttributeValues(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("invalid-create"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameVisibilityTimeout): "43201",
		},
	})
	var invalid *types.InvalidAttributeValue
	if err == nil || !errors.As(err, &invalid) {
		t.Fatalf("CreateQueue(invalid attribute) error = %v, want InvalidAttributeValue", err)
	}
	if _, err := client.GetQueueUrl(context.Background(), &sqs.GetQueueUrlInput{QueueName: aws.String("invalid-create")}); err == nil {
		t.Fatal("GetQueueUrl(invalid-create) error = nil, want missing queue")
	}

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameVisibilityTimeout): "5",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue(valid) error = %v", err)
	}
	_, err = client.SetQueueAttributes(context.Background(), &sqs.SetQueueAttributesInput{
		QueueUrl: created.QueueUrl,
		Attributes: map[string]string{
			string(types.QueueAttributeNameVisibilityTimeout): "43201",
		},
	})
	if err == nil || !errors.As(err, &invalid) {
		t.Fatalf("SetQueueAttributes(invalid attribute) error = %v, want InvalidAttributeValue", err)
	}
	attrs, err := client.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl:       created.QueueUrl,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameVisibilityTimeout},
	})
	if err != nil {
		t.Fatalf("GetQueueAttributes() error = %v", err)
	}
	if attrs.Attributes[string(types.QueueAttributeNameVisibilityTimeout)] != "5" {
		t.Fatalf("VisibilityTimeout = %q, want original value 5", attrs.Attributes[string(types.QueueAttributeNameVisibilityTimeout)])
	}
}

func TestSQSCompatibilityAdapterRejectsAdditionalInvalidAttributeValues(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	cases := []struct {
		name  string
		attr  types.QueueAttributeName
		value string
	}{
		{name: "maximum message size", attr: types.QueueAttributeNameMaximumMessageSize, value: "1023"},
		{name: "receive wait time", attr: types.QueueAttributeNameReceiveMessageWaitTimeSeconds, value: "21"},
		{name: "kms data key reuse", attr: types.QueueAttributeNameKmsDataKeyReusePeriodSeconds, value: "59"},
	}

	for _, tc := range cases {
		t.Run("CreateQueue/"+tc.name, func(t *testing.T) {
			queueName := strings.ReplaceAll(tc.name, " ", "-")
			_, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
				QueueName: aws.String(queueName),
				Attributes: map[string]string{
					string(tc.attr): tc.value,
				},
			})
			var invalid *types.InvalidAttributeValue
			if err == nil || !errors.As(err, &invalid) {
				t.Fatalf("CreateQueue(%s=%s) error = %v, want InvalidAttributeValue", tc.attr, tc.value, err)
			}
			if _, err := client.GetQueueUrl(context.Background(), &sqs.GetQueueUrlInput{QueueName: aws.String(queueName)}); err == nil {
				t.Fatalf("GetQueueUrl(%s) error = nil, want missing queue", queueName)
			}
		})
	}

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue(valid) error = %v", err)
	}
	for _, tc := range cases {
		t.Run("SetQueueAttributes/"+tc.name, func(t *testing.T) {
			_, err := client.SetQueueAttributes(context.Background(), &sqs.SetQueueAttributesInput{
				QueueUrl: created.QueueUrl,
				Attributes: map[string]string{
					string(tc.attr): tc.value,
				},
			})
			var invalid *types.InvalidAttributeValue
			if err == nil || !errors.As(err, &invalid) {
				t.Fatalf("SetQueueAttributes(%s=%s) error = %v, want InvalidAttributeValue", tc.attr, tc.value, err)
			}
			attrs, err := client.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
				QueueUrl:       created.QueueUrl,
				AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll},
			})
			if err != nil {
				t.Fatalf("GetQueueAttributes() error = %v", err)
			}
			if _, ok := attrs.Attributes[string(tc.attr)]; ok {
				t.Fatalf("GetQueueAttributes(All) returned invalid %s: %#v", tc.attr, attrs.Attributes)
			}
		})
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidAttributeNames(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("invalid-create"),
		Attributes: map[string]string{
			"NotAnAttribute": "x",
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidAttributeName" {
		t.Fatalf("CreateQueue(invalid attribute name) error = %v, want InvalidAttributeName", err)
	}
	if _, err := client.GetQueueUrl(context.Background(), &sqs.GetQueueUrlInput{QueueName: aws.String("invalid-create")}); err == nil {
		t.Fatal("GetQueueUrl(invalid-create) error = nil, want missing queue")
	}

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameVisibilityTimeout): "5",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue(valid) error = %v", err)
	}
	_, err = client.SetQueueAttributes(context.Background(), &sqs.SetQueueAttributesInput{
		QueueUrl: created.QueueUrl,
		Attributes: map[string]string{
			"NotAnAttribute": "x",
		},
	})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidAttributeName" {
		t.Fatalf("SetQueueAttributes(invalid attribute name) error = %v, want InvalidAttributeName", err)
	}
	attrs, err := client.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl:       created.QueueUrl,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll},
	})
	if err != nil {
		t.Fatalf("GetQueueAttributes() error = %v", err)
	}
	if _, ok := attrs.Attributes["NotAnAttribute"]; ok {
		t.Fatalf("GetQueueAttributes(All) returned invalid attribute name: %#v", attrs.Attributes)
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidMessageContents(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "invalid character", body: "bad\x00body"},
		{name: "empty", body: ""},
		{name: "oversized", body: strings.Repeat("x", 1048577)},
	} {
		_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:    created.QueueUrl,
			MessageBody: aws.String(tc.body),
		})
		var invalid *types.InvalidMessageContents
		if err == nil || !errors.As(err, &invalid) {
			t.Fatalf("SendMessage(%s body) error = %v, want InvalidMessageContents", tc.name, err)
		}
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage() = %#v, want no invalid message enqueued", received.Messages)
	}
}

func TestSQSCompatibilityAdapterRejectsMessagesAboveQueueMaximumSize(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameMaximumMessageSize): "1024",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String(strings.Repeat("x", 1025)),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("SendMessage(over queue MaximumMessageSize) error = %v, want InvalidParameterValue", err)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage() = %#v, want no oversized message enqueued", received.Messages)
	}
}

func TestSQSCompatibilityAdapterCountsAttributesAgainstQueueMaximumMessageSize(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("attribute-queue-size"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameMaximumMessageSize): "1024",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String(strings.Repeat("x", 1024)),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"kind": {DataType: aws.String("String"), StringValue: aws.String("event")},
		},
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("SendMessage(attributes exceed queue maximum) error = %v, want InvalidParameterValue", err)
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidQueueNames(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, tc := range []struct {
		name       string
		queueName  string
		attributes map[string]string
	}{
		{
			name:      "fifo without suffix",
			queueName: "jobs",
			attributes: map[string]string{
				string(types.QueueAttributeNameFifoQueue): "true",
			},
		},
		{name: "standard with dot", queueName: "jobs.fifo"},
		{name: "too long", queueName: strings.Repeat("x", 81)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
				QueueName:  aws.String(tc.queueName),
				Attributes: tc.attributes,
			})
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
				t.Fatalf("CreateQueue(%s) error = %v, want InvalidParameterValue", tc.name, err)
			}

			_, err = client.GetQueueUrl(context.Background(), &sqs.GetQueueUrlInput{QueueName: aws.String(tc.queueName)})
			if err == nil {
				t.Fatalf("GetQueueUrl(%s) error = nil, want rejected queue absent", tc.name)
			}
		})
	}
}

func TestSQSCompatibilityAdapterRejectsFIFOMessageWithoutGroupID(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("payload"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "MissingParameter" {
		t.Fatalf("SendMessage(FIFO without MessageGroupId) error = %v, want MissingParameter", err)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage() = %#v, want no FIFO message enqueued without group ID", received.Messages)
	}
}

func TestSQSCompatibilityAdapterRejectsFIFOMessageDelaySeconds(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:               created.QueueUrl,
		MessageBody:            aws.String("payload"),
		MessageGroupId:         aws.String("group-a"),
		MessageDeduplicationId: aws.String("dedup-a"),
		DelaySeconds:           1,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("SendMessage(FIFO DelaySeconds) error = %v, want InvalidParameterValue", err)
	}

	time.Sleep(1100 * time.Millisecond)
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage() = %#v, want no FIFO message enqueued with per-message delay", received.Messages)
	}
}

func TestSQSCompatibilityAdapterRejectsFIFOMessageWithoutDeduplicationID(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:       created.QueueUrl,
		MessageBody:    aws.String("payload"),
		MessageGroupId: aws.String("group-a"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("SendMessage(FIFO without MessageDeduplicationId) error = %v, want InvalidParameterValue", err)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage() = %#v, want no FIFO message enqueued without deduplication ID", received.Messages)
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidFIFOMessageIDs(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, tc := range []struct {
		name                   string
		messageGroupID         string
		messageDeduplicationID string
	}{
		{name: "group id too long", messageGroupID: strings.Repeat("g", 129), messageDeduplicationID: "dedup-a"},
		{name: "deduplication id too long", messageGroupID: "group-a", messageDeduplicationID: strings.Repeat("d", 129)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
				QueueName: aws.String(strings.ReplaceAll(tc.name, " ", "-") + ".fifo"),
				Attributes: map[string]string{
					string(types.QueueAttributeNameFifoQueue): "true",
				},
			})
			if err != nil {
				t.Fatalf("CreateQueue() error = %v", err)
			}
			_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
				QueueUrl:               created.QueueUrl,
				MessageBody:            aws.String("payload"),
				MessageGroupId:         aws.String(tc.messageGroupID),
				MessageDeduplicationId: aws.String(tc.messageDeduplicationID),
			})
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
				t.Fatalf("SendMessage(%s) error = %v, want InvalidParameterValue", tc.name, err)
			}

			received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
				QueueUrl:            created.QueueUrl,
				MaxNumberOfMessages: 1,
				WaitTimeSeconds:     0,
			})
			if err != nil {
				t.Fatalf("ReceiveMessage() error = %v", err)
			}
			if len(received.Messages) != 0 {
				t.Fatalf("ReceiveMessage() = %#v, want no FIFO message enqueued with invalid ID", received.Messages)
			}
		})
	}
}

func TestSQSCompatibilityAdapterDeduplicatesFIFOMessageID(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for _, body := range []string{"first", "duplicate"} {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:               created.QueueUrl,
			MessageBody:            aws.String(body),
			MessageGroupId:         aws.String("group-a"),
			MessageDeduplicationId: aws.String("dedup-a"),
		}); err != nil {
			t.Fatalf("SendMessage(%s) error = %v", body, err)
		}
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 || aws.ToString(received.Messages[0].Body) != "first" {
		t.Fatalf("ReceiveMessage() = %#v, want only first FIFO message delivered", received.Messages)
	}
}

func TestSQSCompatibilityAdapterDeduplicatesFIFOMessageBodyWhenContentBased(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue):                 "true",
			string(types.QueueAttributeNameContentBasedDeduplication): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:       created.QueueUrl,
			MessageBody:    aws.String("same-payload"),
			MessageGroupId: aws.String("group-a"),
		}); err != nil {
			t.Fatalf("SendMessage(%d) error = %v", i, err)
		}
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 || aws.ToString(received.Messages[0].Body) != "same-payload" {
		t.Fatalf("ReceiveMessage() = %#v, want one content-deduplicated FIFO message", received.Messages)
	}
}

func TestSQSCompatibilityAdapterBlocksInflightFIFOMessageGroup(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for _, msg := range []struct {
		body  string
		group string
		dedup string
	}{
		{body: "a1", group: "group-a", dedup: "dedup-a1"},
		{body: "a2", group: "group-a", dedup: "dedup-a2"},
		{body: "b1", group: "group-b", dedup: "dedup-b1"},
	} {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:               created.QueueUrl,
			MessageBody:            aws.String(msg.body),
			MessageGroupId:         aws.String(msg.group),
			MessageDeduplicationId: aws.String(msg.dedup),
		}); err != nil {
			t.Fatalf("SendMessage(%s) error = %v", msg.body, err)
		}
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 3,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   30,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	bodies := []string{}
	for _, msg := range received.Messages {
		bodies = append(bodies, aws.ToString(msg.Body))
	}
	if strings.Join(bodies, ",") != "a1,b1" {
		t.Fatalf("ReceiveMessage() bodies = %#v, want a1,b1 while a2 group is blocked", bodies)
	}
}

func TestSQSCompatibilityAdapterReturnsFIFOSequenceNumbers(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("sequence-numbers.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	sent, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:               created.QueueUrl,
		MessageBody:            aws.String("single"),
		MessageGroupId:         aws.String("group-a"),
		MessageDeduplicationId: aws.String("dedup-single"),
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if aws.ToString(sent.SequenceNumber) == "" {
		t.Fatalf("SendMessage() SequenceNumber = %q, want FIFO sequence number", aws.ToString(sent.SequenceNumber))
	}

	batched, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{Id: aws.String("first"), MessageBody: aws.String("batch-one"), MessageGroupId: aws.String("group-a"), MessageDeduplicationId: aws.String("dedup-batch-one")},
			{Id: aws.String("second"), MessageBody: aws.String("batch-two"), MessageGroupId: aws.String("group-b"), MessageDeduplicationId: aws.String("dedup-batch-two")},
		},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(batched.Successful) != 2 {
		t.Fatalf("SendMessageBatch() successful = %#v, want 2 entries", batched.Successful)
	}
	for _, entry := range batched.Successful {
		if aws.ToString(entry.SequenceNumber) == "" {
			t.Fatalf("SendMessageBatch() entry %s SequenceNumber = %q, want FIFO sequence number", aws.ToString(entry.Id), aws.ToString(entry.SequenceNumber))
		}
	}
}

func TestSQSCompatibilityAdapterReturnsFIFOReceiveAttributes(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("receive-attributes.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	sent, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:               created.QueueUrl,
		MessageBody:            aws.String("payload"),
		MessageGroupId:         aws.String("group-a"),
		MessageDeduplicationId: aws.String("dedup-a"),
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:                    created.QueueUrl,
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{types.MessageSystemAttributeNameAll},
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 1", len(received.Messages))
	}
	attrs := received.Messages[0].Attributes
	if attrs[string(types.MessageSystemAttributeNameMessageGroupId)] != "group-a" {
		t.Fatalf("MessageGroupId = %q, want group-a", attrs[string(types.MessageSystemAttributeNameMessageGroupId)])
	}
	if attrs[string(types.MessageSystemAttributeNameMessageDeduplicationId)] != "dedup-a" {
		t.Fatalf("MessageDeduplicationId = %q, want dedup-a", attrs[string(types.MessageSystemAttributeNameMessageDeduplicationId)])
	}
	if attrs[string(types.MessageSystemAttributeNameSequenceNumber)] != aws.ToString(sent.SequenceNumber) {
		t.Fatalf("SequenceNumber = %q, want %q", attrs[string(types.MessageSystemAttributeNameSequenceNumber)], aws.ToString(sent.SequenceNumber))
	}
}

func TestSQSCompatibilityAdapterReturnsDeprecatedFIFOReceiveAttributeNames(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("receive-attribute-names.fifo"),
		Attributes: map[string]string{
			string(types.QueueAttributeNameFifoQueue): "true",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	sent, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:               created.QueueUrl,
		MessageBody:            aws.String("payload"),
		MessageGroupId:         aws.String("group-a"),
		MessageDeduplicationId: aws.String("dedup-a"),
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:       created.QueueUrl,
		AttributeNames: []types.QueueAttributeName{types.QueueAttributeNameAll},
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 1", len(received.Messages))
	}
	attrs := received.Messages[0].Attributes
	if attrs[string(types.MessageSystemAttributeNameMessageGroupId)] != "group-a" {
		t.Fatalf("MessageGroupId = %q, want group-a", attrs[string(types.MessageSystemAttributeNameMessageGroupId)])
	}
	if attrs[string(types.MessageSystemAttributeNameMessageDeduplicationId)] != "dedup-a" {
		t.Fatalf("MessageDeduplicationId = %q, want dedup-a", attrs[string(types.MessageSystemAttributeNameMessageDeduplicationId)])
	}
	if attrs[string(types.MessageSystemAttributeNameSequenceNumber)] != aws.ToString(sent.SequenceNumber) {
		t.Fatalf("SequenceNumber = %q, want %q", attrs[string(types.MessageSystemAttributeNameSequenceNumber)], aws.ToString(sent.SequenceNumber))
	}
}

func TestSQSCompatibilityAdapterReturnsReceiveSystemAttributes(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("receive-system-attributes"),
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("payload"),
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	first, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:                    created.QueueUrl,
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{types.MessageSystemAttributeNameAll},
		VisibilityTimeout:           30,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(first) error = %v", err)
	}
	if len(first.Messages) != 1 {
		t.Fatalf("ReceiveMessage(first) returned %d messages, want 1", len(first.Messages))
	}
	firstAttrs := first.Messages[0].Attributes
	if firstAttrs[string(types.MessageSystemAttributeNameApproximateReceiveCount)] != "1" {
		t.Fatalf("ApproximateReceiveCount(first) = %q, want 1", firstAttrs[string(types.MessageSystemAttributeNameApproximateReceiveCount)])
	}
	sentTimestamp := firstAttrs[string(types.MessageSystemAttributeNameSentTimestamp)]
	firstReceiveTimestamp := firstAttrs[string(types.MessageSystemAttributeNameApproximateFirstReceiveTimestamp)]
	if sentTimestamp == "" {
		t.Fatalf("SentTimestamp(first) is empty")
	}
	if firstReceiveTimestamp == "" {
		t.Fatalf("ApproximateFirstReceiveTimestamp(first) is empty")
	}
	if _, err := client.ChangeMessageVisibility(context.Background(), &sqs.ChangeMessageVisibilityInput{
		QueueUrl:          created.QueueUrl,
		ReceiptHandle:     first.Messages[0].ReceiptHandle,
		VisibilityTimeout: 0,
	}); err != nil {
		t.Fatalf("ChangeMessageVisibility() error = %v", err)
	}

	second, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl: created.QueueUrl,
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			types.MessageSystemAttributeNameApproximateReceiveCount,
			types.MessageSystemAttributeNameApproximateFirstReceiveTimestamp,
			types.MessageSystemAttributeNameSentTimestamp,
		},
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(second) error = %v", err)
	}
	if len(second.Messages) != 1 {
		t.Fatalf("ReceiveMessage(second) returned %d messages, want 1", len(second.Messages))
	}
	secondAttrs := second.Messages[0].Attributes
	if secondAttrs[string(types.MessageSystemAttributeNameApproximateReceiveCount)] != "2" {
		t.Fatalf("ApproximateReceiveCount(second) = %q, want 2", secondAttrs[string(types.MessageSystemAttributeNameApproximateReceiveCount)])
	}
	if secondAttrs[string(types.MessageSystemAttributeNameSentTimestamp)] != sentTimestamp {
		t.Fatalf("SentTimestamp(second) = %q, want %q", secondAttrs[string(types.MessageSystemAttributeNameSentTimestamp)], sentTimestamp)
	}
	if secondAttrs[string(types.MessageSystemAttributeNameApproximateFirstReceiveTimestamp)] != firstReceiveTimestamp {
		t.Fatalf("ApproximateFirstReceiveTimestamp(second) = %q, want %q", secondAttrs[string(types.MessageSystemAttributeNameApproximateFirstReceiveTimestamp)], firstReceiveTimestamp)
	}
}

func TestSQSCompatibilityAdapterReturnsMessageBodyMD5Digests(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("body-digests"),
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	sent, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("single"),
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if aws.ToString(sent.MD5OfMessageBody) != sqsBodyMD5("single") {
		t.Fatalf("SendMessage() MD5OfMessageBody = %q, want %q", aws.ToString(sent.MD5OfMessageBody), sqsBodyMD5("single"))
	}

	batched, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{Id: aws.String("first"), MessageBody: aws.String("batch-one")},
			{Id: aws.String("second"), MessageBody: aws.String("batch-two")},
		},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(batched.Successful) != 2 {
		t.Fatalf("SendMessageBatch() successful = %#v, want 2 entries", batched.Successful)
	}
	wantBatchMD5 := map[string]string{"first": sqsBodyMD5("batch-one"), "second": sqsBodyMD5("batch-two")}
	for _, entry := range batched.Successful {
		if aws.ToString(entry.MD5OfMessageBody) != wantBatchMD5[aws.ToString(entry.Id)] {
			t.Fatalf("SendMessageBatch() entry %s MD5OfMessageBody = %q, want %q", aws.ToString(entry.Id), aws.ToString(entry.MD5OfMessageBody), wantBatchMD5[aws.ToString(entry.Id)])
		}
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 3,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 3 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 3", len(received.Messages))
	}
	for _, msg := range received.Messages {
		body := aws.ToString(msg.Body)
		if aws.ToString(msg.MD5OfBody) != sqsBodyMD5(body) {
			t.Fatalf("ReceiveMessage(%s) MD5OfBody = %q, want %q", body, aws.ToString(msg.MD5OfBody), sqsBodyMD5(body))
		}
	}
}

func TestSQSCompatibilityAdapterReturnsSenderID(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("sender-id"),
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("payload"),
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl: created.QueueUrl,
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			types.MessageSystemAttributeNameSenderId,
		},
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 1", len(received.Messages))
	}
	if received.Messages[0].Attributes[string(types.MessageSystemAttributeNameSenderId)] != "homeport" {
		t.Fatalf("SenderId = %q, want homeport", received.Messages[0].Attributes[string(types.MessageSystemAttributeNameSenderId)])
	}
}

func TestSQSCompatibilityAdapterReturnsAWSTraceHeader(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("trace-header"),
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	traceHeader := "Root=1-5f84c7a7-3e5d7c1f0f9a4b2c1d2e3f4a;Parent=53995c3f42cd8ad8;Sampled=1"
	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("payload"),
		MessageSystemAttributes: map[string]types.MessageSystemAttributeValue{
			string(types.MessageSystemAttributeNameForSendsAWSTraceHeader): {
				DataType:    aws.String("String"),
				StringValue: aws.String(traceHeader),
			},
		},
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl: created.QueueUrl,
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			types.MessageSystemAttributeNameAWSTraceHeader,
		},
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 1", len(received.Messages))
	}
	if received.Messages[0].Attributes[string(types.MessageSystemAttributeNameAWSTraceHeader)] != traceHeader {
		t.Fatalf("AWSTraceHeader = %q, want %q", received.Messages[0].Attributes[string(types.MessageSystemAttributeNameAWSTraceHeader)], traceHeader)
	}
}

func TestSQSCompatibilityAdapterReturnsBatchAWSTraceHeader(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("batch-trace-header"),
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	traceHeader := "Root=1-5f84c7a7-aaaaaaaaaaaaaaaaaaaaaaaa;Parent=bbbbbbbbbbbbbbbb;Sampled=1"
	if _, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{
				Id:          aws.String("traced"),
				MessageBody: aws.String("payload"),
				MessageSystemAttributes: map[string]types.MessageSystemAttributeValue{
					string(types.MessageSystemAttributeNameForSendsAWSTraceHeader): {
						DataType:    aws.String("String"),
						StringValue: aws.String(traceHeader),
					},
				},
			},
		},
	}); err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl: created.QueueUrl,
		MessageSystemAttributeNames: []types.MessageSystemAttributeName{
			types.MessageSystemAttributeNameAWSTraceHeader,
		},
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 1", len(received.Messages))
	}
	if received.Messages[0].Attributes[string(types.MessageSystemAttributeNameAWSTraceHeader)] != traceHeader {
		t.Fatalf("AWSTraceHeader = %q, want %q", received.Messages[0].Attributes[string(types.MessageSystemAttributeNameAWSTraceHeader)], traceHeader)
	}
}

func TestSQSCompatibilityAdapterReturnsTraceHeaderSystemAttributeMD5(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("trace-header-md5"),
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	traceHeader := "Root=1-5f84c7a7-cccccccccccccccccccccccc;Parent=dddddddddddddddd;Sampled=1"
	wantMD5 := sqsAttributeMD5("AWSTraceHeader", "String", traceHeader)
	sent, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("single"),
		MessageSystemAttributes: map[string]types.MessageSystemAttributeValue{
			string(types.MessageSystemAttributeNameForSendsAWSTraceHeader): {
				DataType:    aws.String("String"),
				StringValue: aws.String(traceHeader),
			},
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if aws.ToString(sent.MD5OfMessageSystemAttributes) != wantMD5 {
		t.Fatalf("SendMessage() MD5OfMessageSystemAttributes = %q, want %q", aws.ToString(sent.MD5OfMessageSystemAttributes), wantMD5)
	}

	batch, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{
				Id:          aws.String("traced"),
				MessageBody: aws.String("batch"),
				MessageSystemAttributes: map[string]types.MessageSystemAttributeValue{
					string(types.MessageSystemAttributeNameForSendsAWSTraceHeader): {
						DataType:    aws.String("String"),
						StringValue: aws.String(traceHeader),
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(batch.Successful) != 1 {
		t.Fatalf("SendMessageBatch() successful entries = %d, want 1", len(batch.Successful))
	}
	if aws.ToString(batch.Successful[0].MD5OfMessageSystemAttributes) != wantMD5 {
		t.Fatalf("SendMessageBatch() MD5OfMessageSystemAttributes = %q, want %q", aws.ToString(batch.Successful[0].MD5OfMessageSystemAttributes), wantMD5)
	}
}

func TestSQSCompatibilityAdapterReturnsStringMessageAttributes(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("message-attributes"),
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	attrs := map[string]string{"priority": "high", "source": "billing"}
	wantMD5 := sqsAttributesMD5(attrs)
	sent, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("single"),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"priority": {DataType: aws.String("String"), StringValue: aws.String(attrs["priority"])},
			"source":   {DataType: aws.String("String"), StringValue: aws.String(attrs["source"])},
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if aws.ToString(sent.MD5OfMessageAttributes) != wantMD5 {
		t.Fatalf("SendMessage() MD5OfMessageAttributes = %q, want %q", aws.ToString(sent.MD5OfMessageAttributes), wantMD5)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:              created.QueueUrl,
		MessageAttributeNames: []string{"All"},
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 1", len(received.Messages))
	}
	msg := received.Messages[0]
	if aws.ToString(msg.MD5OfMessageAttributes) != wantMD5 {
		t.Fatalf("ReceiveMessage() MD5OfMessageAttributes = %q, want %q", aws.ToString(msg.MD5OfMessageAttributes), wantMD5)
	}
	for name, value := range attrs {
		got, ok := msg.MessageAttributes[name]
		if !ok {
			t.Fatalf("ReceiveMessage() missing message attribute %q in %#v", name, msg.MessageAttributes)
		}
		if aws.ToString(got.DataType) != "String" || aws.ToString(got.StringValue) != value {
			t.Fatalf("ReceiveMessage() attribute %q = type %q value %q, want String %q", name, aws.ToString(got.DataType), aws.ToString(got.StringValue), value)
		}
	}

	batch, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{
				Id:          aws.String("attributed"),
				MessageBody: aws.String("batch"),
				MessageAttributes: map[string]types.MessageAttributeValue{
					"priority": {DataType: aws.String("String"), StringValue: aws.String(attrs["priority"])},
					"source":   {DataType: aws.String("String"), StringValue: aws.String(attrs["source"])},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(batch.Successful) != 1 {
		t.Fatalf("SendMessageBatch() successful entries = %d, want 1", len(batch.Successful))
	}
	if aws.ToString(batch.Successful[0].MD5OfMessageAttributes) != wantMD5 {
		t.Fatalf("SendMessageBatch() MD5OfMessageAttributes = %q, want %q", aws.ToString(batch.Successful[0].MD5OfMessageAttributes), wantMD5)
	}
}

func TestSQSCompatibilityAdapterReturnsBinaryMessageAttributes(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("binary-message-attributes"),
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	payload := []byte{0, 1, 2, 3, 255}
	wantMD5 := sqsBinaryAttributeMD5("payload", "Binary", payload)
	sent, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("single"),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"payload": {DataType: aws.String("Binary"), BinaryValue: payload},
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if aws.ToString(sent.MD5OfMessageAttributes) != wantMD5 {
		t.Fatalf("SendMessage() MD5OfMessageAttributes = %q, want %q", aws.ToString(sent.MD5OfMessageAttributes), wantMD5)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:              created.QueueUrl,
		MessageAttributeNames: []string{"payload"},
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 1", len(received.Messages))
	}
	msg := received.Messages[0]
	if aws.ToString(msg.MD5OfMessageAttributes) != wantMD5 {
		t.Fatalf("ReceiveMessage() MD5OfMessageAttributes = %q, want %q", aws.ToString(msg.MD5OfMessageAttributes), wantMD5)
	}
	got := msg.MessageAttributes["payload"]
	if aws.ToString(got.DataType) != "Binary" || !bytes.Equal(got.BinaryValue, payload) {
		t.Fatalf("ReceiveMessage() payload attribute = type %q value %v, want Binary %v", aws.ToString(got.DataType), got.BinaryValue, payload)
	}

	batch, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{
				Id:          aws.String("binary"),
				MessageBody: aws.String("batch"),
				MessageAttributes: map[string]types.MessageAttributeValue{
					"payload": {DataType: aws.String("Binary"), BinaryValue: payload},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(batch.Successful) != 1 {
		t.Fatalf("SendMessageBatch() successful entries = %d, want 1", len(batch.Successful))
	}
	if aws.ToString(batch.Successful[0].MD5OfMessageAttributes) != wantMD5 {
		t.Fatalf("SendMessageBatch() MD5OfMessageAttributes = %q, want %q", aws.ToString(batch.Successful[0].MD5OfMessageAttributes), wantMD5)
	}
}

func TestSQSCompatibilityAdapterNormalizesNumberMessageAttributes(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("number-message-attributes"),
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	wantValue := "1.23"
	wantMD5 := sqsAttributeMD5("amount", "Number", wantValue)
	sent, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("single"),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"amount": {DataType: aws.String("Number"), StringValue: aws.String("001.2300")},
		},
	})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if aws.ToString(sent.MD5OfMessageAttributes) != wantMD5 {
		t.Fatalf("SendMessage() MD5OfMessageAttributes = %q, want %q", aws.ToString(sent.MD5OfMessageAttributes), wantMD5)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:              created.QueueUrl,
		MessageAttributeNames: []string{"amount"},
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 1", len(received.Messages))
	}
	got := received.Messages[0].MessageAttributes["amount"]
	if aws.ToString(got.DataType) != "Number" || aws.ToString(got.StringValue) != wantValue {
		t.Fatalf("ReceiveMessage() amount attribute = type %q value %q, want Number %q", aws.ToString(got.DataType), aws.ToString(got.StringValue), wantValue)
	}
	if aws.ToString(received.Messages[0].MD5OfMessageAttributes) != wantMD5 {
		t.Fatalf("ReceiveMessage() MD5OfMessageAttributes = %q, want %q", aws.ToString(received.Messages[0].MD5OfMessageAttributes), wantMD5)
	}

	batch, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{
				Id:          aws.String("number"),
				MessageBody: aws.String("batch"),
				MessageAttributes: map[string]types.MessageAttributeValue{
					"amount": {DataType: aws.String("Number"), StringValue: aws.String("001.2300")},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(batch.Successful) != 1 {
		t.Fatalf("SendMessageBatch() successful entries = %d, want 1", len(batch.Successful))
	}
	if aws.ToString(batch.Successful[0].MD5OfMessageAttributes) != wantMD5 {
		t.Fatalf("SendMessageBatch() MD5OfMessageAttributes = %q, want %q", aws.ToString(batch.Successful[0].MD5OfMessageAttributes), wantMD5)
	}
}

func TestSQSCompatibilityAdapterNormalizesScientificNumberAttributesWithoutLosingPrecisionBudget(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("number-normalization")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "leading fractional zeroes do not consume precision",
			value: "0." + strings.Repeat("0", 100) + strings.Repeat("1", 38),
			want:  "0." + strings.Repeat("0", 100) + strings.Repeat("1", 38),
		},
		{name: "scientific notation", value: "001.2300e2", want: "1.23e2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sent, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
				QueueUrl:    created.QueueUrl,
				MessageBody: aws.String(tc.name),
				MessageAttributes: map[string]types.MessageAttributeValue{
					"amount": {DataType: aws.String("Number"), StringValue: aws.String(tc.value)},
				},
			})
			if err != nil {
				t.Fatalf("SendMessage() error = %v", err)
			}
			if got, want := aws.ToString(sent.MD5OfMessageAttributes), sqsAttributeMD5("amount", "Number", tc.want); got != want {
				t.Fatalf("SendMessage() MD5OfMessageAttributes = %q, want %q", got, want)
			}
		})
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:              created.QueueUrl,
		MessageAttributeNames: []string{"All"},
		MaxNumberOfMessages:   2,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 2 {
		t.Fatalf("ReceiveMessage() returned %d messages, want 2", len(received.Messages))
	}
	values := map[string]string{}
	for _, message := range received.Messages {
		values[aws.ToString(message.Body)] = aws.ToString(message.MessageAttributes["amount"].StringValue)
	}
	if got, want := values["leading fractional zeroes do not consume precision"], "0."+strings.Repeat("0", 100)+strings.Repeat("1", 38); got != want {
		t.Fatalf("received precision-boundary value = %q, want %q", got, want)
	}
	if got := values["scientific notation"]; got != "1.23e2" {
		t.Fatalf("received scientific value = %q, want 1.23e2", got)
	}
}

func TestSQSCompatibilityAdapterAccepts256CharacterUnicodeCustomDataType(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("unicode-custom-type")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	dataType := "String." + strings.Repeat("界", 249)
	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("payload"),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"label": {DataType: aws.String(dataType), StringValue: aws.String("value")},
		},
	}); err != nil {
		t.Fatalf("SendMessage() error = %v, want 256-character custom data type accepted", err)
	}
}

func TestSQSCompatibilityAdapterRejectsMoreThanTenMessageAttributes(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("message-attribute-limit")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	attributes := map[string]types.MessageAttributeValue{}
	for index := 0; index < 11; index++ {
		attributes[fmt.Sprintf("attribute-%d", index)] = types.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String("value")}
	}
	_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:          created.QueueUrl,
		MessageBody:       aws.String("payload"),
		MessageAttributes: attributes,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("SendMessage() error = %v, want InvalidParameterValue", err)
	}

	batch, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{{
			Id:                aws.String("too-many"),
			MessageBody:       aws.String("payload"),
			MessageAttributes: attributes,
		}},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(batch.Successful) != 0 || len(batch.Failed) != 1 || aws.ToString(batch.Failed[0].Code) != "InvalidParameterValue" {
		t.Fatalf("SendMessageBatch() = %#v, want one InvalidParameterValue failure", batch)
	}
}

func TestSQSCompatibilityAdapterCountsMessageAttributesTowardMessageSize(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("message-size")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	attributes := map[string]types.MessageAttributeValue{
		"kind": {DataType: aws.String("String"), StringValue: aws.String("event")},
	}
	messageBody := strings.Repeat("x", 1048576)
	_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:          created.QueueUrl,
		MessageBody:       aws.String(messageBody),
		MessageAttributes: attributes,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("SendMessage(oversized with attributes) error = %v, want InvalidParameterValue", err)
	}

	_, err = client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{{
			Id:                aws.String("oversized"),
			MessageBody:       aws.String(messageBody),
			MessageAttributes: attributes,
		}},
	})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "BatchRequestTooLong" {
		t.Fatalf("SendMessageBatch(oversized with attributes) error = %v, want BatchRequestTooLong", err)
	}
	empty, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after oversized batch) error = %v", err)
	}
	if len(empty.Messages) != 0 {
		t.Fatalf("ReceiveMessage(after oversized batch) = %#v, want no enqueued messages", empty.Messages)
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidMessageAttributes(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("invalid-message-attributes"),
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for _, tc := range []struct {
		name       string
		attributes map[string]types.MessageAttributeValue
	}{
		{
			name: "reserved prefix",
			attributes: map[string]types.MessageAttributeValue{
				"AWS.Trace": {DataType: aws.String("String"), StringValue: aws.String("value")},
			},
		},
		{
			name: "unsupported data type",
			attributes: map[string]types.MessageAttributeValue{
				"trace": {DataType: aws.String("Boolean"), StringValue: aws.String("true")},
			},
		},
		{
			name: "empty custom data type label",
			attributes: map[string]types.MessageAttributeValue{
				"amount": {DataType: aws.String("Number."), StringValue: aws.String("1")},
			},
		},
		{
			name: "invalid custom data type label",
			attributes: map[string]types.MessageAttributeValue{
				"trace": {DataType: aws.String("String.\x01"), StringValue: aws.String("value")},
			},
		},
		{
			name: "empty string value",
			attributes: map[string]types.MessageAttributeValue{
				"trace": {DataType: aws.String("String")},
			},
		},
		{
			name: "non-numeric number value",
			attributes: map[string]types.MessageAttributeValue{
				"amount": {DataType: aws.String("Number"), StringValue: aws.String("not-a-number")},
			},
		},
		{
			name: "number exceeds 38 digits of precision",
			attributes: map[string]types.MessageAttributeValue{
				"amount": {DataType: aws.String("Number"), StringValue: aws.String(strings.Repeat("1", 39))},
			},
		},
		{
			name: "number below minimum exponent",
			attributes: map[string]types.MessageAttributeValue{
				"amount": {DataType: aws.String("Number"), StringValue: aws.String("1e-129")},
			},
		},
		{
			name: "number above maximum exponent",
			attributes: map[string]types.MessageAttributeValue{
				"amount": {DataType: aws.String("Number"), StringValue: aws.String("1e127")},
			},
		},
		{
			name: "string list value",
			attributes: map[string]types.MessageAttributeValue{
				"trace": {DataType: aws.String("String"), StringValue: aws.String("value"), StringListValues: []string{"a", "b"}},
			},
		},
		{
			name: "binary list value",
			attributes: map[string]types.MessageAttributeValue{
				"trace": {DataType: aws.String("Binary"), BinaryValue: []byte{1}, BinaryListValues: [][]byte{{1, 2}}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
				QueueUrl:          created.QueueUrl,
				MessageBody:       aws.String("payload"),
				MessageAttributes: tc.attributes,
			})
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
				t.Fatalf("SendMessage(%s) error = %v, want InvalidParameterValue", tc.name, err)
			}
		})
	}

	batch, err := client.SendMessageBatch(context.Background(), &sqs.SendMessageBatchInput{
		QueueUrl: created.QueueUrl,
		Entries: []types.SendMessageBatchRequestEntry{
			{
				Id:          aws.String("invalid"),
				MessageBody: aws.String("batch"),
				MessageAttributes: map[string]types.MessageAttributeValue{
					"Amazon.Trace": {DataType: aws.String("String"), StringValue: aws.String("value")},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("SendMessageBatch() error = %v", err)
	}
	if len(batch.Successful) != 0 || len(batch.Failed) != 1 {
		t.Fatalf("SendMessageBatch() successful=%#v failed=%#v, want one failed entry", batch.Successful, batch.Failed)
	}
	if aws.ToString(batch.Failed[0].Code) != "InvalidParameterValue" || !batch.Failed[0].SenderFault {
		t.Fatalf("SendMessageBatch() failed entry = %#v, want sender InvalidParameterValue", batch.Failed[0])
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{QueueUrl: created.QueueUrl})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage() returned %d messages after invalid sends, want 0", len(received.Messages))
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidSendMessageDelaySeconds(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:     created.QueueUrl,
		MessageBody:  aws.String("payload"),
		DelaySeconds: 901,
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidParameterValue" {
		t.Fatalf("SendMessage(invalid DelaySeconds) error = %v, want InvalidParameterValue", err)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage() = %#v, want no invalid message enqueued", received.Messages)
	}
}

func TestSQSCompatibilityAdapterPurgesQueueMessages(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	for _, body := range []string{"visible", "inflight"} {
		if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
			QueueUrl:    created.QueueUrl,
			MessageBody: aws.String(body),
		}); err != nil {
			t.Fatalf("SendMessage(%s) error = %v", body, err)
		}
	}
	if _, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   60,
	}); err != nil {
		t.Fatalf("ReceiveMessage(inflight) error = %v", err)
	}
	if _, err := client.PurgeQueue(context.Background(), &sqs.PurgeQueueInput{QueueUrl: created.QueueUrl}); err != nil {
		t.Fatalf("PurgeQueue() error = %v", err)
	}
	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 2,
		WaitTimeSeconds:     0,
		VisibilityTimeout:   0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage(after purge) error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage(after purge) = %#v, want no messages", received.Messages)
	}
}

func TestSQSCompatibilityAdapterReturnsUnsupportedOperation(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("Action=UnsupportedThing"))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatalf("Do(unsupported action) error = %v", err)
	}
	defer resp.Body.Close()

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode(unsupported action) error = %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest || body["__type"] != "UnsupportedOperation" {
		t.Fatalf("unsupported action response = status %d body %#v, want UnsupportedOperation", resp.StatusCode, body)
	}
}

func TestSQSCompatibilityAdapterReturnsRequestThrottledWhenQueueQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(compataws.WithSQSQueueQuota(1)))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs-a")}); err != nil {
		t.Fatalf("CreateQueue(first) error = %v", err)
	}

	_, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs-b")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "RequestThrottled" {
		t.Fatalf("CreateQueue(over quota) error = %v, want RequestThrottled", err)
	}
}

func TestSQSCompatibilityAdapterReturnsRequestThrottledWhenMessageQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(compataws.WithSQSMessageQuota(1)))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("first"),
	}); err != nil {
		t.Fatalf("SendMessage(first) error = %v", err)
	}
	_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("second"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "RequestThrottled" {
		t.Fatalf("SendMessage(over quota) error = %v, want RequestThrottled", err)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{QueueUrl: created.QueueUrl})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 1 || aws.ToString(received.Messages[0].Body) != "first" {
		t.Fatalf("ReceiveMessage() = %#v, want only first message", received.Messages)
	}
}

func TestSQSCompatibilityAdapterAuthorizesAndAuditsAWSSDKCalls(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"sqs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"sqs:SendMessage"}, Resources: []string{"*"}},
		)),
		compataws.WithSQSAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	_, err = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("blocked"),
	})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("SendMessage() error = %v, want AccessDenied", err)
	}

	received, err := client.ReceiveMessage(context.Background(), &sqs.ReceiveMessageInput{
		QueueUrl:            created.QueueUrl,
		MaxNumberOfMessages: 1,
		WaitTimeSeconds:     0,
	})
	if err != nil {
		t.Fatalf("ReceiveMessage() error = %v", err)
	}
	if len(received.Messages) != 0 {
		t.Fatalf("ReceiveMessage() returned %d messages, want denied send to leave queue empty", len(received.Messages))
	}

	assertDecision(t, auditLog.Decisions(), "sqs:CreateQueue", true)
	assertDecision(t, auditLog.Decisions(), "sqs:SendMessage", false)
	assertDecision(t, auditLog.Decisions(), "sqs:ReceiveMessage", true)
}

func TestSQSCompatibilityAdapterDeniesPlannedActions(t *testing.T) {
	for _, tc := range []struct {
		name       string
		action     string
		needsQueue bool
		call       func(context.Context, *sqs.Client, *string) error
		check      func(context.Context, *testing.T, *sqs.Client, *string)
	}{
		{
			name:   "CreateQueue",
			action: "sqs:CreateQueue",
			call: func(ctx context.Context, client *sqs.Client, queueURL *string) error {
				_, err := client.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
				return err
			},
		},
		{
			name:       "SendMessage",
			action:     "sqs:SendMessage",
			needsQueue: true,
			call: func(ctx context.Context, client *sqs.Client, queueURL *string) error {
				_, err := client.SendMessage(ctx, &sqs.SendMessageInput{
					QueueUrl:    queueURL,
					MessageBody: aws.String("blocked"),
				})
				return err
			},
			check: func(ctx context.Context, t *testing.T, client *sqs.Client, queueURL *string) {
				received, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: queueURL})
				if err != nil {
					t.Fatalf("ReceiveMessage(after denied send) error = %v", err)
				}
				if len(received.Messages) != 0 {
					t.Fatalf("ReceiveMessage(after denied send) returned %d messages", len(received.Messages))
				}
			},
		},
		{
			name:       "ReceiveMessage",
			action:     "sqs:ReceiveMessage",
			needsQueue: true,
			call: func(ctx context.Context, client *sqs.Client, queueURL *string) error {
				_, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{QueueUrl: queueURL})
				return err
			},
		},
		{
			name:       "DeleteQueue",
			action:     "sqs:DeleteQueue",
			needsQueue: true,
			call: func(ctx context.Context, client *sqs.Client, queueURL *string) error {
				_, err := client.DeleteQueue(ctx, &sqs.DeleteQueueInput{QueueUrl: queueURL})
				return err
			},
			check: func(ctx context.Context, t *testing.T, client *sqs.Client, queueURL *string) {
				if _, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String("jobs")}); err != nil {
					t.Fatalf("GetQueueUrl(after denied delete) error = %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(compataws.NewSQSAdapter(
				compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(
					authz.Rule{Effect: authz.Allow, Actions: []string{"sqs:*"}, Resources: []string{"*"}},
					authz.Rule{Effect: authz.Deny, Actions: []string{tc.action}, Resources: []string{"*"}},
				)),
			))
			defer server.Close()

			client := sqs.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *sqs.Options) {
				o.BaseEndpoint = aws.String(server.URL)
			})

			ctx := context.Background()
			var queueURL *string
			if tc.needsQueue {
				created, err := client.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
				if err != nil {
					t.Fatalf("CreateQueue(setup) error = %v", err)
				}
				queueURL = created.QueueUrl
			}

			err := tc.call(ctx, client, queueURL)
			if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
				t.Fatalf("%s(denied) error = %v, want AccessDenied", tc.name, err)
			}
			if tc.check != nil {
				tc.check(ctx, t, client, queueURL)
			}
		})
	}
}

func TestSQSCompatibilityAdapterPersistsAuditDecisions(t *testing.T) {
	auditPath := filepath.Join(t.TempDir(), "sqs-audit.jsonl")
	auditLog := authz.NewFileAuditLog(auditPath)
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"sqs:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"sqs:SendMessage"}, Resources: []string{"*"}},
		)),
		compataws.WithSQSAuditSink(func(decision authz.Decision) {
			if err := auditLog.Record(decision); err != nil {
				t.Errorf("Record() error = %v", err)
			}
		}),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	_, _ = client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("blocked"),
	})

	persisted := authz.NewFileAuditLog(auditPath)
	decisions, err := persisted.Decisions()
	if err != nil {
		t.Fatalf("Decisions() error = %v", err)
	}
	assertDecision(t, decisions, "sqs:CreateQueue", true)
	assertDecision(t, decisions, "sqs:SendMessage", false)
}

func TestSQSCompatibilityAdapterAuthorizesSourceIPCidrCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"sqs:CreateQueue"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "source_ip", Values: []string{"127.0.0.0/8", "::1/128"}},
			},
		})),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")}); err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
}

func TestSQSCompatibilityAdapterAuthorizesCurrentTimeCondition(t *testing.T) {
	now := time.Now()
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"sqs:CreateQueue"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "current_time", Values: []string{now.Add(-time.Minute).Format(time.RFC3339) + "/" + now.Add(time.Minute).Format(time.RFC3339)}},
			},
		})),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")}); err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
}

func TestSQSCompatibilityAdapterAuthorizesUserAgentCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"sqs:CreateQueue"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "user_agent", Values: []string{"aws-sdk-go-v2*"}},
			},
		})),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")}); err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
}

func TestSQSCompatibilityAdapterAuthorizesRequestIDCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"sqs:CreateQueue"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "request_id", Values: []string{"homeport"}},
			},
		})),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")}); err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
}

func TestSQSCompatibilityAdapterAuthorizesCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"sqs:CreateQueue"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_age", Values: []string{"10m"}},
			},
		})),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "10m"))
	})

	if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")}); err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	denied := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "48h"))
	})
	if _, err := denied.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("drafts")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateQueue(denied) error = %v, want AccessDenied", err)
	}
}

func TestSQSCompatibilityAdapterRejectsExpiredCredential(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"sqs:*"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"sqs:CreateQueue"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_expired", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Expired", "true"))
	})

	if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateQueue(expired credential) error = %v, want AccessDenied", err)
	}
}

func TestSQSCompatibilityAdapterAuthorizesPrincipalAttributeCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"sqs:CreateQueue"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "principal:department", Values: []string{"finance"}},
			},
		})),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Principal-Attribute-Department", "finance"))
	})

	if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")}); err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	denied := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Principal-Attribute-Department", "engineering"))
	})
	if _, err := denied.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("drafts")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateQueue(denied) error = %v, want AccessDenied", err)
	}
}

func TestSQSCompatibilityAdapterAuthorizesClaimCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"sqs:CreateQueue"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "claim:mfa", Values: []string{"true"}},
			},
		})),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "true"))
	})

	if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")}); err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	denied := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "false"))
	})
	if _, err := denied.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("drafts")}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateQueue(denied) error = %v, want AccessDenied", err)
	}
}

func TestSQSCompatibilityAdapterAuthorizesCreateQueueTagCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"sqs:CreateQueue"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "tag:env", Values: []string{"dev"}},
			},
		})),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Tags:      map[string]string{"env": "dev"},
	}); err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
}

func TestSQSCompatibilityAdapterAuthorizesSendMessageWithPersistedQueueTagCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"sqs:CreateQueue"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"sqs:SendMessage"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "tag:env", Values: []string{"dev"}},
				},
			},
		)),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Tags:      map[string]string{"env": "dev"},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	if _, err := client.SendMessage(context.Background(), &sqs.SendMessageInput{
		QueueUrl:    created.QueueUrl,
		MessageBody: aws.String("payload"),
	}); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
}

func TestSQSCompatibilityAdapterRoundTripsQueueTags(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Tags: map[string]string{
			"env":   "dev",
			"owner": "homeport",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	tags, err := client.ListQueueTags(context.Background(), &sqs.ListQueueTagsInput{QueueUrl: created.QueueUrl})
	if err != nil {
		t.Fatalf("ListQueueTags() error = %v", err)
	}
	if tags.Tags["env"] != "dev" || tags.Tags["owner"] != "homeport" {
		t.Fatalf("ListQueueTags() = %#v, want env/owner tags", tags.Tags)
	}
}

func TestSQSCompatibilityAdapterTagQueueUpdatesQueueTags(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	if _, err := client.TagQueue(context.Background(), &sqs.TagQueueInput{
		QueueUrl: created.QueueUrl,
		Tags:     map[string]string{"env": "dev"},
	}); err != nil {
		t.Fatalf("TagQueue() error = %v", err)
	}

	tags, err := client.ListQueueTags(context.Background(), &sqs.ListQueueTagsInput{QueueUrl: created.QueueUrl})
	if err != nil {
		t.Fatalf("ListQueueTags() error = %v", err)
	}
	if tags.Tags["env"] != "dev" {
		t.Fatalf("ListQueueTags() = %#v, want env=dev", tags.Tags)
	}
}

func TestSQSCompatibilityAdapterUntagQueueRemovesQueueTags(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{
		QueueName: aws.String("jobs"),
		Tags: map[string]string{
			"env":   "dev",
			"owner": "homeport",
		},
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	if _, err := client.UntagQueue(context.Background(), &sqs.UntagQueueInput{
		QueueUrl: created.QueueUrl,
		TagKeys:  []string{"env"},
	}); err != nil {
		t.Fatalf("UntagQueue() error = %v", err)
	}

	tags, err := client.ListQueueTags(context.Background(), &sqs.ListQueueTagsInput{QueueUrl: created.QueueUrl})
	if err != nil {
		t.Fatalf("ListQueueTags() error = %v", err)
	}
	if _, ok := tags.Tags["env"]; ok {
		t.Fatalf("ListQueueTags() = %#v, want env tag removed", tags.Tags)
	}
	if tags.Tags["owner"] != "homeport" {
		t.Fatalf("ListQueueTags() = %#v, want owner tag retained", tags.Tags)
	}
}

func TestSQSCompatibilityAdapterPaginatesListQueues(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, name := range []string{"jobs-a", "jobs-b", "jobs-c"} {
		if _, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String(name)}); err != nil {
			t.Fatalf("CreateQueue(%s) error = %v", name, err)
		}
	}

	first, err := client.ListQueues(context.Background(), &sqs.ListQueuesInput{
		MaxResults:      aws.Int32(2),
		QueueNamePrefix: aws.String("jobs-"),
	})
	if err != nil {
		t.Fatalf("ListQueues(first) error = %v", err)
	}
	if len(first.QueueUrls) != 2 || first.NextToken == nil || *first.NextToken == "" {
		t.Fatalf("ListQueues(first) = %#v, want two queues and next token", first)
	}

	second, err := client.ListQueues(context.Background(), &sqs.ListQueuesInput{
		MaxResults:      aws.Int32(2),
		NextToken:       first.NextToken,
		QueueNamePrefix: aws.String("jobs-"),
	})
	if err != nil {
		t.Fatalf("ListQueues(second) error = %v", err)
	}
	if len(second.QueueUrls) != 1 || second.NextToken != nil {
		t.Fatalf("ListQueues(second) = %#v, want final queue without next token", second)
	}
}

func TestSQSCompatibilityAdapterRejectsInvalidListQueuesPagination(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter())
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{
			name: "max results zero",
			call: func() error {
				_, err := client.ListQueues(context.Background(), &sqs.ListQueuesInput{MaxResults: aws.Int32(0)})
				return err
			},
		},
		{
			name: "max results too high",
			call: func() error {
				_, err := client.ListQueues(context.Background(), &sqs.ListQueuesInput{MaxResults: aws.Int32(1001)})
				return err
			},
		},
		{
			name: "malformed next token",
			call: func() error {
				_, err := client.ListQueues(context.Background(), &sqs.ListQueuesInput{NextToken: aws.String("not-a-token")})
				return err
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidAttributeValue" {
				t.Fatalf("ListQueues(%s) error = %v, want InvalidAttributeValue", tc.name, err)
			}
		})
	}
}

func TestSQSCompatibilityAdapterAuthorizesDeleteQueueBeforeDeleting(t *testing.T) {
	denyDelete := true
	server := httptest.NewServer(compataws.NewSQSAdapter(
		compataws.WithSQSAuthorizer(authz.AuthorizerFunc(func(ctx context.Context, req authz.Request) (authz.Decision, error) {
			allowed := !(denyDelete && req.Action == "sqs:DeleteQueue")
			return authz.Decision{Request: req, Allowed: allowed, Reason: "test policy"}, nil
		})),
	))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	created, err := client.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("jobs")})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	_, err = client.DeleteQueue(context.Background(), &sqs.DeleteQueueInput{QueueUrl: created.QueueUrl})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("DeleteQueue(denied) error = %v, want AccessDenied", err)
	}
	if _, err := client.GetQueueUrl(context.Background(), &sqs.GetQueueUrlInput{QueueName: aws.String("jobs")}); err != nil {
		t.Fatalf("GetQueueUrl(after denied delete) error = %v", err)
	}

	denyDelete = false
	if _, err := client.DeleteQueue(context.Background(), &sqs.DeleteQueueInput{QueueUrl: created.QueueUrl}); err != nil {
		t.Fatalf("DeleteQueue(allowed) error = %v", err)
	}
	if _, err := client.GetQueueUrl(context.Background(), &sqs.GetQueueUrlInput{QueueName: aws.String("jobs")}); err == nil {
		t.Fatal("GetQueueUrl(after allowed delete) error = nil, want missing queue")
	}
}

func TestSQSCompatibilityAdapterShapesAuthorizerFailure(t *testing.T) {
	server := httptest.NewServer(compataws.NewSQSAdapter(compataws.WithSQSAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
		return authz.Decision{}, errors.New("authorizer unavailable")
	}))))
	defer server.Close()

	client := sqs.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sqs.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListQueues(context.Background(), &sqs.ListQueuesInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InternalFailure" {
		t.Fatalf("ListQueues(authorizer failure) error = %v, want InternalFailure", err)
	}
}

func assertDecision(t *testing.T, decisions []authz.Decision, action string, allowed bool) {
	t.Helper()
	for _, decision := range decisions {
		if decision.Request.Action == action && decision.Allowed == allowed {
			return
		}
	}
	t.Fatalf("missing audit decision action=%s allowed=%t in %#v", action, allowed, decisions)
}

func sqsBodyMD5(body string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(body)))
}

func sqsAttributeMD5(name, dataType, value string) string {
	var buf bytes.Buffer
	writeSQSAttributeMD5String(&buf, name)
	writeSQSAttributeMD5String(&buf, dataType)
	buf.WriteByte(1)
	writeSQSAttributeMD5String(&buf, value)
	return fmt.Sprintf("%x", md5.Sum(buf.Bytes()))
}

func sqsAttributesMD5(attrs map[string]string) string {
	var buf bytes.Buffer
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		writeSQSAttributeMD5String(&buf, name)
		writeSQSAttributeMD5String(&buf, "String")
		buf.WriteByte(1)
		writeSQSAttributeMD5String(&buf, attrs[name])
	}
	return fmt.Sprintf("%x", md5.Sum(buf.Bytes()))
}

func sqsBinaryAttributeMD5(name, dataType string, value []byte) string {
	var buf bytes.Buffer
	writeSQSAttributeMD5String(&buf, name)
	writeSQSAttributeMD5String(&buf, dataType)
	buf.WriteByte(2)
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(value)))
	buf.Write(value)
	return fmt.Sprintf("%x", md5.Sum(buf.Bytes()))
}

func writeSQSAttributeMD5String(buf *bytes.Buffer, value string) {
	_ = binary.Write(buf, binary.BigEndian, uint32(len(value)))
	buf.WriteString(value)
}

func sqsHeader(key, value string) func(*middleware.Stack) error {
	return func(stack *middleware.Stack) error {
		return stack.Build.Add(middleware.BuildMiddlewareFunc("sqsHeader", func(ctx context.Context, in middleware.BuildInput, next middleware.BuildHandler) (middleware.BuildOutput, middleware.Metadata, error) {
			if req, ok := in.Request.(*smithyhttp.Request); ok {
				req.Header.Set(key, value)
			}
			return next.HandleBuild(ctx, in)
		}), middleware.After)
	}
}
