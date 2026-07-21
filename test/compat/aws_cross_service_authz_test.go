package compat_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestAWSCompatibilityAdaptersShareCrossServiceAuthzPolicy(t *testing.T) {
	authorizer := authz.NewPolicyAuthorizer(
		authz.Rule{Effect: authz.Allow, Actions: []string{"sqs:*", "sns:*"}, Resources: []string{"*"}},
		authz.Rule{Effect: authz.Deny, Actions: []string{"s3:*"}, Resources: []string{"*"}},
	)

	sqsServer := httptest.NewServer(compataws.NewSQSAdapter(compataws.WithSQSAuthorizer(authorizer)))
	defer sqsServer.Close()
	snsServer := httptest.NewServer(compataws.NewSNSAdapter(compataws.WithSNSAuthorizer(authorizer)))
	defer snsServer.Close()
	s3Server := httptest.NewServer(compataws.NewS3Adapter(compataws.WithS3Authorizer(authorizer)))
	defer s3Server.Close()

	cfg := aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}
	sqsClient := sqs.NewFromConfig(cfg, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(sqsServer.URL)
	})
	snsClient := sns.NewFromConfig(cfg, func(o *sns.Options) {
		o.BaseEndpoint = aws.String(snsServer.URL)
	})
	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s3Server.URL)
		o.UsePathStyle = true
	})

	if _, err := sqsClient.CreateQueue(context.Background(), &sqs.CreateQueueInput{QueueName: aws.String("events")}); err != nil {
		t.Fatalf("SQS CreateQueue() error = %v", err)
	}
	if _, err := snsClient.CreateTopic(context.Background(), &sns.CreateTopicInput{Name: aws.String("events")}); err != nil {
		t.Fatalf("SNS CreateTopic() error = %v", err)
	}

	_, err := s3Client.CreateBucket(context.Background(), &s3.CreateBucketInput{Bucket: aws.String("blocked-bucket")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDenied" {
		t.Fatalf("S3 CreateBucket() error = %v, want AccessDenied", err)
	}
}
