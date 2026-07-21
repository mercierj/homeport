package aws

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type EventBridgeAdapter struct {
	mu         sync.Mutex
	rules      map[string]eventRule
	nextID     int
	ruleQuota  int
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type EventBridgeOption func(*EventBridgeAdapter)

type eventRule struct {
	Name         string
	Arn          string
	EventBusName string
	EventPattern string
	Description  string
	ScheduleExpr string
	RoleARN      string
	State        string
	Tags         map[string]string
	Targets      map[string]eventTarget
}

type eventTarget struct {
	ID  string
	ARN string
}

func NewEventBridgeAdapter(options ...EventBridgeOption) *EventBridgeAdapter {
	adapter := &EventBridgeAdapter{rules: map[string]eventRule{}, authorizer: authz.AllowAll}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithEventBridgeAuthorizer(authorizer authz.Authorizer) EventBridgeOption {
	return func(adapter *EventBridgeAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithEventBridgeAuditSink(sink func(authz.Decision)) EventBridgeOption {
	return func(adapter *EventBridgeAdapter) {
		adapter.auditSink = sink
	}
}

func WithEventBridgeRuleQuota(maxRules int) EventBridgeOption {
	return func(adapter *EventBridgeAdapter) {
		adapter.ruleQuota = maxRules
	}
}

func (EventBridgeAdapter) Provider() string { return "aws" }
func (EventBridgeAdapter) Service() string  { return "eventbridge" }
func (EventBridgeAdapter) Routes() []string { return []string{"POST /compat/aws/eventbridge"} }
func (EventBridgeAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_EVENTBRIDGE": "http://homeport:8080/api/v1/compat/aws/eventbridge",
		"HOMEPORT_COMPAT_BACKEND":      "n8n",
	}
}
func (EventBridgeAdapter) ConformanceChecks() []string {
	return []string{"put-rule", "describe-rule", "list-rules", "put-events", "put-targets", "list-targets-by-rule", "list-rule-names-by-target", "remove-targets", "enable-rule", "disable-rule", "delete-rule", "tag-resource", "list-tags-for-resource", "untag-resource"}
}

func (a *EventBridgeAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeEventBridgeError(w, "ValidationException", err.Error())
		return
	}
	if action != "PutEvents" && !a.authorized(w, r, action, eventBridgeResource(body)) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "PutRule":
		name := stringValue(body["Name"])
		busName := eventBridgeBusName(body)
		key := eventBridgeRuleKey(busName, name)
		if !validEventBridgeRuleName(name) {
			writeEventBridgeError(w, "ValidationException", "Name must be 1-64 letters, numbers, periods, hyphens, or underscores")
			return
		}
		if pattern := stringValue(body["EventPattern"]); pattern != "" && !validEventBridgePattern(pattern) {
			writeEventBridgeError(w, "InvalidEventPatternException", "EventPattern must be valid JSON")
			return
		}
		if stringValue(body["EventPattern"]) == "" && stringValue(body["ScheduleExpression"]) == "" {
			writeEventBridgeError(w, "ValidationException", "EventPattern or ScheduleExpression is required")
			return
		}
		if state := stringValue(body["State"]); state != "" && state != "ENABLED" && state != "DISABLED" && state != "ENABLED_WITH_ALL_CLOUDTRAIL_MANAGEMENT_EVENTS" {
			writeEventBridgeError(w, "ValidationException", "invalid State")
			return
		}
		if _, exists := a.rules[key]; !exists && a.ruleQuota > 0 && len(a.rules) >= a.ruleQuota {
			writeEventBridgeError(w, "LimitExceededException", "rule quota exceeded")
			return
		}
		rule := eventRule{
			Name:         name,
			Arn:          eventBridgeRuleARN(busName, name),
			EventBusName: busName,
			EventPattern: stringValue(body["EventPattern"]),
			Description:  stringValue(body["Description"]),
			ScheduleExpr: stringValue(body["ScheduleExpression"]),
			RoleARN:      stringValue(body["RoleArn"]),
			State:        stringValue(body["State"]),
			Tags:         eventBridgeTags(body["Tags"]),
		}
		if rule.State == "" {
			rule.State = "ENABLED"
		}
		if existing, ok := a.rules[key]; ok {
			rule.Targets = existing.Targets
			rule.Tags = existing.Tags
		}
		a.rules[key] = rule
		writeJSON(w, http.StatusOK, map[string]string{"RuleArn": rule.Arn})
	case "DescribeRule":
		rule, ok := a.rules[eventBridgeRuleKey(eventBridgeBusName(body), stringValue(body["Name"]))]
		if !ok {
			writeEventBridgeError(w, "ResourceNotFoundException", "rule not found")
			return
		}
		writeJSON(w, http.StatusOK, eventBridgeRuleShape(rule))
	case "ListRules":
		keys := make([]string, 0, len(a.rules))
		busName := eventBridgeBusName(body)
		for key, rule := range a.rules {
			if rule.EventBusName == busName && (stringValue(body["NamePrefix"]) == "" || strings.HasPrefix(rule.Name, stringValue(body["NamePrefix"]))) {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		start, ok := eventBridgePageStart(stringValue(body["NextToken"]), len(keys))
		if !ok {
			writeEventBridgeError(w, "InvalidToken", "invalid NextToken")
			return
		}
		limit, ok := cloudWatchLogsLimit(body, 100, 100, "Limit")
		if !ok {
			writeEventBridgeError(w, "ValidationException", "Limit must be between 1 and 100")
			return
		}
		end := start + limit
		if end > len(keys) {
			end = len(keys)
		}
		rules := make([]map[string]string, 0, end-start)
		for _, key := range keys[start:end] {
			rule := a.rules[key]
			rules = append(rules, eventBridgeRuleShape(rule))
		}
		response := map[string]any{"Rules": rules}
		if end < len(keys) {
			response["NextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "PutEvents":
		entries, _ := body["Entries"].([]any)
		if len(entries) == 0 || len(entries) > 10 {
			writeEventBridgeError(w, "ValidationException", "Entries must contain between 1 and 10 items")
			return
		}
		out := make([]map[string]string, len(entries))
		failed := 0
		for i, raw := range entries {
			entry, _ := raw.(map[string]any)
			if !a.authorized(w, r, action, eventBridgeEntryResource(entry)) {
				return
			}
			if stringValue(entry["Source"]) == "" || stringValue(entry["DetailType"]) == "" || stringValue(entry["Detail"]) == "" {
				failed++
				out[i] = map[string]string{"ErrorCode": "InvalidArgument", "ErrorMessage": "Source, DetailType, and Detail are required"}
				continue
			}
			if !validEventBridgePattern(stringValue(entry["Detail"])) {
				failed++
				out[i] = map[string]string{"ErrorCode": "MalformedDetail", "ErrorMessage": "Detail must be a JSON object"}
				continue
			}
			a.nextID++
			out[i] = map[string]string{"EventId": "homeport-event-" + strconv.Itoa(a.nextID)}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"FailedEntryCount": failed,
			"Entries":          out,
		})
	case "PutTargets":
		targets := eventBridgeObjects(body["Targets"])
		if len(targets) == 0 || len(targets) > 10 {
			writeEventBridgeError(w, "ValidationException", "Targets must contain between 1 and 10 entries")
			return
		}
		key := eventBridgeRuleKey(eventBridgeBusName(body), stringValue(body["Rule"]))
		rule, ok := a.rules[key]
		if !ok {
			writeEventBridgeError(w, "ResourceNotFoundException", "rule not found")
			return
		}
		if rule.Targets == nil {
			rule.Targets = map[string]eventTarget{}
		}
		newTargets := len(rule.Targets)
		for _, raw := range targets {
			id, arn := stringValue(raw["Id"]), stringValue(raw["Arn"])
			if id == "" || arn == "" {
				writeEventBridgeError(w, "ValidationException", "target Id and Arn are required")
				return
			}
			if _, exists := rule.Targets[id]; !exists {
				newTargets++
			}
		}
		if newTargets > 5 {
			writeEventBridgeError(w, "LimitExceededException", "rule target quota exceeded")
			return
		}
		for _, raw := range targets {
			id, arn := stringValue(raw["Id"]), stringValue(raw["Arn"])
			rule.Targets[id] = eventTarget{ID: id, ARN: arn}
		}
		a.rules[key] = rule
		writeJSON(w, http.StatusOK, map[string]any{"FailedEntryCount": 0, "FailedEntries": []any{}})
	case "ListTargetsByRule":
		rule, ok := a.rules[eventBridgeRuleKey(eventBridgeBusName(body), stringValue(body["Rule"]))]
		if !ok {
			writeEventBridgeError(w, "ResourceNotFoundException", "rule not found")
			return
		}
		ids := make([]string, 0, len(rule.Targets))
		for id := range rule.Targets {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		start, ok := eventBridgePageStart(stringValue(body["NextToken"]), len(ids))
		if !ok {
			writeEventBridgeError(w, "InvalidToken", "invalid NextToken")
			return
		}
		limit, ok := cloudWatchLogsLimit(body, 100, 100, "Limit")
		if !ok {
			writeEventBridgeError(w, "ValidationException", "Limit must be between 1 and 100")
			return
		}
		end := start + limit
		if end > len(ids) {
			end = len(ids)
		}
		targets := make([]map[string]string, 0, end-start)
		for _, id := range ids[start:end] {
			target := rule.Targets[id]
			targets = append(targets, map[string]string{"Id": target.ID, "Arn": target.ARN})
		}
		response := map[string]any{"Targets": targets}
		if end < len(ids) {
			response["NextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "ListRuleNamesByTarget":
		targetARN := stringValue(body["TargetArn"])
		names := make([]string, 0)
		busName := eventBridgeBusName(body)
		for _, rule := range a.rules {
			for _, target := range rule.Targets {
				if rule.EventBusName == busName && target.ARN == targetARN {
					names = append(names, rule.Name)
					break
				}
			}
		}
		sort.Strings(names)
		start, ok := eventBridgePageStart(stringValue(body["NextToken"]), len(names))
		if !ok {
			writeEventBridgeError(w, "InvalidToken", "invalid NextToken")
			return
		}
		limit, ok := cloudWatchLogsLimit(body, 100, 100, "Limit")
		if !ok {
			writeEventBridgeError(w, "ValidationException", "Limit must be between 1 and 100")
			return
		}
		end := start + limit
		if end > len(names) {
			end = len(names)
		}
		response := map[string]any{"RuleNames": names[start:end]}
		if end < len(names) {
			response["NextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "RemoveTargets":
		key := eventBridgeRuleKey(eventBridgeBusName(body), stringValue(body["Rule"]))
		rule, ok := a.rules[key]
		if !ok {
			writeEventBridgeError(w, "ResourceNotFoundException", "rule not found")
			return
		}
		for _, id := range eventBridgeStrings(body["Ids"]) {
			delete(rule.Targets, id)
		}
		a.rules[key] = rule
		writeJSON(w, http.StatusOK, map[string]any{"FailedEntryCount": 0, "FailedEntries": []any{}})
	case "EnableRule", "DisableRule":
		name := stringValue(body["Name"])
		key := eventBridgeRuleKey(eventBridgeBusName(body), name)
		rule, ok := a.rules[key]
		if !ok {
			writeEventBridgeError(w, "ResourceNotFoundException", "rule not found")
			return
		}
		if action == "EnableRule" {
			rule.State = "ENABLED"
		} else {
			rule.State = "DISABLED"
		}
		a.rules[key] = rule
		writeJSON(w, http.StatusOK, map[string]string{})
	case "DeleteRule":
		name := stringValue(body["Name"])
		key := eventBridgeRuleKey(eventBridgeBusName(body), name)
		if _, ok := a.rules[key]; !ok {
			writeEventBridgeError(w, "ResourceNotFoundException", "rule not found")
			return
		}
		delete(a.rules, key)
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ListTagsForResource":
		_, rule := a.ruleByARN(stringValue(body["ResourceARN"]))
		if rule == nil {
			writeEventBridgeError(w, "ResourceNotFoundException", "rule not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"Tags": eventBridgeTagsJSON(rule.Tags)})
	case "TagResource":
		key, rule := a.ruleByARN(stringValue(body["ResourceARN"]))
		if rule == nil {
			writeEventBridgeError(w, "ResourceNotFoundException", "rule not found")
			return
		}
		if rule.Tags == nil {
			rule.Tags = map[string]string{}
		}
		mergeStringMap(rule.Tags, eventBridgeTags(body["Tags"]))
		a.rules[key] = *rule
		writeJSON(w, http.StatusOK, map[string]string{})
	case "UntagResource":
		key, rule := a.ruleByARN(stringValue(body["ResourceARN"]))
		if rule == nil {
			writeEventBridgeError(w, "ResourceNotFoundException", "rule not found")
			return
		}
		for _, key := range eventBridgeTagKeys(body["TagKeys"]) {
			delete(rule.Tags, key)
		}
		a.rules[key] = *rule
		writeJSON(w, http.StatusOK, map[string]string{})
	default:
		writeEventBridgeError(w, "UnsupportedOperation", "EventBridge action is not implemented")
	}
}

func (a *EventBridgeAdapter) ruleByARN(arn string) (string, *eventRule) {
	for key, rule := range a.rules {
		if rule.Arn == arn {
			return key, &rule
		}
	}
	return "", nil
}

func eventBridgeRuleARN(busName, name string) string {
	if busName == "default" {
		return "arn:aws:events:us-east-1:000000000000:rule/" + name
	}
	return "arn:aws:events:us-east-1:000000000000:rule/" + eventBridgeBusID(busName) + "/" + name
}

func eventBridgeBusARN(name string) string {
	return "arn:aws:events:us-east-1:000000000000:event-bus/" + name
}

func eventBridgeBusName(body map[string]any) string {
	if name := stringValue(body["EventBusName"]); name != "" {
		return eventBridgeBusID(name)
	}
	return "default"
}

func eventBridgeBusID(name string) string {
	if strings.HasPrefix(name, "arn:") {
		return name[strings.LastIndex(name, "/")+1:]
	}
	return name
}

func eventBridgeRuleKey(busName, name string) string {
	return eventBridgeBusID(busName) + "\x00" + name
}

func eventBridgeRuleShape(rule eventRule) map[string]string {
	return map[string]string{
		"Name":               rule.Name,
		"Arn":                rule.Arn,
		"EventBusName":       rule.EventBusName,
		"EventPattern":       rule.EventPattern,
		"Description":        rule.Description,
		"ScheduleExpression": rule.ScheduleExpr,
		"RoleArn":            rule.RoleARN,
		"State":              rule.State,
	}
}

func eventBridgeTags(raw any) map[string]string {
	values, _ := raw.([]any)
	tags := map[string]string{}
	for _, value := range values {
		tag, _ := value.(map[string]any)
		key := stringValue(tag["Key"])
		if key != "" {
			tags[key] = stringValue(tag["Value"])
		}
	}
	return tags
}

func eventBridgeTagsJSON(tags map[string]string) []map[string]string {
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

func eventBridgeTagKeys(raw any) []string {
	values, _ := raw.([]any)
	keys := make([]string, 0, len(values))
	for _, value := range values {
		if key := stringValue(value); key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

func eventBridgeObjects(raw any) []map[string]any {
	values, _ := raw.([]any)
	objects := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if object, ok := value.(map[string]any); ok {
			objects = append(objects, object)
		}
	}
	return objects
}

func eventBridgeStrings(raw any) []string {
	values, _ := raw.([]any)
	strings := make([]string, 0, len(values))
	for _, value := range values {
		if value := stringValue(value); value != "" {
			strings = append(strings, value)
		}
	}
	return strings
}

func eventBridgePageStart(token string, count int) (int, bool) {
	if token == "" {
		return 0, true
	}
	start, err := strconv.Atoi(token)
	return start, err == nil && start >= 0 && start <= count
}

func validEventBridgeRuleName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validEventBridgePattern(pattern string) bool {
	var object map[string]any
	return json.Unmarshal([]byte(pattern), &object) == nil && object != nil
}

func eventBridgeResource(body map[string]any) string {
	if arn := stringValue(body["ResourceARN"]); arn != "" {
		return arn
	}
	if name := stringValue(body["Rule"]); name != "" {
		return eventBridgeRuleARN(eventBridgeBusName(body), name)
	}
	if name := stringValue(body["Name"]); name != "" {
		return eventBridgeRuleARN(eventBridgeBusName(body), name)
	}
	if name := stringValue(body["EventBusName"]); name != "" {
		if strings.HasPrefix(name, "arn:") {
			return name
		}
		return eventBridgeBusARN(name)
	}
	return "*"
}

func eventBridgeEntryResource(entry map[string]any) string {
	if name := stringValue(entry["EventBusName"]); name != "" {
		if strings.HasPrefix(name, "arn:") {
			return name
		}
		return eventBridgeBusARN(name)
	}
	return eventBridgeBusARN("default")
}

func (a *EventBridgeAdapter) authorized(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "events:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "aws",
			"service":      "eventbridge",
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
		writeEventBridgeErrorStatus(w, http.StatusInternalServerError, "InternalException", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeEventBridgeErrorStatus(w, http.StatusForbidden, "AccessDeniedException", decision.Reason)
		return false
	}
	return true
}

func writeEventBridgeError(w http.ResponseWriter, code, message string) {
	writeEventBridgeErrorStatus(w, http.StatusBadRequest, code, message)
}

func writeEventBridgeErrorStatus(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"__type": code, "message": message})
}
