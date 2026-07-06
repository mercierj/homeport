package compat_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
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

	topic, err := client.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	sub, err := client.Subscribe(context.Background(), &sns.SubscribeInput{
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
	published, err := client.Publish(context.Background(), &sns.PublishInput{
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
