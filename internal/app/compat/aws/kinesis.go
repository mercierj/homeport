package aws

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/homeport/homeport/internal/domain/authz"
)

type KinesisAdapter struct {
	mu          sync.Mutex
	streams     map[string]*kinesisStream
	iterators   map[string]kinesisIterator
	shardTokens map[string]kinesisShardPageToken
	streamQuota int
	shardQuota  int
	now         func() time.Time
	authorizer  authz.Authorizer
	auditSink   func(authz.Decision)
}

type KinesisOption func(*KinesisAdapter)

type kinesisStream struct {
	Name           string
	CreatedAt      time.Time
	Records        []kinesisRecord
	RetentionHours int
	Shards         []string
	ShardRanges    map[string]kinesisHashRange
	ClosedShards   map[string]bool
	ShardCreatedAt map[string]time.Time
	ShardClosedAt  map[string]time.Time
	NextShard      int
	Tags           map[string]string
}

type kinesisHashRange struct {
	Start *big.Int
	End   *big.Int
}

var (
	kinesisHashKeyPattern    = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,38})$`)
	kinesisStreamNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,128}$`)
)

type kinesisRecord struct {
	Data         string
	PartitionKey string
	Sequence     string
	ShardID      string
	CreatedAt    time.Time
}

type kinesisIterator struct {
	Stream    string
	ShardID   string
	Start     int
	ExpiresAt time.Time
}

type kinesisShardPageToken struct {
	Stream    string
	Start     int
	Shards    []map[string]any
	ExpiresAt time.Time
}

func NewKinesisAdapter(options ...KinesisOption) *KinesisAdapter {
	adapter := &KinesisAdapter{
		streams:     make(map[string]*kinesisStream),
		iterators:   make(map[string]kinesisIterator),
		shardTokens: make(map[string]kinesisShardPageToken),
		shardQuota:  1000,
		now:         time.Now,
		authorizer:  authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithKinesisAuthorizer(authorizer authz.Authorizer) KinesisOption {
	return func(adapter *KinesisAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithKinesisAuditSink(sink func(authz.Decision)) KinesisOption {
	return func(adapter *KinesisAdapter) {
		adapter.auditSink = sink
	}
}

func WithKinesisStreamQuota(maxStreams int) KinesisOption {
	return func(adapter *KinesisAdapter) {
		adapter.streamQuota = maxStreams
	}
}

func WithKinesisShardQuota(maxShards int) KinesisOption {
	return func(adapter *KinesisAdapter) {
		if maxShards > 0 {
			adapter.shardQuota = maxShards
		}
	}
}

func WithKinesisClock(clock func() time.Time) KinesisOption {
	return func(adapter *KinesisAdapter) {
		if clock != nil {
			adapter.now = clock
		}
	}
}

func (KinesisAdapter) Provider() string { return "aws" }
func (KinesisAdapter) Service() string  { return "kinesis" }
func (KinesisAdapter) Routes() []string { return []string{"POST /compat/aws/kinesis"} }
func (KinesisAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_KINESIS": "http://homeport:8080/api/v1/compat/aws/kinesis",
		"HOMEPORT_COMPAT_BACKEND":  "redpanda",
	}
}
func (KinesisAdapter) ConformanceChecks() []string {
	return []string{"create-stream", "delete-stream", "list-streams", "list-shards", "describe-stream", "describe-stream-summary", "list-tags-for-stream", "add-tags-to-stream", "remove-tags-from-stream", "increase-stream-retention-period", "decrease-stream-retention-period", "split-shard", "merge-shards", "put-record", "put-records", "get-shard-iterator", "get-records"}
}

func (a *KinesisAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if !a.authorized(w, r, action, body) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateStream":
		name := stringValue(body["StreamName"])
		if !kinesisStreamNamePattern.MatchString(name) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid stream name"})
			return
		}
		shardCount, ok := kinesisCreateShardCount(body)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid shard count"})
			return
		}
		if _, ok := a.streams[name]; ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceInUseException", "message": "stream already exists"})
			return
		}
		if a.streamQuota > 0 && len(a.streams) >= a.streamQuota {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"__type":  "LimitExceededException",
				"message": "stream quota exceeded",
			})
			return
		}
		if a.shardQuota > 0 && shardCount > a.shardQuota {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"__type":  "LimitExceededException",
				"message": "shard quota exceeded",
			})
			return
		}
		stream := newKinesisStream(name, shardCount, a.now())
		stream.Tags = kinesisTags(body["Tags"])
		a.streams[name] = stream
		writeJSON(w, http.StatusOK, map[string]string{})
	case "DeleteStream":
		name := stringValue(body["StreamName"])
		if a.streams[name] == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		delete(a.streams, name)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ListStreams":
		names := make([]string, 0, len(a.streams))
		for name := range a.streams {
			names = append(names, name)
		}
		sort.Strings(names)
		start := 0
		if after := stringValue(body["ExclusiveStartStreamName"]); after != "" {
			start = len(names)
			for i, name := range names {
				if name > after {
					start = i
					break
				}
			}
		}
		limit, ok := kinesisLimit(body, 100, "Limit")
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid limit"})
			return
		}
		end := start + limit
		if end > len(names) {
			end = len(names)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"StreamNames":    names[start:end],
			"HasMoreStreams": end < len(names),
		})
	case "ListShards":
		name, start, expired, ok := a.listShardsPage(body)
		if expired {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ExpiredNextTokenException", "message": "NextToken has expired"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid ListShards parameters"})
			return
		}
		stream := a.streams[name]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		if rawTimestamp, hasTimestamp := body["StreamCreationTimestamp"]; hasTimestamp {
			timestamp, ok := kinesisEpochTimestamp(rawTimestamp)
			if !ok {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid StreamCreationTimestamp"})
				return
			}
			if stream.CreatedAt.UnixMilli() != timestamp.UnixMilli() {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
				return
			}
		}
		limit, ok := kinesisListShardsLimit(body)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid MaxResults"})
			return
		}
		shards := stream.shards()
		if token := stringValue(body["NextToken"]); token != "" {
			shards = a.shardTokens[token].Shards
		} else if shards, ok = stream.filteredShards(body["ShardFilter"]); !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid ShardFilter"})
			return
		}
		if start > len(shards) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid NextToken"})
			return
		}
		end := start + limit
		if end > len(shards) {
			end = len(shards)
		}
		result := map[string]any{"Shards": shards[start:end]}
		if end < len(shards) {
			result["NextToken"] = a.issueShardPageToken(name, end, shards)
		}
		writeJSON(w, http.StatusOK, result)
	case "DescribeStream":
		name := stringValue(body["StreamName"])
		stream := a.streams[name]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"StreamDescription": map[string]any{
			"StreamName":           name,
			"StreamStatus":         "ACTIVE",
			"StreamARN":            kinesisStreamARN(name),
			"RetentionPeriodHours": stream.RetentionHours,
			"Shards":               stream.shards(),
		}})
	case "DescribeStreamSummary":
		name := stringValue(body["StreamName"])
		stream := a.streams[name]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"StreamDescriptionSummary": map[string]any{
			"StreamName":           name,
			"StreamARN":            kinesisStreamARN(name),
			"StreamStatus":         "ACTIVE",
			"RetentionPeriodHours": stream.RetentionHours,
			"OpenShardCount":       len(stream.Shards),
			"StreamModeDetails":    map[string]string{"StreamMode": "PROVISIONED"},
		}})
	case "ListTagsForStream":
		name := stringValue(body["StreamName"])
		stream := a.streams[name]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"Tags": kinesisTagsJSON(stream.Tags), "HasMoreTags": false})
	case "AddTagsToStream":
		name := stringValue(body["StreamName"])
		stream := a.streams[name]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		if stream.Tags == nil {
			stream.Tags = map[string]string{}
		}
		mergeStringMap(stream.Tags, kinesisStringMap(body["Tags"]))
		writeJSON(w, http.StatusOK, map[string]string{})
	case "RemoveTagsFromStream":
		name := stringValue(body["StreamName"])
		stream := a.streams[name]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		for _, tagKey := range kmsStringList(body["TagKeys"]) {
			delete(stream.Tags, tagKey)
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "IncreaseStreamRetentionPeriod", "DecreaseStreamRetentionPeriod":
		name := stringValue(body["StreamName"])
		stream := a.streams[name]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		stream.RetentionHours = intValue(body, stream.RetentionHours, "RetentionPeriodHours")
		writeJSON(w, http.StatusOK, map[string]string{})
	case "SplitShard":
		stream := a.streams[stringValue(body["StreamName"])]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		shardToSplit := stringValue(body["ShardToSplit"])
		newStartingHashKey := stringValue(body["NewStartingHashKey"])
		if !stream.hasShard(shardToSplit) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "shard not found"})
			return
		}
		if !stream.validSplitShard(shardToSplit, newStartingHashKey) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid NewStartingHashKey"})
			return
		}
		if a.shardQuota > 0 && len(stream.Shards) >= a.shardQuota {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "LimitExceededException", "message": "shard quota exceeded"})
			return
		}
		if !stream.splitShard(shardToSplit, newStartingHashKey, a.now()) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid NewStartingHashKey"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "MergeShards":
		stream := a.streams[stringValue(body["StreamName"])]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		first := stringValue(body["ShardToMerge"])
		second := stringValue(body["AdjacentShardToMerge"])
		if first == second || !stream.mergeShards(first, second, a.now()) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "shard not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "PutRecord":
		name := stringValue(body["StreamName"])
		partitionKey := stringValue(body["PartitionKey"])
		if name == "" || partitionKey == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "stream name and partition key are required"})
			return
		}
		if utf8.RuneCountInString(partitionKey) > 256 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "partition key is too long"})
			return
		}
		dataValue, hasData := body["Data"]
		data, isStringData := dataValue.(string)
		if !hasData || !isStringData {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "data is required"})
			return
		}
		decodedData, err := base64.StdEncoding.DecodeString(data)
		if err != nil || len(decodedData)+len(partitionKey) > 10*1024*1024 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "record data and partition key exceed 10 MiB"})
			return
		}
		stream := a.streams[name]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		seq := fmt.Sprintf("%d", len(stream.Records)+1)
		explicitHashKey, hasExplicitHashKey := body["ExplicitHashKey"]
		shardID, ok := stream.shardForPartitionKey(partitionKey, stringValue(explicitHashKey), hasExplicitHashKey)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid ExplicitHashKey"})
			return
		}
		stream.Records = append(stream.Records, kinesisRecord{
			Data:         data,
			PartitionKey: partitionKey,
			Sequence:     seq,
			ShardID:      shardID,
			CreatedAt:    a.now().UTC().Truncate(time.Millisecond),
		})
		writeJSON(w, http.StatusOK, map[string]string{"ShardId": stream.Records[len(stream.Records)-1].ShardID, "SequenceNumber": seq})
	case "PutRecords":
		name, ok := kinesisStreamNameOrARN(body)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "a valid stream name or ARN is required"})
			return
		}
		stream := a.streams[name]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		rawRecords, ok := body["Records"].([]any)
		if !ok || len(rawRecords) < 1 || len(rawRecords) > 500 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "Records must contain between 1 and 500 entries"})
			return
		}
		records := make([]kinesisRecord, 0, len(rawRecords))
		totalBytes := 0
		for _, rawRecord := range rawRecords {
			record, ok := rawRecord.(map[string]any)
			if !ok {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid record"})
				return
			}
			partitionKey := stringValue(record["PartitionKey"])
			dataValue, hasData := record["Data"]
			data, isStringData := dataValue.(string)
			if partitionKey == "" || utf8.RuneCountInString(partitionKey) > 256 || !hasData || !isStringData {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid record partition key or data"})
				return
			}
			decodedData, err := base64.StdEncoding.DecodeString(data)
			if err != nil || len(decodedData)+len(partitionKey) > 10*1024*1024 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "record data and partition key exceed 10 MiB"})
				return
			}
			totalBytes += len(decodedData) + len(partitionKey)
			if totalBytes > 10*1024*1024 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "records exceed 10 MiB"})
				return
			}
			explicitHashKey, hasExplicitHashKey := record["ExplicitHashKey"]
			shardID, ok := stream.shardForPartitionKey(partitionKey, stringValue(explicitHashKey), hasExplicitHashKey)
			if !ok {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid ExplicitHashKey"})
				return
			}
			records = append(records, kinesisRecord{Data: data, PartitionKey: partitionKey, ShardID: shardID, CreatedAt: a.now().UTC().Truncate(time.Millisecond)})
		}
		results := make([]map[string]string, 0, len(records))
		for index := range records {
			records[index].Sequence = fmt.Sprintf("%d", len(stream.Records)+1)
			stream.Records = append(stream.Records, records[index])
			results = append(results, map[string]string{"ShardId": records[index].ShardID, "SequenceNumber": records[index].Sequence})
		}
		writeJSON(w, http.StatusOK, map[string]any{"FailedRecordCount": 0, "Records": results})
	case "GetShardIterator":
		stream := a.streams[stringValue(body["StreamName"])]
		if stream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		if !stream.knownShard(stringValue(body["ShardId"])) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "shard not found"})
			return
		}
		shardID := stringValue(body["ShardId"])
		start, ok := kinesisShardIteratorStart(stream.recordsForShard(shardID), stringValue(body["ShardIteratorType"]), stringValue(body["StartingSequenceNumber"]), body["Timestamp"])
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid shard iterator parameters"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ShardIterator": a.issueIterator(stringValue(body["StreamName"]), shardID, start)})
	case "GetRecords":
		iterator, ok := a.iterators[stringValue(body["ShardIterator"])]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid shard iterator"})
			return
		}
		if !a.now().Before(iterator.ExpiresAt) {
			delete(a.iterators, stringValue(body["ShardIterator"]))
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ExpiredIteratorException", "message": "shard iterator has expired"})
			return
		}
		kinesisStream := a.streams[iterator.Stream]
		if kinesisStream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "stream not found"})
			return
		}
		records := kinesisStream.recordsForShard(iterator.ShardID)
		if iterator.Start > len(records) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid shard iterator"})
			return
		}
		limit, ok := kinesisGetRecordsLimit(body)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidArgumentException", "message": "invalid limit"})
			return
		}
		end := iterator.Start
		responseDataBytes := 0
		out := make([]map[string]any, 0, limit)
		for end < len(records) && end-iterator.Start < limit {
			record := records[end]
			decodedData, err := base64.StdEncoding.DecodeString(record.Data)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"__type": "InternalFailure", "message": "stored record data is invalid"})
				return
			}
			if len(out) > 0 && responseDataBytes+len(decodedData) > 10*1024*1024 {
				break
			}
			responseDataBytes += len(decodedData)
			out = append(out, map[string]any{
				"Data":                        record.Data,
				"PartitionKey":                record.PartitionKey,
				"SequenceNumber":              record.Sequence,
				"ApproximateArrivalTimestamp": float64(record.CreatedAt.UnixMilli()) / 1000,
			})
			end++
		}
		millisBehindLatest := int64(0)
		if end > iterator.Start && len(records) > 0 {
			millisBehindLatest = records[len(records)-1].CreatedAt.Sub(records[end-1].CreatedAt).Milliseconds()
			if millisBehindLatest < 0 {
				millisBehindLatest = 0
			}
		}
		var nextIterator any
		if !kinesisStream.ClosedShards[iterator.ShardID] || end < len(records) {
			nextIterator = a.issueIterator(iterator.Stream, iterator.ShardID, end)
		}
		writeJSON(w, http.StatusOK, map[string]any{"Records": out, "NextShardIterator": nextIterator, "MillisBehindLatest": millisBehindLatest})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported Kinesis action: " + action})
	}
}

func kinesisGetRecordsLimit(body map[string]any) (int, bool) {
	raw, ok := body["Limit"]
	if !ok {
		return 10000, true
	}
	value, ok := raw.(float64)
	if !ok || value != math.Trunc(value) || value < 1 || value > 10000 {
		return 0, false
	}
	return int(value), true
}

func kinesisListShardsLimit(body map[string]any) (int, bool) {
	raw, ok := body["MaxResults"]
	if !ok {
		return 1000, true
	}
	value, ok := raw.(float64)
	if !ok || value != math.Trunc(value) || value < 1 || value > 10000 {
		return 0, false
	}
	if value > 1000 {
		return 1000, true
	}
	return int(value), true
}

func kinesisCreateShardCount(body map[string]any) (int, bool) {
	raw, ok := body["ShardCount"]
	if !ok {
		return 1, true
	}
	value, ok := raw.(float64)
	if !ok || value != math.Trunc(value) || value < 1 || value > float64(math.MaxInt) {
		return 0, false
	}
	return int(value), true
}

func kinesisShardIteratorStart(records []kinesisRecord, iteratorType, sequence string, rawTimestamp any) (int, bool) {
	switch iteratorType {
	case "TRIM_HORIZON":
		return 0, true
	case "LATEST":
		return len(records), true
	case "AT_SEQUENCE_NUMBER", "AFTER_SEQUENCE_NUMBER":
		if sequence == "" {
			return 0, false
		}
		for index, record := range records {
			if record.Sequence != sequence {
				continue
			}
			if iteratorType == "AFTER_SEQUENCE_NUMBER" {
				return index + 1, true
			}
			return index, true
		}
		return 0, false
	case "AT_TIMESTAMP":
		timestamp, ok := kinesisTimestamp(rawTimestamp)
		if !ok {
			return 0, false
		}
		for index, record := range records {
			if !record.CreatedAt.Before(timestamp) {
				return index, true
			}
		}
		return len(records), true
	default:
		return 0, false
	}
}

func kinesisTimestamp(raw any) (time.Time, bool) {
	switch value := raw.(type) {
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return time.Time{}, false
		}
		seconds, fraction := math.Modf(value)
		return time.Unix(int64(seconds), int64(fraction*float64(time.Second))).UTC(), true
	case string:
		if timestamp, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return timestamp.UTC(), true
		}
		if seconds, err := strconv.ParseFloat(value, 64); err == nil {
			if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
				return time.Time{}, false
			}
			whole, fraction := math.Modf(seconds)
			return time.Unix(int64(whole), int64(fraction*float64(time.Second))).UTC(), true
		}
	}
	return time.Time{}, false
}

func (stream *kinesisStream) shardForPartitionKey(key, explicitHashKey string, hasExplicitHashKey bool) (string, bool) {
	hash := new(big.Int)
	if hasExplicitHashKey {
		if !kinesisHashKeyPattern.MatchString(explicitHashKey) {
			return "", false
		}
		if _, ok := hash.SetString(explicitHashKey, 10); !ok || hash.Cmp(kinesisMaxHashKey()) > 0 {
			return "", false
		}
	} else {
		digest := md5.Sum([]byte(key))
		hash.SetBytes(digest[:])
	}
	for _, shardID := range stream.Shards {
		rangeForShard := stream.ShardRanges[shardID]
		if hash.Cmp(rangeForShard.Start) >= 0 && hash.Cmp(rangeForShard.End) <= 0 {
			return shardID, true
		}
	}
	return "", false
}

func (stream *kinesisStream) recordsForShard(shardID string) []kinesisRecord {
	records := make([]kinesisRecord, 0, len(stream.Records))
	for _, record := range stream.Records {
		if record.ShardID == shardID {
			records = append(records, record)
		}
	}
	return records
}

func (a *KinesisAdapter) issueIterator(stream, shardID string, start int) string {
	token := newKinesisToken()
	a.iterators[token] = kinesisIterator{Stream: stream, ShardID: shardID, Start: start, ExpiresAt: a.now().Add(5 * time.Minute)}
	return token
}

func (a *KinesisAdapter) issueShardPageToken(stream string, start int, shards []map[string]any) string {
	token := newKinesisToken()
	a.shardTokens[token] = kinesisShardPageToken{Stream: stream, Start: start, Shards: shards, ExpiresAt: a.now().Add(5 * time.Minute)}
	return token
}

func newKinesisToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		panic("generate Kinesis token: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(bytes)
}

func newKinesisStream(name string, shardCount int, now time.Time) *kinesisStream {
	if shardCount < 1 {
		shardCount = 1
	}
	stream := &kinesisStream{Name: name, CreatedAt: now.UTC().Truncate(time.Millisecond), RetentionHours: 24, Tags: map[string]string{}, ShardRanges: map[string]kinesisHashRange{}, ClosedShards: map[string]bool{}, ShardCreatedAt: map[string]time.Time{}, ShardClosedAt: map[string]time.Time{}}
	width := new(big.Int).Add(kinesisMaxHashKey(), big.NewInt(1))
	width.Div(width, big.NewInt(int64(shardCount)))
	start := big.NewInt(0)
	for i := 0; i < shardCount; i++ {
		end := new(big.Int).Sub(new(big.Int).Add(start, width), big.NewInt(1))
		if i == shardCount-1 {
			end = kinesisMaxHashKey()
		}
		stream.addShardForRange(start, end, now)
		start = new(big.Int).Add(end, big.NewInt(1))
	}
	return stream
}

func kinesisStreamARN(name string) string {
	return "arn:aws:kinesis:us-east-1:000000000000:stream/" + name
}

func kinesisStreamNameOrARN(body map[string]any) (string, bool) {
	name := stringValue(body["StreamName"])
	arn := stringValue(body["StreamARN"])
	if arn == "" {
		return name, name != ""
	}
	prefix := "arn:aws:kinesis:us-east-1:000000000000:stream/"
	if !strings.HasPrefix(arn, prefix) {
		return "", false
	}
	arnName := strings.TrimPrefix(arn, prefix)
	if !kinesisStreamNamePattern.MatchString(arnName) || (name != "" && name != arnName) {
		return "", false
	}
	return arnName, true
}

func (a *KinesisAdapter) listShardsPage(body map[string]any) (string, int, bool, bool) {
	if token := stringValue(body["NextToken"]); token != "" {
		if stringValue(body["StreamName"]) != "" || stringValue(body["ExclusiveStartShardId"]) != "" || body["StreamCreationTimestamp"] != nil || body["ShardFilter"] != nil {
			return "", 0, false, false
		}
		page, ok := a.shardTokens[token]
		if !ok {
			return "", 0, false, false
		}
		if !a.now().Before(page.ExpiresAt) {
			delete(a.shardTokens, token)
			return "", 0, true, false
		}
		if stringValue(body["StreamARN"]) != "" {
			name, ok := kinesisStreamNameOrARN(body)
			if !ok || name != page.Stream {
				return "", 0, false, false
			}
		}
		return page.Stream, page.Start, false, true
	}
	name, ok := kinesisStreamNameOrARN(body)
	if !ok {
		return "", 0, false, false
	}
	stream := a.streams[name]
	if stream == nil {
		return name, 0, false, true
	}
	exclusiveStart := stringValue(body["ExclusiveStartShardId"])
	if exclusiveStart == "" {
		return name, 0, false, true
	}
	for index, shardID := range stream.Shards {
		if shardID == exclusiveStart {
			return name, index + 1, false, true
		}
	}
	return "", 0, false, false
}

func kinesisEpochTimestamp(raw any) (time.Time, bool) {
	value, ok := raw.(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(value * 1000)).UTC(), true
}

func kinesisStringMap(raw any) map[string]string {
	values, _ := raw.(map[string]any)
	tags := map[string]string{}
	for key, value := range values {
		tags[key] = stringValue(value)
	}
	return tags
}

func kinesisTags(raw any) map[string]string {
	if tags := kinesisStringMap(raw); len(tags) > 0 {
		return tags
	}
	return map[string]string{}
}

func kinesisTagsJSON(tags map[string]string) []map[string]string {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, map[string]string{"Key": key, "Value": tags[key]})
	}
	return out
}

func kinesisLimit(body map[string]any, max int, name string) (int, bool) {
	value, ok := body[name]
	if !ok {
		return max, true
	}
	limit := 0
	switch typed := value.(type) {
	case int:
		limit = typed
	case int32:
		limit = int(typed)
	case int64:
		limit = int(typed)
	case float64:
		limit = int(typed)
	case string:
		parsed, err := strconv.Atoi(typed)
		if err != nil {
			return 0, false
		}
		limit = parsed
	default:
		return 0, false
	}
	return limit, limit >= 1 && limit <= max
}

func kinesisMaxHashKey() *big.Int {
	return new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))
}

func (s *kinesisStream) addShardForRange(start, end *big.Int, now time.Time) string {
	shardID := fmt.Sprintf("shardId-%012d", s.NextShard)
	s.NextShard++
	s.Shards = append(s.Shards, shardID)
	s.ShardRanges[shardID] = kinesisHashRange{Start: new(big.Int).Set(start), End: new(big.Int).Set(end)}
	delete(s.ClosedShards, shardID)
	delete(s.ShardClosedAt, shardID)
	s.ShardCreatedAt[shardID] = now.UTC().Truncate(time.Millisecond)
	return shardID
}

func (s *kinesisStream) removeShard(shardID string, now time.Time) bool {
	for i, candidate := range s.Shards {
		if candidate == shardID {
			s.Shards = append(s.Shards[:i], s.Shards[i+1:]...)
			s.ClosedShards[shardID] = true
			s.ShardClosedAt[shardID] = now.UTC().Truncate(time.Millisecond)
			return true
		}
	}
	return false
}

func (s *kinesisStream) splitShard(shardID, newStartingHashKey string, now time.Time) bool {
	if !s.validSplitShard(shardID, newStartingHashKey) {
		return false
	}
	rangeForShard, ok := s.ShardRanges[shardID]
	split, ok := new(big.Int).SetString(newStartingHashKey, 10)
	if !ok {
		return false
	}
	if !s.removeShard(shardID, now) {
		return false
	}
	s.addShardForRange(rangeForShard.Start, new(big.Int).Sub(split, big.NewInt(1)), now)
	s.addShardForRange(split, rangeForShard.End, now)
	return true
}

func (s *kinesisStream) validSplitShard(shardID, newStartingHashKey string) bool {
	rangeForShard, ok := s.ShardRanges[shardID]
	if !ok || !kinesisHashKeyPattern.MatchString(newStartingHashKey) {
		return false
	}
	split, ok := new(big.Int).SetString(newStartingHashKey, 10)
	return ok && split.Cmp(rangeForShard.Start) > 0 && split.Cmp(rangeForShard.End) <= 0
}

func (s *kinesisStream) mergeShards(first, second string, now time.Time) bool {
	firstRange, firstOK := s.ShardRanges[first]
	secondRange, secondOK := s.ShardRanges[second]
	if !firstOK || !secondOK {
		return false
	}
	left, right := firstRange, secondRange
	if left.Start.Cmp(right.Start) > 0 {
		left, right = right, left
	}
	if new(big.Int).Add(left.End, big.NewInt(1)).Cmp(right.Start) != 0 {
		return false
	}
	if !s.removeShard(first, now) || !s.removeShard(second, now) {
		return false
	}
	s.addShardForRange(left.Start, right.End, now)
	return true
}

func (s *kinesisStream) hasShard(shardID string) bool {
	for _, candidate := range s.Shards {
		if candidate == shardID {
			return true
		}
	}
	return false
}

func (s *kinesisStream) knownShard(shardID string) bool {
	_, ok := s.ShardRanges[shardID]
	return ok
}

func (s *kinesisStream) shards() []map[string]any {
	out := make([]map[string]any, 0, len(s.Shards))
	for _, shardID := range s.Shards {
		rangeForShard := s.ShardRanges[shardID]
		out = append(out, map[string]any{
			"ShardId": shardID,
			"HashKeyRange": map[string]string{
				"StartingHashKey": rangeForShard.Start.String(),
				"EndingHashKey":   rangeForShard.End.String(),
			},
		})
	}
	return out
}

func (s *kinesisStream) filteredShards(raw any) ([]map[string]any, bool) {
	if raw == nil {
		return s.shards(), true
	}
	filter, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	typeName := stringValue(filter["Type"])
	ids := make([]string, 0, len(s.ShardRanges))
	for shardID := range s.ShardRanges {
		ids = append(ids, shardID)
	}
	sort.Strings(ids)
	include := func(shardID string) bool { return true }
	switch typeName {
	case "AT_LATEST":
		include = func(shardID string) bool { return !s.ClosedShards[shardID] }
	case "AT_TRIM_HORIZON":
		at := s.CreatedAt
		include = func(shardID string) bool {
			closedAt, closed := s.ShardClosedAt[shardID]
			return !s.ShardCreatedAt[shardID].After(at) && (!closed || !closedAt.Before(at))
		}
	case "FROM_TRIM_HORIZON":
	case "AFTER_SHARD_ID":
		start := stringValue(filter["ShardId"])
		if start == "" || !s.knownShard(start) {
			return nil, false
		}
		include = func(shardID string) bool { return shardID > start }
	case "AT_TIMESTAMP", "FROM_TIMESTAMP":
		value, ok := filter["Timestamp"].(float64)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return nil, false
		}
		at := time.UnixMilli(int64(value * 1000)).UTC()
		if typeName == "AT_TIMESTAMP" {
			include = func(shardID string) bool {
				closedAt, closed := s.ShardClosedAt[shardID]
				return !s.ShardCreatedAt[shardID].After(at) && (!closed || !closedAt.Before(at))
			}
		} else {
			include = func(shardID string) bool {
				closedAt, closed := s.ShardClosedAt[shardID]
				return !closed || !closedAt.Before(at)
			}
		}
	default:
		return nil, false
	}
	out := make([]map[string]any, 0, len(ids))
	for _, shardID := range ids {
		if !include(shardID) {
			continue
		}
		rangeForShard := s.ShardRanges[shardID]
		out = append(out, map[string]any{"ShardId": shardID, "HashKeyRange": map[string]string{"StartingHashKey": rangeForShard.Start.String(), "EndingHashKey": rangeForShard.End.String()}})
	}
	return out, true
}

func (a *KinesisAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "kinesis:" + action,
		Resource:            a.resourceARN(body),
		Context: map[string]string{
			"provider":     "aws",
			"service":      "kinesis",
			"method":       r.Method,
			"request_id":   "homeport",
			"source_ip":    sourceIP(r),
			"current_time": time.Now().UTC().Format(time.RFC3339),
			"user_agent":   r.UserAgent(),
		},
		Claims: awsClaims(r),
	}
	if value := r.Header.Get("X-Homeport-Credential-Age"); value != "" {
		req.Context["credential_age"] = value
	}
	if value := r.Header.Get("X-Homeport-Credential-Expired"); value != "" {
		req.Context["credential_expired"] = value
	}
	decision, err := a.authorizer.Authorize(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"__type": "InternalFailure", "message": err.Error()})
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"__type":  "AccessDenied",
			"message": decision.Reason,
		})
		return false
	}
	return true
}

func (a *KinesisAdapter) resourceARN(body map[string]any) string {
	if token := stringValue(body["NextToken"]); token != "" {
		a.mu.Lock()
		page, ok := a.shardTokens[token]
		a.mu.Unlock()
		if ok {
			return kinesisStreamARN(page.Stream)
		}
	}
	return kinesisARN(body)
}

func parseShardIterator(value string) (string, int, bool) {
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] == ':' {
			name := value[:i]
			start, err := strconv.Atoi(value[i+1:])
			return name, start, name != "" && err == nil && start >= 0
		}
	}
	return "", 0, false
}

func kinesisARN(body map[string]any) string {
	if arn := stringValue(body["StreamARN"]); arn != "" {
		return arn
	}
	name := stringValue(body["StreamName"])
	if name == "" {
		name, _, _ = parseShardIterator(stringValue(body["ShardIterator"]))
	}
	if name == "" {
		name = "unknown"
	}
	return kinesisStreamARN(name)
}
