package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	appwizard "github.com/homeport/homeport/internal/app/wizard"
	domainwizard "github.com/homeport/homeport/internal/domain/wizard"
	"github.com/homeport/homeport/internal/pkg/httputil"
)

type WizardHandler struct {
	service *appwizard.Service
}

type wizardSessionPatch struct {
	CurrentStep       domainwizard.Step   `json:"current_step,omitempty"`
	CompletedSteps    []domainwizard.Step `json:"completed_steps,omitempty"`
	SourceProvider    string              `json:"source_provider,omitempty"`
	SelectedResources []string            `json:"selected_resources,omitempty"`
	BundleID          string              `json:"bundle_id,omitempty"`
	SecretsResolved   *bool               `json:"secrets_resolved,omitempty"`
	DeploymentID      string              `json:"deployment_id,omitempty"`
	SyncPlanID        string              `json:"sync_plan_id,omitempty"`
	CutoverID         string              `json:"cutover_id,omitempty"`
	Metadata          map[string]string   `json:"metadata,omitempty"`
}

func (p wizardSessionPatch) appPatch() appwizard.SessionPatch {
	return appwizard.SessionPatch{
		CurrentStep:       p.CurrentStep,
		CompletedSteps:    p.CompletedSteps,
		SourceProvider:    p.SourceProvider,
		SelectedResources: p.SelectedResources,
		BundleID:          p.BundleID,
		SecretsResolved:   p.SecretsResolved,
		DeploymentID:      p.DeploymentID,
		SyncPlanID:        p.SyncPlanID,
		CutoverID:         p.CutoverID,
		Metadata:          p.Metadata,
	}
}

func NewWizardHandler(service *appwizard.Service) *WizardHandler {
	if service == nil {
		service = appwizard.NewService(".")
	}
	return &WizardHandler{service: service}
}

func (h *WizardHandler) RegisterRoutes(r chi.Router) {
	r.Route("/wizard/sessions", func(r chi.Router) {
		r.Post("/", h.CreateSession)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.GetSession)
			r.Patch("/", h.UpdateSession)
		})
	})
}

func (h *WizardHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	session, err := h.service.Create()
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	render.JSON(w, r, session)
}

func (h *WizardHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	session, err := h.service.Get(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	render.JSON(w, r, session)
}

func (h *WizardHandler) UpdateSession(w http.ResponseWriter, r *http.Request) {
	var patch wizardSessionPatch
	if !httputil.DecodeJSON(w, r, &patch) {
		return
	}
	session, err := h.service.UpdatePatch(chi.URLParam(r, "id"), patch.appPatch())
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	render.JSON(w, r, session)
}
