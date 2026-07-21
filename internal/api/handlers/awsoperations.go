package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	apiMiddleware "github.com/homeport/homeport/internal/api/middleware"
	"github.com/homeport/homeport/internal/app/awsoperations"
	"github.com/homeport/homeport/internal/domain/authz"
)

// AWSOperationsHandler exposes only the local post-cutover projection. It has
// no AWS client and dispatches operations exclusively through local backends.
type AWSOperationsHandler struct {
	service    *awsoperations.Service
	lambda     *awsoperations.LambdaDriver
	sqs        *awsoperations.SQSDriver
	drivers    *awsoperations.DriverRegistry
	authorizer authz.Authorizer
	auditSink  func(authz.Decision) error
}

func NewAWSOperationsHandler(service *awsoperations.Service, drivers ...awsoperations.Driver) *AWSOperationsHandler {
	return NewAWSOperationsHandlerWithAuthorization(service, authz.AllowAll, nil, drivers...)
}

// NewAWSOperationsHandlerWithAuthorization injects the policy and audit
// boundaries for mutable post-cutover operations. Authorization defaults to
// allow-all only for callers using the backwards-compatible constructor.
func NewAWSOperationsHandlerWithAuthorization(service *awsoperations.Service, authorizer authz.Authorizer, auditSink func(authz.Decision) error, drivers ...awsoperations.Driver) *AWSOperationsHandler {
	if authorizer == nil {
		authorizer = authz.AllowAll
	}
	h := &AWSOperationsHandler{service: service, authorizer: authorizer, auditSink: auditSink}
	for _, d := range drivers {
		switch typed := d.(type) {
		case *awsoperations.LambdaDriver:
			h.lambda = typed
		case *awsoperations.SQSDriver:
			h.sqs = typed
		}
	}
	registry, err := awsoperations.NewDriverRegistry(drivers...)
	if err == nil {
		h.drivers = registry
	}
	return h
}
func (h *AWSOperationsHandler) RegisterRoutes(r chi.Router) {
	r.Get("/workspaces", h.ListWorkspaces)
	r.Route("/workspaces/{workspaceID}", func(r chi.Router) {
		r.Get("/", h.GetWorkspace)
		r.Get("/services", h.ListServices)
		r.Get("/services/{service}", h.GetService)
		r.Get("/services/{service}/health", h.GetServiceHealth)
		r.Get("/services/{service}/resources", h.ListServiceResources)
		r.Get("/services/{service}/resources/{resourceID}", h.GetServiceResource)
		r.Get("/services/lambda/resources", h.ListLambdaResources)
		r.Route("/services/lambda/resources/{resourceID}", func(r chi.Router) {
			r.Get("/", h.GetLambdaResource)
			r.Put("/", h.UpdateLambdaResource)
			r.Delete("/", h.DeleteLambdaResource)
			r.Post("/invoke", h.InvokeLambdaResource)
			r.Get("/logs", h.ListLambdaLogs)
		})
		r.Get("/services/sqs/resources", h.ListSQSResources)
		r.Route("/services/sqs/resources/{resourceID}", func(r chi.Router) {
			r.Get("/messages", h.ListSQSMessages)
			r.Delete("/messages", h.PurgeSQSQueue)
			r.Route("/messages/{messageID}", func(r chi.Router) { r.Post("/retry", h.RetrySQSMessage); r.Delete("/", h.DeleteSQSMessage) })
		})
	})
}
func (h *AWSOperationsHandler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	workspaces, err := h.service.List()
	if err != nil {
		respondError(w, r, 500, "Unable to load AWS operations workspaces")
		return
	}
	result := make([]awsOperationsWorkspaceResponse, 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.Provider == "aws" {
			result = append(result, workspaceResponse(workspace))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	respondJSON(w, r, 200, struct {
		Workspaces []awsOperationsWorkspaceResponse `json:"workspaces"`
	}{result})
}
func (h *AWSOperationsHandler) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.workspace(w, r)
	if ok {
		respondJSON(w, r, 200, workspaceResponse(workspace))
	}
}
func (h *AWSOperationsHandler) ListServices(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.workspace(w, r)
	if !ok {
		return
	}
	services := make([]awsOperationsServiceResponse, 0, len(workspace.Services))
	for _, metadata := range awsoperations.RegisteredServices() {
		if state, exists := workspace.Services[metadata.Key]; exists {
			services = append(services, awsOperationsServiceResponse{Service: metadata.Key, DisplayName: metadata.DisplayName, Target: metadata.Target, Family: metadata.Family, PanelKind: metadata.PanelKind, Status: state.Status, Capabilities: append([]awsoperations.Capability(nil), state.Capabilities...), Reason: state.Reason})
		}
	}
	respondJSON(w, r, 200, struct {
		WorkspaceID string                         `json:"workspace_id"`
		Services    []awsOperationsServiceResponse `json:"services"`
	}{workspace.ID, services})
}

func (h *AWSOperationsHandler) GetService(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.workspace(w, r)
	if !ok {
		return
	}
	key, metadata, state, found := awsOperationService(*workspace, chi.URLParam(r, "service"))
	if !found {
		respondError(w, r, http.StatusNotFound, "AWS operations service not found in workspace")
		return
	}
	respondJSON(w, r, http.StatusOK, awsOperationsServiceDetailResponse{Service: key, DisplayName: metadata.DisplayName, Target: metadata.Target, Family: metadata.Family, PanelKind: metadata.PanelKind, Status: state.Status, Capabilities: append([]awsoperations.Capability(nil), state.Capabilities...), Reason: state.Reason})
}

func (h *AWSOperationsHandler) GetServiceHealth(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.workspace(w, r)
	if !ok {
		return
	}
	key, metadata, state, found := awsOperationService(*workspace, chi.URLParam(r, "service"))
	if !found {
		respondError(w, r, http.StatusNotFound, "AWS operations service not found in workspace")
		return
	}
	respondJSON(w, r, http.StatusOK, struct {
		Service awsoperations.ServiceKey    `json:"service"`
		Target  string                      `json:"target"`
		Status  awsoperations.ServiceStatus `json:"status"`
		Reason  string                      `json:"reason,omitempty"`
	}{key, metadata.Target, state.Status, state.Reason})
}

func (h *AWSOperationsHandler) ListServiceResources(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.workspace(w, r)
	if !ok {
		return
	}
	key, metadata, state, found := awsOperationService(*workspace, chi.URLParam(r, "service"))
	if !found || h.drivers == nil {
		respondError(w, r, http.StatusNotFound, "AWS operations service not found in workspace")
		return
	}
	driver, found := h.drivers.Get(key)
	if !found {
		respondError(w, r, http.StatusConflict, "AWS operations local driver is unavailable")
		return
	}
	if state.Status != awsoperations.ServiceStatusAvailable {
		driver = awsoperations.NewUnavailableDriver(metadata)
	}
	items, err := driver.List(r.Context(), *workspace)
	if err != nil {
		h.operationError(w, r, err)
		return
	}
	h.resources(w, r, workspace.ID, key, items)
}

// GetServiceResource exposes an attested resource projection for every
// catalogue service. The lookup is by imported identity and never turns a
// browser-provided local identifier into a backend operation.
func (h *AWSOperationsHandler) GetServiceResource(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.workspace(w, r)
	if !ok {
		return
	}
	key, metadata, _, found := awsOperationService(*workspace, chi.URLParam(r, "service"))
	if !found {
		respondError(w, r, http.StatusNotFound, "AWS operations service not found in workspace")
		return
	}
	resourceID := chi.URLParam(r, "resourceID")
	for _, binding := range workspace.Bindings {
		if binding.Service != key || binding.ImportedResourceID != resourceID {
			continue
		}
		items, err := awsoperations.NewUnavailableDriver(metadata).List(r.Context(), *workspace)
		if err != nil {
			h.operationError(w, r, err)
			return
		}
		for _, item := range items {
			record, isUnavailableRecord := item.(awsoperations.UnavailableResourceRecord)
			if isUnavailableRecord && record.ImportedResourceID == binding.ImportedResourceID {
				respondJSON(w, r, http.StatusOK, struct {
					WorkspaceID string                   `json:"workspace_id"`
					Service     awsoperations.ServiceKey `json:"service"`
					Resource    any                      `json:"resource"`
				}{workspace.ID, key, record})
				return
			}
		}
	}
	respondError(w, r, http.StatusNotFound, "AWS operations resource not found in workspace")
}

func awsOperationService(workspace awsoperations.Workspace, raw string) (awsoperations.ServiceKey, awsoperations.ServiceMetadata, awsoperations.ServiceState, bool) {
	key := awsoperations.ServiceKey(raw)
	metadata, registered := awsoperations.ServiceMetadataFor(key)
	state, visible := workspace.Services[key]
	return key, metadata, state, registered && visible
}
func (h *AWSOperationsHandler) ListLambdaResources(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.workspace(w, r)
	if !ok {
		return
	}
	if state := workspace.Services[awsoperations.ServiceLambda]; state.Status != awsoperations.ServiceStatusAvailable {
		metadata, _ := awsoperations.ServiceMetadataFor(awsoperations.ServiceLambda)
		items, _ := awsoperations.NewUnavailableDriver(metadata).List(r.Context(), *workspace)
		h.resources(w, r, workspace.ID, awsoperations.ServiceLambda, items)
		return
	}
	if h.lambda == nil {
		h.backendUnavailable(w, r)
		return
	}
	items, err := h.lambda.List(r.Context(), *workspace)
	if err != nil {
		h.operationError(w, r, err)
		return
	}
	h.resources(w, r, workspace.ID, awsoperations.ServiceLambda, items)
}
func (h *AWSOperationsHandler) ListSQSResources(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.workspace(w, r)
	if !ok {
		return
	}
	if state := workspace.Services[awsoperations.ServiceSQS]; state.Status != awsoperations.ServiceStatusAvailable {
		metadata, _ := awsoperations.ServiceMetadataFor(awsoperations.ServiceSQS)
		items, _ := awsoperations.NewUnavailableDriver(metadata).List(r.Context(), *workspace)
		h.resources(w, r, workspace.ID, awsoperations.ServiceSQS, items)
		return
	}
	if h.sqs == nil {
		h.backendUnavailable(w, r)
		return
	}
	items, err := h.sqs.List(r.Context(), *workspace)
	if err != nil {
		h.operationError(w, r, err)
		return
	}
	h.resources(w, r, workspace.ID, awsoperations.ServiceSQS, items)
}
func (h *AWSOperationsHandler) resources(w http.ResponseWriter, r *http.Request, id string, service awsoperations.ServiceKey, items []any) {
	if items == nil {
		items = []any{}
	}
	respondJSON(w, r, 200, struct {
		WorkspaceID string                   `json:"workspace_id"`
		Service     awsoperations.ServiceKey `json:"service"`
		Resources   []any                    `json:"resources"`
	}{id, service, items})
}

type lambdaResourceRequest struct {
	Name           string            `json:"name"`
	Runtime        string            `json:"runtime"`
	Handler        string            `json:"handler"`
	MemoryMB       int               `json:"memory_mb"`
	TimeoutSeconds int               `json:"timeout_seconds"`
	Environment    map[string]string `json:"environment"`
	Description    string            `json:"description"`
}

func (v lambdaResourceRequest) input() awsoperations.FunctionInput {
	return awsoperations.FunctionInput{Name: v.Name, Runtime: v.Runtime, Handler: v.Handler, MemoryMB: v.MemoryMB, TimeoutSeconds: v.TimeoutSeconds, Environment: v.Environment, Description: v.Description}
}
func (h *AWSOperationsHandler) GetLambdaResource(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.lambdaWorkspace(w, r)
	if !ok {
		return
	}
	result, err := h.lambda.Get(r.Context(), *workspace, chi.URLParam(r, "resourceID"))
	if err != nil {
		h.operationError(w, r, err)
		return
	}
	h.resource(w, r, workspace.ID, awsoperations.ServiceLambda, result)
}
func (h *AWSOperationsHandler) UpdateLambdaResource(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.lambdaWorkspace(w, r)
	if !ok {
		return
	}
	if !h.authorizeMutation(w, r, workspace, awsoperations.ServiceLambda, chi.URLParam(r, "resourceID"), "update") {
		return
	}
	var input lambdaResourceRequest
	if !decodeAWSOperationsRequest(w, r, &input) {
		return
	}
	result, err := h.lambda.Update(r.Context(), *workspace, chi.URLParam(r, "resourceID"), input.input())
	if err != nil {
		h.operationError(w, r, err)
		return
	}
	h.resource(w, r, workspace.ID, awsoperations.ServiceLambda, result)
}
func (h *AWSOperationsHandler) DeleteLambdaResource(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.lambdaWorkspace(w, r)
	if !ok {
		return
	}
	if !h.authorizeMutation(w, r, workspace, awsoperations.ServiceLambda, chi.URLParam(r, "resourceID"), "delete") {
		return
	}
	if err := h.lambda.Delete(r.Context(), *workspace, chi.URLParam(r, "resourceID")); err != nil {
		h.operationError(w, r, err)
		return
	}
	h.operation(w, r, workspace.ID, awsoperations.ServiceLambda, chi.URLParam(r, "resourceID"), "deleted", nil)
}
func (h *AWSOperationsHandler) InvokeLambdaResource(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.lambdaWorkspace(w, r)
	if !ok {
		return
	}
	if !h.authorizeMutation(w, r, workspace, awsoperations.ServiceLambda, chi.URLParam(r, "resourceID"), "invoke") {
		return
	}
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, r, 400, "Unable to read invocation payload")
		return
	}
	result, err := h.lambda.Invoke(r.Context(), *workspace, chi.URLParam(r, "resourceID"), payload)
	if err != nil {
		h.operationError(w, r, err)
		return
	}
	h.operation(w, r, workspace.ID, awsoperations.ServiceLambda, chi.URLParam(r, "resourceID"), "invoked", result)
}
func (h *AWSOperationsHandler) ListLambdaLogs(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.lambdaWorkspace(w, r)
	if !ok {
		return
	}
	logs, err := h.lambda.Logs(r.Context(), *workspace, chi.URLParam(r, "resourceID"))
	if err != nil {
		h.operationError(w, r, err)
		return
	}
	if logs == nil {
		logs = []awsoperations.LogRecord{}
	}
	respondJSON(w, r, 200, struct {
		WorkspaceID string                    `json:"workspace_id"`
		Service     awsoperations.ServiceKey  `json:"service"`
		ResourceID  string                    `json:"resource_id"`
		Logs        []awsoperations.LogRecord `json:"logs"`
	}{workspace.ID, awsoperations.ServiceLambda, chi.URLParam(r, "resourceID"), logs})
}
func (h *AWSOperationsHandler) ListSQSMessages(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.sqsWorkspace(w, r)
	if !ok {
		return
	}
	messages, err := h.sqs.Messages(r.Context(), *workspace, chi.URLParam(r, "resourceID"), r.URL.Query().Get("status"))
	if err != nil {
		h.operationError(w, r, err)
		return
	}
	if messages == nil {
		messages = []awsoperations.MessageRecord{}
	}
	respondJSON(w, r, 200, struct {
		WorkspaceID string                        `json:"workspace_id"`
		Service     awsoperations.ServiceKey      `json:"service"`
		ResourceID  string                        `json:"resource_id"`
		Messages    []awsoperations.MessageRecord `json:"messages"`
	}{workspace.ID, awsoperations.ServiceSQS, chi.URLParam(r, "resourceID"), messages})
}
func (h *AWSOperationsHandler) RetrySQSMessage(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.sqsWorkspace(w, r)
	if !ok {
		return
	}
	if !h.authorizeMutation(w, r, workspace, awsoperations.ServiceSQS, chi.URLParam(r, "resourceID"), "retry") {
		return
	}
	if err := h.sqs.Retry(r.Context(), *workspace, chi.URLParam(r, "resourceID"), chi.URLParam(r, "messageID")); err != nil {
		h.operationError(w, r, err)
		return
	}
	h.operation(w, r, workspace.ID, awsoperations.ServiceSQS, chi.URLParam(r, "resourceID"), "retried", nil)
}
func (h *AWSOperationsHandler) DeleteSQSMessage(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.sqsWorkspace(w, r)
	if !ok {
		return
	}
	if !h.authorizeMutation(w, r, workspace, awsoperations.ServiceSQS, chi.URLParam(r, "resourceID"), "delete-message") {
		return
	}
	if err := h.sqs.Delete(r.Context(), *workspace, chi.URLParam(r, "resourceID"), chi.URLParam(r, "messageID")); err != nil {
		h.operationError(w, r, err)
		return
	}
	h.operation(w, r, workspace.ID, awsoperations.ServiceSQS, chi.URLParam(r, "resourceID"), "deleted", nil)
}
func (h *AWSOperationsHandler) PurgeSQSQueue(w http.ResponseWriter, r *http.Request) {
	workspace, ok := h.sqsWorkspace(w, r)
	if !ok {
		return
	}
	if !h.authorizeMutation(w, r, workspace, awsoperations.ServiceSQS, chi.URLParam(r, "resourceID"), "purge") {
		return
	}
	status := r.URL.Query().Get("status")
	if status == "" {
		respondError(w, r, 400, "status query parameter is required")
		return
	}
	deleted, err := h.sqs.Purge(r.Context(), *workspace, chi.URLParam(r, "resourceID"), status)
	if err != nil {
		h.operationError(w, r, err)
		return
	}
	h.operation(w, r, workspace.ID, awsoperations.ServiceSQS, chi.URLParam(r, "resourceID"), "purged", map[string]any{"deleted": deleted})
}
func (h *AWSOperationsHandler) resource(w http.ResponseWriter, r *http.Request, workspaceID string, service awsoperations.ServiceKey, resource any) {
	respondJSON(w, r, 200, struct {
		WorkspaceID string                   `json:"workspace_id"`
		Service     awsoperations.ServiceKey `json:"service"`
		Resource    any                      `json:"resource"`
	}{workspaceID, service, resource})
}
func (h *AWSOperationsHandler) operation(w http.ResponseWriter, r *http.Request, workspaceID string, service awsoperations.ServiceKey, resourceID, status string, result any) {
	respondJSON(w, r, 200, struct {
		WorkspaceID string                   `json:"workspace_id"`
		Service     awsoperations.ServiceKey `json:"service"`
		ResourceID  string                   `json:"resource_id"`
		Status      string                   `json:"status"`
		Result      any                      `json:"result,omitempty"`
	}{workspaceID, service, resourceID, status, result})
}
func (h *AWSOperationsHandler) lambdaWorkspace(w http.ResponseWriter, r *http.Request) (*awsoperations.Workspace, bool) {
	workspace, ok := h.workspace(w, r)
	if !ok {
		return nil, false
	}
	if h.lambda == nil {
		h.backendUnavailable(w, r)
		return nil, false
	}
	return workspace, true
}
func (h *AWSOperationsHandler) sqsWorkspace(w http.ResponseWriter, r *http.Request) (*awsoperations.Workspace, bool) {
	workspace, ok := h.workspace(w, r)
	if !ok {
		return nil, false
	}
	if h.sqs == nil {
		h.backendUnavailable(w, r)
		return nil, false
	}
	return workspace, true
}
func (h *AWSOperationsHandler) workspace(w http.ResponseWriter, r *http.Request) (*awsoperations.Workspace, bool) {
	if h == nil || h.service == nil {
		respondError(w, r, 500, "AWS operations service is not configured")
		return nil, false
	}
	workspace, err := h.service.Get(chi.URLParam(r, "workspaceID"))
	if err != nil || workspace.Provider != "aws" {
		respondError(w, r, 404, "AWS operations workspace not found")
		return nil, false
	}
	return workspace, true
}
func decodeAWSOperationsRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		respondError(w, r, 400, "Invalid request body")
		return false
	}
	return true
}
func (h *AWSOperationsHandler) backendUnavailable(w http.ResponseWriter, r *http.Request) {
	respondError(w, r, 409, "AWS operations local backend is unavailable")
}
func (h *AWSOperationsHandler) operationError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, awsoperations.ErrResourceNotBound):
		respondError(w, r, 403, "AWS operations resource is not bound to this workspace")
	case errors.Is(err, awsoperations.ErrServiceUnavailable), errors.Is(err, awsoperations.ErrCapabilityUnavailable):
		respondError(w, r, 409, "AWS operations service is unavailable")
	default:
		respondError(w, r, 500, "AWS operations local backend failed")
	}
}

// authorizeMutation derives the authorization resource from the persisted
// binding. The URL's local identifier is used only to find that binding and is
// never forwarded as an authorization resource on its own.
func (h *AWSOperationsHandler) authorizeMutation(w http.ResponseWriter, r *http.Request, workspace *awsoperations.Workspace, service awsoperations.ServiceKey, localResourceID, operation string) bool {
	binding, found := awsOperationsBinding(*workspace, service, localResourceID)
	if !found {
		respondError(w, r, http.StatusForbidden, "AWS operations resource is not bound to this workspace")
		return false
	}
	principal := "anonymous"
	if session := apiMiddleware.GetSession(r); session != nil && session.Username != "" {
		principal = "user:" + session.Username
	}
	request := authz.Request{
		Principal: principal,
		Action:    "aws-operations:" + string(service) + ":" + operation,
		Resource:  "aws-operations://workspaces/" + workspace.ID + "/services/" + string(service) + "/resources/" + binding.ImportedResourceID,
		Context: map[string]string{
			"provider":             "aws",
			"workspace_id":         workspace.ID,
			"service":              string(service),
			"bound_resource_id":    binding.LocalResourceID,
			"imported_resource_id": binding.ImportedResourceID,
			"local_stack_id":       binding.LocalStackID,
			"method":               r.Method,
		},
	}
	decision, err := h.authorizer.Authorize(r.Context(), request)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "AWS operations authorization failed")
		return false
	}
	if h.auditSink == nil {
		respondError(w, r, http.StatusServiceUnavailable, "AWS operations audit persistence is unavailable")
		return false
	}
	if err := h.auditSink(decision); err != nil {
		respondError(w, r, http.StatusServiceUnavailable, "AWS operations audit persistence failed")
		return false
	}
	if !decision.Allowed {
		respondError(w, r, http.StatusForbidden, "AWS operations access denied")
		return false
	}
	return true
}

func awsOperationsBinding(workspace awsoperations.Workspace, service awsoperations.ServiceKey, localResourceID string) (awsoperations.ResourceBinding, bool) {
	for _, binding := range workspace.Bindings {
		if binding.Service == service && binding.LocalResourceID == localResourceID {
			return binding, true
		}
	}
	return awsoperations.ResourceBinding{}, false
}

type awsOperationsServiceResponse struct {
	Service      awsoperations.ServiceKey    `json:"service"`
	DisplayName  string                      `json:"display_name"`
	Target       string                      `json:"target"`
	Family       string                      `json:"family"`
	PanelKind    string                      `json:"panel_kind"`
	Status       awsoperations.ServiceStatus `json:"status"`
	Capabilities []awsoperations.Capability  `json:"capabilities"`
	Reason       string                      `json:"reason,omitempty"`
}
type awsOperationsServiceDetailResponse struct {
	Service      awsoperations.ServiceKey    `json:"service"`
	DisplayName  string                      `json:"display_name"`
	Target       string                      `json:"target"`
	Family       string                      `json:"family"`
	PanelKind    string                      `json:"panel_kind"`
	Status       awsoperations.ServiceStatus `json:"status"`
	Capabilities []awsoperations.Capability  `json:"capabilities"`
	Reason       string                      `json:"reason,omitempty"`
}
type awsOperationsWorkspaceResponse struct {
	ID                 string                                                  `json:"id"`
	DiscoveryID        string                                                  `json:"discovery_id"`
	Name               string                                                  `json:"name"`
	Provider           string                                                  `json:"provider"`
	CutoverCompletedAt time.Time                                               `json:"cutover_completed_at"`
	Services           map[awsoperations.ServiceKey]awsoperations.ServiceState `json:"services"`
	Bindings           []awsoperations.ResourceBinding                         `json:"bindings"`
}

func workspaceResponse(workspace *awsoperations.Workspace) awsOperationsWorkspaceResponse {
	services := make(map[awsoperations.ServiceKey]awsoperations.ServiceState, len(workspace.Services))
	for key, state := range workspace.Services {
		state.Capabilities = append([]awsoperations.Capability(nil), state.Capabilities...)
		services[key] = state
	}
	bindings := workspace.Bindings
	if bindings == nil {
		bindings = []awsoperations.ResourceBinding{}
	}
	return awsOperationsWorkspaceResponse{ID: workspace.ID, DiscoveryID: workspace.DiscoveryID, Name: workspace.Name, Provider: workspace.Provider, CutoverCompletedAt: workspace.CutoverCompletedAt, Services: services, Bindings: bindings}
}
