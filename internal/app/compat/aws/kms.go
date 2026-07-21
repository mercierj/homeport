package aws

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"hash"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type KMSAdapter struct {
	mu         sync.Mutex
	macKey     []byte
	keys       map[string]*kmsKey
	keyQuota   int
	nextKeyID  int
	authorizer authz.Authorizer
	auditSink  func(authz.Decision)
}

type KMSOption func(*KMSAdapter)

type kmsKey struct {
	ID           string
	ARN          string
	Description  string
	Policy       string
	State        string
	CreatedAt    time.Time
	DeletionDate time.Time
	DeletionDays int
	Tags         map[string]string
}

func NewKMSAdapter(options ...KMSOption) *KMSAdapter {
	adapter := &KMSAdapter{
		macKey:     []byte("homeport-vault-transit-compat-key"),
		keys:       map[string]*kmsKey{},
		authorizer: authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithKMSAuthorizer(authorizer authz.Authorizer) KMSOption {
	return func(adapter *KMSAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithKMSAuditSink(sink func(authz.Decision)) KMSOption {
	return func(adapter *KMSAdapter) {
		adapter.auditSink = sink
	}
}

func WithKMSKeyQuota(maxKeys int) KMSOption {
	return func(adapter *KMSAdapter) {
		adapter.keyQuota = maxKeys
	}
}

func (KMSAdapter) Provider() string { return "aws" }
func (KMSAdapter) Service() string  { return "kms" }
func (KMSAdapter) Routes() []string { return []string{"POST /compat/aws/kms"} }
func (KMSAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_KMS":     "http://homeport:8080/api/v1/compat/aws/kms",
		"HOMEPORT_COMPAT_BACKEND":  "vault-transit",
		"HOMEPORT_COMPAT_PROTOCOL": "kms",
	}
}
func (KMSAdapter) ConformanceChecks() []string {
	return []string{"create-key", "describe-key", "update-key-description", "list-keys", "get-key-policy", "put-key-policy", "schedule-key-deletion", "cancel-key-deletion", "enable-key", "disable-key", "list-resource-tags", "tag-resource", "untag-resource", "encrypt", "decrypt", "generate-mac", "verify-mac", "sign", "verify"}
}

func (a *KMSAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	action, body, err := decodeAWSAction(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	if !a.authorized(w, r, action, body) {
		return
	}

	switch action {
	case "CreateKey":
		a.mu.Lock()
		defer a.mu.Unlock()
		if !kmsTagsValid(body["Tags"]) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "TagException", "message": "invalid tag"})
			return
		}
		if rawPolicy, provided := body["Policy"]; provided {
			policy := stringValue(rawPolicy)
			if len(policy) > 32768 {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"__type":  "LimitExceededException",
					"message": "key policy exceeds 32768 characters",
				})
				return
			}
			if !kmsPolicyValid(policy) {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"__type":  "MalformedPolicyDocumentException",
					"message": "key policy is not valid JSON",
				})
				return
			}
		}
		if a.keyQuota > 0 && len(a.keys) >= a.keyQuota {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"__type":  "LimitExceededException",
				"message": "key quota exceeded",
			})
			return
		}
		a.nextKeyID++
		keyID := "key-" + strconv.Itoa(a.nextKeyID)
		key := &kmsKey{
			ID:          keyID,
			ARN:         kmsKeyARN(keyID),
			Description: stringValue(body["Description"]),
			Policy:      kmsPolicy(body["Policy"]),
			State:       "Enabled",
			CreatedAt:   time.Now().UTC(),
			Tags:        kmsTags(body["Tags"]),
		}
		a.keys[key.ID] = key
		writeJSON(w, http.StatusOK, map[string]any{"KeyMetadata": key.metadata()})
	case "DescribeKey":
		a.mu.Lock()
		defer a.mu.Unlock()
		key := a.key(stringValue(body["KeyId"]))
		if key == nil {
			writeKMSNotFound(w)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"KeyMetadata": key.metadata()})
	case "UpdateKeyDescription":
		a.mu.Lock()
		defer a.mu.Unlock()
		key := a.key(stringValue(body["KeyId"]))
		if key == nil {
			writeKMSNotFound(w)
			return
		}
		description, ok := body["Description"].(string)
		if !ok || len(description) > 8192 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ValidationException", "message": "Description is required and must be at most 8192 characters"})
			return
		}
		if key.State == "PendingDeletion" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "KMSInvalidStateException", "message": "key is pending deletion"})
			return
		}
		key.Description = description
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ListKeys":
		a.mu.Lock()
		defer a.mu.Unlock()
		ids := make([]string, 0, len(a.keys))
		for id := range a.keys {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		start := 0
		if marker := stringValue(body["Marker"]); marker != "" {
			start = -1
			for i, id := range ids {
				if id == marker {
					start = i + 1
					break
				}
			}
			if start < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidMarkerException", "message": "invalid Marker"})
				return
			}
		}
		limit, ok := cloudWatchLogsLimit(body, 100, 1000, "Limit")
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ValidationException", "message": "Limit must be between 1 and 1000"})
			return
		}
		end := start + limit
		if end > len(ids) {
			end = len(ids)
		}
		keys := make([]map[string]string, 0, len(ids))
		for _, id := range ids[start:end] {
			key := a.keys[id]
			keys = append(keys, map[string]string{"KeyId": key.ID, "KeyArn": key.ARN})
		}
		response := map[string]any{
			"Keys":      keys,
			"Truncated": end < len(ids),
		}
		if end < len(ids) && end > start {
			response["NextMarker"] = ids[end-1]
		}
		writeJSON(w, http.StatusOK, response)
	case "GetKeyPolicy":
		a.mu.Lock()
		defer a.mu.Unlock()
		key := a.key(stringValue(body["KeyId"]))
		if key == nil {
			writeKMSNotFound(w)
			return
		}
		if !kmsPolicyNameValid(stringValue(body["PolicyName"])) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"__type":  "UnsupportedOperationException",
				"message": "only the default key policy is supported",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"Policy": key.Policy})
	case "PutKeyPolicy":
		a.mu.Lock()
		defer a.mu.Unlock()
		key := a.key(stringValue(body["KeyId"]))
		if key == nil {
			writeKMSNotFound(w)
			return
		}
		if !kmsPolicyNameValid(stringValue(body["PolicyName"])) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"__type":  "UnsupportedOperationException",
				"message": "only the default key policy is supported",
			})
			return
		}
		policy := stringValue(body["Policy"])
		if len(policy) > 32768 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"__type":  "LimitExceededException",
				"message": "key policy exceeds 32768 characters",
			})
			return
		}
		if !kmsPolicyValid(policy) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"__type":  "MalformedPolicyDocumentException",
				"message": "key policy is not valid JSON",
			})
			return
		}
		key.Policy = policy
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ScheduleKeyDeletion":
		a.mu.Lock()
		defer a.mu.Unlock()
		key := a.key(stringValue(body["KeyId"]))
		if key == nil {
			writeKMSNotFound(w)
			return
		}
		days, ok := cloudWatchLogsLimit(body, 30, 30, "PendingWindowInDays")
		if !ok || days < 7 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ValidationException", "message": "PendingWindowInDays must be between 7 and 30"})
			return
		}
		key.State = "PendingDeletion"
		key.DeletionDays = days
		key.DeletionDate = time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
		writeJSON(w, http.StatusOK, map[string]any{
			"KeyId":               key.ID,
			"KeyState":            key.State,
			"PendingWindowInDays": days,
			"DeletionDate":        key.DeletionDate.Unix(),
		})
	case "CancelKeyDeletion":
		a.mu.Lock()
		defer a.mu.Unlock()
		key := a.key(stringValue(body["KeyId"]))
		if key == nil {
			writeKMSNotFound(w)
			return
		}
		if key.State != "PendingDeletion" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "KMSInvalidStateException", "message": "key is not pending deletion"})
			return
		}
		key.State = "Disabled"
		key.DeletionDate = time.Time{}
		key.DeletionDays = 0
		writeJSON(w, http.StatusOK, map[string]string{"KeyId": key.ID})
	case "DisableKey", "EnableKey":
		a.mu.Lock()
		defer a.mu.Unlock()
		key := a.key(stringValue(body["KeyId"]))
		if key == nil {
			writeKMSNotFound(w)
			return
		}
		if key.State == "PendingDeletion" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "KMSInvalidStateException", "message": "key is pending deletion"})
			return
		}
		if action == "DisableKey" {
			key.State = "Disabled"
		} else {
			key.State = "Enabled"
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "ListResourceTags":
		a.mu.Lock()
		defer a.mu.Unlock()
		key := a.key(stringValue(body["KeyId"]))
		if key == nil {
			writeKMSNotFound(w)
			return
		}
		tags := kmsTagsJSON(key.Tags)
		start := 0
		if marker := stringValue(body["Marker"]); marker != "" {
			start = -1
			for i, tag := range tags {
				if tag["TagKey"] == marker {
					start = i + 1
					break
				}
			}
			if start < 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "InvalidMarkerException", "message": "invalid Marker"})
				return
			}
		}
		limit, ok := cloudWatchLogsLimit(body, 50, 50, "Limit")
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "ValidationException", "message": "Limit must be between 1 and 50"})
			return
		}
		end := start + limit
		if end > len(tags) {
			end = len(tags)
		}
		response := map[string]any{"Tags": tags[start:end], "Truncated": end < len(tags)}
		if end < len(tags) {
			response["NextMarker"] = tags[end-1]["TagKey"]
		}
		writeJSON(w, http.StatusOK, response)
	case "TagResource":
		a.mu.Lock()
		defer a.mu.Unlock()
		key := a.key(stringValue(body["KeyId"]))
		if key == nil {
			writeKMSNotFound(w)
			return
		}
		if !kmsTagsValid(body["Tags"]) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "TagException", "message": "invalid tag"})
			return
		}
		if key.Tags == nil {
			key.Tags = map[string]string{}
		}
		mergeStringMap(key.Tags, kmsTags(body["Tags"]))
		writeJSON(w, http.StatusOK, map[string]string{})
	case "UntagResource":
		a.mu.Lock()
		defer a.mu.Unlock()
		key := a.key(stringValue(body["KeyId"]))
		if key == nil {
			writeKMSNotFound(w)
			return
		}
		for _, tagKey := range kmsStringList(body["TagKeys"]) {
			delete(key.Tags, tagKey)
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "Encrypt":
		plain, err := decodeBlob(body["Plaintext"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		keyID := stringValue(body["KeyId"])
		if !a.validateKeyState(w, keyID) {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"CiphertextBlob": encodeBlob(append([]byte("homeport:"), plain...)),
			"KeyId":          keyID,
		})
	case "Decrypt":
		ciphertext, err := decodeBlob(body["CiphertextBlob"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		keyID := stringValue(body["KeyId"])
		if !a.validateKeyState(w, keyID) {
			return
		}
		plain := ciphertext
		if len(ciphertext) >= len("homeport:") && string(ciphertext[:len("homeport:")]) == "homeport:" {
			plain = ciphertext[len("homeport:"):]
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"Plaintext": encodeBlob(plain),
			"KeyId":     keyID,
		})
	case "GenerateMac":
		message, err := decodeBlob(body["Message"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		keyID := stringValue(body["KeyId"])
		if !a.validateKeyState(w, keyID) {
			return
		}
		algorithm := kmsAlgorithm(body["MacAlgorithm"])
		writeJSON(w, http.StatusOK, map[string]any{
			"KeyId":        keyID,
			"Mac":          encodeBlob(a.mac(message, algorithm)),
			"MacAlgorithm": algorithm,
		})
	case "VerifyMac":
		message, err := decodeBlob(body["Message"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		macValue, err := decodeBlob(body["Mac"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		keyID := stringValue(body["KeyId"])
		if !a.validateKeyState(w, keyID) {
			return
		}
		algorithm := kmsAlgorithm(body["MacAlgorithm"])
		writeJSON(w, http.StatusOK, map[string]any{
			"KeyId":        keyID,
			"MacValid":     hmac.Equal(macValue, a.mac(message, algorithm)),
			"MacAlgorithm": algorithm,
		})
	case "Sign":
		message, err := decodeBlob(body["Message"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		keyID := stringValue(body["KeyId"])
		if !a.validateKeyState(w, keyID) {
			return
		}
		algorithm := kmsAlgorithm(body["SigningAlgorithm"])
		writeJSON(w, http.StatusOK, map[string]any{
			"KeyId":            keyID,
			"Signature":        encodeBlob(a.mac(message, algorithm)),
			"SigningAlgorithm": algorithm,
		})
	case "Verify":
		message, err := decodeBlob(body["Message"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		signature, err := decodeBlob(body["Signature"])
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
			return
		}
		keyID := stringValue(body["KeyId"])
		if !a.validateKeyState(w, keyID) {
			return
		}
		algorithm := kmsAlgorithm(body["SigningAlgorithm"])
		writeJSON(w, http.StatusOK, map[string]any{
			"KeyId":            keyID,
			"SignatureValid":   hmac.Equal(signature, a.mac(message, algorithm)),
			"SigningAlgorithm": algorithm,
		})
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "unsupported KMS action: " + action})
	}
}

func (a *KMSAdapter) key(keyID string) *kmsKey {
	if keyID == "" {
		return nil
	}
	if key := a.keys[keyID]; key != nil {
		return key
	}
	for _, key := range a.keys {
		if key.ARN == keyID {
			return key
		}
	}
	return nil
}

func (k *kmsKey) metadata() map[string]any {
	out := map[string]any{
		"Arn":          k.ARN,
		"CreationDate": k.CreatedAt.Unix(),
		"Description":  k.Description,
		"Enabled":      k.State == "Enabled",
		"KeyId":        k.ID,
		"KeyManager":   "CUSTOMER",
		"KeyState":     k.State,
		"KeyUsage":     "ENCRYPT_DECRYPT",
		"Origin":       "EXTERNAL",
	}
	if !k.DeletionDate.IsZero() {
		out["DeletionDate"] = k.DeletionDate.Unix()
		out["PendingDeletionWindowInDays"] = k.DeletionDays
	}
	return out
}

func kmsKeyARN(keyID string) string {
	return "arn:aws:kms:us-east-1:000000000000:key/" + keyID
}

func kmsPolicy(raw any) string {
	if policy := stringValue(raw); policy != "" {
		return policy
	}
	return `{"Version":"2012-10-17","Statement":[{"Sid":"Enable IAM User Permissions","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::000000000000:root"},"Action":"kms:*","Resource":"*"}]}`
}

func kmsPolicyValid(policy string) bool {
	if !json.Valid([]byte(policy)) {
		return false
	}
	for _, character := range policy {
		if character == '\t' || character == '\n' || character == '\r' || (character >= 0x20 && character <= 0xFF) {
			continue
		}
		return false
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(policy), &document); err != nil || document == nil {
		return false
	}
	var version string
	if rawVersion, ok := document["Version"]; !ok || json.Unmarshal(rawVersion, &version) != nil || version == "" {
		return false
	}
	rawStatements, ok := document["Statement"]
	if !ok {
		return false
	}
	var statements []json.RawMessage
	if json.Unmarshal(rawStatements, &statements) == nil {
		if len(statements) == 0 {
			return false
		}
		for _, statement := range statements {
			if !kmsPolicyStatementValid(statement) {
				return false
			}
		}
		return true
	}
	return kmsPolicyStatementValid(rawStatements)
}

func kmsPolicyStatementValid(raw json.RawMessage) bool {
	var statement map[string]json.RawMessage
	return json.Unmarshal(raw, &statement) == nil && len(statement) > 0
}

func kmsPolicyNameValid(policyName string) bool {
	return policyName == "" || policyName == "default"
}

func kmsTags(raw any) map[string]string {
	values, _ := raw.([]any)
	tags := map[string]string{}
	for _, value := range values {
		tag, _ := value.(map[string]any)
		key := stringValue(tag["TagKey"])
		if key != "" {
			tags[key] = stringValue(tag["TagValue"])
		}
	}
	return tags
}

func kmsTagsValid(raw any) bool {
	if raw == nil {
		return true
	}
	values, ok := raw.([]any)
	if !ok || len(values) > 50 {
		return false
	}
	for _, value := range values {
		tag, ok := value.(map[string]any)
		if !ok {
			return false
		}
		key, keyOK := tag["TagKey"].(string)
		tagValue, valueOK := tag["TagValue"].(string)
		if !keyOK || !valueOK || key == "" || len(key) > 128 || len(tagValue) > 256 {
			return false
		}
	}
	return true
}

func kmsTagsJSON(tags map[string]string) []map[string]string {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, map[string]string{"TagKey": key, "TagValue": tags[key]})
	}
	return out
}

func kmsStringList(raw any) []string {
	values, _ := raw.([]any)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if item := stringValue(value); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func writeKMSNotFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "NotFoundException", "message": "key not found"})
}

func writeKMSInvalidState(w http.ResponseWriter) {
	writeJSON(w, http.StatusBadRequest, map[string]string{"__type": "KMSInvalidStateException", "message": "key state does not allow this operation"})
}

func (a *KMSAdapter) validateKeyState(w http.ResponseWriter, keyID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := a.key(keyID)
	if key == nil {
		if kmsSeedKey(keyID) {
			return true
		}
		writeKMSNotFound(w)
		return false
	}
	if key.State != "Enabled" {
		writeKMSInvalidState(w)
		return false
	}
	return true
}

func kmsSeedKey(keyID string) bool {
	return keyID == "alias/homeport" || keyID == "alias/homeport-hmac"
}

func (a *KMSAdapter) authorized(w http.ResponseWriter, r *http.Request, action string, body map[string]any) bool {
	req := authz.Request{
		Principal:           awsPrincipal(r),
		PrincipalAttributes: awsPrincipalAttributes(r),
		Action:              "kms:" + action,
		Resource:            kmsARN(body),
		Context: map[string]string{
			"provider":     "aws",
			"service":      "kms",
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"__type": "KMSInternalException", "message": err.Error()})
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

func (a *KMSAdapter) mac(message []byte, algorithm string) []byte {
	h := hmac.New(kmsHash(algorithm), a.macKey)
	_, _ = h.Write(message)
	return h.Sum(nil)
}

func kmsHash(algorithm string) func() hash.Hash {
	switch algorithm {
	case "HMAC_SHA_384", "RSASSA_PSS_SHA_384", "RSASSA_PKCS1_V1_5_SHA_384", "ECDSA_SHA_384":
		return sha512.New384
	case "HMAC_SHA_512", "RSASSA_PSS_SHA_512", "RSASSA_PKCS1_V1_5_SHA_512", "ECDSA_SHA_512":
		return sha512.New
	default:
		return sha256.New
	}
}

func kmsAlgorithm(value any) string {
	if algorithm := stringValue(value); algorithm != "" {
		return algorithm
	}
	return "HMAC_SHA_256"
}

func decodeBlob(value any) ([]byte, error) {
	switch v := value.(type) {
	case []byte:
		return v, nil
	case string:
		return base64.StdEncoding.DecodeString(v)
	default:
		return nil, base64.CorruptInputError(0)
	}
}

func encodeBlob(value []byte) string {
	return base64.StdEncoding.EncodeToString(value)
}

func kmsARN(body map[string]any) string {
	keyID := stringValue(body["KeyId"])
	if keyID == "" {
		keyID = "unknown"
	}
	return "arn:aws:kms:us-east-1:homeport:" + keyID
}
