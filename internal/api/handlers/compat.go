package handlers

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	appcompat "github.com/homeport/homeport/internal/app/compat"
)

type CompatHandler struct {
	registry *appcompat.Registry
}

func NewCompatHandler(registry *appcompat.Registry) *CompatHandler {
	return &CompatHandler{registry: registry}
}

func (h *CompatHandler) RegisterRoutes(r chi.Router) {
	r.Get("/compat", h.HandleList)
	r.Handle("/compat/{provider}/{service}", h)
	r.Handle("/compat/{provider}/{service}/*", h)
}

func (h *CompatHandler) HandleList(w http.ResponseWriter, r *http.Request) {
	type adapterInfo struct {
		Provider          string            `json:"provider"`
		Service           string            `json:"service"`
		Routes            []string          `json:"routes"`
		TargetEnv         map[string]string `json:"target_env"`
		ConformanceChecks []string          `json:"conformance_checks"`
	}

	adapters := h.registry.List()
	out := make([]adapterInfo, 0, len(adapters))
	for _, adapter := range adapters {
		out = append(out, adapterInfo{
			Provider:          adapter.Provider(),
			Service:           adapter.Service(),
			Routes:            adapter.Routes(),
			TargetEnv:         adapter.TargetEnv(),
			ConformanceChecks: adapter.ConformanceChecks(),
		})
	}
	render.JSON(w, r, out)
}

func (h *CompatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	adapter, err := h.registry.Get(chi.URLParam(r, "provider"), chi.URLParam(r, "service"))
	if err != nil {
		render.Status(r, http.StatusNotFound)
		render.JSON(w, r, map[string]string{"message": err.Error()})
		return
	}
	adapter.ServeHTTP(w, trimCompatAdapterPrefix(r, chi.URLParam(r, "provider"), chi.URLParam(r, "service")))
}

func trimCompatAdapterPrefix(r *http.Request, provider, service string) *http.Request {
	prefix := "/compat/" + provider + "/" + service
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return r
	}
	clone := r.Clone(r.Context())
	urlCopy := *r.URL
	urlCopy.Path = strings.TrimPrefix(r.URL.Path, prefix)
	if urlCopy.Path == "" {
		urlCopy.Path = "/"
	}
	urlCopy.RawPath = ""
	clone.URL = &urlCopy
	return clone
}
