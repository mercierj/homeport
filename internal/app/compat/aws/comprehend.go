package aws

import (
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

// ComprehendAdapter provides the local document-classifier management slice.
type ComprehendAdapter struct {
	mu          sync.Mutex
	classifiers map[string]comprehendClassifier
	nextID      int
	quota       int
	authorizer  authz.Authorizer
	auditSink   func(authz.Decision)
}
type ComprehendOption func(*ComprehendAdapter)

type comprehendClassifier struct {
	ARN      string
	Name     string
	Language string
}

func NewComprehendAdapter(options ...ComprehendOption) *ComprehendAdapter {
	a := &ComprehendAdapter{classifiers: map[string]comprehendClassifier{}, authorizer: authz.AllowAll}
	for _, option := range options {
		option(a)
	}
	return a
}
func WithComprehendAuthorizer(authorizer authz.Authorizer) ComprehendOption {
	return func(a *ComprehendAdapter) {
		if authorizer != nil {
			a.authorizer = authorizer
		}
	}
}
func WithComprehendAuditSink(sink func(authz.Decision)) ComprehendOption {
	return func(a *ComprehendAdapter) { a.auditSink = sink }
}
func WithComprehendQuota(quota int) ComprehendOption {
	return func(a *ComprehendAdapter) {
		if quota > 0 {
			a.quota = quota
		}
	}
}

func (ComprehendAdapter) Provider() string { return "aws" }
func (ComprehendAdapter) Service() string  { return "comprehend" }
func (ComprehendAdapter) Routes() []string { return []string{"POST /compat/aws/comprehend"} }
func (ComprehendAdapter) TargetEnv() map[string]string {
	return map[string]string{"AWS_ENDPOINT_URL_COMPREHEND": "http://homeport:8080/api/v1/compat/aws/comprehend", "HOMEPORT_COMPAT_BACKEND": "spacy"}
}
func (ComprehendAdapter) ConformanceChecks() []string {
	return []string{"create-document-classifier", "describe-document-classifier", "list-document-classifiers", "delete-document-classifier"}
}

func (a *ComprehendAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeComprehendError(w, http.StatusMethodNotAllowed, "InvalidRequestException", "Comprehend compatibility requests must use POST")
		return
	}
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeComprehendError(w, http.StatusBadRequest, "InvalidRequestException", err.Error())
		return
	}
	if !a.authorized(w, r, action, body) {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch action {
	case "CreateDocumentClassifier":
		name := stringValue(body["DocumentClassifierName"])
		if name == "" {
			writeComprehendError(w, http.StatusBadRequest, "InvalidRequestException", "document classifier name is required")
			return
		}
		if a.hasName(name) {
			writeComprehendError(w, http.StatusBadRequest, "ResourceInUseException", "document classifier name already exists")
			return
		}
		if a.quota > 0 && len(a.classifiers) >= a.quota {
			writeComprehendError(w, http.StatusBadRequest, "ResourceLimitExceededException", "document classifier quota exceeded")
			return
		}
		a.nextID++
		classifier := comprehendClassifier{ARN: "arn:aws:comprehend:us-east-1:000000000000:document-classifier/" + name + "/homeport-" + strconv.Itoa(a.nextID), Name: name, Language: stringValue(body["LanguageCode"])}
		a.classifiers[classifier.ARN] = classifier
		writeJSON(w, http.StatusOK, map[string]string{"DocumentClassifierArn": classifier.ARN})
	case "DescribeDocumentClassifier":
		classifier, ok := a.classifiers[stringValue(body["DocumentClassifierArn"])]
		if !ok {
			writeComprehendError(w, http.StatusBadRequest, "ResourceNotFoundException", "document classifier not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"DocumentClassifierProperties": comprehendClassifierShape(classifier)})
	case "ListDocumentClassifiers":
		classifiers := make([]comprehendClassifier, 0, len(a.classifiers))
		for _, classifier := range a.classifiers {
			classifiers = append(classifiers, classifier)
		}
		sort.Slice(classifiers, func(i, j int) bool { return classifiers[i].Name < classifiers[j].Name })
		start, limit, ok := comprehendPage(body, len(classifiers))
		if !ok {
			writeComprehendError(w, http.StatusBadRequest, "InvalidRequestException", "NextToken or MaxResults is invalid")
			return
		}
		end := start + limit
		if end > len(classifiers) {
			end = len(classifiers)
		}
		shapes := make([]map[string]string, 0, end-start)
		for _, classifier := range classifiers[start:end] {
			shapes = append(shapes, comprehendClassifierShape(classifier))
		}
		response := map[string]any{"DocumentClassifierPropertiesList": shapes}
		if end < len(classifiers) {
			response["NextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, response)
	case "DeleteDocumentClassifier":
		arn := stringValue(body["DocumentClassifierArn"])
		if _, ok := a.classifiers[arn]; !ok {
			writeComprehendError(w, http.StatusBadRequest, "ResourceNotFoundException", "document classifier not found")
			return
		}
		delete(a.classifiers, arn)
		writeJSON(w, http.StatusOK, map[string]any{})
	default:
		writeComprehendError(w, http.StatusBadRequest, "InvalidRequestException", "Comprehend action is not implemented")
	}
}

func (a *ComprehendAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	resource := stringValue(body["DocumentClassifierArn"])
	if resource == "" {
		resource = "arn:aws:comprehend:us-east-1:000000000000:document-classifier/" + stringValue(body["DocumentClassifierName"])
	}
	d, err := a.authorizer.Authorize(r.Context(), authz.Request{Principal: awsPrincipal(r), PrincipalAttributes: awsPrincipalAttributes(r), Action: "comprehend:" + action, Resource: resource, Context: map[string]string{"provider": "aws", "service": "comprehend", "method": r.Method, "request_id": "homeport", "source_ip": sourceIP(r), "current_time": time.Now().UTC().Format(time.RFC3339), "user_agent": r.UserAgent()}, Claims: awsClaims(r)})
	if err != nil {
		writeComprehendError(w, http.StatusInternalServerError, "InternalServerException", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(d)
	}
	if !d.Allowed {
		writeComprehendError(w, http.StatusForbidden, "AccessDeniedException", d.Reason)
		return false
	}
	return true
}

func comprehendPage(body map[string]any, count int) (int, int, bool) {
	start := 0
	if token := stringValue(body["NextToken"]); token != "" {
		parsed, err := strconv.Atoi(token)
		if err != nil || parsed < 0 || parsed >= count {
			return 0, 0, false
		}
		start = parsed
	}
	limit := 100
	if value, present := comprehendInt(body["MaxResults"]); present {
		parsed := value
		if parsed < 1 || parsed > 1000 {
			return 0, 0, false
		}
		limit = parsed
	}
	return start, limit, true
}

func comprehendInt(value any) (int, bool) {
	switch value := value.(type) {
	case float64:
		if value != float64(int(value)) {
			return 0, false
		}
		return int(value), true
	case string:
		parsed, err := strconv.Atoi(value)
		return parsed, err == nil
	case nil:
		return 0, false
	default:
		return 0, false
	}
}

func (a *ComprehendAdapter) hasName(name string) bool {
	for _, classifier := range a.classifiers {
		if classifier.Name == name {
			return true
		}
	}
	return false
}

func comprehendClassifierShape(classifier comprehendClassifier) map[string]string {
	return map[string]string{"DocumentClassifierArn": classifier.ARN, "DocumentClassifierName": classifier.Name, "LanguageCode": classifier.Language, "Status": "TRAINED"}
}

func writeComprehendError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"__type": code, "message": message})
}
