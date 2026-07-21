package compat_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
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
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
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

func TestKinesisCompatibilityAdapterPutRecordsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("batch-events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	result, err := client.PutRecords(ctx, &kinesis.PutRecordsInput{
		StreamName: aws.String("batch-events"),
		Records: []types.PutRecordsRequestEntry{
			{PartitionKey: aws.String("one"), Data: []byte("first")},
			{PartitionKey: aws.String("two"), Data: []byte("second")},
		},
	})
	if err != nil {
		t.Fatalf("PutRecords() error = %v", err)
	}
	if aws.ToInt32(result.FailedRecordCount) != 0 || len(result.Records) != 2 {
		t.Fatalf("PutRecords() = %#v, want two successful records", result)
	}
	for index, record := range result.Records {
		if aws.ToString(record.ShardId) == "" || aws.ToString(record.SequenceNumber) == "" || aws.ToString(record.ErrorCode) != "" {
			t.Fatalf("PutRecords() entry %d = %#v, want shard and sequence result", index, record)
		}
	}
	iterator, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String("batch-events"), ShardId: aws.String("shardId-000000000000"), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	read, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterator.ShardIterator})
	if err != nil || len(read.Records) != 2 || string(read.Records[0].Data) != "first" || string(read.Records[1].Data) != "second" {
		t.Fatalf("GetRecords() = %#v, %v; want ordered PutRecords payloads", read.Records, err)
	}
}

func TestKinesisCompatibilityAdapterPutRecordsAcceptsStreamARN(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	const streamName = "arn-batch-events"
	streamARN := "arn:aws:kinesis:us-east-1:000000000000:stream/" + streamName
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String(streamName), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	for _, input := range []*kinesis.PutRecordsInput{
		{StreamARN: aws.String(streamARN), Records: []types.PutRecordsRequestEntry{{PartitionKey: aws.String("arn-only"), Data: []byte("first")}}},
		{StreamName: aws.String(streamName), StreamARN: aws.String(streamARN), Records: []types.PutRecordsRequestEntry{{PartitionKey: aws.String("name-and-arn"), Data: []byte("second")}}},
	} {
		if _, err := client.PutRecords(ctx, input); err != nil {
			t.Fatalf("PutRecords(%#v) error = %v", input, err)
		}
	}
	iterator, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String(streamName), ShardId: aws.String("shardId-000000000000"), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	read, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterator.ShardIterator})
	if err != nil || len(read.Records) != 2 || string(read.Records[0].Data) != "first" || string(read.Records[1].Data) != "second" {
		t.Fatalf("GetRecords() = %#v, %v; want ARN-addressed records", read.Records, err)
	}
}

func TestKinesisCompatibilityAdapterListsShardsWithPagination(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisClock(func() time.Time { return now })))
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	const streamName = "listed-shards"
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String(streamName), ShardCount: aws.Int32(3)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	first, err := client.ListShards(ctx, &kinesis.ListShardsInput{StreamARN: aws.String("arn:aws:kinesis:us-east-1:000000000000:stream/" + streamName), MaxResults: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListShards(first page) error = %v", err)
	}
	if len(first.Shards) != 1 || aws.ToString(first.Shards[0].ShardId) != "shardId-000000000000" || first.Shards[0].HashKeyRange == nil || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListShards(first page) = %#v, want first shard, range, and next token", first)
	}
	second, err := client.ListShards(ctx, &kinesis.ListShardsInput{NextToken: first.NextToken, MaxResults: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListShards(second page) error = %v", err)
	}
	if len(second.Shards) != 1 || aws.ToString(second.Shards[0].ShardId) != "shardId-000000000001" || aws.ToString(second.NextToken) == "" {
		t.Fatalf("ListShards(second page) = %#v, want second shard and next token", second)
	}
	last, err := client.ListShards(ctx, &kinesis.ListShardsInput{NextToken: second.NextToken})
	if err != nil {
		t.Fatalf("ListShards(last page) error = %v", err)
	}
	if len(last.Shards) != 1 || aws.ToString(last.Shards[0].ShardId) != "shardId-000000000002" || aws.ToString(last.NextToken) != "" {
		t.Fatalf("ListShards(last page) = %#v, want final shard without token", last)
	}
}

func TestKinesisCompatibilityAdapterExpiresListShardsNextToken(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisClock(func() time.Time { return now })))
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("expired-list-token"), ShardCount: aws.Int32(2)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	page, err := client.ListShards(ctx, &kinesis.ListShardsInput{StreamName: aws.String("expired-list-token"), MaxResults: aws.Int32(1)})
	if err != nil || aws.ToString(page.NextToken) == "" {
		t.Fatalf("ListShards() = %#v, %v; want page token", page, err)
	}
	now = now.Add(5 * time.Minute)
	_, err = client.ListShards(ctx, &kinesis.ListShardsInput{NextToken: page.NextToken})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ExpiredNextTokenException" {
		t.Fatalf("ListShards(expired token) error = %v, want ExpiredNextTokenException", err)
	}
}

func TestKinesisCompatibilityAdapterAuthorizesListShardsContinuationOnTokenStream(t *testing.T) {
	const streamName = "authorized-list-token"
	streamARN := "arn:aws:kinesis:us-east-1:000000000000:stream/" + streamName
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
		Effect:    authz.Allow,
		Actions:   []string{"kinesis:CreateStream", "kinesis:ListShards"},
		Resources: []string{streamARN},
	}))))
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String(streamName), ShardCount: aws.Int32(2)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	page, err := client.ListShards(ctx, &kinesis.ListShardsInput{StreamName: aws.String(streamName), MaxResults: aws.Int32(1)})
	if err != nil || aws.ToString(page.NextToken) == "" {
		t.Fatalf("ListShards(first page) = %#v, %v; want authorized token", page, err)
	}
	if _, err := client.ListShards(ctx, &kinesis.ListShardsInput{NextToken: page.NextToken}); err != nil {
		t.Fatalf("ListShards(continuation) error = %v, want token-bound stream authorization", err)
	}
}

func TestKinesisCompatibilityAdapterValidatesListShardsUnsupportedAndTokenParameters(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	const streamName = "list-parameter-validation"
	streamARN := "arn:aws:kinesis:us-east-1:000000000000:stream/" + streamName
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String(streamName), ShardCount: aws.Int32(2)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	page, err := client.ListShards(ctx, &kinesis.ListShardsInput{StreamName: aws.String(streamName), MaxResults: aws.Int32(1)})
	if err != nil || aws.ToString(page.NextToken) == "" {
		t.Fatalf("ListShards(first page) = %#v, %v; want token", page, err)
	}
	_, err = client.ListShards(ctx, &kinesis.ListShardsInput{NextToken: page.NextToken, ExclusiveStartShardId: aws.String("shardId-000000000000")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
		t.Fatalf("ListShards(token plus exclusive start) error = %v, want InvalidArgumentException", err)
	}
	if _, err := client.ListShards(ctx, &kinesis.ListShardsInput{NextToken: page.NextToken, StreamARN: aws.String(streamARN)}); err != nil {
		t.Fatalf("ListShards(token plus matching ARN) error = %v, want successful continuation", err)
	}
}

func TestKinesisCompatibilityAdapterFiltersListShardsAcrossResharding(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisClock(func() time.Time { return now })))
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("filtered-shards"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	now = now.Add(time.Hour + time.Millisecond)
	if _, err := client.SplitShard(ctx, &kinesis.SplitShardInput{StreamName: aws.String("filtered-shards"), ShardToSplit: aws.String("shardId-000000000000"), NewStartingHashKey: aws.String("170141183460469231731687303715884105728")}); err != nil {
		t.Fatalf("SplitShard() error = %v", err)
	}
	for _, tc := range []struct {
		name   string
		filter types.ShardFilter
		want   []string
	}{
		{name: "at latest", filter: types.ShardFilter{Type: types.ShardFilterTypeAtLatest}, want: []string{"shardId-000000000001", "shardId-000000000002"}},
		{name: "at trim horizon", filter: types.ShardFilter{Type: types.ShardFilterTypeAtTrimHorizon}, want: []string{"shardId-000000000000"}},
		{name: "at fractional creation timestamp", filter: types.ShardFilter{Type: types.ShardFilterTypeAtTimestamp, Timestamp: aws.Time(now.Add(-time.Hour + 500*time.Millisecond))}, want: []string{"shardId-000000000000"}},
		{name: "at exact reshard millisecond", filter: types.ShardFilter{Type: types.ShardFilterTypeAtTimestamp, Timestamp: aws.Time(now)}, want: []string{"shardId-000000000000", "shardId-000000000001", "shardId-000000000002"}},
		{name: "from reshard timestamp", filter: types.ShardFilter{Type: types.ShardFilterTypeFromTimestamp, Timestamp: aws.Time(now)}, want: []string{"shardId-000000000000", "shardId-000000000001", "shardId-000000000002"}},
		{name: "after parent", filter: types.ShardFilter{Type: types.ShardFilterTypeAfterShardId, ShardId: aws.String("shardId-000000000000")}, want: []string{"shardId-000000000001", "shardId-000000000002"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := client.ListShards(ctx, &kinesis.ListShardsInput{StreamName: aws.String("filtered-shards"), ShardFilter: &tc.filter})
			if err != nil {
				t.Fatalf("ListShards() error = %v", err)
			}
			got := make([]string, 0, len(result.Shards))
			for _, shard := range result.Shards {
				got = append(got, aws.ToString(shard.ShardId))
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("ListShards() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestKinesisCompatibilityAdapterSelectsListShardsByStreamCreationTimestamp(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 500*int(time.Millisecond), time.UTC)
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisClock(func() time.Time { return now })))
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("timestamped-stream"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	if _, err := client.ListShards(ctx, &kinesis.ListShardsInput{StreamName: aws.String("timestamped-stream"), StreamCreationTimestamp: aws.Time(now)}); err != nil {
		t.Fatalf("ListShards(matching creation timestamp) error = %v", err)
	}
	_, err := client.ListShards(ctx, &kinesis.ListShardsInput{StreamName: aws.String("timestamped-stream"), StreamCreationTimestamp: aws.Time(now.Add(time.Millisecond))})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("ListShards(mismatched creation timestamp) error = %v, want ResourceNotFoundException", err)
	}
}

func TestKinesisCompatibilityAdapterRejectsInvalidPutRecordsAtomically(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("batch-validation"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	_, err := client.PutRecords(ctx, &kinesis.PutRecordsInput{StreamName: aws.String("batch-validation"), Records: []types.PutRecordsRequestEntry{
		{PartitionKey: aws.String("valid"), Data: []byte("must not persist")},
		{PartitionKey: aws.String("invalid"), ExplicitHashKey: aws.String("01"), Data: []byte("bad hash")},
	}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
		t.Fatalf("PutRecords(invalid entry) error = %v, want InvalidArgumentException", err)
	}
	iterator, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String("batch-validation"), ShardId: aws.String("shardId-000000000000"), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	read, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterator.ShardIterator})
	if err != nil || len(read.Records) != 0 {
		t.Fatalf("GetRecords() = %#v, %v; want no records after rejected batch", read.Records, err)
	}
}

func TestKinesisCompatibilityAdapterHonorsShardIteratorStartPositions(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("positions"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	first, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("positions"), PartitionKey: aws.String("one"), Data: []byte("first")})
	if err != nil {
		t.Fatalf("PutRecord(first) error = %v", err)
	}
	if _, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("positions"), PartitionKey: aws.String("two"), Data: []byte("second")}); err != nil {
		t.Fatalf("PutRecord(second) error = %v", err)
	}

	for _, tc := range []struct {
		name      string
		iterator  types.ShardIteratorType
		sequence  *string
		timestamp *time.Time
		want      []string
	}{
		{name: "trim horizon", iterator: types.ShardIteratorTypeTrimHorizon, want: []string{"first", "second"}},
		{name: "latest", iterator: types.ShardIteratorTypeLatest, want: []string{}},
		{name: "at sequence number", iterator: types.ShardIteratorTypeAtSequenceNumber, sequence: first.SequenceNumber, want: []string{"first", "second"}},
		{name: "after sequence number", iterator: types.ShardIteratorTypeAfterSequenceNumber, sequence: first.SequenceNumber, want: []string{"second"}},
		{name: "at timestamp before trim horizon", iterator: types.ShardIteratorTypeAtTimestamp, timestamp: aws.Time(time.Unix(0, 0)), want: []string{"first", "second"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			iter, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{
				StreamName:             aws.String("positions"),
				ShardId:                aws.String("shardId-000000000000"),
				ShardIteratorType:      tc.iterator,
				StartingSequenceNumber: tc.sequence,
				Timestamp:              tc.timestamp,
			})
			if err != nil {
				t.Fatalf("GetShardIterator() error = %v", err)
			}
			records, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iter.ShardIterator})
			if err != nil {
				t.Fatalf("GetRecords() error = %v", err)
			}
			got := make([]string, 0, len(records.Records))
			for _, record := range records.Records {
				got = append(got, string(record.Data))
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("GetRecords(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name      string
		iterator  types.ShardIteratorType
		sequence  *string
		timestamp *time.Time
	}{
		{name: "missing at sequence number", iterator: types.ShardIteratorTypeAtSequenceNumber},
		{name: "unknown sequence number", iterator: types.ShardIteratorTypeAfterSequenceNumber, sequence: aws.String("999")},
		{name: "missing timestamp", iterator: types.ShardIteratorTypeAtTimestamp},
		{name: "unknown iterator type", iterator: types.ShardIteratorType("UNKNOWN")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{
				StreamName:             aws.String("positions"),
				ShardId:                aws.String("shardId-000000000000"),
				ShardIteratorType:      tc.iterator,
				StartingSequenceNumber: tc.sequence,
				Timestamp:              tc.timestamp,
			})
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
				t.Fatalf("GetShardIterator(%s) error = %v, want InvalidArgumentException", tc.name, err)
			}
		})
	}
}

func TestKinesisCompatibilityAdapterScopesAndExpiresShardIterators(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisClock(func() time.Time { return now })))
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("scoped"), ShardCount: aws.Int32(2)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}

	byShard := map[string][]string{}
	for index := 0; len(byShard) < 2 && index < 100; index++ {
		payload := fmt.Sprintf("record-%d", index)
		put, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("scoped"), PartitionKey: aws.String(fmt.Sprintf("key-%d", index)), Data: []byte(payload)})
		if err != nil {
			t.Fatalf("PutRecord(%d) error = %v", index, err)
		}
		byShard[aws.ToString(put.ShardId)] = append(byShard[aws.ToString(put.ShardId)], payload)
	}
	if len(byShard) != 2 {
		t.Fatalf("PutRecord() shard distribution = %#v, want two shards", byShard)
	}

	var iterator *string
	for shardID, want := range byShard {
		iter, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String("scoped"), ShardId: aws.String(shardID), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
		if err != nil {
			t.Fatalf("GetShardIterator(%s) error = %v", shardID, err)
		}
		records, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iter.ShardIterator})
		if err != nil {
			t.Fatalf("GetRecords(%s) error = %v", shardID, err)
		}
		got := make([]string, 0, len(records.Records))
		for _, record := range records.Records {
			got = append(got, string(record.Data))
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("GetRecords(%s) = %v, want only %v", shardID, got, want)
		}
		iterator = iter.ShardIterator
	}

	now = now.Add(5*time.Minute + time.Nanosecond)
	_, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterator})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ExpiredIteratorException" {
		t.Fatalf("GetRecords(expired iterator) error = %v, want ExpiredIteratorException", err)
	}
}

func TestKinesisCompatibilityAdapterRoutesRecordsByMD5HashRanges(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("hashed"), ShardCount: aws.Int32(2)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}

	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
	middle := new(big.Int).Div(max, big.NewInt(2))
	for _, key := range []string{"alpha", "bravo", "charlie"} {
		digest := md5.Sum([]byte(key))
		expectedShard := "shardId-000000000000"
		if new(big.Int).SetBytes(digest[:]).Cmp(middle) > 0 {
			expectedShard = "shardId-000000000001"
		}
		put, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("hashed"), PartitionKey: aws.String(key), Data: []byte(key)})
		if err != nil {
			t.Fatalf("PutRecord(%s) error = %v", key, err)
		}
		if aws.ToString(put.ShardId) != expectedShard {
			t.Fatalf("PutRecord(%s) shard = %q, want MD5-routed %q", key, aws.ToString(put.ShardId), expectedShard)
		}
	}
	for _, tc := range []struct {
		hash      string
		wantShard string
	}{
		{hash: "0", wantShard: "shardId-000000000000"},
		{hash: max.String(), wantShard: "shardId-000000000001"},
	} {
		put, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("hashed"), PartitionKey: aws.String("same-key"), ExplicitHashKey: aws.String(tc.hash), Data: []byte(tc.hash)})
		if err != nil {
			t.Fatalf("PutRecord(explicit %s) error = %v", tc.hash, err)
		}
		if aws.ToString(put.ShardId) != tc.wantShard {
			t.Fatalf("PutRecord(explicit %s) shard = %q, want %q", tc.hash, aws.ToString(put.ShardId), tc.wantShard)
		}
	}
}

func TestKinesisCompatibilityAdapterPreservesHashRoutingAcrossResharding(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("resharded"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	splitAt := "170141183460469231731687303715884105728"
	if _, err := client.SplitShard(ctx, &kinesis.SplitShardInput{StreamName: aws.String("resharded"), ShardToSplit: aws.String("shardId-000000000000"), NewStartingHashKey: aws.String(splitAt)}); err != nil {
		t.Fatalf("SplitShard() error = %v", err)
	}
	described, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{StreamName: aws.String("resharded")})
	if err != nil || len(described.StreamDescription.Shards) != 2 {
		t.Fatalf("DescribeStream(after split) = %#v, %v; want two shards", described, err)
	}
	lowShard := aws.ToString(described.StreamDescription.Shards[0].ShardId)
	highShard := aws.ToString(described.StreamDescription.Shards[1].ShardId)
	max := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1)).String()
	for _, tc := range []struct {
		hash      string
		wantShard string
	}{{hash: "0", wantShard: lowShard}, {hash: max, wantShard: highShard}} {
		put, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("resharded"), PartitionKey: aws.String("stable"), ExplicitHashKey: aws.String(tc.hash), Data: []byte(tc.hash)})
		if err != nil || aws.ToString(put.ShardId) != tc.wantShard {
			t.Fatalf("PutRecord(%s after split) = %#v, %v; want shard %q", tc.hash, put, err, tc.wantShard)
		}
	}
	if _, err := client.MergeShards(ctx, &kinesis.MergeShardsInput{StreamName: aws.String("resharded"), ShardToMerge: aws.String(lowShard), AdjacentShardToMerge: aws.String(highShard)}); err != nil {
		t.Fatalf("MergeShards() error = %v", err)
	}
	described, err = client.DescribeStream(ctx, &kinesis.DescribeStreamInput{StreamName: aws.String("resharded")})
	if err != nil || len(described.StreamDescription.Shards) != 1 {
		t.Fatalf("DescribeStream(after merge) = %#v, %v; want one shard", described, err)
	}
	mergedShard := aws.ToString(described.StreamDescription.Shards[0].ShardId)
	for _, hash := range []string{"0", max} {
		put, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("resharded"), PartitionKey: aws.String("stable"), ExplicitHashKey: aws.String(hash), Data: []byte(hash)})
		if err != nil || aws.ToString(put.ShardId) != mergedShard {
			t.Fatalf("PutRecord(%s after merge) = %#v, %v; want shard %q", hash, put, err, mergedShard)
		}
	}
}

func TestKinesisCompatibilityAdapterValidatesHashKeysAndRetainsClosedShardIterators(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("validated"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	for _, hash := range []string{"", "+1", "001", "340282366920938463463374607431768211456"} {
		_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("validated"), PartitionKey: aws.String("key"), ExplicitHashKey: aws.String(hash), Data: []byte("invalid")})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
			t.Fatalf("PutRecord(ExplicitHashKey=%q) error = %v, want InvalidArgumentException", hash, err)
		}
	}
	put, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("validated"), PartitionKey: aws.String("key"), Data: []byte("before-split")})
	if err != nil {
		t.Fatalf("PutRecord(before split) error = %v", err)
	}
	_, err = client.SplitShard(ctx, &kinesis.SplitShardInput{StreamName: aws.String("validated"), ShardToSplit: aws.String("shardId-000000000000"), NewStartingHashKey: aws.String("001")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
		t.Fatalf("SplitShard(invalid key) error = %v, want InvalidArgumentException", err)
	}
	described, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{StreamName: aws.String("validated")})
	if err != nil || len(described.StreamDescription.Shards) != 1 {
		t.Fatalf("DescribeStream(after invalid split) = %#v, %v; want original shard", described, err)
	}
	if _, err := client.SplitShard(ctx, &kinesis.SplitShardInput{StreamName: aws.String("validated"), ShardToSplit: aws.String("shardId-000000000000"), NewStartingHashKey: aws.String("170141183460469231731687303715884105728")}); err != nil {
		t.Fatalf("SplitShard(valid key) error = %v", err)
	}
	iterator, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String("validated"), ShardId: aws.String("shardId-000000000000"), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
	if err != nil {
		t.Fatalf("GetShardIterator(closed parent) error = %v", err)
	}
	records, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterator.ShardIterator})
	if err != nil || len(records.Records) != 1 || string(records.Records[0].Data) != "before-split" {
		t.Fatalf("GetRecords(closed parent) = %#v, %v; want original record from %s", records, err, aws.ToString(put.ShardId))
	}
}

func TestKinesisCompatibilityAdapterRejectsNonFiniteShardIteratorTimestamp(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String("timestamps"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}

	for _, timestamp := range []string{"NaN", "+Inf", "-Inf"} {
		t.Run(timestamp, func(t *testing.T) {
			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader(fmt.Sprintf(`{"StreamName":"timestamps","ShardId":"shardId-000000000000","ShardIteratorType":"AT_TIMESTAMP","Timestamp":"%s"}`, timestamp)))
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			request.Header.Set("Content-Type", "application/x-amz-json-1.1")
			request.Header.Set("X-Amz-Target", "Kinesis_20131202.GetShardIterator")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			defer response.Body.Close()
			var body map[string]string
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if response.StatusCode != http.StatusBadRequest || body["__type"] != "InvalidArgumentException" {
				t.Fatalf("GetShardIterator(%s) = status %d body %#v, want InvalidArgumentException", timestamp, response.StatusCode, body)
			}
		})
	}
}

func TestKinesisCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewKinesisAdapter())
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

	runAWS("kinesis", "create-stream", "--stream-name", "cli-events", "--shard-count", "1")

	var described struct {
		StreamDescription struct {
			StreamName   string `json:"StreamName"`
			StreamStatus string `json:"StreamStatus"`
			Shards       []struct {
				ShardID string `json:"ShardId"`
			} `json:"Shards"`
		} `json:"StreamDescription"`
	}
	if err := json.Unmarshal(runAWS("kinesis", "describe-stream", "--stream-name", "cli-events"), &described); err != nil {
		t.Fatalf("decode describe-stream output: %v", err)
	}
	if described.StreamDescription.StreamName != "cli-events" || described.StreamDescription.StreamStatus != "ACTIVE" || len(described.StreamDescription.Shards) != 1 {
		t.Fatalf("describe-stream = %#v, want active one-shard stream", described.StreamDescription)
	}

	var put struct {
		SequenceNumber string `json:"SequenceNumber"`
	}
	if err := json.Unmarshal(runAWS("kinesis", "put-record",
		"--stream-name", "cli-events",
		"--partition-key", "one",
		"--data", "hello",
		"--cli-binary-format", "raw-in-base64-out",
	), &put); err != nil {
		t.Fatalf("decode put-record output: %v", err)
	}
	if put.SequenceNumber == "" {
		t.Fatal("put-record returned empty SequenceNumber")
	}

	var iter struct {
		ShardIterator string `json:"ShardIterator"`
	}
	if err := json.Unmarshal(runAWS("kinesis", "get-shard-iterator",
		"--stream-name", "cli-events",
		"--shard-id", described.StreamDescription.Shards[0].ShardID,
		"--shard-iterator-type", "TRIM_HORIZON",
	), &iter); err != nil {
		t.Fatalf("decode get-shard-iterator output: %v", err)
	}
	if iter.ShardIterator == "" {
		t.Fatal("get-shard-iterator returned empty ShardIterator")
	}

	var records struct {
		Records []struct {
			Data         string `json:"Data"`
			PartitionKey string `json:"PartitionKey"`
		} `json:"Records"`
	}
	if err := json.Unmarshal(runAWS("kinesis", "get-records", "--shard-iterator", iter.ShardIterator), &records); err != nil {
		t.Fatalf("decode get-records output: %v", err)
	}
	if len(records.Records) != 1 || records.Records[0].PartitionKey != "one" || kinesisCLIData(records.Records[0].Data) != "hello" {
		t.Fatalf("get-records = %#v, want one hello record", records.Records)
	}
}

func TestKinesisCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewKinesisAdapter())
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
    kinesis = %q
  }
}

resource "aws_kinesis_stream" "deploy" {
  name             = "terraform-events"
  shard_count      = 1
  retention_period = 24
  tags = {
    env = "test"
  }
}

output "stream_arn" {
  value = aws_kinesis_stream.deploy.arn
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

	if arn := strings.TrimSpace(string(runTerraform("output", "-raw", "stream_arn"))); arn == "" {
		t.Fatalf("terraform output stream_arn is empty")
	}
}

func kinesisCLIData(value string) string {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err == nil {
		return string(decoded)
	}
	return value
}

func TestKinesisCompatibilityAdapterReturnsLimitExceededWhenStreamQuotaIsExceeded(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisStreamQuota(1)))
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events-a"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream(first) error = %v", err)
	}
	_, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events-b"), ShardCount: aws.Int32(1)})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("CreateStream(over quota) error = %v, want LimitExceededException", err)
	}
}

func TestKinesisCompatibilityAdapterReturnsProviderErrorsForDuplicateAndMissingStreams(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream(first) error = %v", err)
	}

	for name, call := range map[string]struct {
		run  func() error
		code string
	}{
		"CreateStream duplicate": {
			run: func() error {
				_, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)})
				return err
			},
			code: "ResourceInUseException",
		},
		"GetShardIterator missing stream": {
			run: func() error {
				_, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{
					StreamName:        aws.String("missing"),
					ShardId:           aws.String("shardId-000000000000"),
					ShardIteratorType: types.ShardIteratorTypeTrimHorizon,
				})
				return err
			},
			code: "ResourceNotFoundException",
		},
		"GetShardIterator missing shard": {
			run: func() error {
				_, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{
					StreamName:        aws.String("events"),
					ShardId:           aws.String("shardId-999999999999"),
					ShardIteratorType: types.ShardIteratorTypeTrimHorizon,
				})
				return err
			},
			code: "ResourceNotFoundException",
		},
	} {
		t.Run(name, func(t *testing.T) {
			var apiErr smithy.APIError
			if err := call.run(); err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != call.code {
				t.Fatalf("%s error = %v, want %s", name, err, call.code)
			}
		})
	}
}

func TestKinesisCompatibilityAdapterRejectsMissingRequiredStreamName(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.CreateStream(context.Background(), &kinesis.CreateStreamInput{
		StreamName: aws.String(""),
		ShardCount: aws.Int32(1),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
		t.Fatalf("CreateStream(empty name) error = %v, want InvalidArgumentException", err)
	}

	listed, err := client.ListStreams(context.Background(), &kinesis.ListStreamsInput{})
	if err != nil {
		t.Fatalf("ListStreams() error = %v", err)
	}
	if len(listed.StreamNames) != 0 {
		t.Fatalf("ListStreams() = %v, want no streams after rejected create", listed.StreamNames)
	}
}

func TestKinesisCompatibilityAdapterRejectsMissingRequiredPartitionKey(t *testing.T) {
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

	_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName:   aws.String("events"),
		PartitionKey: aws.String(""),
		Data:         []byte("invalid"),
	})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
		t.Fatalf("PutRecord(empty partition key) error = %v, want InvalidArgumentException", err)
	}

	iter, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{
		StreamName:        aws.String("events"),
		ShardId:           aws.String("shardId-000000000000"),
		ShardIteratorType: types.ShardIteratorTypeTrimHorizon,
	})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	records, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iter.ShardIterator})
	if err != nil {
		t.Fatalf("GetRecords() error = %v", err)
	}
	if len(records.Records) != 0 {
		t.Fatalf("GetRecords() returned %d records after rejected PutRecord", len(records.Records))
	}
}

func TestKinesisCompatibilityAdapterRejectsInvalidCreateStreamShape(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	for _, tc := range []struct {
		name  string
		input kinesis.CreateStreamInput
	}{
		{name: "zero shard count", input: kinesis.CreateStreamInput{StreamName: aws.String("zero-shards"), ShardCount: aws.Int32(0)}},
		{name: "overlong stream name", input: kinesis.CreateStreamInput{StreamName: aws.String(strings.Repeat("x", 129)), ShardCount: aws.Int32(1)}},
		{name: "invalid stream name character", input: kinesis.CreateStreamInput{StreamName: aws.String("has space"), ShardCount: aws.Int32(1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.CreateStream(context.Background(), &tc.input)
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
				t.Fatalf("CreateStream() error = %v, want InvalidArgumentException", err)
			}
		})
	}
	listed, err := client.ListStreams(context.Background(), &kinesis.ListStreamsInput{})
	if err != nil || len(listed.StreamNames) != 0 {
		t.Fatalf("ListStreams(after invalid creates) = %#v, %v; want no streams", listed.StreamNames, err)
	}
}

func TestKinesisCompatibilityAdapterEnforcesPerStreamShardQuota(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisShardQuota(1)))
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String("too-many-shards"), ShardCount: aws.Int32(2)})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("CreateStream(over shard quota) error = %v, want LimitExceededException", err)
	}
	listed, err := client.ListStreams(context.Background(), &kinesis.ListStreamsInput{})
	if err != nil || len(listed.StreamNames) != 0 {
		t.Fatalf("ListStreams(after quota rejection) = %#v, %v; want no streams", listed.StreamNames, err)
	}
}

func TestKinesisCompatibilityAdapterEnforcesShardQuotaDuringSplit(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisShardQuota(1)))
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	_, err := client.SplitShard(ctx, &kinesis.SplitShardInput{StreamName: aws.String("events"), ShardToSplit: aws.String("shardId-000000000000"), NewStartingHashKey: aws.String("170141183460469231731687303715884105728")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("SplitShard(over quota) error = %v, want LimitExceededException", err)
	}
	described, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{StreamName: aws.String("events")})
	if err != nil || len(described.StreamDescription.Shards) != 1 {
		t.Fatalf("DescribeStream(after quota rejection) = %#v, %v; want original shard", described, err)
	}
}

func TestKinesisCompatibilityAdapterValidatesSplitBeforeCheckingShardQuota(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisShardQuota(1)))
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	for _, tc := range []struct {
		name  string
		shard string
		key   string
		code  string
	}{
		{name: "missing shard", shard: "missing", key: "1", code: "ResourceNotFoundException"},
		{name: "invalid split key", shard: "shardId-000000000000", key: "001", code: "InvalidArgumentException"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.SplitShard(ctx, &kinesis.SplitShardInput{StreamName: aws.String("events"), ShardToSplit: aws.String(tc.shard), NewStartingHashKey: aws.String(tc.key)})
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != tc.code {
				t.Fatalf("SplitShard() error = %v, want %s", err, tc.code)
			}
		})
	}
}

func TestKinesisCompatibilityAdapterRejectsOverlongPartitionKey(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	_, err := client.PutRecord(context.Background(), &kinesis.PutRecordInput{StreamName: aws.String("events"), PartitionKey: aws.String(strings.Repeat("x", 257)), Data: []byte("invalid")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
		t.Fatalf("PutRecord(overlong partition key) error = %v, want InvalidArgumentException", err)
	}
}

func TestKinesisCompatibilityAdapterEnforcesDecodedRecordSizeLimit(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	if _, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("events"), PartitionKey: aws.String("k"), Data: bytes.Repeat([]byte("x"), 10*1024*1024-1)}); err != nil {
		t.Fatalf("PutRecord(at exact combined limit) error = %v", err)
	}
	_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("events"), PartitionKey: aws.String("k"), Data: bytes.Repeat([]byte("x"), 10*1024*1024)})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
		t.Fatalf("PutRecord(over combined limit) error = %v, want InvalidArgumentException", err)
	}
	iterator, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String("events"), ShardId: aws.String("shardId-000000000000"), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	records, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterator.ShardIterator})
	if err != nil || len(records.Records) != 1 {
		t.Fatalf("GetRecords(after rejected oversized record) = %d records, %v; want one", len(records.Records), err)
	}
}

func TestKinesisCompatibilityAdapterRejectsMalformedBase64RecordData(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader(`{"StreamName":"events","PartitionKey":"key","Data":"not valid base64!"}`))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/x-amz-json-1.1")
	request.Header.Set("X-Amz-Target", "Kinesis_20131202.PutRecord")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if response.StatusCode != http.StatusBadRequest || body["__type"] != "InvalidArgumentException" {
		t.Fatalf("PutRecord(malformed base64) = status %d body %#v, want InvalidArgumentException", response.StatusCode, body)
	}
}

func TestKinesisCompatibilityAdapterRequiresStringDataMember(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	for _, tc := range []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "explicit empty data", body: `{"StreamName":"events","PartitionKey":"key","Data":""}`, wantStatus: http.StatusOK},
		{name: "missing data", body: `{"StreamName":"events","PartitionKey":"key"}`, wantStatus: http.StatusBadRequest},
		{name: "non string data", body: `{"StreamName":"events","PartitionKey":"key","Data":1}`, wantStatus: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader(tc.body))
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			request.Header.Set("Content-Type", "application/x-amz-json-1.1")
			request.Header.Set("X-Amz-Target", "Kinesis_20131202.PutRecord")
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.wantStatus {
				t.Fatalf("PutRecord(%s) status = %d, want %d", tc.name, response.StatusCode, tc.wantStatus)
			}
		})
	}
}

func TestKinesisCompatibilityAdapterPaginatesListStreams(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	for _, name := range []string{"events-a", "events-b", "events-c"} {
		if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String(name), ShardCount: aws.Int32(1)}); err != nil {
			t.Fatalf("CreateStream(%s) error = %v", name, err)
		}
	}

	first, err := client.ListStreams(ctx, &kinesis.ListStreamsInput{Limit: aws.Int32(2)})
	if err != nil {
		t.Fatalf("ListStreams(first) error = %v", err)
	}
	if len(first.StreamNames) != 2 || !aws.ToBool(first.HasMoreStreams) {
		t.Fatalf("ListStreams(first) = %#v, want two streams and hasMore", first)
	}

	second, err := client.ListStreams(ctx, &kinesis.ListStreamsInput{
		Limit:                    aws.Int32(2),
		ExclusiveStartStreamName: aws.String(first.StreamNames[1]),
	})
	if err != nil {
		t.Fatalf("ListStreams(second) error = %v", err)
	}
	if len(second.StreamNames) != 1 || aws.ToBool(second.HasMoreStreams) {
		t.Fatalf("ListStreams(second) = %#v, want final stream", second)
	}
}

func TestKinesisCompatibilityAdapterRejectsInvalidListStreamsLimit(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	_, err := client.ListStreams(context.Background(), &kinesis.ListStreamsInput{Limit: aws.Int32(101)})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
		t.Fatalf("ListStreams(invalid limit) error = %v, want InvalidArgumentException", err)
	}
}

func TestKinesisCompatibilityAdapterAdvancesGetRecordsIterator(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	for _, value := range []string{"one", "two"} {
		if _, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
			StreamName:   aws.String("events"),
			PartitionKey: aws.String(value),
			Data:         []byte(value),
		}); err != nil {
			t.Fatalf("PutRecord(%s) error = %v", value, err)
		}
	}
	iter, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{
		StreamName:        aws.String("events"),
		ShardId:           aws.String("shardId-000000000000"),
		ShardIteratorType: types.ShardIteratorTypeTrimHorizon,
	})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	first, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iter.ShardIterator, Limit: aws.Int32(1)})
	if err != nil {
		t.Fatalf("GetRecords(first) error = %v", err)
	}
	if len(first.Records) != 1 || string(first.Records[0].Data) != "one" {
		t.Fatalf("GetRecords(first) = %#v, want one", first.Records)
	}
	second, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: first.NextShardIterator, Limit: aws.Int32(1)})
	if err != nil {
		t.Fatalf("GetRecords(second) error = %v", err)
	}
	if len(second.Records) != 1 || string(second.Records[0].Data) != "two" {
		t.Fatalf("GetRecords(second) = %#v, want two", second.Records)
	}
}

func TestKinesisCompatibilityAdapterRejectsInvalidGetRecordsLimit(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	if _, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("events"), PartitionKey: aws.String("key"), Data: []byte("record")}); err != nil {
		t.Fatalf("PutRecord() error = %v", err)
	}
	for _, limit := range []int32{0, -1, 10001} {
		iterator, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String("events"), ShardId: aws.String("shardId-000000000000"), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
		if err != nil {
			t.Fatalf("GetShardIterator() error = %v", err)
		}
		_, err = client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterator.ShardIterator, Limit: aws.Int32(limit)})
		var apiErr smithy.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
			t.Fatalf("GetRecords(Limit=%d) error = %v, want InvalidArgumentException", limit, err)
		}
	}
}

func TestKinesisCompatibilityAdapterReturnsRecordArrivalMetadata(t *testing.T) {
	now := time.Date(2026, time.July, 21, 12, 0, 0, 123456789, time.UTC)
	firstArrival := now.Truncate(time.Millisecond)
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisClock(func() time.Time { return now })))
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	if _, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("events"), PartitionKey: aws.String("key"), Data: []byte("first")}); err != nil {
		t.Fatalf("PutRecord(first) error = %v", err)
	}
	now = now.Add(time.Second)
	if _, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("events"), PartitionKey: aws.String("key"), Data: []byte("second")}); err != nil {
		t.Fatalf("PutRecord(second) error = %v", err)
	}
	iterator, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String("events"), ShardId: aws.String("shardId-000000000000"), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	records, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterator.ShardIterator, Limit: aws.Int32(1)})
	if err != nil || len(records.Records) != 1 {
		t.Fatalf("GetRecords() = %#v, %v; want one record", records, err)
	}
	if records.Records[0].ApproximateArrivalTimestamp == nil || !records.Records[0].ApproximateArrivalTimestamp.Equal(firstArrival) {
		t.Fatalf("GetRecords() arrival timestamp = %v, want %v", records.Records[0].ApproximateArrivalTimestamp, firstArrival)
	}
	if aws.ToInt64(records.MillisBehindLatest) != 1000 {
		t.Fatalf("GetRecords() MillisBehindLatest = %d, want 1000", aws.ToInt64(records.MillisBehindLatest))
	}
}

func TestKinesisCompatibilityAdapterEndsClosedShardIterator(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	if _, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("events"), PartitionKey: aws.String("key"), Data: []byte("before-split")}); err != nil {
		t.Fatalf("PutRecord() error = %v", err)
	}
	if _, err := client.SplitShard(ctx, &kinesis.SplitShardInput{StreamName: aws.String("events"), ShardToSplit: aws.String("shardId-000000000000"), NewStartingHashKey: aws.String("170141183460469231731687303715884105728")}); err != nil {
		t.Fatalf("SplitShard() error = %v", err)
	}
	iterator, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String("events"), ShardId: aws.String("shardId-000000000000"), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
	if err != nil {
		t.Fatalf("GetShardIterator(closed parent) error = %v", err)
	}
	records, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterator.ShardIterator})
	if err != nil || len(records.Records) != 1 {
		t.Fatalf("GetRecords(closed parent) = %#v, %v; want final record", records, err)
	}
	if records.NextShardIterator != nil {
		t.Fatalf("GetRecords(closed parent) NextShardIterator = %q, want nil", aws.ToString(records.NextShardIterator))
	}
}

func TestKinesisCompatibilityAdapterRejectsNonIntegerGetRecordsLimit(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	iterator, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String("events"), ShardId: aws.String("shardId-000000000000"), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	for _, limit := range []string{"1.5", "10000.1", `"1"`} {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, strings.NewReader(fmt.Sprintf(`{"ShardIterator":%q,"Limit":%s}`, aws.ToString(iterator.ShardIterator), limit)))
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		request.Header.Set("Content-Type", "application/x-amz-json-1.1")
		request.Header.Set("X-Amz-Target", "Kinesis_20131202.GetRecords")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("Do() error = %v", err)
		}
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			response.Body.Close()
			t.Fatalf("Decode() error = %v", err)
		}
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || body["__type"] != "InvalidArgumentException" {
			t.Fatalf("GetRecords(Limit=%s) = status %d body %#v, want InvalidArgumentException", limit, response.StatusCode, body)
		}
	}
}

func TestKinesisCompatibilityAdapterCapsGetRecordsResponseDataAt10MiB(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	data := bytes.Repeat([]byte("x"), 6*1024*1024)
	for index := 0; index < 2; index++ {
		if _, err := client.PutRecord(ctx, &kinesis.PutRecordInput{StreamName: aws.String("events"), PartitionKey: aws.String("key"), Data: data}); err != nil {
			t.Fatalf("PutRecord(%d) error = %v", index, err)
		}
	}
	iterator, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{StreamName: aws.String("events"), ShardId: aws.String("shardId-000000000000"), ShardIteratorType: types.ShardIteratorTypeTrimHorizon})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	records, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iterator.ShardIterator, Limit: aws.Int32(2)})
	if err != nil || len(records.Records) != 1 || records.NextShardIterator == nil {
		t.Fatalf("GetRecords() count=%d continuation=%t error=%v, want one record and a continuation", len(records.Records), records.NextShardIterator != nil, err)
	}
}

func TestKinesisCompatibilityAdapterUpdatesRetentionPeriod(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	if _, err := client.IncreaseStreamRetentionPeriod(ctx, &kinesis.IncreaseStreamRetentionPeriodInput{
		StreamName:           aws.String("events"),
		RetentionPeriodHours: aws.Int32(48),
	}); err != nil {
		t.Fatalf("IncreaseStreamRetentionPeriod() error = %v", err)
	}
	desc, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{StreamName: aws.String("events")})
	if err != nil {
		t.Fatalf("DescribeStream(after increase) error = %v", err)
	}
	if aws.ToInt32(desc.StreamDescription.RetentionPeriodHours) != 48 {
		t.Fatalf("RetentionPeriodHours after increase = %d, want 48", aws.ToInt32(desc.StreamDescription.RetentionPeriodHours))
	}

	if _, err := client.DecreaseStreamRetentionPeriod(ctx, &kinesis.DecreaseStreamRetentionPeriodInput{
		StreamName:           aws.String("events"),
		RetentionPeriodHours: aws.Int32(24),
	}); err != nil {
		t.Fatalf("DecreaseStreamRetentionPeriod() error = %v", err)
	}
	desc, err = client.DescribeStream(ctx, &kinesis.DescribeStreamInput{StreamName: aws.String("events")})
	if err != nil {
		t.Fatalf("DescribeStream(after decrease) error = %v", err)
	}
	if aws.ToInt32(desc.StreamDescription.RetentionPeriodHours) != 24 {
		t.Fatalf("RetentionPeriodHours after decrease = %d, want 24", aws.ToInt32(desc.StreamDescription.RetentionPeriodHours))
	}
}

func TestKinesisCompatibilityAdapterSplitsAndMergesShards(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	if _, err := client.SplitShard(ctx, &kinesis.SplitShardInput{
		StreamName:         aws.String("events"),
		ShardToSplit:       aws.String("shardId-000000000000"),
		NewStartingHashKey: aws.String("170141183460469231731687303715884105728"),
	}); err != nil {
		t.Fatalf("SplitShard() error = %v", err)
	}
	desc, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{StreamName: aws.String("events")})
	if err != nil {
		t.Fatalf("DescribeStream(after split) error = %v", err)
	}
	if len(desc.StreamDescription.Shards) != 2 {
		t.Fatalf("shards after split = %#v, want two shards", desc.StreamDescription.Shards)
	}

	if _, err := client.MergeShards(ctx, &kinesis.MergeShardsInput{
		StreamName:           aws.String("events"),
		ShardToMerge:         desc.StreamDescription.Shards[0].ShardId,
		AdjacentShardToMerge: desc.StreamDescription.Shards[1].ShardId,
	}); err != nil {
		t.Fatalf("MergeShards() error = %v", err)
	}
	desc, err = client.DescribeStream(ctx, &kinesis.DescribeStreamInput{StreamName: aws.String("events")})
	if err != nil {
		t.Fatalf("DescribeStream(after merge) error = %v", err)
	}
	if len(desc.StreamDescription.Shards) != 1 {
		t.Fatalf("shards after merge = %#v, want one shard", desc.StreamDescription.Shards)
	}
}

func TestKinesisCompatibilityAdapterKeepsShardsOnFailedMerge(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	if _, err := client.CreateStream(ctx, &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(2)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	_, err := client.MergeShards(ctx, &kinesis.MergeShardsInput{StreamName: aws.String("events"), ShardToMerge: aws.String("shardId-000000000000"), AdjacentShardToMerge: aws.String("missing")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ResourceNotFoundException" {
		t.Fatalf("MergeShards(invalid) error = %v, want ResourceNotFoundException", err)
	}
	described, err := client.DescribeStream(ctx, &kinesis.DescribeStreamInput{StreamName: aws.String("events")})
	if err != nil || len(described.StreamDescription.Shards) != 2 {
		t.Fatalf("DescribeStream(after failed merge) = %#v, %v; want two original shards", described, err)
	}
}

func TestKinesisCompatibilityAdapterRejectsMalformedShardIterator(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter())
	defer server.Close()
	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	if _, err := client.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream() error = %v", err)
	}
	_, err := client.GetRecords(context.Background(), &kinesis.GetRecordsInput{ShardIterator: aws.String("events:not-a-position")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidArgumentException" {
		t.Fatalf("GetRecords(malformed iterator) error = %v, want InvalidArgumentException", err)
	}
}

func TestKinesisCompatibilityAdapterAuthorizesCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter(
		compataws.WithKinesisAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"kinesis:CreateStream"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_age", Values: []string{"10m"}},
			},
		})),
	))
	defer server.Close()

	allowed := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "10m"))
	})
	if _, err := allowed.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream(allowed) error = %v", err)
	}

	denied := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Credential-Age", "48h"))
	})
	if _, err := denied.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String("drafts"), ShardCount: aws.Int32(1)}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateStream(denied) error = %v, want AccessDenied", err)
	}
}

func TestKinesisCompatibilityAdapterAuthorizesClaimCondition(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter(
		compataws.WithKinesisAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"kinesis:CreateStream"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "claim:mfa", Values: []string{"true"}},
			},
		})),
	))
	defer server.Close()

	allowed := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "true"))
	})
	if _, err := allowed.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String("events"), ShardCount: aws.Int32(1)}); err != nil {
		t.Fatalf("CreateStream(allowed) error = %v", err)
	}

	denied := kinesis.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *kinesis.Options) {
		o.BaseEndpoint = aws.String(server.URL)
		o.APIOptions = append(o.APIOptions, sqsHeader("X-Homeport-Claim-Mfa", "false"))
	})
	if _, err := denied.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String("drafts"), ShardCount: aws.Int32(1)}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("CreateStream(denied) error = %v, want AccessDenied", err)
	}
}

func TestKinesisCompatibilityAdapterAuthorizesExpiredCredentialAndPrincipalAttributeConditions(t *testing.T) {
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
			server := httptest.NewServer(compataws.NewKinesisAdapter(
				compataws.WithKinesisAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
					Effect:     authz.Allow,
					Actions:    []string{"kinesis:CreateStream"},
					Resources:  []string{"*"},
					Conditions: []authz.Condition{tc.condition},
				})),
			))
			defer server.Close()

			allowedName, allowedValue, _ := strings.Cut(tc.allowed, ":")
			allowed := kinesis.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *kinesis.Options) {
				o.BaseEndpoint = aws.String(server.URL)
				o.APIOptions = append(o.APIOptions, sqsHeader(allowedName, allowedValue))
			})
			if _, err := allowed.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String(tc.name + "-allowed"), ShardCount: aws.Int32(1)}); err != nil {
				t.Fatalf("CreateStream(allowed %s) error = %v", tc.name, err)
			}

			deniedName, deniedValue, _ := strings.Cut(tc.denied, ":")
			denied := kinesis.NewFromConfig(aws.Config{
				Region:      "us-east-1",
				Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
			}, func(o *kinesis.Options) {
				o.BaseEndpoint = aws.String(server.URL)
				o.APIOptions = append(o.APIOptions, sqsHeader(deniedName, deniedValue))
			})
			if _, err := denied.CreateStream(context.Background(), &kinesis.CreateStreamInput{StreamName: aws.String(tc.name + "-denied"), ShardCount: aws.Int32(1)}); err == nil || !strings.Contains(err.Error(), "AccessDenied") {
				t.Fatalf("CreateStream(denied %s) error = %v, want AccessDenied", tc.name, err)
			}
		})
	}
}

func TestKinesisCompatibilityAdapterAuthorizesAndAuditsAWSSDKCalls(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewKinesisAdapter(
		compataws.WithKinesisAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"kinesis:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"kinesis:PutRecord"}, Resources: []string{"*"}},
		)),
		compataws.WithKinesisAuditSink(auditLog.Record),
	))
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
	_, err := client.PutRecord(ctx, &kinesis.PutRecordInput{
		StreamName:   aws.String("events"),
		PartitionKey: aws.String("one"),
		Data:         []byte("blocked"),
	})
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("PutRecord() error = %v, want AccessDenied", err)
	}

	iter, err := client.GetShardIterator(ctx, &kinesis.GetShardIteratorInput{
		StreamName:        aws.String("events"),
		ShardId:           aws.String("shardId-000000000000"),
		ShardIteratorType: types.ShardIteratorTypeTrimHorizon,
	})
	if err != nil {
		t.Fatalf("GetShardIterator() error = %v", err)
	}
	records, err := client.GetRecords(ctx, &kinesis.GetRecordsInput{ShardIterator: iter.ShardIterator})
	if err != nil {
		t.Fatalf("GetRecords() error = %v", err)
	}
	if len(records.Records) != 0 {
		t.Fatalf("GetRecords() returned %d records after denied PutRecord", len(records.Records))
	}

	assertDecision(t, auditLog.Decisions(), "kinesis:CreateStream", true)
	assertDecision(t, auditLog.Decisions(), "kinesis:PutRecord", false)
	assertDecision(t, auditLog.Decisions(), "kinesis:GetRecords", true)
}

func TestKinesisCompatibilityAdapterShapesAuthorizerFailure(t *testing.T) {
	server := httptest.NewServer(compataws.NewKinesisAdapter(compataws.WithKinesisAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
		return authz.Decision{}, errors.New("authorizer unavailable")
	}))))
	defer server.Close()

	client := kinesis.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *kinesis.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.ListStreams(context.Background(), &kinesis.ListStreamsInput{})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InternalFailure" {
		t.Fatalf("ListStreams(authorizer failure) error = %v, want InternalFailure", err)
	}
}
