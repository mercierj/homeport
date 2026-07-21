package aws

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type SecretsAdapter struct {
	mu         sync.Mutex
	secrets    map[string]*secretRecord
	quota      int
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type SecretsOption func(*SecretsAdapter)

type secretRecord struct {
	Name            string
	Description     string
	Versions        map[string]string
	RequestTokens   map[string]string
	CurrentVersion  string
	PreviousVersion string
	NextVersion     int
	Policy          string
	Tags            map[string]string
}

func NewSecretsAdapter(options ...SecretsOption) *SecretsAdapter {
	adapter := &SecretsAdapter{
		secrets:    make(map[string]*secretRecord),
		authorizer: authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithSecretsAuthorizer(authorizer authz.Authorizer) SecretsOption {
	return func(adapter *SecretsAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithSecretsAuditSink(sink func(authz.Decision)) SecretsOption {
	return func(adapter *SecretsAdapter) {
		adapter.auditSink = sink
	}
}

func WithSecretsQuota(maxSecrets int) SecretsOption {
	return func(adapter *SecretsAdapter) {
		adapter.quota = maxSecrets
	}
}

func (SecretsAdapter) Provider() string { return "aws" }
func (SecretsAdapter) Service() string  { return "secretsmanager" }
func (SecretsAdapter) Routes() []string { return []string{"POST /compat/aws/secretsmanager"} }
func (SecretsAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_SECRETSMANAGER": "http://homeport:8080/api/v1/compat/aws/secretsmanager",
		"HOMEPORT_COMPAT_BACKEND":         "vault",
	}
}
func (SecretsAdapter) ConformanceChecks() []string {
	return []string{"create-secret", "put-secret-value", "update-secret", "delete-secret", "get-secret-value", "describe-secret", "get-resource-policy", "put-resource-policy", "delete-resource-policy", "tag-resource", "untag-resource", "list-secrets"}
}
func (a *SecretsAdapter) PutSecret(name, value string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.secrets[name] = newSecretRecord(name, value, "")
}

func (a *SecretsAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	case "CreateSecret":
		name := stringValue(body["Name"])
		if name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterException", "message": "Name is required"})
			return
		}
		value := stringValue(body["SecretString"])
		token := stringValue(body["ClientRequestToken"])
		if record := a.secrets[name]; record != nil {
			versionID := record.RequestTokens[token]
			if token != "" && versionID != "" {
				if record.Versions[versionID] != value {
					writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidRequestException", "message": "ClientRequestToken is already associated with a different secret value"})
					return
				}
				writeJSON(w, http.StatusOK, map[string]string{"ARN": secretARN(r, name), "Name": name, "VersionId": versionID})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceExistsException", "message": "secret already exists"})
			return
		}
		if a.quota > 0 && len(a.secrets) >= a.quota {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"__type": "LimitExceededException", "message": "secret quota exceeded"})
			return
		}
		record := newSecretRecord(name, value, token)
		record.Description = stringValue(body["Description"])
		record.Tags = secretsTags(body["Tags"])
		a.secrets[name] = record
		writeJSON(w, http.StatusOK, map[string]string{
			"ARN":       secretARN(r, name),
			"Name":      name,
			"VersionId": record.CurrentVersion,
		})
	case "UpdateSecret":
		name := secretName(stringValue(body["SecretId"]))
		record := a.secrets[name]
		if record == nil {
			writeSecretNotFound(w)
			return
		}
		if description, ok := body["Description"].(string); ok {
			record.Description = description
		}
		if _, hasValue := body["SecretString"]; hasValue {
			record.put(stringValue(body["SecretString"]), stringValue(body["ClientRequestToken"]))
		}
		writeJSON(w, http.StatusOK, map[string]string{"ARN": secretARN(r, name), "Name": name})
	case "PutSecretValue":
		name := secretName(stringValue(body["SecretId"]))
		record := a.secrets[name]
		if record == nil {
			writeSecretNotFound(w)
			return
		}
		token := stringValue(body["ClientRequestToken"])
		versionID := record.RequestTokens[token]
		value := stringValue(body["SecretString"])
		if versionID != "" && record.Versions[versionID] != value {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidRequestException", "message": "ClientRequestToken is already associated with a different secret value"})
			return
		}
		if token == "" || versionID == "" {
			versionID = record.put(value, token)
			if token != "" {
				record.RequestTokens[token] = versionID
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"ARN":       secretARN(r, name),
			"Name":      name,
			"VersionId": versionID,
		})
	case "DeleteSecret":
		name := secretName(stringValue(body["SecretId"]))
		if a.secrets[name] == nil {
			writeSecretNotFound(w)
			return
		}
		delete(a.secrets, name)
		writeJSON(w, http.StatusOK, map[string]string{
			"ARN":  secretARN(r, name),
			"Name": name,
		})
	case "GetSecretValue":
		name := secretName(stringValue(body["SecretId"]))
		record := a.secrets[name]
		if record == nil {
			writeSecretNotFound(w)
			return
		}
		versionID, value, ok := record.value(secretVersionID(body), secretVersionStage(body))
		if !ok {
			writeSecretNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"ARN":          secretARN(r, name),
			"Name":         name,
			"SecretString": value,
			"VersionId":    versionID,
		})
	case "DescribeSecret":
		name := secretName(stringValue(body["SecretId"]))
		record := a.secrets[name]
		if record == nil {
			writeSecretNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ARN":                secretARN(r, name),
			"Name":               name,
			"Description":        record.Description,
			"Tags":               secretsTagsJSON(record.Tags),
			"VersionIdsToStages": record.stages(),
		})
	case "GetResourcePolicy":
		name := secretName(stringValue(body["SecretId"]))
		record := a.secrets[name]
		if record == nil {
			writeSecretNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"ARN":            secretARN(r, name),
			"Name":           name,
			"ResourcePolicy": secretsPolicy(record.Policy),
		})
	case "PutResourcePolicy":
		name := secretName(stringValue(body["SecretId"]))
		record := a.secrets[name]
		if record == nil {
			writeSecretNotFound(w)
			return
		}
		record.Policy = stringValue(body["ResourcePolicy"])
		writeJSON(w, http.StatusOK, map[string]string{
			"ARN":  secretARN(r, name),
			"Name": name,
		})
	case "DeleteResourcePolicy":
		name := secretName(stringValue(body["SecretId"]))
		record := a.secrets[name]
		if record == nil {
			writeSecretNotFound(w)
			return
		}
		record.Policy = ""
		writeJSON(w, http.StatusOK, map[string]string{
			"ARN":  secretARN(r, name),
			"Name": name,
		})
	case "TagResource":
		name := secretName(stringValue(body["SecretId"]))
		record := a.secrets[name]
		if record == nil {
			writeSecretNotFound(w)
			return
		}
		if record.Tags == nil {
			record.Tags = map[string]string{}
		}
		mergeStringMap(record.Tags, secretsTags(body["Tags"]))
		writeJSON(w, http.StatusOK, map[string]string{})
	case "UntagResource":
		name := secretName(stringValue(body["SecretId"]))
		record := a.secrets[name]
		if record == nil {
			writeSecretNotFound(w)
			return
		}
		for _, tagKey := range kmsStringList(body["TagKeys"]) {
			delete(record.Tags, tagKey)
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ListSecrets":
		names := make([]string, 0, len(a.secrets))
		for name := range a.secrets {
			names = append(names, name)
		}
		sort.Strings(names)
		start := 0
		if token := stringValue(body["NextToken"]); token != "" {
			parsed, err := strconv.Atoi(token)
			if err != nil || parsed < 0 || parsed > len(names) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterException", "message": "invalid NextToken"})
				return
			}
			start = parsed
		}
		maxResults := 100
		if _, ok := body["MaxResults"]; ok {
			maxResults = intValue(body, 0, "MaxResults")
			if maxResults < 1 || maxResults > 100 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidParameterException", "message": "invalid MaxResults"})
				return
			}
		}
		end := start + maxResults
		if end > len(names) {
			end = len(names)
		}
		secretList := make([]map[string]string, 0, end-start)
		for _, name := range names[start:end] {
			secretList = append(secretList, map[string]string{
				"ARN":  secretARN(r, name),
				"Name": name,
			})
		}
		result := map[string]any{"SecretList": secretList}
		if end < len(names) {
			result["NextToken"] = strconv.Itoa(end)
		}
		writeJSON(w, http.StatusOK, result)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported Secrets Manager action: " + action})
	}
}

func newSecretRecord(name, value, versionID string) *secretRecord {
	record := &secretRecord{Name: name, Versions: map[string]string{}, RequestTokens: map[string]string{}, NextVersion: 1, Tags: map[string]string{}}
	record.put(value, versionID)
	if versionID != "" {
		record.RequestTokens[versionID] = versionID
	}
	return record
}

func secretsTags(raw any) map[string]string {
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

func secretsPolicy(policy string) string {
	if policy != "" {
		return policy
	}
	return `{"Version":"2012-10-17","Statement":[]}`
}

func secretsTagsJSON(tags map[string]string) []map[string]string {
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

func (s *secretRecord) put(value, versionID string) string {
	if versionID == "" {
		versionID = strconv.Itoa(s.NextVersion)
		s.NextVersion++
	}
	s.PreviousVersion = s.CurrentVersion
	s.Versions[versionID] = value
	s.CurrentVersion = versionID
	return versionID
}

func (s *secretRecord) stages() map[string][]string {
	out := make(map[string][]string, len(s.Versions))
	for versionID := range s.Versions {
		if versionID == s.CurrentVersion {
			out[versionID] = []string{"AWSCURRENT"}
		} else if versionID == s.PreviousVersion {
			out[versionID] = []string{"AWSPREVIOUS"}
		} else {
			out[versionID] = nil
		}
	}
	return out
}

func (s *secretRecord) value(versionID, stage string) (string, string, bool) {
	if versionID == "" {
		switch stage {
		case "", "AWSCURRENT":
			versionID = s.CurrentVersion
		case "AWSPREVIOUS":
			versionID = s.PreviousVersion
		default:
			return "", "", false
		}
	}
	value, ok := s.Versions[versionID]
	return versionID, value, ok
}

func secretVersionID(body map[string]any) string {
	return cloudWatchField(body, "VersionId", "versionId")
}

func secretVersionStage(body map[string]any) string {
	return cloudWatchField(body, "VersionStage", "versionStage")
}

func writeSecretNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ResourceNotFoundException", "message": "secret not found"})
}

func (a *SecretsAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "secretsmanager:" + action,
		Resource:            secretsARN(r, body),
		Context: map[string]string{
			"provider":     "aws",
			"service":      "secretsmanager",
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"__type": "InternalServiceError", "message": err.Error()})
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

func secretName(secretID string) string {
	if after, ok := strings.CutPrefix(secretID, "arn:aws:secretsmanager:"); ok {
		if _, name, ok := strings.Cut(after, ":secret:"); ok {
			return strings.TrimSuffix(name, "-homeport")
		}
	}
	return secretID
}

func secretARN(r *http.Request, name string) string {
	return "arn:aws:secretsmanager:us-east-1:000000000000:secret:" + name + "-homeport"
}

func secretsARN(r *http.Request, body map[string]any) string {
	name := secretName(stringValue(body["SecretId"]))
	if name == "" {
		name = stringValue(body["Name"])
	}
	if name == "" {
		name = "unknown"
	}
	return secretARN(r, name)
}
