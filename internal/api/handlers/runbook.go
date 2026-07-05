package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	apprunbook "github.com/homeport/homeport/internal/app/runbook"
)

type RunbookHandler struct {
	service *apprunbook.Service
}

func NewRunbookHandler(service *apprunbook.Service) *RunbookHandler {
	if service == nil {
		service = apprunbook.NewService(".")
	}
	return &RunbookHandler{service: service}
}

func (h *RunbookHandler) RegisterRoutes(r chi.Router) {
	r.Route("/runbooks/{id}", func(r chi.Router) {
		r.Get("/", h.GetRunbook)
		r.Post("/run", h.RunRunbook)
		r.Post("/rollback", h.RollbackRunbook)
		r.Post("/steps/{stepID}/run", h.RunStep)
	})
}

func (h *RunbookHandler) GetRunbook(w http.ResponseWriter, r *http.Request) {
	book, err := h.service.Get(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, book)
}

func (h *RunbookHandler) RunStep(w http.ResponseWriter, r *http.Request) {
	book, err := h.service.Get(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	step := book.FirstUnpassedStep()
	if step == nil {
		respondJSON(w, r, http.StatusOK, map[string]string{"status": "complete"})
		return
	}
	if step.ID != chi.URLParam(r, "stepID") {
		respondError(w, r, http.StatusConflict, "step is not the next runnable step")
		return
	}
	result, err := h.service.RunNext(r.Context(), book.ID)
	if err != nil {
		respondError(w, r, http.StatusConflict, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, result)
}

func (h *RunbookHandler) RunRunbook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.service.RunAll(r.Context(), id); err != nil {
		respondError(w, r, http.StatusConflict, err.Error())
		return
	}
	book, err := h.service.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, book)
}

func (h *RunbookHandler) RollbackRunbook(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.service.Rollback(r.Context(), id); err != nil {
		respondError(w, r, http.StatusConflict, err.Error())
		return
	}
	book, err := h.service.Get(id)
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	respondJSON(w, r, http.StatusOK, book)
}
