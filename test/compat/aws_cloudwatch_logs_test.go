package compat_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
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
