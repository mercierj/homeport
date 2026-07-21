package aws

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type DynamoDBAdapter struct {
	mu         sync.Mutex
	tables     map[string]*dynamoTable
	tableQuota int
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type DynamoDBOption func(*DynamoDBAdapter)

type dynamoTable struct {
	Name                   string
	KeyName                string
	AttributeDefinitions   any
	KeySchema              any
	GlobalSecondaryIndexes any
	StreamSpecification    any
	BillingMode            string
	Items                  map[string]map[string]any
	Tags                   map[string]string
}

func NewDynamoDBAdapter(options ...DynamoDBOption) *DynamoDBAdapter {
	adapter := &DynamoDBAdapter{
		tables:     map[string]*dynamoTable{},
		authorizer: authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithDynamoDBAuthorizer(authorizer authz.Authorizer) DynamoDBOption {
	return func(adapter *DynamoDBAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithDynamoDBAuditSink(sink func(authz.Decision)) DynamoDBOption {
	return func(adapter *DynamoDBAdapter) {
		adapter.auditSink = sink
	}
}

func WithDynamoDBTableQuota(maxTables int) DynamoDBOption {
	return func(adapter *DynamoDBAdapter) {
		adapter.tableQuota = maxTables
	}
}

func (DynamoDBAdapter) Provider() string { return "aws" }
func (DynamoDBAdapter) Service() string  { return "dynamodb" }
func (DynamoDBAdapter) Routes() []string { return []string{"POST /compat/aws/dynamodb"} }
func (DynamoDBAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_DYNAMODB": "http://homeport:8080/api/v1/compat/aws/dynamodb",
		"AWS_ACCESS_KEY_ID":         "homeport",
		"AWS_SECRET_ACCESS_KEY":     "homeport",
		"AWS_REGION":                "us-east-1",
		"HOMEPORT_COMPAT_BACKEND":   "scylla-alternator",
	}
}
func (DynamoDBAdapter) ConformanceChecks() []string {
	return []string{"create-table", "describe-table", "list-tables", "put-item", "get-item", "query", "scan", "describe-time-to-live", "list-tags-of-resource", "tag-resource", "untag-resource", "delete-table"}
}

func (a *DynamoDBAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	case "CreateTable":
		name := stringValue(body["TableName"])
		if name == "" {
			writeDynamoValidation(w, "TableName is required")
			return
		}
		if !dynamoTableNameValid(name) {
			writeDynamoValidation(w, "TableName is invalid")
			return
		}
		if _, ok := a.tables[name]; ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceInUseException", "message": "table already exists"})
			return
		}
		if a.tableQuota > 0 && len(a.tables) >= a.tableQuota {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"__type": "LimitExceededException", "message": "table quota exceeded"})
			return
		}
		table := &dynamoTable{
			Name:                   name,
			KeyName:                dynamoKeyName(body["KeySchema"]),
			AttributeDefinitions:   body["AttributeDefinitions"],
			KeySchema:              body["KeySchema"],
			GlobalSecondaryIndexes: body["GlobalSecondaryIndexes"],
			StreamSpecification:    body["StreamSpecification"],
			BillingMode:            stringValue(body["BillingMode"]),
			Items:                  map[string]map[string]any{},
			Tags:                   dynamoTags(body["Tags"]),
		}
		a.tables[name] = table
		writeJSON(w, http.StatusOK, map[string]any{"TableDescription": dynamoTableDescription(table, "ACTIVE")})
	case "DescribeTable":
		table := a.tables[stringValue(body["TableName"])]
		if table == nil {
			writeDynamoNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"Table": dynamoTableDescription(table, "ACTIVE")})
	case "ListTables":
		a.writeListTables(w, body)
	case "PutItem":
		table := a.tables[stringValue(body["TableName"])]
		if table == nil {
			writeDynamoNotFound(w)
			return
		}
		item, ok := body["Item"].(map[string]any)
		if !ok {
			writeDynamoValidation(w, "Item is required")
			return
		}
		key := dynamoAttributeString(item[table.KeyName])
		if key == "" {
			writeDynamoValidation(w, "hash key is required")
			return
		}
		table.Items[key] = item
		writeJSON(w, http.StatusOK, map[string]any{})
	case "GetItem":
		table := a.tables[stringValue(body["TableName"])]
		if table == nil {
			writeDynamoNotFound(w)
			return
		}
		key := dynamoKeyString(table, body["Key"])
		item := table.Items[key]
		if item == nil {
			writeJSON(w, http.StatusOK, map[string]any{})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"Item": item})
	case "Query":
		table := a.tables[stringValue(body["TableName"])]
		if table == nil {
			writeDynamoNotFound(w)
			return
		}
		queryKeyName := table.KeyName
		if indexName := stringValue(body["IndexName"]); indexName != "" {
			queryKeyName = dynamoIndexKeyName(table, indexName)
			if queryKeyName == "" {
				writeDynamoValidation(w, "IndexName is invalid")
				return
			}
		}
		key, ok := dynamoQueryKey(body, queryKeyName)
		if !ok {
			writeDynamoValidation(w, "KeyConditionExpression is invalid")
			return
		}
		items := []map[string]any{}
		if stringValue(body["IndexName"]) != "" {
			for _, item := range table.Items {
				if dynamoAttributeString(item[queryKeyName]) == key {
					items = append(items, item)
				}
			}
		} else if item := table.Items[key]; item != nil {
			items = append(items, item)
		}
		sort.Slice(items, func(i, j int) bool {
			return dynamoAttributeString(items[i][table.KeyName]) < dynamoAttributeString(items[j][table.KeyName])
		})
		start := 0
		if itemKey := dynamoKeyString(table, body["ExclusiveStartKey"]); itemKey != "" {
			found := false
			for i, item := range items {
				if dynamoAttributeString(item[table.KeyName]) == itemKey {
					start = i + 1
					found = true
					break
				}
			}
			if !found {
				writeDynamoValidation(w, "ExclusiveStartKey is invalid")
				return
			}
		}
		limit, ok := cloudWatchLogsLimit(body, 1000, 1000, "Limit")
		if !ok {
			writeDynamoValidation(w, "Limit must be between 1 and 1000")
			return
		}
		end := start + limit
		if end > len(items) {
			end = len(items)
		}
		response := map[string]any{"Items": items[start:end], "Count": end - start, "ScannedCount": end - start}
		if end < len(items) {
			response["LastEvaluatedKey"] = map[string]any{table.KeyName: items[end-1][table.KeyName]}
		}
		writeJSON(w, http.StatusOK, response)
	case "Scan":
		a.writeScan(w, body)
	case "DescribeTimeToLive":
		if a.tables[stringValue(body["TableName"])] == nil {
			writeDynamoNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"TimeToLiveDescription": map[string]string{"TimeToLiveStatus": "DISABLED"}})
	case "ListTagsOfResource":
		table := a.tableByARN(stringValue(body["ResourceArn"]))
		if table == nil {
			writeDynamoNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"Tags": dynamoTagsJSON(table.Tags)})
	case "TagResource":
		table := a.tableByARN(stringValue(body["ResourceArn"]))
		if table == nil {
			writeDynamoNotFound(w)
			return
		}
		mergeStringMap(table.Tags, dynamoTags(body["Tags"]))
		writeJSON(w, http.StatusOK, map[string]any{})
	case "UntagResource":
		table := a.tableByARN(stringValue(body["ResourceArn"]))
		if table == nil {
			writeDynamoNotFound(w)
			return
		}
		for _, key := range kmsStringList(body["TagKeys"]) {
			delete(table.Tags, key)
		}
		writeJSON(w, http.StatusOK, map[string]any{})
	case "DeleteTable":
		name := stringValue(body["TableName"])
		table := a.tables[name]
		if table == nil {
			writeDynamoNotFound(w)
			return
		}
		delete(a.tables, name)
		writeJSON(w, http.StatusOK, map[string]any{"TableDescription": dynamoTableDescription(table, "DELETING")})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "UnknownOperationException", "message": "unsupported DynamoDB action"})
	}
}

func (a *DynamoDBAdapter) writeScan(w http.ResponseWriter, body map[string]any) {
	table := a.tables[stringValue(body["TableName"])]
	if table == nil {
		writeDynamoNotFound(w)
		return
	}
	keys := make([]string, 0, len(table.Items))
	for key := range table.Items {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	start := 0
	if key := dynamoKeyString(table, body["ExclusiveStartKey"]); key != "" {
		index := sort.SearchStrings(keys, key)
		if index == len(keys) || keys[index] != key {
			writeDynamoValidation(w, "ExclusiveStartKey is invalid")
			return
		}
		start = index + 1
	}
	limit, ok := cloudWatchLogsLimit(body, 1000, 1000, "Limit")
	if !ok {
		writeDynamoValidation(w, "Limit must be between 1 and 1000")
		return
	}
	end := start + limit
	if end > len(keys) {
		end = len(keys)
	}
	items := make([]map[string]any, 0, end-start)
	for _, key := range keys[start:end] {
		items = append(items, table.Items[key])
	}
	response := map[string]any{"Items": items, "Count": len(items), "ScannedCount": len(items)}
	if end < len(keys) {
		response["LastEvaluatedKey"] = map[string]any{table.KeyName: table.Items[keys[end-1]][table.KeyName]}
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *DynamoDBAdapter) writeListTables(w http.ResponseWriter, body map[string]any) {
	names := make([]string, 0, len(a.tables))
	for name := range a.tables {
		names = append(names, name)
	}
	sort.Strings(names)

	start := 0
	if after := stringValue(body["ExclusiveStartTableName"]); after != "" {
		if !dynamoTableNameCharsValid(after) {
			writeDynamoValidation(w, "ExclusiveStartTableName is invalid")
			return
		}
		for i, name := range names {
			if name > after {
				start = i
				break
			}
			start = len(names)
		}
	}
	limit, ok := cloudWatchLogsLimit(body, 100, 100, "Limit")
	if !ok {
		writeDynamoValidation(w, "Limit must be between 1 and 100")
		return
	}
	end := start + limit
	if end > len(names) {
		end = len(names)
	}
	response := map[string]any{"TableNames": names[start:end]}
	if end < len(names) {
		response["LastEvaluatedTableName"] = names[end-1]
	}
	writeJSON(w, http.StatusOK, response)
}

func dynamoTableNameCharsValid(name string) bool {
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func dynamoTableNameValid(name string) bool {
	return len(name) >= 3 && len(name) <= 255 && dynamoTableNameCharsValid(name)
}

func (a *DynamoDBAdapter) tableByARN(arn string) *dynamoTable {
	name := arn[strings.LastIndex(arn, "/")+1:]
	return a.tables[name]
}

func (a *DynamoDBAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "dynamodb:" + action,
		Resource:            dynamoARN(body),
		Context: map[string]string{
			"provider":     "aws",
			"service":      "dynamodb",
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"__type": "InternalServerError", "message": err.Error()})
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

func dynamoARN(body map[string]any) string {
	if arn := stringValue(body["ResourceArn"]); arn != "" {
		return arn
	}
	return dynamoTableARN(stringValue(body["TableName"]))
}

func dynamoTableARN(name string) string {
	if name == "" {
		name = "unknown"
	}
	return "arn:aws:dynamodb:us-east-1:homeport:table/" + name
}

func dynamoTableDescription(table *dynamoTable, status string) map[string]any {
	description := map[string]any{
		"TableName":              table.Name,
		"TableStatus":            status,
		"TableArn":               "arn:aws:dynamodb:us-east-1:000000000000:table/" + table.Name,
		"AttributeDefinitions":   table.AttributeDefinitions,
		"KeySchema":              table.KeySchema,
		"GlobalSecondaryIndexes": dynamoIndexDescriptions(table),
		"BillingModeSummary":     map[string]string{"BillingMode": table.BillingMode},
	}
	if dynamoStreamEnabled(table.StreamSpecification) {
		description["StreamSpecification"] = table.StreamSpecification
		description["LatestStreamArn"] = dynamoTableARN(table.Name) + "/stream/2026-07-09T00:00:00.000"
		description["LatestStreamLabel"] = "2026-07-09T00:00:00.000"
	}
	return description
}

func dynamoIndexDescriptions(table *dynamoTable) []map[string]any {
	indexes, _ := table.GlobalSecondaryIndexes.([]any)
	out := make([]map[string]any, 0, len(indexes))
	for _, item := range indexes {
		index, _ := item.(map[string]any)
		if index == nil {
			continue
		}
		description := map[string]any{}
		for key, value := range index {
			description[key] = value
		}
		description["IndexStatus"] = "ACTIVE"
		if name := stringValue(index["IndexName"]); name != "" {
			description["IndexArn"] = dynamoTableARN(table.Name) + "/index/" + name
		}
		out = append(out, description)
	}
	return out
}

func dynamoStreamEnabled(value any) bool {
	spec, _ := value.(map[string]any)
	enabled, _ := spec["StreamEnabled"].(bool)
	return enabled
}

func dynamoTags(value any) map[string]string {
	tags := map[string]string{}
	items, _ := value.([]any)
	for _, item := range items {
		tag, _ := item.(map[string]any)
		key := stringValue(tag["Key"])
		if key != "" {
			tags[key] = stringValue(tag["Value"])
		}
	}
	return tags
}

func dynamoTagsJSON(tags map[string]string) []map[string]string {
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

func dynamoKeyName(value any) string {
	items, _ := value.([]any)
	for _, item := range items {
		entry, _ := item.(map[string]any)
		if stringValue(entry["KeyType"]) == "HASH" {
			return stringValue(entry["AttributeName"])
		}
	}
	return "id"
}

func dynamoIndexKeyName(table *dynamoTable, indexName string) string {
	indexes, _ := table.GlobalSecondaryIndexes.([]any)
	for _, item := range indexes {
		index, _ := item.(map[string]any)
		if stringValue(index["IndexName"]) == indexName {
			return dynamoKeyName(index["KeySchema"])
		}
	}
	return ""
}

func dynamoKeyString(table *dynamoTable, value any) string {
	key, _ := value.(map[string]any)
	return dynamoAttributeString(key[table.KeyName])
}

func dynamoQueryKey(body map[string]any, expectedName string) (string, bool) {
	left, right, ok := strings.Cut(stringValue(body["KeyConditionExpression"]), "=")
	if !ok {
		return "", false
	}
	name := strings.TrimSpace(left)
	if names, ok := body["ExpressionAttributeNames"].(map[string]any); ok {
		name = stringValue(names[name])
	}
	if name != expectedName {
		return "", false
	}
	parts := strings.Fields(right)
	if len(parts) == 0 {
		return "", false
	}
	values, _ := body["ExpressionAttributeValues"].(map[string]any)
	value, ok := values[parts[0]]
	return dynamoAttributeString(value), ok
}

func dynamoAttributeString(value any) string {
	attr, _ := value.(map[string]any)
	return stringValue(attr["S"])
}

func writeDynamoNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "table not found"})
}

func writeDynamoValidation(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ValidationException", "message": message})
}
