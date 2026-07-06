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
	var patch domainwizard.Session
	if !httputil.DecodeJSON(w, r, &patch) {
		return
	}
	session, err := h.service.Update(chi.URLParam(r, "id"), patch)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, err.Error())
		return
	}
	render.JSON(w, r, session)
}
