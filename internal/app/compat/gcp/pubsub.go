package gcp

import (
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/domain/authz"
)

type PubSubAdapter struct {
	mu            sync.Mutex
	topics        map[string]pubSubTopic
	subscriptions map[string]pubSubSubscription
	idempotency   map[string]pubSubStoredResponse
	topicQuota    int
	subQuota      int
	nextOperation int
	authorizer    authz.Authorizer
	auditSink     func(authz.Decision)
}

type pubSubTopic struct {
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels,omitempty"`
	OperationID string            `json:"operationId,omitempty"`
}

type pubSubSubscription struct {
	Name        string            `json:"name"`
	Topic       string            `json:"topic"`
	Labels      map[string]string `json:"labels,omitempty"`
	OperationID string            `json:"operationId,omitempty"`
}

type pubSubStoredResponse struct {
	status int
	body   any
}

type PubSubOption func(*PubSubAdapter)

func NewPubSubAdapter(options ...PubSubOption) *PubSubAdapter {
	adapter := &PubSubAdapter{
		topics:        map[string]pubSubTopic{},
		subscriptions: map[string]pubSubSubscription{},
		idempotency:   map[string]pubSubStoredResponse{},
		authorizer:    authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithPubSubAuthorizer(authorizer authz.Authorizer) PubSubOption {
	return func(adapter *PubSubAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithPubSubAuditSink(sink func(authz.Decision)) PubSubOption {
	return func(adapter *PubSubAdapter) {
		adapter.auditSink = sink
	}
}

func WithPubSubTopicQuota(maxTopics int) PubSubOption {
	return func(adapter *PubSubAdapter) {
		adapter.topicQuota = maxTopics
	}
}

func WithPubSubSubscriptionQuota(maxSubscriptions int) PubSubOption {
	return func(adapter *PubSubAdapter) {
		adapter.subQuota = maxSubscriptions
	}
}

func (PubSubAdapter) Provider() string { return "gcp" }
func (PubSubAdapter) Service() string  { return "pub-sub" }
func (PubSubAdapter) Routes() []string {
	return []string{"GET /compat/gcp/pub-sub/", "PUT /compat/gcp/pub-sub/", "DELETE /compat/gcp/pub-sub/"}
}
func (PubSubAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"PUBSUB_EMULATOR_HOST":    "homeport:8080/api/v1/compat/gcp/pub-sub",
		"HOMEPORT_COMPAT_BACKEND": "nats-jetstream",
	}
}
func (PubSubAdapter) ConformanceChecks() []string {
	return []string{"create-topic", "get-topic", "list-topics", "delete-topic", "create-subscription", "get-subscription", "list-subscriptions", "delete-subscription"}
}

func (a *PubSubAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/compat/gcp/pub-sub")
	topic, list := pubSubTopicPath(path)
	subscription, listSubscriptions := pubSubSubscriptionPath(path)
	action := ""
	if topic != "" || list {
		action = pubSubTopicAction(r.Method, list)
	}
	if action == "" && (subscription != "" || listSubscriptions) {
		action = pubSubSubscriptionAction(r.Method, listSubscriptions)
	}
	if action == "" {
		writeGCPError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "unsupported Pub/Sub action")
		return
	}
	resource := topic
	if resource == "" {
		resource = subscription
	}
	if resource == "" && list {
		resource = pubSubListResource(path, "topics")
	}
	if resource == "" && listSubscriptions {
		resource = pubSubListResource(path, "subscriptions")
	}
	if resource == "" {
		resource = "projects/unknown/topics/*"
	}
	if !a.authorized(w, r, action, resource) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if replay, ok := a.idempotency[pubSubIdempotencyKey(r)]; ok {
		writeGCPJSON(w, replay.status, replay.body)
		return
	}

	switch {
	case r.Method == http.MethodPut && topic != "":
		if _, ok := a.topics[topic]; ok {
			writeGCPError(w, http.StatusConflict, "ALREADY_EXISTS", "topic already exists")
			return
		}
		var body struct {
			Labels map[string]string `json:"labels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeGCPError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed request")
			return
		}
		if a.topicQuota > 0 && len(a.topics) >= a.topicQuota {
			writeGCPError(w, http.StatusTooManyRequests, "RESOURCE_EXHAUSTED", "topic quota exceeded")
			return
		}
		a.topics[topic] = pubSubTopic{Name: topic, Labels: body.Labels, OperationID: a.newOperationID()}
		a.writeIdempotentJSON(w, r, http.StatusOK, a.topics[topic])
	case r.Method == http.MethodGet && list:
		topics := make([]pubSubTopic, 0, len(a.topics))
		for _, topic := range a.topics {
			topics = append(topics, topic)
		}
		sort.Slice(topics, func(i, j int) bool { return topics[i].Name < topics[j].Name })
		page, next, ok := pubSubPage(topics, r)
		if !ok {
			writeGCPError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid pagination query")
			return
		}
		writeGCPJSON(w, http.StatusOK, map[string]any{"topics": page, "nextPageToken": next})
	case r.Method == http.MethodGet && topic != "":
		value, ok := a.topics[topic]
		if !ok {
			writeGCPError(w, http.StatusNotFound, "NOT_FOUND", "topic not found")
			return
		}
		writeGCPJSON(w, http.StatusOK, value)
	case r.Method == http.MethodDelete && topic != "":
		if _, ok := a.topics[topic]; !ok {
			writeGCPError(w, http.StatusNotFound, "NOT_FOUND", "topic not found")
			return
		}
		delete(a.topics, topic)
		a.writeIdempotentJSON(w, r, http.StatusOK, map[string]string{"operationId": a.newOperationID()})
	case r.Method == http.MethodPut && subscription != "":
		if _, ok := a.subscriptions[subscription]; ok {
			writeGCPError(w, http.StatusConflict, "ALREADY_EXISTS", "subscription already exists")
			return
		}
		var body struct {
			Topic  string            `json:"topic"`
			Labels map[string]string `json:"labels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeGCPError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "malformed request")
			return
		}
		if body.Topic == "" {
			writeGCPError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "topic is required")
			return
		}
		if _, ok := a.topics[body.Topic]; !ok {
			writeGCPError(w, http.StatusNotFound, "NOT_FOUND", "topic not found")
			return
		}
		if a.subQuota > 0 && len(a.subscriptions) >= a.subQuota {
			writeGCPError(w, http.StatusTooManyRequests, "RESOURCE_EXHAUSTED", "subscription quota exceeded")
			return
		}
		a.subscriptions[subscription] = pubSubSubscription{Name: subscription, Topic: body.Topic, Labels: body.Labels, OperationID: a.newOperationID()}
		a.writeIdempotentJSON(w, r, http.StatusOK, a.subscriptions[subscription])
	case r.Method == http.MethodGet && listSubscriptions:
		subscriptions := make([]pubSubSubscription, 0, len(a.subscriptions))
		for _, subscription := range a.subscriptions {
			subscriptions = append(subscriptions, subscription)
		}
		sort.Slice(subscriptions, func(i, j int) bool { return subscriptions[i].Name < subscriptions[j].Name })
		page, next, ok := pubSubPage(subscriptions, r)
		if !ok {
			writeGCPError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid pagination query")
			return
		}
		writeGCPJSON(w, http.StatusOK, map[string]any{"subscriptions": page, "nextPageToken": next})
	case r.Method == http.MethodGet && subscription != "":
		value, ok := a.subscriptions[subscription]
		if !ok {
			writeGCPError(w, http.StatusNotFound, "NOT_FOUND", "subscription not found")
			return
		}
		writeGCPJSON(w, http.StatusOK, value)
	case r.Method == http.MethodDelete && subscription != "":
		if _, ok := a.subscriptions[subscription]; !ok {
			writeGCPError(w, http.StatusNotFound, "NOT_FOUND", "subscription not found")
			return
		}
		delete(a.subscriptions, subscription)
		a.writeIdempotentJSON(w, r, http.StatusOK, map[string]string{"operationId": a.newOperationID()})
	default:
		writeGCPError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "unsupported Pub/Sub action")
	}
}

func (a *PubSubAdapter) authorized(w http.ResponseWriter, r *http.Request, action, resource string) bool {
	req := authz.Request{
		Principal:           gcpPrincipal(r),
		PrincipalAttributes: gcpPrincipalAttributes(r),
		Claims:              gcpClaims(r),
		Action:              action,
		Resource:            resource,
		Context: map[string]string{
			"provider":     "gcp",
			"service":      "pub-sub",
			"method":       r.Method,
			"request_id":   "homeport",
			"source_ip":    gcpSourceIP(r),
			"current_time": time.Now().UTC().Format(time.RFC3339),
			"user_agent":   r.UserAgent(),
		},
	}
	if value := r.Header.Get("X-Homeport-Credential-Expired"); value != "" {
		req.Context["credential_expired"] = value
	}
	if value := r.Header.Get("X-Homeport-Credential-Age"); value != "" {
		req.Context["credential_age"] = value
	}
	decision, err := a.authorizer.Authorize(r.Context(), req)
	if err != nil {
		writeGCPError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeGCPError(w, http.StatusForbidden, "PERMISSION_DENIED", decision.Reason)
		return false
	}
	return true
}

func (a *PubSubAdapter) writeIdempotentJSON(w http.ResponseWriter, r *http.Request, status int, body any) {
	if key := pubSubIdempotencyKey(r); key != "" {
		a.idempotency[key] = pubSubStoredResponse{status: status, body: body}
	}
	writeGCPJSON(w, status, body)
}

func (a *PubSubAdapter) newOperationID() string {
	a.nextOperation++
	return "operations/" + strconv.Itoa(a.nextOperation)
}

func pubSubIdempotencyKey(r *http.Request) string {
	key := r.Header.Get("X-Idempotency-Key")
	if key == "" || r.Method == http.MethodGet {
		return ""
	}
	return r.Method + " " + r.URL.Path + " " + key
}

func pubSubTopicPath(path string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/v1/"), "/")
	if len(parts) == 3 && parts[0] == "projects" && parts[2] == "topics" {
		return "", true
	}
	if len(parts) == 4 && parts[0] == "projects" && parts[2] == "topics" {
		return strings.Join(parts, "/"), false
	}
	return "", false
}

func pubSubListResource(path, collection string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/v1/"), "/")
	if len(parts) == 3 && parts[0] == "projects" && parts[2] == collection {
		return strings.Join(parts, "/") + "/*"
	}
	return ""
}

func pubSubTopicAction(method string, list bool) string {
	switch method {
	case http.MethodPut:
		return "pubsub.projects.topics.create"
	case http.MethodGet:
		if list {
			return "pubsub.projects.topics.list"
		}
		return "pubsub.projects.topics.get"
	case http.MethodDelete:
		return "pubsub.projects.topics.delete"
	default:
		return ""
	}
}

func pubSubSubscriptionPath(path string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(path, "/v1/"), "/")
	if len(parts) == 3 && parts[0] == "projects" && parts[2] == "subscriptions" {
		return "", true
	}
	if len(parts) == 4 && parts[0] == "projects" && parts[2] == "subscriptions" {
		return strings.Join(parts, "/"), false
	}
	return "", false
}

func pubSubSubscriptionAction(method string, list bool) string {
	switch method {
	case http.MethodPut:
		return "pubsub.projects.subscriptions.create"
	case http.MethodGet:
		if list {
			return "pubsub.projects.subscriptions.list"
		}
		return "pubsub.projects.subscriptions.get"
	case http.MethodDelete:
		return "pubsub.projects.subscriptions.delete"
	default:
		return ""
	}
}

func pubSubPage[T any](items []T, r *http.Request) ([]T, string, bool) {
	start := 0
	if token := r.URL.Query().Get("pageToken"); token != "" {
		var err error
		start, err = strconv.Atoi(token)
		if err != nil {
			return nil, "", false
		}
	}
	if start < 0 || start > len(items) {
		return nil, "", false
	}
	size := 0
	if value := r.URL.Query().Get("pageSize"); value != "" {
		var err error
		size, err = strconv.Atoi(value)
		if err != nil {
			return nil, "", false
		}
	}
	if size < 0 {
		return nil, "", false
	}
	if size <= 0 || start+size > len(items) {
		size = len(items) - start
	}
	next := ""
	if start+size < len(items) {
		next = strconv.Itoa(start + size)
	}
	return items[start : start+size], next, true
}

func gcpPrincipal(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if token, ok := strings.CutPrefix(auth, "Bearer "); ok && token != "" {
		return token
	}
	return "anonymous"
}

func gcpPrincipalAttributes(r *http.Request) map[string]string {
	attributes := map[string]string{}
	const prefix = "x-homeport-principal-attribute-"
	for key, values := range r.Header {
		name, ok := strings.CutPrefix(strings.ToLower(key), prefix)
		if ok && len(values) > 0 && values[0] != "" {
			attributes[name] = values[0]
		}
	}
	return attributes
}

func gcpClaims(r *http.Request) map[string]string {
	claims := map[string]string{}
	const prefix = "x-homeport-claim-"
	for key, values := range r.Header {
		name, ok := strings.CutPrefix(strings.ToLower(key), prefix)
		if ok && len(values) > 0 && values[0] != "" {
			claims[name] = values[0]
		}
	}
	return claims
}

func gcpSourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func writeGCPJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeGCPError(w http.ResponseWriter, status int, code, message string) {
	writeGCPJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    status,
			"status":  code,
			"message": message,
		},
	})
}
