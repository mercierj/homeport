package aws

import (
	"bytes"
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type S3Adapter struct {
	mu          sync.Mutex
	buckets     map[string]map[string][]byte
	bucketTags  map[string]map[string]string
	idempotency map[string]s3StoredResponse
	objectQuota int
	authorizer  authz.Authorizer
	auditSink   func(authz.Decision)
	backendErrs map[string]error
}

type s3StoredResponse struct {
	status  int
	headers map[string]string
}

type S3Option func(*S3Adapter)

func NewS3Adapter(options ...S3Option) *S3Adapter {
	adapter := &S3Adapter{
		buckets:     map[string]map[string][]byte{},
		bucketTags:  map[string]map[string]string{},
		idempotency: map[string]s3StoredResponse{},
		authorizer:  authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithS3Authorizer(authorizer authz.Authorizer) S3Option {
	return func(adapter *S3Adapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithS3AuditSink(sink func(authz.Decision)) S3Option {
	return func(adapter *S3Adapter) {
		adapter.auditSink = sink
	}
}

func WithS3ObjectQuota(maxObjects int) S3Option {
	return func(adapter *S3Adapter) {
		adapter.objectQuota = maxObjects
	}
}

func WithS3BackendErrorForMethod(method string, err error) S3Option {
	return func(adapter *S3Adapter) {
		if adapter.backendErrs == nil {
			adapter.backendErrs = map[string]error{}
		}
		adapter.backendErrs[method] = err
	}
}

func (S3Adapter) Provider() string { return "aws" }
func (S3Adapter) Service() string  { return "s3" }
func (S3Adapter) Routes() []string { return []string{"* /compat/aws/s3"} }
func (S3Adapter) TargetEnv() map[string]string {
	return map[string]string{
		"AWS_ENDPOINT_URL_S3":      "http://homeport:8080/api/v1/compat/aws/s3",
		"AWS_ACCESS_KEY_ID":        "homeport",
		"AWS_SECRET_ACCESS_KEY":    "homeport",
		"AWS_REGION":               "us-east-1",
		"AWS_S3_FORCE_PATH_STYLE":  "true",
		"HOMEPORT_COMPAT_BACKEND":  "minio",
		"HOMEPORT_COMPAT_PROTOCOL": "s3",
	}
}
func (S3Adapter) ConformanceChecks() []string {
	return []string{"create-bucket", "head-bucket", "list-objects-v2", "put-object", "get-object", "delete-object", "delete-bucket", "bucket-tags"}
}

func (a *S3Adapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Amz-RequestId", "homeport")
	bucket, key := s3Path(r)
	action := s3Action(r.Method, key, r.URL.Query().Has("tagging"), r.URL.Query().Has("policy"))
	if bucket == "" {
		writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "unsupported S3 request")
		return
	}
	if !validS3BucketName(bucket) {
		writeS3Error(w, http.StatusBadRequest, "InvalidBucketName", "bucket name is invalid")
		return
	}
	if action == "" {
		writeS3Error(w, http.StatusNotImplemented, "NotImplemented", "S3 action is not implemented")
		return
	}
	if !a.authorized(w, r, action, bucket, key) {
		return
	}
	if err := a.backendError(r.Method); err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	switch action {
	case "CreateBucket":
		idempotencyKey := s3IdempotencyKey(r, action, bucket)
		if idempotencyKey != "" {
			if stored, ok := a.idempotency[idempotencyKey]; ok {
				writeS3StoredResponse(w, stored)
				return
			}
		}
		if a.buckets[bucket] != nil {
			writeS3Error(w, http.StatusConflict, "BucketAlreadyOwnedByYou", "bucket already exists")
			return
		}
		a.buckets[bucket] = map[string][]byte{}
		a.bucketTags[bucket] = map[string]string{}
		if idempotencyKey != "" {
			a.idempotency[idempotencyKey] = s3StoredResponse{status: http.StatusOK}
		}
		w.WriteHeader(http.StatusOK)
	case "HeadBucket":
		if a.buckets[bucket] == nil {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		w.WriteHeader(http.StatusOK)
	case "ListObjectsV2":
		objects := a.buckets[bucket]
		if objects == nil {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		if !a.writeListObjectsV2(w, bucket, r.URL.Query().Get("continuation-token"), r.URL.Query().Get("max-keys")) {
			return
		}
	case "PutBucketTagging":
		if a.buckets[bucket] == nil {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		idempotencyKey := s3IdempotencyKey(r, action, bucket)
		if idempotencyKey != "" {
			if stored, ok := a.idempotency[idempotencyKey]; ok {
				writeS3StoredResponse(w, stored)
				return
			}
		}
		tags, err := decodeS3Tags(r.Body)
		if err != nil {
			writeS3Error(w, http.StatusBadRequest, "MalformedXML", "the XML you provided was not well-formed or did not validate against our published schema")
			return
		}
		a.bucketTags[bucket] = tags
		if idempotencyKey != "" {
			a.idempotency[idempotencyKey] = s3StoredResponse{status: http.StatusOK}
		}
		w.WriteHeader(http.StatusOK)
	case "GetBucketTagging":
		if a.buckets[bucket] == nil {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		if len(a.bucketTags[bucket]) == 0 {
			writeS3Error(w, http.StatusNotFound, "NoSuchTagSet", "bucket has no tags")
			return
		}
		writeS3Tags(w, a.bucketTags[bucket])
	case "GetBucketPolicy":
		if a.buckets[bucket] == nil {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		writeS3Error(w, http.StatusNotFound, "NoSuchBucketPolicy", "bucket policy not found")
	case "PutObject":
		if a.buckets[bucket] == nil {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		idempotencyKey := s3IdempotencyKey(r, action, bucket+"/"+key)
		if idempotencyKey != "" {
			if stored, ok := a.idempotency[idempotencyKey]; ok {
				writeS3StoredResponse(w, stored)
				return
			}
		}
		if _, exists := a.buckets[bucket][key]; !exists && a.objectQuota > 0 && a.objectCount() >= a.objectQuota {
			writeS3Error(w, http.StatusTooManyRequests, "SlowDown", "object quota exceeded")
			return
		}
		body, _ := io.ReadAll(r.Body)
		a.buckets[bucket][key] = append([]byte(nil), body...)
		etag := s3ETag(body)
		w.Header().Set("ETag", etag)
		if idempotencyKey != "" {
			a.idempotency[idempotencyKey] = s3StoredResponse{status: http.StatusOK, headers: map[string]string{"ETag": etag}}
		}
		w.WriteHeader(http.StatusOK)
	case "GetObject":
		objects := a.buckets[bucket]
		if objects == nil {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		body, ok := objects[key]
		if !ok {
			writeS3Error(w, http.StatusNotFound, "NoSuchKey", "object not found")
			return
		}
		w.Header().Set("ETag", s3ETag(body))
		_, _ = io.Copy(w, bytes.NewReader(body))
	case "DeleteObject":
		objects := a.buckets[bucket]
		if objects == nil {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		delete(objects, key)
		w.WriteHeader(http.StatusNoContent)
	case "DeleteBucket":
		objects := a.buckets[bucket]
		if objects == nil {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucket", "bucket not found")
			return
		}
		if len(objects) > 0 {
			writeS3Error(w, http.StatusConflict, "BucketNotEmpty", "bucket is not empty")
			return
		}
		delete(a.buckets, bucket)
		delete(a.bucketTags, bucket)
		w.WriteHeader(http.StatusNoContent)
	}
}

type s3Tagging struct {
	XMLName xml.Name `xml:"Tagging"`
	TagSet  []s3Tag  `xml:"TagSet>Tag"`
}

type s3Tag struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

type s3ListBucketResult struct {
	XMLName               xml.Name       `xml:"ListBucketResult"`
	XMLNS                 string         `xml:"xmlns,attr"`
	Name                  string         `xml:"Name"`
	MaxKeys               int            `xml:"MaxKeys"`
	KeyCount              int            `xml:"KeyCount"`
	IsTruncated           bool           `xml:"IsTruncated"`
	Contents              []s3ListObject `xml:"Contents"`
	NextContinuationToken string         `xml:"NextContinuationToken,omitempty"`
}

type s3ListObject struct {
	Key          string `xml:"Key"`
	ETag         string `xml:"ETag"`
	Size         int    `xml:"Size"`
	StorageClass string `xml:"StorageClass"`
}

func decodeS3Tags(r io.Reader) (map[string]string, error) {
	var tagging s3Tagging
	if err := xml.NewDecoder(r).Decode(&tagging); err != nil {
		return nil, err
	}
	if tagging.XMLName.Local != "Tagging" || len(tagging.TagSet) == 0 {
		return nil, fmt.Errorf("tag set is required")
	}
	tags := map[string]string{}
	for _, tag := range tagging.TagSet {
		if tag.Key == "" {
			return nil, fmt.Errorf("tag key is required")
		}
		tags[tag.Key] = tag.Value
	}
	return tags, nil
}

func writeS3Tags(w http.ResponseWriter, tags map[string]string) {
	keys := make([]string, 0, len(tags))
	for key := range tags {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tagging := s3Tagging{}
	for _, key := range keys {
		tagging.TagSet = append(tagging.TagSet, s3Tag{Key: key, Value: tags[key]})
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, xml.Header)
	_ = xml.NewEncoder(w).Encode(tagging)
}

func (a *S3Adapter) writeListObjectsV2(w http.ResponseWriter, bucket, token, maxKeysValue string) bool {
	objects := a.buckets[bucket]
	keys := make([]string, 0, len(objects))
	for key := range objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	start := 0
	if token != "" {
		parsed, err := strconv.Atoi(token)
		if err != nil || parsed < 0 || parsed > len(keys) {
			writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "invalid continuation token")
			return false
		}
		start = parsed
	}
	maxKeys := 1000
	if maxKeysValue != "" {
		parsed, err := strconv.Atoi(maxKeysValue)
		if err != nil || parsed < 0 {
			writeS3Error(w, http.StatusBadRequest, "InvalidRequest", "invalid max-keys")
			return false
		}
		maxKeys = parsed
	}
	end := start + maxKeys
	if end > len(keys) {
		end = len(keys)
	}

	w.Header().Set("Content-Type", "application/xml")
	result := s3ListBucketResult{
		XMLNS:       "http://s3.amazonaws.com/doc/2006-03-01/",
		Name:        bucket,
		MaxKeys:     maxKeys,
		KeyCount:    end - start,
		IsTruncated: end < len(keys),
	}
	for _, key := range keys[start:end] {
		result.Contents = append(result.Contents, s3ListObject{
			Key:          key,
			ETag:         s3ETag(objects[key]),
			Size:         len(objects[key]),
			StorageClass: "STANDARD",
		})
	}
	if end < len(keys) {
		result.NextContinuationToken = strconv.Itoa(end)
	}
	_ = xml.NewEncoder(w).Encode(result)
	return true
}

func s3ETag(body []byte) string {
	return fmt.Sprintf(`"%x"`, md5.Sum(body))
}

func (a *S3Adapter) objectCount() int {
	count := 0
	for _, objects := range a.buckets {
		count += len(objects)
	}
	return count
}

func (a *S3Adapter) backendError(method string) error {
	return a.backendErrs[method]
}

func s3IdempotencyKey(r *http.Request, action, bucket string) string {
	key := r.Header.Get("X-Idempotency-Key")
	if key == "" {
		return ""
	}
	return action + ":" + bucket + ":" + key
}

func writeS3StoredResponse(w http.ResponseWriter, stored s3StoredResponse) {
	for key, value := range stored.headers {
		w.Header().Set(key, value)
	}
	w.WriteHeader(stored.status)
}

func (a *S3Adapter) authorized(w http.ResponseWriter, r *http.Request, action, bucket, key string) bool {
	context := map[string]string{
		"current_time": time.Now().UTC().Format(time.RFC3339),
		"provider":     "aws",
		"service":      "s3",
		"method":       r.Method,
		"region":       "us-east-1",
		"request_id":   "homeport",
		"source_ip":    sourceIP(r),
		"user_agent":   r.UserAgent(),
	}
	if value := r.Header.Get("X-Homeport-Credential-Expired"); value != "" {
		context["credential_expired"] = value
	}
	if value := r.Header.Get("X-Homeport-Credential-Age"); value != "" {
		context["credential_age"] = value
	}
	for tagKey, tagValue := range a.bucketTags[bucket] {
		context["tag:"+tagKey] = tagValue
	}
	req := authz.Request{
		Principal: awsPrincipal(r),
		Action:    "s3:" + action,
		Resource:  s3ARN(bucket, key),
		Context:   context,
		Claims:    awsClaims(r),
	}
	decision, err := a.authorizer.Authorize(r.Context(), req)
	if err != nil {
		writeS3Error(w, http.StatusInternalServerError, "InternalError", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeS3Error(w, http.StatusForbidden, "AccessDenied", decision.Reason)
		return false
	}
	return true
}

func s3Path(r *http.Request) (string, string) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 0 {
		return "", ""
	}
	bucket := parts[0]
	if len(parts) == 1 {
		return bucket, ""
	}
	return bucket, parts[1]
}

func s3Action(method, key string, tagging, policy bool) string {
	switch method {
	case http.MethodPut:
		if key == "" && tagging {
			return "PutBucketTagging"
		}
		if key == "" {
			return "CreateBucket"
		}
		return "PutObject"
	case http.MethodHead:
		if key == "" {
			return "HeadBucket"
		}
	case http.MethodGet:
		if key == "" && policy {
			return "GetBucketPolicy"
		}
		if key == "" && tagging {
			return "GetBucketTagging"
		}
		if key == "" {
			return "ListObjectsV2"
		}
		if key != "" {
			return "GetObject"
		}
	case http.MethodDelete:
		if key == "" {
			return "DeleteBucket"
		}
		if key != "" {
			return "DeleteObject"
		}
	}
	return ""
}

func s3ARN(bucket, key string) string {
	if key == "" {
		return "arn:aws:s3:us-east-1:homeport:s3/" + bucket
	}
	return "arn:aws:s3:us-east-1:homeport:s3/" + bucket + "/" + key
}

func validS3BucketName(bucket string) bool {
	if len(bucket) < 3 || len(bucket) > 63 {
		return false
	}
	if !isS3BucketEdge(bucket[0]) || !isS3BucketEdge(bucket[len(bucket)-1]) {
		return false
	}
	for i := 0; i < len(bucket); i++ {
		c := bucket[i]
		if !(isS3BucketEdge(c) || c == '.' || c == '-') {
			return false
		}
	}
	return !strings.Contains(bucket, "..") && !strings.Contains(bucket, ".-") && !strings.Contains(bucket, "-.")
}

func isS3BucketEdge(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

func writeS3Error(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `<Error><Code>%s</Code><Message>%s</Message><RequestId>homeport</RequestId></Error>`, code, message)
}
