package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/homeport/homeport/internal/app/awsoperations"
	appcutover "github.com/homeport/homeport/internal/app/cutover"
	"github.com/homeport/homeport/internal/app/migrate"
	domaincutover "github.com/homeport/homeport/internal/domain/cutover"
)

// CutoverHandler handles cutover-related HTTP requests.
type CutoverHandler struct {
	service         *appcutover.Service
	awsOperations   *awsoperations.Service
	activationStore *awsActivationStore
	eventMu         sync.Mutex
	events          map[string][]appcutover.CutoverEvent
	subscribers     map[string]map[chan appcutover.CutoverEvent]struct{}
}

// NewCutoverHandler creates a new cutover handler.
func NewCutoverHandler() *CutoverHandler {
	handler := &CutoverHandler{
		service:     appcutover.NewService(),
		events:      make(map[string][]appcutover.CutoverEvent),
		subscribers: make(map[string]map[chan appcutover.CutoverEvent]struct{}),
	}
	handler.activationStore, _ = newAWSActivationStore("")
	if discoveries, err := migrate.NewStateStore(""); err == nil {
		if workspaces, err := awsoperations.NewStore(""); err == nil {
			handler.awsOperations = awsoperations.NewService(discoveries, workspaces)
		}
	}
	return handler
}

// NewCutoverHandlerWithAWSOperations creates a handler with an explicit operations service.
// It is useful to applications that already own the discovery and workspace stores.
func NewCutoverHandlerWithAWSOperations(operations *awsoperations.Service) *CutoverHandler {
	handler := &CutoverHandler{
		service:       appcutover.NewService(),
		awsOperations: operations,
		events:        make(map[string][]appcutover.CutoverEvent),
		subscribers:   make(map[string]map[chan appcutover.CutoverEvent]struct{}),
	}
	handler.activationStore, _ = newAWSActivationStore("")
	return handler
}

// RegisterAWSLocalBindings accepts identities emitted by trusted deployment or
// cutover code. It is intentionally not registered as an HTTP endpoint.
func (h *CutoverHandler) RegisterAWSLocalBindings(discoveryID, targetStackID string, bindings []awsoperations.LocalResourceBinding) error {
	if h.awsOperations == nil {
		return fmt.Errorf("AWS operations service is not configured")
	}
	if err := h.awsOperations.ValidateActivation(awsoperations.ActivationInput{
		DiscoveryID: discoveryID, TargetStackID: targetStackID,
		Activated: []awsoperations.ServiceKey{}, LocalBindings: bindings,
	}); err != nil {
		return fmt.Errorf("validate trusted local bindings: %w", err)
	}
	copyBindings := append([]awsoperations.LocalResourceBinding(nil), bindings...)
	if h.activationStore == nil {
		return fmt.Errorf("AWS activation persistence is not configured")
	}
	if err := h.activationStore.putBindings(trustedBindingKey(discoveryID, targetStackID), copyBindings); err != nil {
		return fmt.Errorf("persist trusted local bindings: %w", err)
	}
	return nil
}

func trustedBindingKey(discoveryID, targetStackID string) string {
	return discoveryID + "\x00" + targetStackID
}

// RegisterRoutes registers cutover routes.
func (h *CutoverHandler) RegisterRoutes(r chi.Router) {
	r.Route("/cutover", func(r chi.Router) {
		r.Post("/preview", h.PreviewCutover)
		r.Post("/validate", h.ValidatePlan)
		r.Post("/start", h.StartCutover)
		r.Route("/{cutoverId}", func(r chi.Router) {
			r.Get("/status", h.GetStatus)
			r.Get("/stream", h.StreamProgress)
			r.Post("/cancel", h.CancelCutover)
			r.Post("/rollback", h.RollbackCutover)
		})
	})
}

func (h *CutoverHandler) PreviewCutover(w http.ResponseWriter, r *http.Request) {
	var input appcutover.PreviewInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	respondJSON(w, r, http.StatusOK, appcutover.BuildPreview(input))
}

// HealthCheckRequest represents a health check in the request.
type HealthCheckRequest struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"` // "http", "tcp"
	Endpoint   string `json:"endpoint"`
	Timeout    int    `json:"timeout_seconds,omitempty"`
	ExpectCode int    `json:"expect_code,omitempty"`
	ExpectBody string `json:"expect_body,omitempty"`
}

// DNSChangeRequest represents a DNS change in the request.
type DNSChangeRequest struct {
	ID         string `json:"id"`
	Domain     string `json:"domain"`
	RecordType string `json:"record_type"` // "A", "CNAME", "AAAA"
	OldValue   string `json:"old_value"`
	NewValue   string `json:"new_value"`
	TTL        int    `json:"ttl,omitempty"`
}

// CreateCutoverRequest represents a request to create a cutover plan.
type CreateCutoverRequest struct {
	BundleID          string                     `json:"bundle_id"`
	Name              string                     `json:"name,omitempty"`
	PreChecks         []HealthCheckRequest       `json:"pre_checks"`
	DNSChanges        []DNSChangeRequest         `json:"dns_changes"`
	PostChecks        []HealthCheckRequest       `json:"post_checks"`
	DryRun            bool                       `json:"dry_run"`
	DNSProvider       string                     `json:"dns_provider,omitempty"`
	DiscoveryID       string                     `json:"discovery_id,omitempty"`
	TargetStackID     string                     `json:"target_stack_id,omitempty"`
	ActivatedServices []awsoperations.ServiceKey `json:"activated_services,omitempty"`
}

// CreateCutoverResponse represents the response.
type CreateCutoverResponse struct {
	CutoverID string `json:"cutover_id"`
}

// ValidatePlan validates a cutover plan without executing it.
func (h *CutoverHandler) ValidatePlan(w http.ResponseWriter, r *http.Request) {
	var req CreateCutoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate the plan
	var errors []string
	var warnings []string

	if len(req.DNSChanges) == 0 {
		warnings = append(warnings, "No DNS changes specified")
	}

	if len(req.PreChecks) == 0 {
		warnings = append(warnings, "No pre-cutover health checks specified")
	}

	if len(req.PostChecks) == 0 {
		warnings = append(warnings, "No post-cutover health checks specified - rollback won't be automated")
	}

	for _, check := range req.PreChecks {
		if check.Endpoint == "" {
			errors = append(errors, fmt.Sprintf("Pre-check '%s' missing endpoint", check.Name))
		}
	}

	for _, change := range req.DNSChanges {
		if change.Domain == "" {
			errors = append(errors, "DNS change missing domain")
		}
		if change.NewValue == "" {
			errors = append(errors, fmt.Sprintf("DNS change for '%s' missing new value", change.Domain))
		}
	}

	respondJSON(w, r, http.StatusOK, map[string]interface{}{
		"valid":    len(errors) == 0,
		"errors":   errors,
		"warnings": warnings,
	})
}

// StartCutover starts a new cutover operation.
func (h *CutoverHandler) StartCutover(w http.ResponseWriter, r *http.Request) {
	var req CreateCutoverRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := validateAWSActivation(req); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	var localBindings []awsoperations.LocalResourceBinding
	if req.DiscoveryID != "" {
		if h.activationStore == nil {
			respondError(w, r, http.StatusInternalServerError, "AWS activation persistence is not configured")
			return
		}
		localBindings = h.activationStore.getBindings(trustedBindingKey(req.DiscoveryID, req.TargetStackID))
		if h.awsOperations == nil {
			respondError(w, r, http.StatusBadRequest, "AWS operations service is not configured")
			return
		}
		if err := h.awsOperations.ValidateActivation(awsoperations.ActivationInput{
			DiscoveryID:   req.DiscoveryID,
			TargetStackID: req.TargetStackID,
			Activated:     req.ActivatedServices,
			LocalBindings: localBindings,
		}); err != nil {
			respondError(w, r, http.StatusBadRequest, err.Error())
			return
		}
	}

	// Convert request to domain types
	preChecks := make([]*domaincutover.HealthCheck, len(req.PreChecks))
	for i, check := range req.PreChecks {
		preChecks[i] = &domaincutover.HealthCheck{
			ID:       check.ID,
			Name:     check.Name,
			Type:     domaincutover.HealthCheckType(check.Type),
			Endpoint: check.Endpoint,
			Timeout:  time.Duration(check.Timeout) * time.Second,
		}
		if preChecks[i].Timeout == 0 {
			preChecks[i].Timeout = 30 * time.Second
		}
	}

	dnsChanges := make([]*domaincutover.DNSChange, len(req.DNSChanges))
	for i, change := range req.DNSChanges {
		dnsChanges[i] = &domaincutover.DNSChange{
			ID:         change.ID,
			Domain:     change.Domain,
			RecordType: change.RecordType,
			OldValue:   change.OldValue,
			NewValue:   change.NewValue,
			TTL:        change.TTL,
		}
		if dnsChanges[i].TTL == 0 {
			dnsChanges[i].TTL = 300
		}
	}

	postChecks := make([]*domaincutover.HealthCheck, len(req.PostChecks))
	for i, check := range req.PostChecks {
		postChecks[i] = &domaincutover.HealthCheck{
			ID:       check.ID,
			Name:     check.Name,
			Type:     domaincutover.HealthCheckType(check.Type),
			Endpoint: check.Endpoint,
			Timeout:  time.Duration(check.Timeout) * time.Second,
		}
		if postChecks[i].Timeout == 0 {
			postChecks[i].Timeout = 30 * time.Second
		}
	}

	// Create the plan
	plan, err := h.service.CreatePlan(&appcutover.CreatePlanRequest{
		BundleID:    req.BundleID,
		Name:        req.Name,
		PreChecks:   preChecks,
		DNSChanges:  dnsChanges,
		PostChecks:  postChecks,
		DryRun:      req.DryRun,
		DNSProvider: req.DNSProvider,
	})

	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	if req.DiscoveryID != "" {
		if h.activationStore == nil || h.activationStore.putPlan(plan.ID, awsoperations.ActivationInput{DiscoveryID: req.DiscoveryID, TargetStackID: req.TargetStackID, Activated: append([]awsoperations.ServiceKey(nil), req.ActivatedServices...), LocalBindings: localBindings}) != nil {
			respondError(w, r, http.StatusInternalServerError, "persist AWS activation")
			return
		}
	}
	if err := h.service.Execute(context.Background(), plan.ID, h.recordCutoverEvent); err != nil {
		respondError(w, r, http.StatusInternalServerError, fmt.Sprintf("start cutover execution: %v", err))
		return
	}

	respondJSON(w, r, http.StatusOK, CreateCutoverResponse{
		CutoverID: plan.ID,
	})
}

// CutoverStatusResponse represents cutover status.
type CutoverStatusResponse struct {
	CutoverID string              `json:"cutover_id"`
	Status    string              `json:"status"`
	Progress  int                 `json:"progress"`
	Steps     []CutoverStepStatus `json:"steps"`
	StartedAt *time.Time          `json:"started_at,omitempty"`
	Logs      []string            `json:"logs"`
	Error     string              `json:"error,omitempty"`
}

// CutoverStepStatus represents the status of a cutover step.
type CutoverStepStatus struct {
	Order       int    `json:"order"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

// GetStatus returns the status of a cutover operation.
func (h *CutoverHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	cutoverID := chi.URLParam(r, "cutoverId")

	exec, err := h.service.GetPlan(cutoverID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}

	// Calculate progress
	totalSteps := len(exec.Plan.Steps)
	completedSteps := 0
	for _, step := range exec.Plan.Steps {
		if step.Status == domaincutover.CutoverStepStatusCompleted {
			completedSteps++
		}
	}

	progress := 0
	if totalSteps > 0 {
		progress = (completedSteps * 100) / totalSteps
	}

	// Convert steps
	steps := make([]CutoverStepStatus, len(exec.Plan.Steps))
	for i, step := range exec.Plan.Steps {
		steps[i] = CutoverStepStatus{
			Order:       step.Order,
			Type:        string(step.Type),
			Description: step.Description,
			Status:      string(step.Status),
			Error:       step.Error,
		}
	}

	respondJSON(w, r, http.StatusOK, CutoverStatusResponse{
		CutoverID: cutoverID,
		Status:    string(exec.Plan.Status),
		Progress:  progress,
		Steps:     steps,
		StartedAt: exec.StartedAt,
		Logs:      exec.Logs,
		Error:     exec.Plan.Error,
	})
}

// StreamProgress streams cutover progress via SSE.
func (h *CutoverHandler) StreamProgress(w http.ResponseWriter, r *http.Request) {
	cutoverID := chi.URLParam(r, "cutoverId")

	exec, err := h.service.GetPlan(cutoverID)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, r, http.StatusInternalServerError, "Streaming not supported")
		return
	}
	// Send initial status
	sendSSEEvent(w, flusher, "status", map[string]interface{}{
		"cutover_id": cutoverID,
		"status":     exec.Plan.Status,
	})

	history, eventCh, unsubscribe := h.subscribeCutoverEvents(cutoverID)
	defer unsubscribe()
	for _, event := range history {
		if sendCutoverEvent(w, flusher, event) {
			return
		}
	}

	// Stream live events. Execution is started by StartCutover, so a browser
	// subscription cannot determine whether activation happens.
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}

			if sendCutoverEvent(w, flusher, event) {
				return
			}
		case <-time.After(30 * time.Second):
			// Send keepalive
			_, _ = fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

func (h *CutoverHandler) recordCutoverEvent(event appcutover.CutoverEvent) {
	if event.Type == "complete" && event.Status == string(domaincutover.CutoverStatusCompleted) {
		if err := h.activateAWSWorkspace(event.PlanID); err != nil {
			event = appcutover.CutoverEvent{Type: "activation_error", PlanID: event.PlanID, Error: fmt.Sprintf("activate AWS operations workspace: %v", err)}
		}
	}
	h.eventMu.Lock()
	h.events[event.PlanID] = append(h.events[event.PlanID], event)
	// The cutover service stops after a failed pre-check without a terminal
	// callback. Record one here so observers do not wait for a stream timeout.
	if event.Type == "step_failed" && event.StepType == "pre_check" {
		h.events[event.PlanID] = append(h.events[event.PlanID], appcutover.CutoverEvent{Type: "error", PlanID: event.PlanID, Status: "failed", Error: event.Error})
	}
	for subscriber := range h.subscribers[event.PlanID] {
		select {
		case subscriber <- event:
		default:
		}
		if event.Type == "step_failed" && event.StepType == "pre_check" {
			select {
			case subscriber <- appcutover.CutoverEvent{Type: "error", PlanID: event.PlanID, Status: "failed", Error: event.Error}:
			default:
			}
		}
	}
	h.eventMu.Unlock()
}

func (h *CutoverHandler) subscribeCutoverEvents(cutoverID string) ([]appcutover.CutoverEvent, <-chan appcutover.CutoverEvent, func()) {
	channel := make(chan appcutover.CutoverEvent, 100)
	h.eventMu.Lock()
	history := append([]appcutover.CutoverEvent(nil), h.events[cutoverID]...)
	if h.subscribers[cutoverID] == nil {
		h.subscribers[cutoverID] = make(map[chan appcutover.CutoverEvent]struct{})
	}
	h.subscribers[cutoverID][channel] = struct{}{}
	h.eventMu.Unlock()
	return history, channel, func() {
		h.eventMu.Lock()
		delete(h.subscribers[cutoverID], channel)
		h.eventMu.Unlock()
	}
}

// sendCutoverEvent returns true for a terminal event.
func sendCutoverEvent(w http.ResponseWriter, flusher http.Flusher, event appcutover.CutoverEvent) bool {
	sendSSEEvent(w, flusher, event.Type, event)
	return event.Type == "complete" || event.Type == "activation_error" || event.Type == "error"
}

func validateAWSActivation(req CreateCutoverRequest) error {
	if req.DiscoveryID == "" && len(req.ActivatedServices) == 0 {
		return nil
	}
	if req.DryRun {
		return fmt.Errorf("AWS operations activation is not available for dry-run cutovers")
	}
	if req.DiscoveryID == "" {
		return fmt.Errorf("discovery_id is required when activated_services are provided")
	}
	if len(req.ActivatedServices) == 0 {
		return fmt.Errorf("activated_services is required when discovery_id is provided")
	}
	if req.TargetStackID == "" {
		return fmt.Errorf("target_stack_id is required when discovery_id is provided")
	}
	if err := validateStackID(req.TargetStackID); err != nil {
		return fmt.Errorf("invalid target_stack_id: %w", err)
	}
	seen := make(map[awsoperations.ServiceKey]bool, len(req.ActivatedServices))
	for _, service := range req.ActivatedServices {
		if _, registered := awsoperations.ServiceMetadataFor(service); !registered {
			return fmt.Errorf("unsupported AWS service: %s", service)
		}
		if seen[service] {
			return fmt.Errorf("duplicate activated AWS service: %s", service)
		}
		seen[service] = true
	}
	return nil
}

func (h *CutoverHandler) activateAWSWorkspace(cutoverID string) error {
	var activation awsoperations.ActivationInput
	ok := false
	if h.activationStore != nil {
		if persisted, found := h.activationStore.getPlan(cutoverID); found {
			activation, ok = persisted, true
		}
	}
	if !ok {
		return nil
	}
	if h.awsOperations == nil {
		return fmt.Errorf("AWS operations service is not configured")
	}
	if _, err := h.awsOperations.Activate(activation); err != nil {
		return err
	}
	if h.activationStore != nil {
		if err := h.activationStore.deletePlan(cutoverID); err != nil {
			return fmt.Errorf("delete completed AWS activation: %w", err)
		}
	}
	return nil
}

// CancelCutover cancels a running cutover.
func (h *CutoverHandler) CancelCutover(w http.ResponseWriter, r *http.Request) {
	cutoverID := chi.URLParam(r, "cutoverId")

	if err := h.service.Cancel(cutoverID); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{
		"status": "cancelled",
	})
}

// RollbackCutover manually triggers a rollback.
func (h *CutoverHandler) RollbackCutover(w http.ResponseWriter, r *http.Request) {
	cutoverID := chi.URLParam(r, "cutoverId")

	if err := h.service.Rollback(context.Background(), cutoverID, nil); err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{
		"status": "rolling_back",
	})
}
