package compat_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
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
