package aws

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type CloudWatchLogsAdapter struct {
	mu         sync.Mutex
	groups     map[string]*logGroup
	groupQuota int
	now        func() time.Time
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type CloudWatchLogsOption func(*CloudWatchLogsAdapter)

type logGroup struct {
	Name            string
	RetentionInDays int
	Tags            map[string]string
	Streams         map[string]*logStream
}

type logStream struct {
	Name          string
	SequenceToken int
	Events        []logEvent
}

type logEvent struct {
	Message   string
	Timestamp int64
}

func NewCloudWatchLogsAdapter(options ...CloudWatchLogsOption) *CloudWatchLogsAdapter {
	adapter := &CloudWatchLogsAdapter{
		groups:     make(map[string]*logGroup),
		now:        time.Now,
		authorizer: authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithCloudWatchLogsAuthorizer(authorizer authz.Authorizer) CloudWatchLogsOption {
	return func(adapter *CloudWatchLogsAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithCloudWatchLogsAuditSink(sink func(authz.Decision)) CloudWatchLogsOption {
	return func(adapter *CloudWatchLogsAdapter) {
		adapter.auditSink = sink
	}
}

func WithCloudWatchLogsGroupQuota(maxGroups int) CloudWatchLogsOption {
	return func(adapter *CloudWatchLogsAdapter) {
		adapter.groupQuota = maxGroups
	}
}

func WithCloudWatchLogsClock(clock func() time.Time) CloudWatchLogsOption {
	return func(adapter *CloudWatchLogsAdapter) {
		if clock != nil {
			adapter.now = clock
		}
	}
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
	if !a.authorized(w, r, action, body) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateLogGroup":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		if group == "" {
			writeCloudWatchLogsInvalidParameter(w, "logGroupName is required")
			return
		}
		if _, ok := a.groups[group]; ok {
			writeCloudWatchLogsAlreadyExists(w, "log group already exists")
			return
		}
		if a.groupQuota > 0 && len(a.groups) >= a.groupQuota {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"__type":  "LimitExceededException",
				"message": "log group quota exceeded",
			})
			return
		}
		a.groups[group] = &logGroup{Name: group, Tags: mapValue(body["tags"]), Streams: map[string]*logStream{}}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "CreateLogStream":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		stream := cloudWatchField(body, "logStreamName", "LogStreamName")
		if group == "" || stream == "" {
			writeCloudWatchLogsInvalidParameter(w, "logGroupName and logStreamName are required")
			return
		}
		logGroup := a.groups[group]
		if logGroup == nil {
			writeCloudWatchLogsNotFound(w, "log group not found")
			return
		}
		if _, ok := logGroup.Streams[stream]; ok {
			writeCloudWatchLogsAlreadyExists(w, "log stream already exists")
			return
		}
		logGroup.Streams[stream] = &logStream{Name: stream}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "DeleteLogStream":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		stream := cloudWatchField(body, "logStreamName", "LogStreamName")
		if group == "" || stream == "" {
			writeCloudWatchLogsInvalidParameter(w, "logGroupName and logStreamName are required")
			return
		}
		logGroup := a.groups[group]
		if logGroup == nil {
			writeCloudWatchLogsNotFound(w, "log group not found")
			return
		}
		if _, ok := logGroup.Streams[stream]; !ok {
			writeCloudWatchLogsNotFound(w, "log stream not found")
			return
		}
		delete(logGroup.Streams, stream)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "DeleteLogGroup":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		if group == "" {
			writeCloudWatchLogsInvalidParameter(w, "logGroupName is required")
			return
		}
		if _, ok := a.groups[group]; !ok {
			writeCloudWatchLogsNotFound(w, "log group not found")
			return
		}
		delete(a.groups, group)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "PutRetentionPolicy":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		if group == "" {
			writeCloudWatchLogsInvalidParameter(w, "logGroupName is required")
			return
		}
		retention := intValue(body, 0, "retentionInDays", "RetentionInDays")
		if !validCloudWatchLogsRetention(retention) {
			writeCloudWatchLogsInvalidParameter(w, "invalid retentionInDays")
			return
		}
		logGroup := a.groups[group]
		if logGroup == nil {
			writeCloudWatchLogsNotFound(w, "log group not found")
			return
		}
		logGroup.RetentionInDays = retention
		writeJSON(w, http.StatusOK, map[string]string{})
	case "DeleteRetentionPolicy":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		if group == "" {
			writeCloudWatchLogsInvalidParameter(w, "logGroupName is required")
			return
		}
		logGroup := a.groups[group]
		if logGroup == nil {
			writeCloudWatchLogsNotFound(w, "log group not found")
			return
		}
		logGroup.RetentionInDays = 0
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ListTagsLogGroup", "ListTagsForResource":
		group := cloudWatchLogsGroup(body)
		logGroup := a.groups[group]
		if logGroup == nil {
			writeCloudWatchLogsNotFound(w, "log group not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]map[string]string{"tags": logGroup.Tags})
	case "TagLogGroup", "TagResource":
		group := cloudWatchLogsGroup(body)
		logGroup := a.groups[group]
		if logGroup == nil {
			writeCloudWatchLogsNotFound(w, "log group not found")
			return
		}
		tags := mapValue(body["tags"])
		tagCount := len(logGroup.Tags)
		for key := range tags {
			if _, exists := logGroup.Tags[key]; !exists {
				tagCount++
			}
		}
		if tagCount > 50 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"__type":  "TooManyTagsException",
				"message": "a log group can have no more than 50 tags",
			})
			return
		}
		if logGroup.Tags == nil {
			logGroup.Tags = map[string]string{}
		}
		mergeStringMap(logGroup.Tags, tags)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "UntagLogGroup", "UntagResource":
		group := cloudWatchLogsGroup(body)
		logGroup := a.groups[group]
		if logGroup == nil {
			writeCloudWatchLogsNotFound(w, "log group not found")
			return
		}
		for _, key := range cloudWatchLogsTagKeys(body) {
			delete(logGroup.Tags, key)
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "DescribeLogGroups":
		prefix := cloudWatchField(body, "logGroupNamePrefix", "LogGroupNamePrefix")
		names := make([]string, 0, len(a.groups))
		for name := range a.groups {
			if prefix == "" || strings.HasPrefix(name, prefix) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		start := 0
		if token := cloudWatchField(body, "nextToken", "NextToken"); token != "" {
			parsed, err := strconv.Atoi(token)
			if err != nil || parsed < 0 || parsed > len(names) {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"__type":  "InvalidParameterException",
					"message": "invalid nextToken",
				})
				return
			}
			start = parsed
		}
		limit, ok := cloudWatchLogsLimit(body, 50, 50, "limit", "Limit")
		if !ok {
			writeCloudWatchLogsInvalidParameter(w, "invalid limit")
			return
		}
		end := start + limit
		if end > len(names) {
			end = len(names)
		}
		groups := make([]map[string]any, 0, end-start)
		for _, name := range names[start:end] {
			group := a.groups[name]
			row := map[string]any{"logGroupName": group.Name}
			if group.RetentionInDays > 0 {
				row["retentionInDays"] = group.RetentionInDays
			}
			groups = append(groups, row)
		}
		result := map[string]any{"logGroups": groups}
		if end < len(names) {
			result["nextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, result)
	case "PutLogEvents":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		stream := cloudWatchField(body, "logStreamName", "LogStreamName")
		if group == "" || stream == "" {
			writeCloudWatchLogsInvalidParameter(w, "logGroupName and logStreamName are required")
			return
		}
		logStream := a.stream(group, stream)
		if logStream == nil {
			writeCloudWatchLogsNotFound(w, "log stream not found")
			return
		}
		events, ok := cloudWatchLogEvents(body["logEvents"])
		if !ok {
			writeCloudWatchLogsInvalidParameter(w, "invalid logEvents")
			return
		}
		accepted, rejected := cloudWatchLogEventsWithinWindow(events, a.groups[group].RetentionInDays, a.now())
		logStream.Events = append(logStream.Events, accepted...)
		logStream.SequenceToken++
		result := map[string]any{"nextSequenceToken": strconv.Itoa(logStream.SequenceToken)}
		if len(rejected) > 0 {
			result["rejectedLogEventsInfo"] = rejected
		}
		writeJSON(w, http.StatusOK, result)
	case "GetLogEvents":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		stream := cloudWatchField(body, "logStreamName", "LogStreamName")
		if group == "" || stream == "" {
			writeCloudWatchLogsInvalidParameter(w, "logGroupName and logStreamName are required")
			return
		}
		logStream := a.stream(group, stream)
		if logStream == nil {
			writeCloudWatchLogsNotFound(w, "log stream not found")
			return
		}
		start := 0
		if token := cloudWatchField(body, "nextToken", "NextToken"); token != "" {
			parsed, err := strconv.Atoi(token)
			if err != nil || parsed < 0 || parsed > len(logStream.Events) {
				writeCloudWatchLogsInvalidParameter(w, "invalid nextToken")
				return
			}
			start = parsed
		}
		limit, ok := cloudWatchLogsLimit(body, 10000, 10000, "limit", "Limit")
		if !ok {
			writeCloudWatchLogsInvalidParameter(w, "invalid limit")
			return
		}
		end := start + limit
		if end > len(logStream.Events) {
			end = len(logStream.Events)
		}
		events := make([]map[string]any, 0, end-start)
		for _, event := range logStream.Events[start:end] {
			events = append(events, map[string]any{"message": event.Message, "timestamp": event.Timestamp})
		}
		result := map[string]any{"events": events}
		if end < len(logStream.Events) {
			result["nextForwardToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, result)
	case "DescribeLogStreams":
		group := cloudWatchField(body, "logGroupName", "LogGroupName")
		if group == "" {
			writeCloudWatchLogsInvalidParameter(w, "logGroupName is required")
			return
		}
		logGroup := a.groups[group]
		if logGroup == nil {
			writeCloudWatchLogsNotFound(w, "log group not found")
			return
		}
		names := make([]string, 0, len(logGroup.Streams))
		for name := range logGroup.Streams {
			names = append(names, name)
		}
		sort.Strings(names)
		start := 0
		if token := cloudWatchField(body, "nextToken", "NextToken"); token != "" {
			parsed, err := strconv.Atoi(token)
			if err != nil || parsed < 0 || parsed > len(names) {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"__type":  "InvalidParameterException",
					"message": "invalid nextToken",
				})
				return
			}
			start = parsed
		}
		limit, ok := cloudWatchLogsLimit(body, 50, 50, "limit", "Limit")
		if !ok {
			writeCloudWatchLogsInvalidParameter(w, "invalid limit")
			return
		}
		end := start + limit
		if end > len(names) {
			end = len(names)
		}
		out := make([]map[string]any, 0, len(names))
		for _, name := range names[start:end] {
			stream := logGroup.Streams[name]
			out = append(out, map[string]any{
				"logStreamName":       stream.Name,
				"uploadSequenceToken": strconv.Itoa(stream.SequenceToken),
			})
		}
		result := map[string]any{"logStreams": out}
		if end < len(names) {
			result["nextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, result)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported CloudWatch Logs action: " + action})
	}
}

func (a *CloudWatchLogsAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "logs:" + action,
		Resource:            cloudWatchLogsARN(body),
		Context: map[string]string{
			"provider":     "aws",
			"service":      "cloudwatchlogs",
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"__type": "ServiceUnavailableException", "message": err.Error()})
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

func (a *CloudWatchLogsAdapter) stream(group, stream string) *logStream {
	logGroup := a.groups[group]
	if logGroup == nil {
		return nil
	}
	return logGroup.Streams[stream]
}

func writeCloudWatchLogsNotFound(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "ResourceNotFoundException",
		"message": message,
	})
}

func writeCloudWatchLogsAlreadyExists(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "ResourceAlreadyExistsException",
		"message": message,
	})
}

func writeCloudWatchLogsInvalidParameter(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{
		"__type":  "InvalidParameterException",
		"message": message,
	})
}

func validCloudWatchLogsRetention(days int) bool {
	switch days {
	case 1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653:
		return true
	default:
		return false
	}
}

func cloudWatchField(body map[string]any, names ...string) string {
	for _, name := range names {
		if value := stringValue(body[name]); value != "" {
			return value
		}
	}
	return ""
}

func cloudWatchLogsTagKeys(body map[string]any) []string {
	values, _ := body["tagKeys"].([]any)
	if values == nil {
		values, _ = body["tags"].([]any)
	}
	keys := make([]string, 0, len(values))
	for _, value := range values {
		if key := stringValue(value); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func cloudWatchLogsGroup(body map[string]any) string {
	if group := cloudWatchField(body, "logGroupName", "LogGroupName"); group != "" {
		return group
	}
	_, group, _ := strings.Cut(cloudWatchField(body, "resourceArn", "ResourceArn"), ":log-group:")
	return group
}

func intValue(body map[string]any, fallback int, names ...string) int {
	for _, name := range names {
		switch value := body[name].(type) {
		case int:
			if value > 0 {
				return value
			}
		case int32:
			if value > 0 {
				return int(value)
			}
		case int64:
			if value > 0 {
				return int(value)
			}
		case float64:
			if value > 0 {
				return int(value)
			}
		case string:
			parsed, err := strconv.Atoi(value)
			if err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return fallback
}

func cloudWatchLogsLimit(body map[string]any, fallback, max int, names ...string) (int, bool) {
	for _, name := range names {
		value, ok := body[name]
		if !ok {
			continue
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
	return fallback, true
}

func cloudWatchLogEvents(value any) ([]logEvent, bool) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 || len(items) > 10000 {
		return nil, false
	}
	events := make([]logEvent, 0, len(items))
	batchBytes := 0
	var previousTimestamp int64
	for _, item := range items {
		event, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		message, messageOK := event["message"].(string)
		timestamp, timestampOK := cloudWatchLogEventTimestamp(event["timestamp"])
		if !messageOK || message == "" || len([]byte(message)) > 1024*1024 || !timestampOK || (len(events) > 0 && timestamp < previousTimestamp) {
			return nil, false
		}
		batchBytes += len([]byte(message)) + 26
		if batchBytes > 1024*1024 {
			return nil, false
		}
		events = append(events, logEvent{Message: message, Timestamp: timestamp})
		previousTimestamp = timestamp
	}
	if events[len(events)-1].Timestamp-events[0].Timestamp > int64(24*time.Hour/time.Millisecond) {
		return nil, false
	}
	return events, true
}

func cloudWatchLogEventTimestamp(value any) (int64, bool) {
	timestamp, ok := value.(float64)
	if !ok || timestamp < 0 || timestamp != math.Trunc(timestamp) || timestamp >= math.Exp2(63) {
		return 0, false
	}
	return int64(timestamp), true
}

func cloudWatchLogEventsWithinWindow(events []logEvent, retentionInDays int, now time.Time) ([]logEvent, map[string]int) {
	oldest := now.Add(-14 * 24 * time.Hour)
	if retentionInDays > 0 {
		retentionStart := now.Add(-time.Duration(retentionInDays) * 24 * time.Hour)
		if retentionStart.After(oldest) {
			oldest = retentionStart
		}
	}
	oldestMillis := oldest.UnixMilli()
	newestMillis := now.Add(2 * time.Hour).UnixMilli()
	acceptedStart := 0
	rejected := map[string]int{}
	for acceptedStart < len(events) && events[acceptedStart].Timestamp < oldestMillis {
		acceptedStart++
	}
	if acceptedStart > 0 {
		rejected["tooOldLogEventEndIndex"] = acceptedStart
	}
	acceptedEnd := len(events)
	for acceptedEnd > acceptedStart && events[acceptedEnd-1].Timestamp > newestMillis {
		acceptedEnd--
		rejected["tooNewLogEventStartIndex"] = acceptedEnd
	}
	return events[acceptedStart:acceptedEnd], rejected
}

func cloudWatchLogsARN(body map[string]any) string {
	if arn := cloudWatchField(body, "resourceArn", "ResourceArn"); arn != "" {
		return arn
	}
	group := cloudWatchField(body, "logGroupName", "LogGroupName", "logGroupNamePrefix", "LogGroupNamePrefix")
	stream := cloudWatchField(body, "logStreamName", "LogStreamName")
	if group == "" {
		group = "unknown"
	}
	if stream == "" {
		return "arn:aws:logs:us-east-1:homeport:log-group:" + group
	}
	return "arn:aws:logs:us-east-1:homeport:log-group:" + group + ":log-stream:" + stream
}
