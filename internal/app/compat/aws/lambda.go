package aws

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type LambdaAdapter struct {
	mu         sync.Mutex
	functions  map[string]lambdaFunction
	quota      int
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type LambdaOption func(*LambdaAdapter)

type lambdaFunction struct {
	Name         string
	Runtime      string
	Role         string
	Handler      string
	CodeRevision int
	CodeSHA256   string
	CodeSize     int
	Tags         map[string]string
}

func NewLambdaAdapter(options ...LambdaOption) *LambdaAdapter {
	adapter := &LambdaAdapter{
		functions:  map[string]lambdaFunction{},
		authorizer: authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithLambdaAuthorizer(authorizer authz.Authorizer) LambdaOption {
	return func(adapter *LambdaAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithLambdaAuditSink(sink func(authz.Decision)) LambdaOption {
	return func(adapter *LambdaAdapter) {
		adapter.auditSink = sink
	}
}

func WithLambdaQuota(maxFunctions int) LambdaOption {
	return func(adapter *LambdaAdapter) {
		adapter.quota = maxFunctions
	}
}

func (LambdaAdapter) Provider() string { return "aws" }
func (LambdaAdapter) Service() string  { return "lambda" }
func (LambdaAdapter) Routes() []string { return []string{"ANY /compat/aws/lambda"} }
func (LambdaAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_LAMBDA": "http://homeport:8080/api/v1/compat/aws/lambda",
		"HOMEPORT_COMPAT_BACKEND": "openfaas",
	}
}
func (LambdaAdapter) ConformanceChecks() []string {
	return []string{"create-function", "get-function", "list-functions", "list-versions-by-function", "invoke", "update-function-code", "delete-function", "list-tags", "tag-resource", "untag-resource"}
}

func (a *LambdaAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/2015-03-31/functions":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeLambdaJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValueException", "message": err.Error()})
			return
		}
		if stringValue(body["FunctionName"]) == "" || stringValue(body["Runtime"]) == "" || stringValue(body["Role"]) == "" || stringValue(body["Handler"]) == "" || body["Code"] == nil {
			writeLambdaJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValueException", "message": "FunctionName, Runtime, Role, Handler, and Code are required"})
			return
		}
		if !a.authorized(w, r, "CreateFunction", body) {
			return
		}
		fn := lambdaFunction{
			Name:         stringValue(body["FunctionName"]),
			Runtime:      stringValue(body["Runtime"]),
			Role:         stringValue(body["Role"]),
			Handler:      stringValue(body["Handler"]),
			CodeRevision: 1,
			CodeSHA256:   lambdaCodeSHA256(body["Code"]),
			CodeSize:     lambdaCodeSize(body["Code"]),
			Tags:         mapValue(body["Tags"]),
		}
		if _, exists := a.functions[fn.Name]; exists {
			writeLambdaConflict(w)
			return
		}
		if a.quota > 0 && len(a.functions) >= a.quota {
			writeLambdaJSON(w, http.StatusTooManyRequests, map[string]string{"__type": "TooManyRequestsException", "message": "function quota exceeded"})
			return
		}
		a.functions[fn.Name] = fn
		writeLambdaJSON(w, http.StatusCreated, lambdaConfiguration(fn))
	case r.Method == http.MethodGet && r.URL.Path == "/2015-03-31/functions":
		if !a.authorized(w, r, "ListFunctions", map[string]any{}) {
			return
		}
		names := make([]string, 0, len(a.functions))
		for name := range a.functions {
			names = append(names, name)
		}
		sort.Strings(names)
		start := 0
		if marker := r.URL.Query().Get("Marker"); marker != "" {
			parsed, err := strconv.Atoi(marker)
			if err != nil || parsed < 0 || parsed > len(names) {
				writeLambdaJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValueException", "message": "invalid Marker"})
				return
			}
			start = parsed
		}
		maxItems := 50
		if value := r.URL.Query().Get("MaxItems"); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				writeLambdaJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValueException", "message": "invalid MaxItems"})
				return
			}
			if parsed < maxItems {
				maxItems = parsed
			}
		}
		end := len(names)
		if start+maxItems < end {
			end = start + maxItems
		}
		functions := make([]map[string]any, 0, end-start)
		for _, name := range names[start:end] {
			functions = append(functions, lambdaConfiguration(a.functions[name]))
		}
		response := map[string]any{"Functions": functions}
		if end < len(names) {
			response["NextMarker"] = strconv.Itoa(end)
		}
		writeLambdaJSON(w, http.StatusOK, response)
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/code"):
		name, ok := lambdaFunctionCodeName(r.URL.Path)
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		if !a.authorized(w, r, "UpdateFunctionCode", map[string]any{"FunctionName": name}) {
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeLambdaJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValueException", "message": err.Error()})
			return
		}
		fn, ok := a.functions[name]
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		fn.CodeRevision++
		fn.CodeSHA256 = lambdaCodeSHA256(map[string]any{"ZipFile": stringValue(body["ZipFile"])})
		fn.CodeSize = lambdaCodeSize(map[string]any{"ZipFile": stringValue(body["ZipFile"])})
		a.functions[name] = fn
		writeLambdaJSON(w, http.StatusOK, lambdaConfiguration(fn))
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/configuration"):
		name, ok := lambdaFunctionConfigurationName(r.URL.Path)
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		if !a.authorized(w, r, "GetFunction", map[string]any{"FunctionName": name}) {
			return
		}
		fn, ok := a.functions[name]
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		writeLambdaJSON(w, http.StatusOK, lambdaConfiguration(fn))
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/versions"):
		name, ok := lambdaFunctionVersionsName(r.URL.Path)
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		if !a.authorized(w, r, "ListVersionsByFunction", map[string]any{"FunctionName": name}) {
			return
		}
		fn, ok := a.functions[name]
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		writeLambdaJSON(w, http.StatusOK, map[string]any{"Versions": []map[string]any{lambdaConfiguration(fn)}})
	case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/code-signing-config"):
		name, ok := lambdaCodeSigningConfigName(r.URL.Path)
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		if !a.authorized(w, r, "GetFunctionCodeSigningConfig", map[string]any{"FunctionName": name}) {
			return
		}
		if a.functions[name].Name == "" {
			writeLambdaNotFound(w)
			return
		}
		writeLambdaJSON(w, http.StatusOK, map[string]string{})
	case r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/2017-03-31/tags/"):
		name, ok := lambdaFunctionName(r.URL.Path)
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		if !a.authorized(w, r, "GetFunction", map[string]any{"FunctionName": name}) {
			return
		}
		fn, ok := a.functions[name]
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		writeLambdaJSON(w, http.StatusOK, map[string]any{"Configuration": lambdaConfiguration(fn), "Code": lambdaCode(fn)})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/invocations"):
		name, ok := lambdaInvocationName(r.URL.Path)
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		if !a.authorized(w, r, "Invoke", map[string]any{"FunctionName": name}) {
			return
		}
		if _, ok := a.functions[name]; !ok {
			writeLambdaNotFound(w)
			return
		}
		writeLambdaJSON(w, http.StatusOK, map[string]string{"function": name})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/2017-03-31/tags/"):
		arn := lambdaTagARN(r.URL.Path)
		if !a.authorized(w, r, "ListTags", map[string]any{"Resource": arn}) {
			return
		}
		fn := a.functionByARN(arn)
		if fn == nil {
			writeLambdaNotFound(w)
			return
		}
		writeLambdaJSON(w, http.StatusOK, map[string]map[string]string{"Tags": fn.Tags})
	case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/2017-03-31/tags/"):
		arn := lambdaTagARN(r.URL.Path)
		if !a.authorized(w, r, "TagResource", map[string]any{"Resource": arn}) {
			return
		}
		fn := a.functionByARN(arn)
		if fn == nil {
			writeLambdaNotFound(w)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeLambdaJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterValueException", "message": "invalid JSON request body"})
			return
		}
		if fn.Tags == nil {
			fn.Tags = map[string]string{}
		}
		mergeStringMap(fn.Tags, mapValue(body["Tags"]))
		a.functions[fn.Name] = *fn
		writeLambdaJSON(w, http.StatusNoContent, map[string]string{})
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/2017-03-31/tags/"):
		arn := lambdaTagARN(r.URL.Path)
		if !a.authorized(w, r, "UntagResource", map[string]any{"Resource": arn}) {
			return
		}
		fn := a.functionByARN(arn)
		if fn == nil {
			writeLambdaNotFound(w)
			return
		}
		for _, key := range r.URL.Query()["tagKeys"] {
			delete(fn.Tags, key)
		}
		a.functions[fn.Name] = *fn
		writeLambdaJSON(w, http.StatusNoContent, map[string]string{})
	case r.Method == http.MethodDelete:
		name, ok := lambdaFunctionName(r.URL.Path)
		if !ok {
			writeLambdaNotFound(w)
			return
		}
		if !a.authorized(w, r, "DeleteFunction", map[string]any{"FunctionName": name}) {
			return
		}
		if _, ok := a.functions[name]; !ok {
			writeLambdaNotFound(w)
			return
		}
		delete(a.functions, name)
		writeLambdaJSON(w, http.StatusNoContent, map[string]string{})
	default:
		writeLambdaJSON(w, http.StatusBadRequest, map[string]string{"__type": "UnsupportedOperation", "message": "unsupported Lambda action"})
	}
}

func (a *LambdaAdapter) functionByARN(arn string) *lambdaFunction {
	for _, fn := range a.functions {
		if lambdaARN(fn.Name) == arn {
			return &fn
		}
	}
	return nil
}

func (a *LambdaAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	resource := stringValue(body["Resource"])
	if resource == "" {
		resource = lambdaARN(stringValue(body["FunctionName"]))
	}
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "lambda:" + action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "aws",
			"service":      "lambda",
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
		writeLambdaJSON(w, http.StatusInternalServerError, map[string]string{"__type": "ServiceException", "message": err.Error()})
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeLambdaAccessDenied(w, decision.Reason)
		return false
	}
	return true
}

func lambdaConfiguration(fn lambdaFunction) map[string]any {
	return map[string]any{
		"FunctionName":     fn.Name,
		"FunctionArn":      lambdaARN(fn.Name),
		"Runtime":          fn.Runtime,
		"Role":             fn.Role,
		"Handler":          fn.Handler,
		"RevisionId":       fmt.Sprintf("%d", fn.CodeRevision),
		"State":            "Active",
		"LastUpdateStatus": "Successful",
		"PackageType":      "Zip",
		"Version":          "$LATEST",
		"LastModified":     time.Now().UTC().Format("2006-01-02T15:04:05.000-0700"),
		"CodeSha256":       fn.CodeSHA256,
		"CodeSize":         fn.CodeSize,
		"MemorySize":       128,
		"Timeout":          3,
		"Architectures":    []string{"x86_64"},
		"TracingConfig":    map[string]string{"Mode": "PassThrough"},
		"EphemeralStorage": map[string]int{"Size": 512},
		"LoggingConfig":    map[string]string{"LogFormat": "Text"},
	}
}

func lambdaCode(fn lambdaFunction) map[string]string {
	return map[string]string{
		"Location":       "http://homeport.local/lambda/" + fn.Name + ".zip",
		"RepositoryType": "S3",
	}
}

func lambdaCodeSHA256(raw any) string {
	code, _ := raw.(map[string]any)
	zip := stringValue(code["ZipFile"])
	if zip == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(zip))
	return base64.StdEncoding.EncodeToString(sum[:])
}

func lambdaCodeSize(raw any) int {
	code, _ := raw.(map[string]any)
	return len(stringValue(code["ZipFile"]))
}

func lambdaARN(name string) string {
	if name == "" {
		name = "unknown"
	}
	return "arn:aws:lambda:us-east-1:000000000000:function:" + name
}

func lambdaFunctionName(path string) (string, bool) {
	const prefix = "/2015-03-31/functions/"
	if !strings.HasPrefix(path, prefix) || strings.HasSuffix(path, "/invocations") || strings.HasSuffix(path, "/configuration") || strings.HasSuffix(path, "/versions") {
		return "", false
	}
	name, err := url.PathUnescape(strings.TrimPrefix(path, prefix))
	return name, err == nil && name != ""
}

func lambdaFunctionVersionsName(path string) (string, bool) {
	const prefix = "/2015-03-31/functions/"
	const suffix = "/versions"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	unescaped, err := url.PathUnescape(name)
	return unescaped, err == nil && unescaped != ""
}

func lambdaCodeSigningConfigName(path string) (string, bool) {
	const prefix = "/2020-06-30/functions/"
	const suffix = "/code-signing-config"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	unescaped, err := url.PathUnescape(name)
	return unescaped, err == nil && unescaped != ""
}

func lambdaFunctionConfigurationName(path string) (string, bool) {
	const prefix = "/2015-03-31/functions/"
	const suffix = "/configuration"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	unescaped, err := url.PathUnescape(name)
	return unescaped, err == nil && unescaped != ""
}

func lambdaInvocationName(path string) (string, bool) {
	const prefix = "/2015-03-31/functions/"
	const suffix = "/invocations"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	unescaped, err := url.PathUnescape(name)
	return unescaped, err == nil && unescaped != ""
}

func lambdaFunctionCodeName(path string) (string, bool) {
	const prefix = "/2015-03-31/functions/"
	const suffix = "/code"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	unescaped, err := url.PathUnescape(name)
	return unescaped, err == nil && unescaped != ""
}

func lambdaTagARN(path string) string {
	const prefix = "/2017-03-31/tags/"
	arn, _ := url.PathUnescape(strings.TrimPrefix(path, prefix))
	return arn
}

func writeLambdaNotFound(w http.ResponseWriter) {
	writeLambdaJSON(w, http.StatusNotFound, map[string]string{"__type": "ResourceNotFoundException", "message": "function not found"})
}

func writeLambdaConflict(w http.ResponseWriter) {
	writeLambdaJSON(w, http.StatusConflict, map[string]string{"__type": "ResourceConflictException", "message": "function already exists"})
}

func writeLambdaAccessDenied(w http.ResponseWriter, message string) {
	writeLambdaJSON(w, http.StatusForbidden, map[string]string{"__type": "AccessDenied", "message": message})
}

func writeLambdaJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		_ = json.NewEncoder(w).Encode(value)
	}
}
