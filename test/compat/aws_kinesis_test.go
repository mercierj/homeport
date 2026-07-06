package compat_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
)

func TestKinesisCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{
		StreamName: aws.String("events"),
		ShardCount: aws.Int32(1),
	}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	if _, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName:   aws.String("events"),
		PartitionKey: aws.String("one"),
		Data:         []byte("hello"),
	}); err != nil {
		t.Fatalf("PutRecord() error = %v", err)
	}
	iter, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{
		StreamName:        aws.String("events"),
		ShardId:           aws.String("shardId-000000000000"),
		ShardIteratorType: types.ShardIteratorTypeTrimHorizon,
	})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	records, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{
		ShardIterator: iter.ShardIterator,
	})
	if err != nil {
		t.Fatalf("GetRecords() error = %v", err)
	}
	if len(records.Records) != 1 || string(records.Records[0].Data) != "hello" {
		t.Fatalf("GetRecords() = %#v, want hello", records.Records)
	}
}
