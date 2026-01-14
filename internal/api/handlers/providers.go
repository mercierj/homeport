package handlers

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/app/providers"
	"github.com/homeport/homeport/internal/domain/provider"
	"github.com/homeport/homeport/internal/pkg/httputil"
)

// ProvidersHandler handles provider-related HTTP requests.
type ProvidersHandler struct {
	service *providers.Service
}

// NewProvidersHandler creates a new providers handler.
func NewProvidersHandler(svc *providers.Service) *ProvidersHandler {
	return &ProvidersHandler{
		service: svc,
	}
}

// RegisterRoutes registers provider routes on the router.
func (h *ProvidersHandler) RegisterRoutes(r chi.Router) {
	r.Route("/providers", func(r chi.Router) {
		r.Get("/", h.HandleListProviders)
		r.Post("/compare", h.HandleCompareProviders)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.HandleGetProvider)
			r.Get("/regions", h.HandleGetProviderRegions)
			r.Get("/instances", h.HandleGetProviderInstances)
		})
	})
}

// ListProvidersResponse represents the response for listing all providers.
type ListProvidersResponse struct {
	Providers []*provider.ProviderInfo `json:"providers"`
}

// HandleListProviders handles GET /api/v1/providers
// It returns a list of all providers with their info.
func (h *ProvidersHandler) HandleListProviders(w http.ResponseWriter, r *http.Request) {
	providersList, err := h.service.ListProviders(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, ListProvidersResponse{
		Providers: providersList,
	})
}

// HandleGetProvider handles GET /api/v1/providers/{id}
// It returns a single provider's details.
func (h *ProvidersHandler) HandleGetProvider(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.BadRequest(w, r, "provider ID is required")
		return
	}

	providerInfo, err := h.service.GetProvider(r.Context(), provider.Provider(id))
	if err != nil {
		if errors.Is(err, providers.ErrProviderNotFound) {
			httputil.NotFound(w, r, "provider not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, providerInfo)
}

// ProviderRegionsResponse represents the response for getting provider regions.
type ProviderRegionsResponse struct {
	Regions []provider.Region `json:"regions"`
}

// HandleGetProviderRegions handles GET /api/v1/providers/{id}/regions
// It returns the regions for a specific provider.
func (h *ProvidersHandler) HandleGetProviderRegions(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.BadRequest(w, r, "provider ID is required")
		return
	}

	regions, err := h.service.GetProviderRegions(r.Context(), provider.Provider(id))
	if err != nil {
		if errors.Is(err, providers.ErrProviderNotFound) {
			httputil.NotFound(w, r, "provider not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, ProviderRegionsResponse{
		Regions: regions,
	})
}

// ProviderInstancesResponse represents the response for getting provider instances.
type ProviderInstancesResponse struct {
	Instances []provider.InstancePricing `json:"instances"`
}

// HandleGetProviderInstances handles GET /api/v1/providers/{id}/instances
// It returns instance types and pricing for a specific provider.
func (h *ProvidersHandler) HandleGetProviderInstances(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		httputil.BadRequest(w, r, "provider ID is required")
		return
	}

	instances, err := h.service.GetProviderInstances(r.Context(), provider.Provider(id))
	if err != nil {
		if errors.Is(err, providers.ErrProviderNotFound) {
			httputil.NotFound(w, r, "provider not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, ProviderInstancesResponse{
		Instances: instances,
	})
}

// CompareRequest represents the request body for provider cost comparison.
type CompareRequest struct {
	// MappingResults contains the resource mappings from a previous analysis
	MappingResults interface{} `json:"mapping_results"`

	// Providers is the list of provider IDs to compare
	Providers []string `json:"providers"`

	// HALevel is the high-availability level (e.g., "none", "basic", "full")
	HALevel string `json:"ha_level"`

	// EstimatedStorageGB is the estimated storage requirement in GB
	EstimatedStorageGB int `json:"estimated_storage_gb"`

	// EstimatedEgressGB is the estimated monthly egress in GB
	EstimatedEgressGB int `json:"estimated_egress_gb"`
}

// ProviderCostEstimate represents the cost estimate for a single provider.
type ProviderCostEstimate struct {
	Provider          string                  `json:"provider"`
	DisplayName       string                  `json:"display_name"`
	IsEU              bool                    `json:"is_eu"`
	Breakdown         *provider.CostBreakdown `json:"breakdown"`
	TotalMonthly      float64                 `json:"total_monthly"`
	Currency          string                  `json:"currency"`
	Savings           float64                 `json:"savings,omitempty"`
	SavingsPercentage float64                 `json:"savings_percentage,omitempty"`
}

// CompareResponse represents the response for provider cost comparison.
type CompareResponse struct {
	Estimates   []ProviderCostEstimate `json:"estimates"`
	BestValue   string                 `json:"best_value"`
	CurrentCost float64                `json:"current_cost,omitempty"`
	Currency    string                 `json:"currency"`
}

// HandleCompareProviders handles POST /api/v1/providers/compare
// It compares costs across multiple providers for the given infrastructure.
func (h *ProvidersHandler) HandleCompareProviders(w http.ResponseWriter, r *http.Request) {
	var req CompareRequest
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	// Validate request
	if len(req.Providers) == 0 {
		httputil.BadRequest(w, r, "at least one provider is required")
		return
	}

	// Convert string provider IDs to provider.Provider types
	providerIDs := make([]provider.Provider, len(req.Providers))
	for i, p := range req.Providers {
		providerIDs[i] = provider.Provider(p)
	}

	// Build service request
	svcReq := providers.CompareRequest{
		MappingResults:     req.MappingResults,
		Providers:          providerIDs,
		HALevel:            req.HALevel,
		EstimatedStorageGB: req.EstimatedStorageGB,
		EstimatedEgressGB:  req.EstimatedEgressGB,
	}

	result, err := h.service.CompareProviders(r.Context(), svcReq)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	// Convert service response to API response
	estimates := make([]ProviderCostEstimate, len(result.Estimates))
	for i, est := range result.Estimates {
		estimates[i] = ProviderCostEstimate{
			Provider:          string(est.Provider),
			DisplayName:       est.DisplayName,
			IsEU:              est.IsEU,
			Breakdown:         est.Breakdown,
			TotalMonthly:      est.TotalMonthly,
			Currency:          est.Currency,
			Savings:           est.Savings,
			SavingsPercentage: est.SavingsPercentage,
		}
	}

	render.JSON(w, r, CompareResponse{
		Estimates:   estimates,
		BestValue:   string(result.BestValue),
		CurrentCost: result.CurrentCost,
		Currency:    result.Currency,
	})
}
