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

type AppSyncAdapter struct {
	mu         sync.Mutex
	apis       map[string]appSyncAPI
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type AppSyncOption func(*AppSyncAdapter)

type appSyncAPI struct {
	id, name, authenticationType string
	createdAt                    time.Time
	tags                         map[string]string
	apiKeys                      map[string]appSyncAPIKey
	xrayEnabled                  bool
	introspectionConfig          string
	dataSources                  map[string]appSyncDataSource
	resolvers                    map[string]appSyncResolver
}
type appSyncAPIKey struct {
	id, description string
	expires         int64
}
type appSyncDataSource struct {
	name, description, sourceType string
}
type appSyncResolver struct{ typeName, fieldName, kind string }

func NewAppSyncAdapter(options ...AppSyncOption) *AppSyncAdapter {
	adapter := &AppSyncAdapter{apis: map[string]appSyncAPI{}, authorizer: authz.AllowAll}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}
func WithAppSyncAuthorizer(authorizer authz.Authorizer) AppSyncOption {
	return func(adapter *AppSyncAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}
func WithAppSyncAuditSink(sink func(authz.Decision)) AppSyncOption {
	return func(adapter *AppSyncAdapter) { adapter.auditSink = sink }
}
func (AppSyncAdapter) Provider() string { return "aws" }
func (AppSyncAdapter) Service() string  { return "appsync" }
func (AppSyncAdapter) Routes() []string { return []string{"ANY /compat/aws/appsync"} }
func (AppSyncAdapter) TargetEnv() map[string]string {
	return map[string]string{"AWS_ENDPOINT_URL_APPSYNC": "http://homeport:8080/api/v1/compat/aws/appsync", "HOMEPORT_COMPAT_BACKEND": "hasura"}
}
func (AppSyncAdapter) ConformanceChecks() []string {
	return []string{"create-graphql-api", "get-graphql-api", "list-graphql-apis", "update-graphql-api", "delete-graphql-api", "xray-configuration", "introspection-configuration", "tag-resource", "list-tags-for-resource", "untag-resource", "create-api-key", "list-api-keys", "update-api-key", "delete-api-key", "create-data-source", "get-data-source", "list-data-sources", "update-data-source", "delete-data-source"}
}

func (a *AppSyncAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/apis")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if !a.authorized(w, r, path, body) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && path == "":
		name, auth := stringValue(body["name"]), stringValue(body["authenticationType"])
		if name == "" || !validAppSyncAuthenticationType(auth) {
			writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "name and a valid authenticationType are required")
			return
		}
		for _, api := range a.apis {
			if api.name == name {
				writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "API already exists")
				return
			}
		}
		introspection, ok := appSyncIntrospectionConfig(body["introspectionConfig"])
		if !ok {
			writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "introspectionConfig is invalid")
			return
		}
		api := appSyncAPI{id: name, name: name, authenticationType: auth, createdAt: time.Now().UTC(), tags: appSyncTags(body["tags"]), apiKeys: map[string]appSyncAPIKey{}, dataSources: map[string]appSyncDataSource{}, resolvers: map[string]appSyncResolver{}, xrayEnabled: appSyncBool(body["xrayEnabled"]), introspectionConfig: introspection}
		a.apis[api.id] = api
		writeJSON(w, http.StatusOK, map[string]any{"graphqlApi": api.shape()})
	case r.Method == http.MethodGet && path == "":
		apis := make([]appSyncAPI, 0, len(a.apis))
		for _, api := range a.apis {
			apis = append(apis, api)
		}
		sort.Slice(apis, func(i, j int) bool { return apis[i].name < apis[j].name })
		start, ok := appSyncStart(r, body, len(apis))
		if !ok {
			writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "nextToken is invalid")
			return
		}
		limit, ok := appSyncLimit(r, body)
		if !ok {
			writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "maxResults must be between 1 and 25")
			return
		}
		end := start + limit
		if end > len(apis) {
			end = len(apis)
		}
		out := make([]map[string]any, 0, end-start)
		for _, api := range apis[start:end] {
			out = append(out, api.shape())
		}
		response := map[string]any{"graphqlApis": out}
		if end < len(apis) {
			response["nextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case strings.HasPrefix(r.URL.Path, "/v1/tags/"):
		id := appSyncIDFromARN(strings.TrimPrefix(r.URL.Path, "/v1/tags/"))
		api, ok := a.apis[id]
		if !ok {
			writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "API does not exist")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"tags": api.tags})
		case http.MethodPost:
			for key, value := range appSyncTags(body["tags"]) {
				api.tags[key] = value
			}
			a.apis[id] = api
			writeJSON(w, http.StatusOK, map[string]any{})
		case http.MethodDelete:
			for _, key := range r.URL.Query()["tagKeys"] {
				delete(api.tags, key)
			}
			a.apis[id] = api
			writeJSON(w, http.StatusOK, map[string]any{})
		default:
			writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "unsupported action")
		}
	case strings.Contains(path, "/types/") && strings.Contains(path, "/resolvers"):
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		api, ok := a.apis[parts[0]]
		if !ok {
			writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "API does not exist")
			return
		}
		typeName, fieldName := stringValue(body["typeName"]), stringValue(body["fieldName"])
		if len(parts) > 2 {
			typeName = parts[2]
		}
		if len(parts) > 4 {
			fieldName = parts[4]
		}
		key := typeName + "." + fieldName
		switch r.Method {
		case http.MethodPost:
			if typeName == "" || fieldName == "" {
				writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "typeName and fieldName are required")
				return
			}
			resolver := appSyncResolver{typeName: typeName, fieldName: fieldName, kind: stringValue(body["kind"])}
			if resolver.kind == "" {
				resolver.kind = "UNIT"
			}
			api.resolvers[key] = resolver
			a.apis[api.id] = api
			writeJSON(w, http.StatusOK, map[string]any{"resolver": resolver.shape(api.id)})
		case http.MethodGet:
			resolver, ok := api.resolvers[key]
			if !ok {
				writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "resolver does not exist")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"resolver": resolver.shape(api.id)})
		default:
			writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "unsupported action")
		}
	case strings.Contains(path, "/datasources"):
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		api, ok := a.apis[parts[0]]
		if !ok {
			writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "API does not exist")
			return
		}
		name := ""
		if len(parts) > 2 {
			name = parts[2]
		}
		switch {
		case r.Method == http.MethodPost && name == "":
			name, sourceType := stringValue(body["name"]), stringValue(body["type"])
			if name == "" || sourceType == "" {
				writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "name and type are required")
				return
			}
			if _, exists := api.dataSources[name]; exists {
				writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "data source already exists")
				return
			}
			source := appSyncDataSource{name: name, description: stringValue(body["description"]), sourceType: sourceType}
			api.dataSources[name] = source
			a.apis[api.id] = api
			writeJSON(w, http.StatusOK, map[string]any{"dataSource": source.shape(api.id)})
		case r.Method == http.MethodGet && name == "":
			sources := make([]appSyncDataSource, 0, len(api.dataSources))
			for _, source := range api.dataSources {
				sources = append(sources, source)
			}
			sort.Slice(sources, func(i, j int) bool { return sources[i].name < sources[j].name })
			start, ok := appSyncStart(r, body, len(sources))
			if !ok {
				writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "nextToken is invalid")
				return
			}
			limit, ok := appSyncLimit(r, body)
			if !ok {
				writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "maxResults must be between 1 and 25")
				return
			}
			end := start + limit
			if end > len(sources) {
				end = len(sources)
			}
			out := make([]map[string]any, 0, end-start)
			for _, source := range sources[start:end] {
				out = append(out, source.shape(api.id))
			}
			response := map[string]any{"dataSources": out}
			if end < len(sources) {
				response["nextToken"] = strconv.Itoa(end)
			}
			writeJSON(w, http.StatusOK, response)
		case r.Method == http.MethodGet && name != "":
			source, ok := api.dataSources[name]
			if !ok {
				writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "data source does not exist")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"dataSource": source.shape(api.id)})
		case r.Method == http.MethodPost && name != "":
			source, ok := api.dataSources[name]
			if !ok {
				writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "data source does not exist")
				return
			}
			if sourceType := stringValue(body["type"]); sourceType != "" {
				source.sourceType = sourceType
			}
			source.description = stringValue(body["description"])
			api.dataSources[name] = source
			a.apis[api.id] = api
			writeJSON(w, http.StatusOK, map[string]any{"dataSource": source.shape(api.id)})
		case r.Method == http.MethodDelete && name != "":
			if _, ok := api.dataSources[name]; !ok {
				writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "data source does not exist")
				return
			}
			delete(api.dataSources, name)
			a.apis[api.id] = api
			writeJSON(w, http.StatusOK, map[string]any{})
		default:
			writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "unsupported action")
		}
	case strings.Contains(path, "/apikeys"):
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		api, ok := a.apis[parts[0]]
		if !ok {
			writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "API does not exist")
			return
		}
		keyID := ""
		if len(parts) > 2 {
			keyID = parts[2]
		}
		switch {
		case r.Method == http.MethodPost && keyID == "":
			keyID = "key-" + strconv.Itoa(len(api.apiKeys)+1)
			key := appSyncAPIKey{id: keyID, description: stringValue(body["description"]), expires: appSyncInt64(body["expires"])}
			api.apiKeys[keyID] = key
			a.apis[api.id] = api
			writeJSON(w, http.StatusOK, map[string]any{"apiKey": key.shape()})
		case r.Method == http.MethodGet && keyID == "":
			keys := make([]appSyncAPIKey, 0, len(api.apiKeys))
			for _, key := range api.apiKeys {
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool { return keys[i].id < keys[j].id })
			start, ok := appSyncStart(r, body, len(keys))
			if !ok {
				writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "nextToken is invalid")
				return
			}
			limit, ok := appSyncLimit(r, body)
			if !ok {
				writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "maxResults must be between 1 and 25")
				return
			}
			end := start + limit
			if end > len(keys) {
				end = len(keys)
			}
			out := make([]map[string]any, 0, end-start)
			for _, key := range keys[start:end] {
				out = append(out, key.shape())
			}
			response := map[string]any{"apiKeys": out}
			if end < len(keys) {
				response["nextToken"] = strconv.Itoa(end)
			}
			writeJSON(w, http.StatusOK, response)
		case r.Method == http.MethodPost && keyID != "":
			key, ok := api.apiKeys[keyID]
			if !ok {
				writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "API key does not exist")
				return
			}
			key.description = stringValue(body["description"])
			key.expires = appSyncInt64(body["expires"])
			api.apiKeys[keyID] = key
			a.apis[api.id] = api
			writeJSON(w, http.StatusOK, map[string]any{"apiKey": key.shape()})
		case r.Method == http.MethodDelete && keyID != "":
			if _, ok := api.apiKeys[keyID]; !ok {
				writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "API key does not exist")
				return
			}
			delete(api.apiKeys, keyID)
			a.apis[api.id] = api
			writeJSON(w, http.StatusOK, map[string]any{})
		default:
			writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "unsupported action")
		}
	case path != "":
		id := strings.TrimPrefix(path, "/")
		api, ok := a.apis[id]
		if !ok {
			writeAppSyncError(w, http.StatusNotFound, "NotFoundException", "API does not exist")
			return
		}
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{"graphqlApi": api.shape()})
		case http.MethodPost:
			name, auth := stringValue(body["name"]), stringValue(body["authenticationType"])
			if name == "" || !validAppSyncAuthenticationType(auth) {
				writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "name and a valid authenticationType are required")
				return
			}
			introspection, ok := appSyncIntrospectionConfig(body["introspectionConfig"])
			if !ok {
				writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "introspectionConfig is invalid")
				return
			}
			api.name, api.authenticationType, api.xrayEnabled, api.introspectionConfig = name, auth, appSyncBool(body["xrayEnabled"]), introspection
			a.apis[id] = api
			writeJSON(w, http.StatusOK, map[string]any{"graphqlApi": api.shape()})
		case http.MethodDelete:
			delete(a.apis, id)
			writeJSON(w, http.StatusOK, map[string]any{"graphqlApi": api.shape()})
		default:
			writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "unsupported action")
		}
	default:
		writeAppSyncError(w, http.StatusBadRequest, "BadRequestException", "unsupported action")
	}
}

func (api appSyncAPI) shape() map[string]any {
	return map[string]any{"apiId": api.id, "name": api.name, "authenticationType": api.authenticationType, "arn": "arn:aws:appsync:us-east-1:000000000000:apis/" + api.id, "xrayEnabled": api.xrayEnabled, "introspectionConfig": api.introspectionConfig}
}
func (key appSyncAPIKey) shape() map[string]any {
	return map[string]any{"id": key.id, "description": key.description, "expires": key.expires, "deletes": key.expires + 60*24*60*60}
}
func (source appSyncDataSource) shape(apiID string) map[string]any {
	return map[string]any{"name": source.name, "description": source.description, "type": source.sourceType, "dataSourceArn": "arn:aws:appsync:us-east-1:000000000000:apis/" + apiID + "/datasources/" + source.name}
}
func (resolver appSyncResolver) shape(apiID string) map[string]any {
	return map[string]any{"typeName": resolver.typeName, "fieldName": resolver.fieldName, "kind": resolver.kind, "resolverArn": "arn:aws:appsync:us-east-1:000000000000:apis/" + apiID + "/types/" + resolver.typeName + "/resolvers/" + resolver.fieldName}
}
func validAppSyncAuthenticationType(value string) bool {
	return value == "API_KEY" || value == "AWS_IAM" || value == "AMAZON_COGNITO_USER_POOLS" || value == "OPENID_CONNECT" || value == "AWS_LAMBDA"
}
func appSyncStart(r *http.Request, body map[string]any, count int) (int, bool) {
	token := appSyncString(r, body, "nextToken")
	if token == "" {
		return 0, true
	}
	start, err := strconv.Atoi(token)
	return start, err == nil && start >= 0 && start < count
}
func appSyncLimit(r *http.Request, body map[string]any) (int, bool) {
	value := appSyncString(r, body, "maxResults")
	if value == "" {
		return 25, true
	}
	maxResults, err := strconv.Atoi(value)
	if err == nil && maxResults > 0 && maxResults <= 25 {
		return maxResults, true
	}
	return 0, false
}
func appSyncString(r *http.Request, body map[string]any, key string) string {
	if value := r.URL.Query().Get(key); value != "" {
		return value
	}
	return stringValue(body[key])
}
func appSyncInt64(value any) int64 {
	if value, ok := value.(float64); ok {
		return int64(value)
	}
	return 0
}
func appSyncBool(value any) bool {
	enabled, _ := value.(bool)
	return enabled
}
func appSyncIntrospectionConfig(value any) (string, bool) {
	config := stringValue(value)
	if config == "" {
		return "ENABLED", true
	}
	return config, config == "ENABLED" || config == "DISABLED"
}
func appSyncTags(value any) map[string]string {
	tags := map[string]string{}
	for key, value := range mapValue(value) {
		if stringValue(value) != "" {
			tags[key] = stringValue(value)
		}
	}
	return tags
}
func appSyncIDFromARN(arn string) string {
	return strings.TrimPrefix(arn, "arn:aws:appsync:us-east-1:000000000000:apis/")
}
func writeAppSyncError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"message": message, "__type": code})
}

func (a *AppSyncAdapter) authorized(w http.ResponseWriter, r *http.Request, path string, body map[string]any) bool {
	id := strings.TrimPrefix(path, "/")
	if strings.Contains(path, "/apikeys") {
		id = strings.Split(strings.TrimPrefix(path, "/"), "/")[0]
	}
	if strings.Contains(path, "/datasources") {
		id = strings.Split(strings.TrimPrefix(path, "/"), "/")[0]
	}
	if strings.HasPrefix(r.URL.Path, "/v1/tags/") {
		id = appSyncIDFromARN(strings.TrimPrefix(r.URL.Path, "/v1/tags/"))
	}
	if id == "" && r.Method == http.MethodPost {
		id = stringValue(body["name"])
	}
	if id == "" {
		id = "*"
	}
	action := map[string]string{http.MethodPost: "CreateGraphqlApi", http.MethodGet: "ListGraphqlApis", http.MethodDelete: "DeleteGraphqlApi"}[r.Method]
	if path != "" {
		if r.Method == http.MethodPost {
			action = "UpdateGraphqlApi"
		}
		if r.Method == http.MethodGet {
			action = "GetGraphqlApi"
		}
	}
	if strings.HasPrefix(r.URL.Path, "/v1/tags/") {
		action = map[string]string{http.MethodGet: "ListTagsForResource", http.MethodPost: "TagResource", http.MethodDelete: "UntagResource"}[r.Method]
	}
	if strings.Contains(path, "/apikeys") {
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		keyID := ""
		if len(parts) > 2 {
			keyID = parts[2]
		}
		switch {
		case r.Method == http.MethodPost && keyID == "":
			action = "CreateApiKey"
		case r.Method == http.MethodGet && keyID == "":
			action = "ListApiKeys"
		case r.Method == http.MethodPost:
			action = "UpdateApiKey"
		case r.Method == http.MethodDelete:
			action = "DeleteApiKey"
		}
	}
	if strings.Contains(path, "/datasources") {
		parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
		name := ""
		if len(parts) > 2 {
			name = parts[2]
		}
		switch {
		case r.Method == http.MethodPost && name == "":
			action = "CreateDataSource"
		case r.Method == http.MethodGet && name == "":
			action = "ListDataSources"
		case r.Method == http.MethodGet:
			action = "GetDataSource"
		case r.Method == http.MethodPost:
			action = "UpdateDataSource"
		case r.Method == http.MethodDelete:
			action = "DeleteDataSource"
		}
	}
	decision, err := a.authorizer.Authorize(r.Context(), authz.Request{Principal: awsPrincipal(r), PrincipalAttributes: awsPrincipalAttributes(r), Action: "appsync:" + action, Resource: "arn:aws:appsync:us-east-1:000000000000:apis/" + id, Context: map[string]string{"provider": "aws", "service": "appsync", "method": r.Method, "source_ip": sourceIP(r), "current_time": time.Now().UTC().Format(time.RFC3339), "user_agent": r.UserAgent()}, Claims: awsClaims(r)})
	if err != nil {
		writeAppSyncError(w, http.StatusInternalServerError, "InternalFailureException", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeAppSyncError(w, http.StatusForbidden, "AccessDeniedException", decision.Reason)
		return false
	}
	return true
}
