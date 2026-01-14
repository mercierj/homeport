package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"

	"github.com/homeport/homeport/internal/app/cache"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

const (
	MaxKeyLength     = 512
	MaxPatternLength = 256
	MaxValueLength   = 1 << 20 // 1MB
	DefaultKeyLimit  = 100
	MaxKeyLimit      = 1000
	MaxKeyTTL        = 31536000 // 1 year
)

var (
	keyPatternRegex = regexp.MustCompile(`^[a-zA-Z0-9_:\-.*?\[\]]+$`)
	cacheKeyRegex   = regexp.MustCompile(`^[a-zA-Z0-9_:\-.]+$`)
)

func validateKeyPattern(pattern string) error {
	if pattern == "" {
		return nil
	}
	if len(pattern) > MaxPatternLength {
		return fmt.Errorf("pattern must be at most %d characters", MaxPatternLength)
	}
	if !keyPatternRegex.MatchString(pattern) {
		return fmt.Errorf("pattern contains invalid characters")
	}
	return nil
}

func validateCacheKey(key string) error {
	if key == "" {
		return fmt.Errorf("key is required")
	}
	if len(key) > MaxKeyLength {
		return fmt.Errorf("key must be at most %d characters", MaxKeyLength)
	}
	if !cacheKeyRegex.MatchString(key) {
		return fmt.Errorf("key must contain only letters, digits, hyphens, underscores, colons, and periods")
	}
	return nil
}

// CacheHandler handles cache-related HTTP requests.
type CacheHandler struct {
	service *cache.Service
}

// NewCacheHandler creates a new cache handler.
func NewCacheHandler(cfg cache.Config) (*CacheHandler, error) {
	svc, err := cache.NewService(cfg)
	if err != nil {
		return nil, err
	}
	return &CacheHandler{service: svc}, nil
}

// Close closes the handler's service connection.
func (h *CacheHandler) Close() error {
	if h.service != nil {
		return h.service.Close()
	}
	return nil
}

// HandleListKeys handles GET /stacks/{stackID}/cache/keys
func (h *CacheHandler) HandleListKeys(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	pattern := r.URL.Query().Get("pattern")
	if err := validateKeyPattern(pattern); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	limit := int64(DefaultKeyLimit)
	if l := r.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.ParseInt(l, 10, 64)
		if err != nil {
			httputil.BadRequest(w, r, "invalid limit parameter: must be a number")
			return
		}
		if parsed < 1 || parsed > MaxKeyLimit {
			httputil.BadRequest(w, r, fmt.Sprintf("limit must be between 1 and %d", MaxKeyLimit))
			return
		}
		limit = parsed
	}

	var cursor uint64
	if c := r.URL.Query().Get("cursor"); c != "" {
		parsed, err := strconv.ParseUint(c, 10, 64)
		if err != nil {
			httputil.BadRequest(w, r, "invalid cursor parameter: must be a number")
			return
		}
		cursor = parsed
	}

	result, err := h.service.ListKeys(r.Context(), stackID, pattern, limit, cursor)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, result)
}

// HandleGetKey handles GET /stacks/{stackID}/cache/keys/{key}
func (h *CacheHandler) HandleGetKey(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	key := chi.URLParam(r, "key")
	decodedKey, err := url.PathUnescape(key)
	if err != nil {
		httputil.BadRequest(w, r, "invalid key encoding")
		return
	}

	if err := validateCacheKey(decodedKey); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	cacheKey, err := h.service.GetKey(r.Context(), stackID, decodedKey)
	if err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	render.JSON(w, r, cacheKey)
}

// HandleSetKey handles PUT /stacks/{stackID}/cache/keys/{key}
func (h *CacheHandler) HandleSetKey(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	key := chi.URLParam(r, "key")
	decodedKey, err := url.PathUnescape(key)
	if err != nil {
		httputil.BadRequest(w, r, "invalid key encoding")
		return
	}

	if err := validateCacheKey(decodedKey); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	var req struct {
		Value string `json:"value"`
		TTL   int64  `json:"ttl"`
	}
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if len(req.Value) > MaxValueLength {
		httputil.BadRequest(w, r, fmt.Sprintf("value must be at most %d bytes", MaxValueLength))
		return
	}

	if req.TTL < 0 || req.TTL > MaxKeyTTL {
		httputil.BadRequest(w, r, fmt.Sprintf("TTL must be between 0 and %d seconds", MaxKeyTTL))
		return
	}

	if err := h.service.SetKey(r.Context(), stackID, decodedKey, req.Value, req.TTL); err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{"status": "set", "key": decodedKey})
}

// HandleDeleteKey handles DELETE /stacks/{stackID}/cache/keys/{key}
func (h *CacheHandler) HandleDeleteKey(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	key := chi.URLParam(r, "key")
	decodedKey, err := url.PathUnescape(key)
	if err != nil {
		httputil.BadRequest(w, r, "invalid key encoding")
		return
	}

	if err := validateCacheKey(decodedKey); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := h.service.DeleteKey(r.Context(), stackID, decodedKey); err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	render.JSON(w, r, map[string]string{"status": "deleted", "key": decodedKey})
}

// HandleBulkDelete handles DELETE /stacks/{stackID}/cache/keys with pattern
func (h *CacheHandler) HandleBulkDelete(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	pattern := r.URL.Query().Get("pattern")
	if pattern == "" {
		httputil.BadRequest(w, r, "pattern query parameter is required for bulk delete")
		return
	}

	if err := validateKeyPattern(pattern); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	deleted, err := h.service.DeleteKeys(r.Context(), stackID, pattern)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"status":  "deleted",
		"pattern": pattern,
		"deleted": deleted,
	})
}

// HandleGetStats handles GET /stacks/{stackID}/cache/stats
func (h *CacheHandler) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	stats, err := h.service.GetStats(r.Context(), stackID)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, stats)
}

// HandleGetKeyInfo handles GET /stacks/{stackID}/cache/keys/{key}/info
func (h *CacheHandler) HandleGetKeyInfo(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackID")
	if err := validateStackID(stackID); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	key := chi.URLParam(r, "key")
	decodedKey, err := url.PathUnescape(key)
	if err != nil {
		httputil.BadRequest(w, r, "invalid key encoding")
		return
	}

	if err := validateCacheKey(decodedKey); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	info, err := h.service.GetKeyInfo(r.Context(), stackID, decodedKey)
	if err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}

	render.JSON(w, r, info)
}
