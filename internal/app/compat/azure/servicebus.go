package azure

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

const serviceBusAPIVersion = "2021-11-01"

type ServiceBusAdapter struct {
	mu          sync.Mutex
	queues      map[string]serviceBusQueue
	idempotency map[string]serviceBusStoredResponse
	queueQuota  int
	nextOp      int
	authorizer  authz.Authorizer
	auditSink   func(authz.Decision)
	backendErr  error
	backendErrs map[string]error
}

type serviceBusQueue struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	Location    string            `json:"location,omitempty"`
	ETag        string            `json:"etag,omitempty"`
	CreatedAt   string            `json:"createdAt,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Properties  map[string]any    `json:"properties,omitempty"`
	OperationID string            `json:"operationId,omitempty"`
}

type serviceBusStoredResponse struct {
	status int
	body   any
}

type ServiceBusOption func(*ServiceBusAdapter)

func NewServiceBusAdapter(options ...ServiceBusOption) *ServiceBusAdapter {
	adapter := &ServiceBusAdapter{
		queues:      map[string]serviceBusQueue{},
		idempotency: map[string]serviceBusStoredResponse{},
		authorizer:  authz.AllowAll,
	}
	for _, option := range options {
		option(adapter)
	}
	return adapter
}

func WithServiceBusAuthorizer(authorizer authz.Authorizer) ServiceBusOption {
	return func(adapter *ServiceBusAdapter) {
		if authorizer != nil {
			adapter.authorizer = authorizer
		}
	}
}

func WithServiceBusAuditSink(sink func(authz.Decision)) ServiceBusOption {
	return func(adapter *ServiceBusAdapter) {
		adapter.auditSink = sink
	}
}

func WithServiceBusQueueQuota(maxQueues int) ServiceBusOption {
	return func(adapter *ServiceBusAdapter) {
		adapter.queueQuota = maxQueues
	}
}

func WithServiceBusBackendError(err error) ServiceBusOption {
	return func(adapter *ServiceBusAdapter) {
		adapter.backendErr = err
	}
}

func WithServiceBusBackendErrorForMethod(method string, err error) ServiceBusOption {
	return func(adapter *ServiceBusAdapter) {
		if adapter.backendErrs == nil {
			adapter.backendErrs = map[string]error{}
		}
		adapter.backendErrs[method] = err
	}
}

func (ServiceBusAdapter) Provider() string { return "azure" }
func (ServiceBusAdapter) Service() string  { return "service-bus" }
func (ServiceBusAdapter) Routes() []string {
	return []string{"GET /compat/azure/service-bus/", "PUT /compat/azure/service-bus/", "DELETE /compat/azure/service-bus/"}
}
func (ServiceBusAdapter) TargetEnv() map[string]string {
	return map[string]string{
		"HOMEPORT_SERVICEBUS_ENDPOINT": "http://homeport:8080/api/v1/compat/azure/service-bus",
		"HOMEPORT_COMPAT_BACKEND":      "rabbitmq-amqp",
	}
}
func (ServiceBusAdapter) ConformanceChecks() []string {
	return []string{"create-queue", "get-queue", "list-queues", "delete-queue"}
}

func (a *ServiceBusAdapter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("x-ms-request-id", serviceBusRequestID(r))
	path := strings.TrimPrefix(r.URL.Path, "/compat/azure/service-bus")
	queue, list := serviceBusQueuePath(path)
	action := serviceBusQueueAction(r.Method)
	if action == "" || (queue == "" && !list) {
		writeAzureError(w, http.StatusBadRequest, "BadRequest", "unsupported Service Bus action")
		return
	}
	apiVersion := r.URL.Query().Get("api-version")
	if apiVersion == "" {
		writeAzureError(w, http.StatusBadRequest, "BadRequest", "missing api-version")
		return
	}
	if apiVersion != serviceBusAPIVersion {
		writeAzureError(w, http.StatusBadRequest, "BadRequest", "unsupported api-version")
		return
	}
	resource := queue
	if resource == "" {
		resource = "/" + strings.Trim(path, "/") + "*"
	}
	var body struct {
		Location   string            `json:"location"`
		Tags       map[string]string `json:"tags"`
		Properties map[string]any    `json:"properties"`
	}
	authzContext := map[string]string{}
	if r.Method == http.MethodPut && queue != "" {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeAzureError(w, http.StatusBadRequest, "BadRequest", "malformed request")
			return
		}
		if !validServiceBusQueueProperties(body.Properties) {
			writeAzureError(w, http.StatusBadRequest, "BadRequest", "invalid queue properties")
			return
		}
		if body.Location != "" {
			authzContext["location"] = body.Location
		}
		serviceBusTagContext(authzContext, body.Tags)
	}
	if r.Method != http.MethodPut && queue != "" {
		a.mu.Lock()
		value, ok := a.queues[queue]
		a.mu.Unlock()
		if ok {
			if value.Location != "" {
				authzContext["location"] = value.Location
			}
			serviceBusTagContext(authzContext, value.Tags)
		}
	}
	if !a.authorized(w, r, action, resource, authzContext) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	idempotencyKey := serviceBusIdempotencyKey(r)
	if stored, ok := a.idempotency[idempotencyKey]; idempotencyKey != "" && ok {
		writeAzureJSON(w, stored.status, stored.body)
		return
	}

	switch {
	case r.Method == http.MethodPut && queue != "":
		if _, ok := a.queues[queue]; ok {
			writeAzureError(w, http.StatusConflict, "Conflict", "queue already exists")
			return
		}
		if a.queueQuota > 0 && len(a.queues) >= a.queueQuota {
			w.Header().Set("Retry-After", "1")
			writeAzureError(w, http.StatusTooManyRequests, "TooManyRequests", "queue quota exceeded")
			return
		}
		if err := a.backendError(r.Method); err != nil {
			writeAzureError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		operationID := a.newOperationID()
		createdAt := time.Now().UTC().Format(time.RFC3339)
		properties := map[string]any{"createdAt": createdAt}
		for key, value := range body.Properties {
			properties[key] = value
		}
		value := serviceBusQueue{
			ID:          queue,
			Name:        serviceBusQueueName(queue),
			Type:        "Microsoft.ServiceBus/namespaces/queues",
			Location:    body.Location,
			ETag:        serviceBusETag(operationID),
			CreatedAt:   createdAt,
			Tags:        body.Tags,
			Properties:  properties,
			OperationID: operationID,
		}
		a.queues[queue] = value
		a.writeIdempotentAzureJSON(w, r, http.StatusOK, value)
	case r.Method == http.MethodGet && list:
		if err := a.backendError(r.Method); err != nil {
			writeAzureError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		queues := make([]serviceBusQueue, 0, len(a.queues))
		for _, queue := range a.queues {
			queues = append(queues, queue)
		}
		sort.Slice(queues, func(i, j int) bool { return queues[i].ID < queues[j].ID })
		page, next, ok := serviceBusPage(queues, r)
		if !ok {
			writeAzureError(w, http.StatusBadRequest, "BadRequest", "invalid pagination query")
			return
		}
		body := map[string]any{"value": page}
		if next != "" {
			body["nextLink"] = next
		}
		writeAzureJSON(w, http.StatusOK, body)
	case r.Method == http.MethodGet && queue != "":
		value, ok := a.queues[queue]
		if !ok {
			writeAzureError(w, http.StatusNotFound, "ResourceNotFound", "queue not found")
			return
		}
		if err := a.backendError(r.Method); err != nil {
			writeAzureError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		writeAzureJSON(w, http.StatusOK, value)
	case r.Method == http.MethodDelete && queue != "":
		value, ok := a.queues[queue]
		if !ok {
			writeAzureError(w, http.StatusNotFound, "ResourceNotFound", "queue not found")
			return
		}
		if match := r.Header.Get("If-Match"); match != "" && match != "*" && match != value.ETag {
			writeAzureError(w, http.StatusPreconditionFailed, "PreconditionFailed", "etag does not match")
			return
		}
		if err := a.backendError(r.Method); err != nil {
			writeAzureError(w, http.StatusInternalServerError, "InternalError", err.Error())
			return
		}
		delete(a.queues, queue)
		a.writeIdempotentAzureJSON(w, r, http.StatusOK, map[string]string{"status": "Deleted", "operationId": a.newOperationID()})
	default:
		writeAzureError(w, http.StatusBadRequest, "BadRequest", "unsupported Service Bus action")
	}
}

func (a *ServiceBusAdapter) backendError(method string) error {
	if err := a.backendErrs[method]; err != nil {
		return err
	}
	return a.backendErr
}

func (a *ServiceBusAdapter) writeIdempotentAzureJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	if key := serviceBusIdempotencyKey(r); key != "" {
		a.idempotency[key] = serviceBusStoredResponse{status: status, body: value}
	}
	writeAzureJSON(w, status, value)
}

func (a *ServiceBusAdapter) authorized(w http.ResponseWriter, r *http.Request, action, resource string, extraContext map[string]string) bool {
	context := map[string]string{
		"provider":     "azure",
		"service":      "service-bus",
		"method":       r.Method,
		"request_id":   serviceBusRequestID(r),
		"source_ip":    azureSourceIP(r),
		"current_time": time.Now().UTC().Format(time.RFC3339),
		"user_agent":   r.UserAgent(),
	}
	if value := r.Header.Get("X-Homeport-Credential-Age"); value != "" {
		context["credential_age"] = value
	}
	if value := r.Header.Get("X-Homeport-Credential-Expired"); value != "" {
		context["credential_expired"] = value
	}
	for header, key := range map[string]string{
		"X-Homeport-Tenant":  "tenant",
		"X-Homeport-Project": "project",
		"X-Homeport-Account": "account",
	} {
		if value := r.Header.Get(header); value != "" {
			context[key] = value
		}
	}
	for key, value := range extraContext {
		context[key] = value
	}
	decision, err := a.authorizer.Authorize(r.Context(), authz.Request{
		Principal:           azurePrincipal(r),
		PrincipalAttributes: azurePrincipalAttributes(r),
		Claims:              azureClaims(r),
		Action:              action,
		Resource:            resource,
		Context:             context,
	})
	if err != nil {
		writeAzureError(w, http.StatusInternalServerError, "InternalError", err.Error())
		return false
	}
	if a.auditSink != nil {
		a.auditSink(decision)
	}
	if !decision.Allowed {
		writeAzureError(w, http.StatusForbidden, "AuthorizationFailed", decision.Reason)
		return false
	}
	return true
}

func serviceBusPage(items []serviceBusQueue, r *http.Request) ([]serviceBusQueue, string, bool) {
	start := 0
	if token := r.URL.Query().Get("$skiptoken"); token != "" {
		var err error
		start, err = strconv.Atoi(token)
		if err != nil {
			return nil, "", false
		}
	} else if skip := r.URL.Query().Get("$skip"); skip != "" {
		var err error
		start, err = strconv.Atoi(skip)
		if err != nil {
			return nil, "", false
		}
	}
	if start < 0 || start > len(items) {
		return nil, "", false
	}
	size := 0
	if top := r.URL.Query().Get("$top"); top != "" {
		var err error
		size, err = strconv.Atoi(top)
		if err != nil || size <= 0 {
			return nil, "", false
		}
	}
	if size <= 0 || start+size > len(items) {
		size = len(items) - start
	}
	next := ""
	if start+size < len(items) {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		next = scheme + "://" + r.Host + r.URL.Path + "?api-version=" + r.URL.Query().Get("api-version") + "&$top=" + strconv.Itoa(size) + "&$skiptoken=" + strconv.Itoa(start+size)
	}
	return items[start : start+size], next, true
}

func serviceBusQueuePath(path string) (string, bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 9 && parts[0] == "subscriptions" && parts[2] == "resourceGroups" && parts[4] == "providers" && strings.EqualFold(parts[5], "Microsoft.ServiceBus") && parts[6] == "namespaces" && parts[8] == "queues" {
		return "", true
	}
	if len(parts) == 10 && parts[0] == "subscriptions" && parts[2] == "resourceGroups" && parts[4] == "providers" && strings.EqualFold(parts[5], "Microsoft.ServiceBus") && parts[6] == "namespaces" && parts[8] == "queues" {
		return "/" + strings.Join(parts, "/"), false
	}
	return "", false
}

func serviceBusQueueAction(method string) string {
	switch method {
	case http.MethodGet:
		return "Microsoft.ServiceBus/namespaces/queues/read"
	case http.MethodPut:
		return "Microsoft.ServiceBus/namespaces/queues/write"
	case http.MethodDelete:
		return "Microsoft.ServiceBus/namespaces/queues/delete"
	default:
		return ""
	}
}

func serviceBusIdempotencyKey(r *http.Request) string {
	key := r.Header.Get("Repeatability-Request-ID")
	if key == "" || r.Method == http.MethodGet {
		return ""
	}
	return r.Method + " " + r.URL.Path + " " + key
}

func serviceBusRequestID(r *http.Request) string {
	if requestID := r.Header.Get("x-ms-client-request-id"); requestID != "" {
		return requestID
	}
	return "homeport"
}

func serviceBusTagContext(context map[string]string, tags map[string]string) {
	for key, value := range tags {
		context["tag:"+key] = value
	}
}

func validServiceBusQueueProperties(properties map[string]any) bool {
	value, ok := properties["maxSizeInMegabytes"]
	if !ok {
		return true
	}
	size, ok := value.(float64)
	return ok && size > 0 && size <= 81920 && size == float64(int(size))
}

func serviceBusETag(operationID string) string {
	return `W/"` + strings.TrimPrefix(operationID, "operations/") + `"`
}

func (a *ServiceBusAdapter) newOperationID() string {
	a.nextOp++
	return "operations/" + strconv.Itoa(a.nextOp)
}

func serviceBusQueueName(resource string) string {
	_, name, ok := strings.Cut(resource, "/queues/")
	if ok {
		return name
	}
	return resource
}

func azurePrincipal(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if token, ok := strings.CutPrefix(auth, "Bearer "); ok && token != "" {
		return token
	}
	return "anonymous"
}

func azurePrincipalAttributes(r *http.Request) map[string]string {
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

func azureClaims(r *http.Request) map[string]string {
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

func azureSourceIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func writeAzureJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAzureError(w http.ResponseWriter, status int, code, message string) {
	writeAzureJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": message,
		},
	})
}
