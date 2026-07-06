package aws

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
)

type CloudWatchLogsAdapter struct {
	mu     sync.Mutex
	groups map[string]map[string]*logStream
}

type logStream struct {
	Name          string
	SequenceToken int
	Messages      []string
}

func NewCloudWatchLogsAdapter() *CloudWatchLogsAdapter {
	return &CloudWatchLogsAdapter{groups: make(map[string]map[string]*logStream)}
}

func (CloudWatchLogsAdapter) Provider() string { return "aws" }
func (CloudWatchLogsAdapter) Service() string  { return "cloudwatchlogs" }
func (CloudWatchLogsAdapter) Routes() []string { return []string{"POST /compat/aws/cloudwatchlogs"} }
func (CloudWatchLogsAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_CLOUDWATCHLOGS": "http://homeport:8080/api/v1/compat/aws/cloudwatchlogs",
		"HOMEPORT_COMPAT_BACKEND":         "loki",
	}
}
func (CloudWatchLogsAdapter) ConformanceChecks() []string {
	return []string{"create-log-group", "create-log-stream", "put-log-events", "describe-log-streams"}
}
func (a *CloudWatchLogsAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateLogGroup":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		if group == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "logGroupName is required"})
			return
		}
		if _, ok := a.groups[group]; !ok {
			a.groups[group] = make(map[string]*logStream)
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "CreateLogStream":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		stream := cloudWatchField(body, "logStreamName", "LogStreamName")
		streams := a.groups[group]
		if streams == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "log group not found"})
			return
		}
		if _, ok := streams[stream]; !ok {
			streams[stream] = &logStream{Name: stream}
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "PutLogEvents":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		stream := cloudWatchField(body, "logStreamName", "LogStreamName")
		logStream := a.stream(group, stream)
		if logStream == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "log stream not found"})
			return
		}
		for _, message := range logEventMessages(body["logEvents"]) {
			logStream.Messages = append(logStream.Messages, message)
		}
		logStream.SequenceToken++
		writeJSON(w, http.StatusOK, map[string]string{
			"nextSequenceToken": strconv.Itoa(logStream.SequenceToken),
		})
	case "DescribeLogStreams":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		streams := a.groups[group]
		if streams == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": "log group not found"})
			return
		}
		names := make([]string, 0, len(streams))
		for name := range streams {
			names = append(names, name)
		}
		sort.Strings(names)
		out := make([]map[string]any, 0, len(names))
		for _, name := range names {
			stream := streams[name]
			out = append(out, map[string]any{
				"logStreamName":       stream.Name,
				"uploadSequenceToken": strconv.Itoa(stream.SequenceToken),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"logStreams": out})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported CloudWatch Logs action: " + action})
	}
}

func (a *CloudWatchLogsAdapter) stream(group, stream string) *logStream {
	streams := a.groups[group]
	if streams == nil {
		return nil
	}
	return streams[stream]
}

func cloudWatchField(body map[string]any, names ...string) string {
	for _, name := range names {
		if value := stringValue(body[name]); value != "" {
			return value
		}
	}
	return ""
}

func logEventMessages(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	messages := make([]string, 0, len(items))
	for _, item := range items {
		event, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if message := stringValue(event["message"]); message != "" {
			messages = append(messages, message)
		}
	}
	return messages
}
