package aws

import (
	"fmt"
	"net/http"
	"sync"
)

type KinesisAdapter struct {
	mu      sync.Mutex
	streams map[string][]kinesisRecord
}

type kinesisRecord struct {
	Data         string
	PartitionKey string
	Sequence     string
}

func NewKinesisAdapter() *KinesisAdapter {
	return &KinesisAdapter{streams: make(map[string][]kinesisRecord)}
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
	return []string{"create-stream", "put-record", "get-records"}
}

func (a *KinesisAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateStream":
		name := stringValue(body["StreamName"])
		if _, ok := a.streams[name]; !ok {
			a.streams[name] = nil
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "DescribeStream":
		name := stringValue(body["StreamName"])
		writeJSON(w, http.StatusOK, map[string]any{"StreamDescription": map[string]any{
			"StreamName":   name,
			"StreamStatus": "ACTIVE",
			"Shards": []map[string]string{{
				"ShardId": "shardId-000000000000",
			}},
		}})
	case "PutRecord":
		name := stringValue(body["StreamName"])
		seq := fmt.Sprintf("%d", len(a.streams[name])+1)
		a.streams[name] = append(a.streams[name], kinesisRecord{
			Data:         stringValue(body["Data"]),
			PartitionKey: stringValue(body["PartitionKey"]),
			Sequence:     seq,
		})
		writeJSON(w, http.StatusOK, map[string]string{"ShardId": "shardId-000000000000", "SequenceNumber": seq})
	case "GetShardIterator":
		writeJSON(w, http.StatusOK, map[string]string{"ShardIterator": stringValue(body["StreamName"]) + ":0"})
	case "GetRecords":
		stream, start := parseShardIterator(stringValue(body["ShardIterator"]))
		records := a.streams[stream]
		out := make([]map[string]string, 0, len(records))
		for _, record := range records[start:] {
			out = append(out, map[string]string{
				"Data":           record.Data,
				"PartitionKey":   record.PartitionKey,
				"SequenceNumber": record.Sequence,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"Records": out, "NextShardIterator": fmt.Sprintf("%s:%d", stream, len(records))})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported Kinesis action: " + action})
	}
}

func parseShardIterator(value string) (string, int) {
	for i := len(value) - 1; i >= 0; i-- {
		if value[i] == ':' {
			return value[:i], 0
		}
	}
	return value, 0
}
