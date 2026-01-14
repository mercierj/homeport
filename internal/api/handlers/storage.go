package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/api/middleware"
	"github.com/homeport/homeport/internal/app/storage"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

var (
	// bucketNameRegex validates S3-compatible bucket names
	// Must be 3-63 characters, lowercase letters, numbers, hyphens, and periods
	// Cannot start or end with hyphen/period, no consecutive periods
	bucketNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-]{1,61}[a-z0-9]$`)
)

// validateBucketName checks if a bucket name is valid
func validateBucketName(name string) error {
	if name == "" {
		return fmt.Errorf("bucket name is required")
	}
	if len(name) < 3 || len(name) > 63 {
		return fmt.Errorf("bucket name must be between 3 and 63 characters")
	}
	if !bucketNameRegex.MatchString(name) {
		return fmt.Errorf("bucket name must contain only lowercase letters, numbers, hyphens, and periods")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("bucket name cannot contain consecutive periods")
	}
	return nil
}

// validateObjectKey checks if an object key is valid
func validateObjectKey(key string) error {
	if key == "" {
		return fmt.Errorf("object key is required")
	}
	if len(key) > 1024 {
		return fmt.Errorf("object key must be at most 1024 characters")
	}
	// Check for path traversal
	if strings.Contains(key, "..") {
		return fmt.Errorf("object key cannot contain '..'")
	}
	return nil
}

// validatePrefix checks if a prefix is valid
func validatePrefix(prefix string) error {
	if len(prefix) > 1024 {
		return fmt.Errorf("prefix must be at most 1024 characters")
	}
	if strings.Contains(prefix, "..") {
		return fmt.Errorf("prefix cannot contain '..'")
	}
	return nil
}

const (
	// MaxDownloadSize limits file downloads to 1GB
	MaxDownloadSize = 1 << 30
)

// StorageHandler handles storage-related HTTP requests.
type StorageHandler struct{}

// NewStorageHandler creates a new storage handler.
func NewStorageHandler() *StorageHandler {
	return &StorageHandler{}
}

// getStorageService creates a storage service from request session credentials.
func (h *StorageHandler) getStorageService(r *http.Request) (*storage.Service, error) {
	// Get session from context
	session := middleware.GetSession(r)
	if session == nil {
		return nil, fmt.Errorf("no session found")
	}

	// Get credential store from context
	credStore := middleware.GetCredentialStore(r)
	if credStore == nil {
		return nil, fmt.Errorf("credential store not available")
	}

	// Get credentials for this session
	creds := credStore.GetStorageCredentials(session.Token)
	if creds == nil {
		return nil, fmt.Errorf("storage credentials not configured")
	}

	endpoint := creds.Endpoint
	if endpoint == "" {
		endpoint = "localhost:9000" // Default for local dev
	}

	// Default to SSL for non-localhost endpoints (security best practice)
	useSSL := !isLocalEndpoint(endpoint)

	return storage.NewService(storage.Config{
		Endpoint:        endpoint,
		AccessKeyID:     creds.AccessKey,
		SecretAccessKey: creds.SecretKey,
		UseSSL:          useSSL,
	})
}

// isLocalEndpoint checks if the endpoint is localhost (no SSL needed).
func isLocalEndpoint(endpoint string) bool {
	host := strings.Split(endpoint, ":")[0]
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// HandleListBuckets handles GET /storage/buckets
func (h *StorageHandler) HandleListBuckets(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getStorageService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Storage connection failed", err)
		return
	}

	buckets, err := svc.ListBuckets(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"buckets": buckets,
		"count":   len(buckets),
	})
}

// HandleCreateBucket handles POST /storage/buckets
func (h *StorageHandler) HandleCreateBucket(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getStorageService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Storage connection failed", err)
		return
	}

	var req struct {
		Name string `json:"name"`
	}
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if err := validateBucketName(req.Name); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := svc.CreateBucket(r.Context(), req.Name); err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{"status": "created", "name": req.Name})
}

// HandleDeleteBucket handles DELETE /storage/buckets/{bucket}
func (h *StorageHandler) HandleDeleteBucket(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getStorageService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Storage connection failed", err)
		return
	}

	bucket := chi.URLParam(r, "bucket")

	if err := validateBucketName(bucket); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := svc.DeleteBucket(r.Context(), bucket); err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{"status": "deleted", "name": bucket})
}

// HandleListObjects handles GET /storage/buckets/{bucket}/objects
func (h *StorageHandler) HandleListObjects(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getStorageService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Storage connection failed", err)
		return
	}

	bucket := chi.URLParam(r, "bucket")
	prefix := r.URL.Query().Get("prefix")

	if err := validateBucketName(bucket); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validatePrefix(prefix); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	objects, err := svc.ListObjects(r.Context(), bucket, prefix)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"objects": objects,
		"count":   len(objects),
		"bucket":  bucket,
		"prefix":  prefix,
	})
}

// HandleUpload handles POST /storage/buckets/{bucket}/upload
func (h *StorageHandler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getStorageService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Storage connection failed", err)
		return
	}

	bucket := chi.URLParam(r, "bucket")
	if err := validateBucketName(bucket); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Parse multipart form (32MB max)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		httputil.BadRequest(w, r, "Failed to parse form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		httputil.BadRequest(w, r, "No file provided")
		return
	}
	defer file.Close()

	key := r.FormValue("key")
	if key == "" {
		key = header.Filename
	}

	if err := validateObjectKey(key); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := svc.UploadObject(r.Context(), bucket, key, file, header.Size, header.Header.Get("Content-Type")); err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "uploaded",
		"bucket": bucket,
		"key":    key,
	})
}

// HandleDownload handles GET /storage/buckets/{bucket}/download/*
func (h *StorageHandler) HandleDownload(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getStorageService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Storage connection failed", err)
		return
	}

	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "*")

	if err := validateBucketName(bucket); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Validate path to prevent directory traversal attacks
	if strings.Contains(key, "..") || filepath.IsAbs(key) {
		httputil.BadRequest(w, r, "Invalid file path")
		return
	}
	// Clean the path and ensure it doesn't escape
	cleanKey := filepath.Clean(key)
	if strings.HasPrefix(cleanKey, "..") {
		httputil.BadRequest(w, r, "Invalid file path")
		return
	}

	if err := validateObjectKey(cleanKey); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	reader, err := svc.DownloadObject(r.Context(), bucket, cleanKey)
	if err != nil {
		httputil.NotFound(w, r, err.Error())
		return
	}
	defer reader.Close()

	// Escape filename for Content-Disposition header (RFC 5987)
	filename := filepath.Base(cleanKey)
	encodedFilename := url.PathEscape(filename)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, filename, encodedFilename))

	// Limit download size to prevent resource exhaustion
	if _, err := io.Copy(w, io.LimitReader(reader, MaxDownloadSize)); err != nil {
		// Client likely disconnected; nothing we can do at this point
		return
	}
}

// HandleDeleteObject handles DELETE /storage/buckets/{bucket}/objects/*
func (h *StorageHandler) HandleDeleteObject(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getStorageService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Storage connection failed", err)
		return
	}

	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "*")

	if err := validateBucketName(bucket); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Validate path to prevent directory traversal attacks
	if strings.Contains(key, "..") || filepath.IsAbs(key) {
		httputil.BadRequest(w, r, "Invalid file path")
		return
	}

	if err := validateObjectKey(key); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := svc.DeleteObject(r.Context(), bucket, key); err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{"status": "deleted"})
}

// HandlePresignedURL handles GET /storage/buckets/{bucket}/presign/*
func (h *StorageHandler) HandlePresignedURL(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getStorageService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Storage connection failed", err)
		return
	}

	bucket := chi.URLParam(r, "bucket")
	key := chi.URLParam(r, "*")

	if err := validateBucketName(bucket); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Validate path to prevent directory traversal attacks
	if strings.Contains(key, "..") || filepath.IsAbs(key) {
		httputil.BadRequest(w, r, "Invalid file path")
		return
	}

	if err := validateObjectKey(key); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	presignedURL, err := svc.GetPresignedURL(r.Context(), bucket, key, 15*time.Minute)
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{"url": presignedURL})
}
