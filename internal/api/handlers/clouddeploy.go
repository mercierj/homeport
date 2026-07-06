package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/app/clouddeploy"
	"github.com/homeport/homeport/internal/app/migrate"
	"github.com/homeport/homeport/internal/pkg/httputil"
)

type CloudDeployHandler struct {
	cloudService   *clouddeploy.Service
	migrateService *migrate.Service
}

type CloudDeployRequest struct {
	Resources []migrate.ResourceInfo        `json:"resources"`
	Config    migrate.TerraformExportConfig `json:"config"`
	Apply     bool                          `json:"apply"`
}

func NewCloudDeployHandler(cloudService *clouddeploy.Service, migrateService *migrate.Service) *CloudDeployHandler {
	if cloudService == nil {
		cloudService = clouddeploy.NewService("")
	}
	if migrateService == nil {
		migrateService = migrate.NewService()
	}
	return &CloudDeployHandler{cloudService: cloudService, migrateService: migrateService}
}

func (h *CloudDeployHandler) RegisterRoutes(r chi.Router) {
	r.Post("/cloud-deploy/start", h.Start)
	r.Get("/cloud-deploy/{id}", h.Get)
}

func (h *CloudDeployHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req CloudDeployRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}
	if !validCloudDeployProvider(req.Config.Provider) {
		respondError(w, r, http.StatusBadRequest, "invalid provider")
		return
	}
	zipData, err := h.migrateService.GenerateTerraformZip(r.Context(), req.Resources, &req.Config)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	job, err := h.cloudService.Start(r.Context(), "", zipData, req.Apply)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	render.JSON(w, r, job)
}

func (h *CloudDeployHandler) Get(w http.ResponseWriter, r *http.Request) {
	job, err := h.cloudService.Get(chi.URLParam(r, "id"))
	if err != nil {
		respondError(w, r, http.StatusNotFound, err.Error())
		return
	}
	render.JSON(w, r, job)
}

func validCloudDeployProvider(provider string) bool {
	switch provider {
	case "hetzner", "scaleway", "ovh":
		return true
	default:
		return false
	}
}
