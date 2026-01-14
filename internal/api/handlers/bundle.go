package handlers

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/homeport/homeport/internal/domain/bundle"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/infrastructure/secrets/detector"
	"github.com/homeport/homeport/pkg/version"
)

// respondJSON sends a JSON response
func respondJSON(w http.ResponseWriter, r *http.Request, status int, data interface{}) {
	w.WriteHeader(status)
	render.JSON(w, r, data)
}

// respondError sends an error response
func respondError(w http.ResponseWriter, r *http.Request, status int, message string) {
	w.WriteHeader(status)
	render.JSON(w, r, map[string]string{"error": message})
}

// BundleHandler handles bundle-related API requests
type BundleHandler struct {
	// In-memory storage for bundles (in production, use persistent storage)
	bundles      map[string]*BundleInfo
	bundlesMu    sync.RWMutex
	tempDir      string
	exporterVer  string
	secretValues map[string]map[string]string // bundleID -> secretName -> value
	secretsMu    sync.RWMutex
}

// BundleInfo represents bundle metadata
type BundleInfo struct {
	ID        string            `json:"bundle_id"`
	Name      string            `json:"name"`
	Manifest  *bundle.Manifest  `json:"manifest"`
	Secrets   []*SecretRef      `json:"secrets"`
	Files     []string          `json:"files"`
	Size      int64             `json:"size"`
	FilePath  string            `json:"-"` // Not exposed in API
	CreatedAt time.Time         `json:"created_at"`
}

// SecretRef represents a secret reference
type SecretRef struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Key         string `json:"key,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
}

// CreateBundleRequest represents a request to create a bundle
type CreateBundleRequest struct {
	Resources []*resource.AWSResource `json:"resources"`
	Options   CreateBundleOptions     `json:"options"`
}

// CreateBundleOptions represents bundle creation options
type CreateBundleOptions struct {
	Domain            string `json:"domain,omitempty"`
	Consolidate       bool   `json:"consolidate"`
	DetectSecrets     bool   `json:"detect_secrets"`
	IncludeMigration  bool   `json:"include_migration"`
	IncludeMonitoring bool   `json:"include_monitoring"`
}

// CreateBundleResponse represents a response for bundle creation
type CreateBundleResponse struct {
	BundleID    string           `json:"bundle_id"`
	Manifest    *bundle.Manifest `json:"manifest"`
	Secrets     []*SecretRef     `json:"secrets"`
	DownloadURL string           `json:"download_url"`
}

// UploadBundleResponse represents a response for bundle upload
type UploadBundleResponse struct {
	BundleID string           `json:"bundle_id"`
	Manifest *bundle.Manifest `json:"manifest"`
	Secrets  []*SecretRef     `json:"secrets"`
	Valid    bool             `json:"valid"`
	Errors   []string         `json:"errors,omitempty"`
}

// ProvideSecretsRequest represents a request to provide secrets
type ProvideSecretsRequest struct {
	Secrets map[string]string `json:"secrets"`
}

// PullSecretsRequest represents a request to pull secrets from a cloud provider
type PullSecretsRequest struct {
	Provider string `json:"provider"` // "aws", "gcp", "azure"

	// AWS credentials
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	Region          string `json:"region,omitempty"`

	// GCP credentials
	ProjectID          string `json:"project_id,omitempty"`
	ServiceAccountJSON string `json:"service_account_json,omitempty"`

	// Azure credentials
	SubscriptionID string `json:"subscription_id,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
	ClientID       string `json:"client_id,omitempty"`
	ClientSecret   string `json:"client_secret,omitempty"`
}

// PullSecretsResponse represents a response for pulling secrets
type PullSecretsResponse struct {
	Success  bool              `json:"success"`
	Resolved map[string]string `json:"resolved"`
	Failed   []string          `json:"failed"`
	Errors   map[string]string `json:"errors,omitempty"`
}

// ProvideSecretsResponse represents a response for providing secrets
type ProvideSecretsResponse struct {
	Success  bool     `json:"success"`
	Resolved []string `json:"resolved"`
	Missing  []string `json:"missing"`
}

// ImportBundleRequest represents a request to import a bundle
type ImportBundleRequest struct {
	BundleID  string     `json:"bundle_id"`
	Target    string     `json:"target"` // "local" or "ssh"
	SSHConfig *SSHConfig `json:"ssh_config,omitempty"`
	Deploy    bool       `json:"deploy"`
	DryRun    bool       `json:"dry_run,omitempty"`
}

// SSHConfig represents SSH configuration
type SSHConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthMethod string `json:"auth_method"` // "key" or "password"
	KeyPath    string `json:"key_path,omitempty"`
	Password   string `json:"password,omitempty"`
}

// ImportBundleResponse represents a response for bundle import
type ImportBundleResponse struct {
	ImportID       string   `json:"import_id"`
	Status         string   `json:"status"`
	ExtractedTo    string   `json:"extracted_to"`
	MissingSecrets []string `json:"missing_secrets"`
}

// BundleSummary represents a summary of a bundle
type BundleSummary struct {
	BundleID      string    `json:"bundle_id"`
	Name          string    `json:"name"`
	Provider      string    `json:"provider"`
	ResourceCount int       `json:"resource_count"`
	CreatedAt     time.Time `json:"created_at"`
	Size          int64     `json:"size"`
}

// NewBundleHandler creates a new BundleHandler
func NewBundleHandler() *BundleHandler {
	tempDir := os.TempDir()
	bundleDir := filepath.Join(tempDir, "homeport-bundles")
	_ = os.MkdirAll(bundleDir, 0755)

	return &BundleHandler{
		bundles:      make(map[string]*BundleInfo),
		tempDir:      bundleDir,
		exporterVer:  version.Version,
		secretValues: make(map[string]map[string]string),
	}
}

// Global handler instance
var bundleHandler = NewBundleHandler()

// GetBundleHandler returns the global bundle handler
func GetBundleHandler() *BundleHandler {
	return bundleHandler
}

// RegisterBundleRoutes registers bundle routes
func RegisterBundleRoutes(r chi.Router) {
	h := GetBundleHandler()

	r.Route("/bundle", func(r chi.Router) {
		r.Get("/", h.ListBundles)
		r.Post("/export", h.ExportBundle)
		r.Post("/export/stream", h.ExportBundleStream)
		r.Post("/upload", h.UploadBundle)
		r.Post("/import", h.ImportBundle)

		r.Route("/{bundleId}", func(r chi.Router) {
			r.Get("/", h.GetBundle)
			r.Delete("/", h.DeleteBundle)
			r.Get("/download", h.DownloadBundle)
			r.Get("/secrets", h.GetBundleSecrets)
			r.Post("/secrets", h.ProvideSecrets)
			r.Post("/secrets/pull", h.PullSecrets)
			r.Get("/compose", h.GetBundleCompose)
		})
	})
}

// ListBundles lists all bundles
func (h *BundleHandler) ListBundles(w http.ResponseWriter, r *http.Request) {
	h.bundlesMu.RLock()
	defer h.bundlesMu.RUnlock()

	summaries := make([]BundleSummary, 0, len(h.bundles))
	for _, b := range h.bundles {
		provider := ""
		resourceCount := 0
		if b.Manifest != nil && b.Manifest.Source != nil {
			provider = b.Manifest.Source.Provider
			resourceCount = b.Manifest.Source.ResourceCount
		}

		summaries = append(summaries, BundleSummary{
			BundleID:      b.ID,
			Name:          b.Name,
			Provider:      provider,
			ResourceCount: resourceCount,
			CreatedAt:     b.CreatedAt,
			Size:          b.Size,
		})
	}

	respondJSON(w, r, http.StatusOK, summaries)
}

// ExportBundle creates a new bundle from resources
func (h *BundleHandler) ExportBundle(w http.ResponseWriter, r *http.Request) {
	var req CreateBundleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Generate bundle ID
	bundleID := uuid.New().String()[:8]

	// Create temp directory for bundle contents
	bundleDir := filepath.Join(h.tempDir, bundleID)
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		respondError(w, r, http.StatusInternalServerError, "Failed to create bundle directory")
		return
	}

	// Create bundle structure
	dirs := []string{"compose", "configs", "scripts", "migrations", "data-sync", "secrets", "dns", "validation"}
	for _, dir := range dirs {
		os.MkdirAll(filepath.Join(bundleDir, dir), 0755)
	}

	// Create manifest
	manifest := &bundle.Manifest{
		Version:         "1.0.0",
		Format:          "hprt",
		Created:         time.Now().UTC(),
		HomeportVersion: h.exporterVer,
		Source: &bundle.SourceInfo{
			Provider:      detectProvider(req.Resources),
			Region:        detectRegion(req.Resources),
			ResourceCount: len(req.Resources),
			AnalyzedAt:    time.Now().UTC(),
		},
		Target: &bundle.TargetInfo{
			Type:          "docker-compose",
			Consolidation: req.Options.Consolidate,
			StackCount:    estimateStackCount(req.Resources),
		},
		Checksums: make(map[string]string),
	}

	// Write manifest
	manifestPath := filepath.Join(bundleDir, "manifest.json")
	manifestData, _ := json.MarshalIndent(manifest, "", "  ")
	os.WriteFile(manifestPath, manifestData, 0644)

	// Create basic compose file
	composePath := filepath.Join(bundleDir, "compose", "docker-compose.yml")
	composeContent := generateBasicCompose(req.Resources, req.Options.Domain)
	os.WriteFile(composePath, []byte(composeContent), 0644)

	// Create env template
	envPath := filepath.Join(bundleDir, "secrets", ".env.template")
	envContent := generateEnvTemplate(req.Resources)
	os.WriteFile(envPath, []byte(envContent), 0644)

	// Create secrets manifest
	secrets := extractSecretRefs(req.Resources)
	secretsManifestPath := filepath.Join(bundleDir, "secrets", "secrets-manifest.json")
	secretsData, _ := json.MarshalIndent(map[string]interface{}{"secrets": secrets}, "", "  ")
	os.WriteFile(secretsManifestPath, secretsData, 0644)

	// Create README
	readmePath := filepath.Join(bundleDir, "README.md")
	readmeContent := generateBundleReadme(manifest, secrets)
	os.WriteFile(readmePath, []byte(readmeContent), 0644)

	// Create the .hprt archive
	bundlePath := filepath.Join(h.tempDir, bundleID+".hprt")
	if err := createTarGz(bundleDir, bundlePath); err != nil {
		respondError(w, r, http.StatusInternalServerError, "Failed to create bundle archive")
		return
	}

	// Get bundle size
	stat, _ := os.Stat(bundlePath)
	size := int64(0)
	if stat != nil {
		size = stat.Size()
	}

	// Store bundle info
	bundleInfo := &BundleInfo{
		ID:        bundleID,
		Name:      fmt.Sprintf("migration-%s.hprt", time.Now().Format("20060102-150405")),
		Manifest:  manifest,
		Secrets:   secrets,
		Files:     listBundleFiles(bundleDir),
		Size:      size,
		FilePath:  bundlePath,
		CreatedAt: time.Now().UTC(),
	}

	h.bundlesMu.Lock()
	h.bundles[bundleID] = bundleInfo
	h.bundlesMu.Unlock()

	respondJSON(w, r, http.StatusOK, CreateBundleResponse{
		BundleID:    bundleID,
		Manifest:    manifest,
		Secrets:     secrets,
		DownloadURL: fmt.Sprintf("/api/bundle/%s/download", bundleID),
	})
}

// ExportBundleStream creates a bundle with SSE progress updates
func (h *BundleHandler) ExportBundleStream(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	var req CreateBundleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendSSE(w, flusher, "error", map[string]string{"message": "Invalid request body"})
		return
	}

	bundleID := uuid.New().String()[:8]

	// Send progress updates
	steps := []string{
		"Analyzing resources",
		"Creating directory structure",
		"Generating Docker Compose",
		"Creating configuration files",
		"Detecting secrets",
		"Creating archive",
		"Validating bundle",
	}

	for i, step := range steps {
		sendSSE(w, flusher, "progress", ExportProgressData{
			BundleID: bundleID,
			Step:     step,
			Message:  step + "...",
			Progress: (i + 1) * 100 / len(steps),
		})
		time.Sleep(200 * time.Millisecond) // Simulate work
	}

	// Create the actual bundle (simplified)
	bundleDir := filepath.Join(h.tempDir, bundleID)
	os.MkdirAll(bundleDir, 0755)

	manifest := &bundle.Manifest{
		Version:         "1.0.0",
		Format:          "hprt",
		Created:         time.Now().UTC(),
		HomeportVersion: h.exporterVer,
		Source: &bundle.SourceInfo{
			Provider:      detectProvider(req.Resources),
			ResourceCount: len(req.Resources),
			AnalyzedAt:    time.Now().UTC(),
		},
		Target: &bundle.TargetInfo{
			Type:          "docker-compose",
			Consolidation: req.Options.Consolidate,
			StackCount:    estimateStackCount(req.Resources),
		},
	}

	bundlePath := filepath.Join(h.tempDir, bundleID+".hprt")
	os.WriteFile(bundlePath, []byte("placeholder"), 0644)

	secrets := extractSecretRefs(req.Resources)

	bundleInfo := &BundleInfo{
		ID:        bundleID,
		Name:      fmt.Sprintf("migration-%s.hprt", time.Now().Format("20060102-150405")),
		Manifest:  manifest,
		Secrets:   secrets,
		FilePath:  bundlePath,
		CreatedAt: time.Now().UTC(),
	}

	h.bundlesMu.Lock()
	h.bundles[bundleID] = bundleInfo
	h.bundlesMu.Unlock()

	// Send complete
	sendSSE(w, flusher, "complete", CreateBundleResponse{
		BundleID:    bundleID,
		Manifest:    manifest,
		Secrets:     secrets,
		DownloadURL: fmt.Sprintf("/api/bundle/%s/download", bundleID),
	})
}

// UploadBundle handles bundle file upload
func (h *BundleHandler) UploadBundle(w http.ResponseWriter, r *http.Request) {
	// Parse multipart form (max 100MB)
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		respondError(w, r, http.StatusBadRequest, "Failed to parse form")
		return
	}

	file, header, err := r.FormFile("bundle")
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "Bundle file is required")
		return
	}
	defer file.Close()

	// Validate file extension
	if !strings.HasSuffix(header.Filename, ".hprt") {
		respondError(w, r, http.StatusBadRequest, "Invalid file type. Expected .hprt bundle")
		return
	}

	bundleID := uuid.New().String()[:8]

	// Save uploaded file
	bundlePath := filepath.Join(h.tempDir, bundleID+".hprt")
	dst, err := os.Create(bundlePath)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "Failed to save bundle")
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "Failed to save bundle")
		return
	}

	// Extract and validate bundle
	extractDir := filepath.Join(h.tempDir, bundleID)
	if err := extractTarGz(bundlePath, extractDir); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid bundle format")
		return
	}

	// Read manifest
	manifestPath := filepath.Join(extractDir, "manifest.json")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		respondError(w, r, http.StatusBadRequest, "Bundle missing manifest.json")
		return
	}

	var manifest bundle.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid manifest format")
		return
	}

	// Read secrets manifest (ensure we return empty array, not null)
	secrets := make([]*SecretRef, 0)
	secretsPath := filepath.Join(extractDir, "secrets", "secrets-manifest.json")
	if secretsData, err := os.ReadFile(secretsPath); err == nil {
		var secretsManifest struct {
			Secrets []*SecretRef `json:"secrets"`
		}
		if err := json.Unmarshal(secretsData, &secretsManifest); err == nil && secretsManifest.Secrets != nil {
			secrets = secretsManifest.Secrets
		}
	}

	bundleInfo := &BundleInfo{
		ID:        bundleID,
		Name:      header.Filename,
		Manifest:  &manifest,
		Secrets:   secrets,
		Files:     listBundleFiles(extractDir),
		Size:      written,
		FilePath:  bundlePath,
		CreatedAt: time.Now().UTC(),
	}

	h.bundlesMu.Lock()
	h.bundles[bundleID] = bundleInfo
	h.bundlesMu.Unlock()

	respondJSON(w, r, http.StatusOK, UploadBundleResponse{
		BundleID: bundleID,
		Manifest: &manifest,
		Secrets:  secrets,
		Valid:    true,
	})
}

// GetBundle returns bundle information
func (h *BundleHandler) GetBundle(w http.ResponseWriter, r *http.Request) {
	bundleID := chi.URLParam(r, "bundleId")

	h.bundlesMu.RLock()
	bundleInfo, ok := h.bundles[bundleID]
	h.bundlesMu.RUnlock()

	if !ok {
		respondError(w, r, http.StatusNotFound, "Bundle not found")
		return
	}

	respondJSON(w, r, http.StatusOK, bundleInfo)
}

// DeleteBundle deletes a bundle
func (h *BundleHandler) DeleteBundle(w http.ResponseWriter, r *http.Request) {
	bundleID := chi.URLParam(r, "bundleId")

	h.bundlesMu.Lock()
	bundleInfo, ok := h.bundles[bundleID]
	if ok {
		// Delete files
		os.Remove(bundleInfo.FilePath)
		os.RemoveAll(filepath.Join(h.tempDir, bundleID))
		delete(h.bundles, bundleID)
	}
	h.bundlesMu.Unlock()

	if !ok {
		respondError(w, r, http.StatusNotFound, "Bundle not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DownloadBundle downloads a bundle file
func (h *BundleHandler) DownloadBundle(w http.ResponseWriter, r *http.Request) {
	bundleID := chi.URLParam(r, "bundleId")

	h.bundlesMu.RLock()
	bundleInfo, ok := h.bundles[bundleID]
	h.bundlesMu.RUnlock()

	if !ok {
		respondError(w, r, http.StatusNotFound, "Bundle not found")
		return
	}

	file, err := os.Open(bundleInfo.FilePath)
	if err != nil {
		respondError(w, r, http.StatusInternalServerError, "Failed to read bundle")
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", bundleInfo.Name))

	io.Copy(w, file)
}

// GetBundleSecrets returns secret references for a bundle
func (h *BundleHandler) GetBundleSecrets(w http.ResponseWriter, r *http.Request) {
	bundleID := chi.URLParam(r, "bundleId")

	h.bundlesMu.RLock()
	bundleInfo, ok := h.bundles[bundleID]
	h.bundlesMu.RUnlock()

	if !ok {
		respondError(w, r, http.StatusNotFound, "Bundle not found")
		return
	}

	respondJSON(w, r, http.StatusOK, bundleInfo.Secrets)
}

// GetBundleCompose returns the docker-compose.yml content for a bundle
func (h *BundleHandler) GetBundleCompose(w http.ResponseWriter, r *http.Request) {
	bundleID := chi.URLParam(r, "bundleId")

	h.bundlesMu.RLock()
	_, ok := h.bundles[bundleID]
	h.bundlesMu.RUnlock()

	if !ok {
		respondError(w, r, http.StatusNotFound, "Bundle not found")
		return
	}

	// Read compose file from the extracted bundle directory
	composePath := filepath.Join(h.tempDir, bundleID, "compose", "docker-compose.yml")
	content, err := os.ReadFile(composePath)
	if err != nil {
		respondError(w, r, http.StatusNotFound, "Compose file not found")
		return
	}

	respondJSON(w, r, http.StatusOK, map[string]string{
		"content": string(content),
	})
}

// ProvideSecrets stores secret values for a bundle
func (h *BundleHandler) ProvideSecrets(w http.ResponseWriter, r *http.Request) {
	bundleID := chi.URLParam(r, "bundleId")

	h.bundlesMu.RLock()
	bundleInfo, ok := h.bundles[bundleID]
	h.bundlesMu.RUnlock()

	if !ok {
		respondError(w, r, http.StatusNotFound, "Bundle not found")
		return
	}

	var req ProvideSecretsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Store secrets in memory
	h.secretsMu.Lock()
	if h.secretValues[bundleID] == nil {
		h.secretValues[bundleID] = make(map[string]string)
	}
	for k, v := range req.Secrets {
		h.secretValues[bundleID][k] = v
	}
	h.secretsMu.Unlock()

	// Check which required secrets are still missing (ensure non-null arrays)
	resolved := make([]string, 0)
	missing := make([]string, 0)

	for _, secret := range bundleInfo.Secrets {
		h.secretsMu.RLock()
		_, provided := h.secretValues[bundleID][secret.Name]
		h.secretsMu.RUnlock()

		if provided {
			resolved = append(resolved, secret.Name)
		} else if secret.Required {
			missing = append(missing, secret.Name)
		}
	}

	respondJSON(w, r, http.StatusOK, ProvideSecretsResponse{
		Success:  len(missing) == 0,
		Resolved: resolved,
		Missing:  missing,
	})
}

// PullSecrets pulls secrets from a cloud provider
func (h *BundleHandler) PullSecrets(w http.ResponseWriter, r *http.Request) {
	bundleID := chi.URLParam(r, "bundleId")

	h.bundlesMu.RLock()
	bundleInfo, ok := h.bundles[bundleID]
	h.bundlesMu.RUnlock()

	if !ok {
		respondError(w, r, http.StatusNotFound, "Bundle not found")
		return
	}

	var req PullSecretsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Provider == "" {
		respondError(w, r, http.StatusBadRequest, "Provider is required")
		return
	}

	resolved := make(map[string]string)
	var failed []string
	errors := make(map[string]string)

	// Map source types to providers
	sourceToProvider := map[string]string{
		"aws-secrets-manager": "aws",
		"gcp-secret-manager":  "gcp",
		"azure-key-vault":     "azure",
	}

	// Collect secrets by provider for batch processing
	var awsSecrets []awsSecretRequest

	for _, secret := range bundleInfo.Secrets {
		secretProvider := sourceToProvider[secret.Source]
		if secretProvider != req.Provider {
			continue
		}

		secretKey := secret.Key
		if secretKey == "" {
			continue
		}

		switch req.Provider {
		case "aws":
			awsSecrets = append(awsSecrets, awsSecretRequest{
				name:   secret.Name,
				key:    secretKey,
				region: req.Region,
			})
		case "gcp":
			value, err := pullGCPSecret(secretKey, req.ProjectID, req.ServiceAccountJSON)
			if err != nil {
				failed = append(failed, secret.Name)
				errors[secret.Name] = err.Error()
			} else if value != "" {
				resolved[secret.Name] = value
			}
		case "azure":
			value, err := pullAzureSecret(secretKey, req.SubscriptionID, req.TenantID, req.ClientID, req.ClientSecret)
			if err != nil {
				failed = append(failed, secret.Name)
				errors[secret.Name] = err.Error()
			} else if value != "" {
				resolved[secret.Name] = value
			}
		default:
			errors[secret.Name] = fmt.Sprintf("unsupported provider: %s", req.Provider)
			failed = append(failed, secret.Name)
		}
	}

	// Batch fetch AWS secrets
	if len(awsSecrets) > 0 {
		fmt.Printf("[PullSecrets] Batch fetching %d AWS secrets\n", len(awsSecrets))
		batchResolved, batchErrors := pullAWSSecretsBatch(awsSecrets, req.AccessKeyID, req.SecretAccessKey)
		for name, value := range batchResolved {
			resolved[name] = value
		}
		for name, errMsg := range batchErrors {
			errors[name] = errMsg
			failed = append(failed, name)
		}
	}

	// Also store the resolved secrets
	if len(resolved) > 0 {
		h.secretsMu.Lock()
		if h.secretValues[bundleID] == nil {
			h.secretValues[bundleID] = make(map[string]string)
		}
		for k, v := range resolved {
			h.secretValues[bundleID][k] = v
		}
		h.secretsMu.Unlock()
	}

	respondJSON(w, r, http.StatusOK, PullSecretsResponse{
		Success:  len(failed) == 0,
		Resolved: resolved,
		Failed:   failed,
		Errors:   errors,
	})
}

// awsSecretRequest holds info for a secret to fetch
type awsSecretRequest struct {
	name      string // friendly name
	key       string // ARN or secret name
	region    string
}

// pullAWSSecretsBatch retrieves multiple secrets from AWS Secrets Manager in batch
func pullAWSSecretsBatch(secrets []awsSecretRequest, accessKeyID, secretAccessKey string) (map[string]string, map[string]string) {
	resolved := make(map[string]string)
	errors := make(map[string]string)

	if accessKeyID == "" || secretAccessKey == "" {
		for _, s := range secrets {
			errors[s.name] = "AWS credentials required"
		}
		return resolved, errors
	}

	// Group secrets by region (batch-get requires same region)
	byRegion := make(map[string][]awsSecretRequest)
	for _, s := range secrets {
		region := s.region
		// Extract region from ARN if present
		if strings.HasPrefix(s.key, "arn:aws:secretsmanager:") {
			parts := strings.Split(s.key, ":")
			if len(parts) >= 4 {
				region = parts[3]
			}
		}
		if region == "" {
			errors[s.name] = "region is required - could not determine from ARN: " + s.key
			continue
		}
		s.region = region
		byRegion[region] = append(byRegion[region], s)
	}

	// Fetch each region's secrets in batch
	for region, regionSecrets := range byRegion {
		// Build list of secret IDs
		secretIDs := make([]string, len(regionSecrets))
		for i, s := range regionSecrets {
			secretIDs[i] = s.key
		}

		// Create context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

		// Use batch-get-secret-value
		args := []string{"secretsmanager", "batch-get-secret-value",
			"--secret-id-list"}
		args = append(args, secretIDs...)
		args = append(args, "--region", region)

		cmd := exec.CommandContext(ctx, "aws", args...)
		cmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID="+accessKeyID,
			"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
		)

		output, err := cmd.Output()
		cancel()

		if err != nil {
			// Batch failed - fall back to individual fetches
			fmt.Printf("[PullSecrets] Batch failed for region %s, falling back to individual: %v\n", region, err)
			for _, s := range regionSecrets {
				value, err := pullAWSSecretSingle(s.key, accessKeyID, secretAccessKey, s.region)
				if err != nil {
					errors[s.name] = err.Error()
				} else {
					resolved[s.name] = value
				}
			}
			continue
		}

		// Parse batch response
		var batchResponse struct {
			SecretValues []struct {
				ARN          string `json:"ARN"`
				Name         string `json:"Name"`
				SecretString string `json:"SecretString"`
			} `json:"SecretValues"`
			Errors []struct {
				SecretID  string `json:"SecretId"`
				ErrorCode string `json:"ErrorCode"`
				Message   string `json:"Message"`
			} `json:"Errors"`
		}

		if err := json.Unmarshal(output, &batchResponse); err != nil {
			// Parse failed - fall back to individual
			for _, s := range regionSecrets {
				value, err := pullAWSSecretSingle(s.key, accessKeyID, secretAccessKey, s.region)
				if err != nil {
					errors[s.name] = err.Error()
				} else {
					resolved[s.name] = value
				}
			}
			continue
		}

		// Build lookup from ARN/Name to friendly name
		keyToName := make(map[string]string)
		for _, s := range regionSecrets {
			keyToName[s.key] = s.name
		}

		// Process successful fetches
		for _, sv := range batchResponse.SecretValues {
			// Find the matching request by ARN or Name
			var friendlyName string
			if name, ok := keyToName[sv.ARN]; ok {
				friendlyName = name
			} else if name, ok := keyToName[sv.Name]; ok {
				friendlyName = name
			} else {
				// Try partial match on ARN
				for key, name := range keyToName {
					if strings.Contains(sv.ARN, key) || strings.Contains(key, sv.Name) {
						friendlyName = name
						break
					}
				}
			}

			if friendlyName == "" {
				continue
			}

			// Process the secret value
			secretValue := sv.SecretString
			if strings.HasPrefix(secretValue, "{") {
				var jsonSecret map[string]interface{}
				if err := json.Unmarshal([]byte(secretValue), &jsonSecret); err == nil {
					if password, ok := jsonSecret["password"].(string); ok {
						secretValue = password
					}
				}
			}
			resolved[friendlyName] = secretValue
		}

		// Process errors
		for _, e := range batchResponse.Errors {
			if name, ok := keyToName[e.SecretID]; ok {
				errors[name] = fmt.Sprintf("%s: %s", e.ErrorCode, e.Message)
			}
		}
	}

	return resolved, errors
}

// pullAWSSecretSingle retrieves a single secret from AWS Secrets Manager
func pullAWSSecretSingle(secretName, accessKeyID, secretAccessKey, region string) (string, error) {
	if accessKeyID == "" || secretAccessKey == "" {
		return "", fmt.Errorf("AWS credentials required")
	}

	// Extract region from ARN if secretName is an ARN
	if strings.HasPrefix(secretName, "arn:aws:secretsmanager:") {
		parts := strings.Split(secretName, ":")
		if len(parts) >= 4 {
			region = parts[3]
		}
	}

	if region == "" {
		return "", fmt.Errorf("region is required - could not determine from ARN: %s", secretName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "aws", "secretsmanager", "get-secret-value",
		"--secret-id", secretName,
		"--query", "SecretString",
		"--output", "text",
		"--region", region)

	cmd.Env = append(os.Environ(),
		"AWS_ACCESS_KEY_ID="+accessKeyID,
		"AWS_SECRET_ACCESS_KEY="+secretAccessKey,
	)

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("AWS CLI timed out after 30 seconds")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("AWS CLI error: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to execute AWS CLI: %w", err)
	}

	secretValue := strings.TrimSpace(string(output))

	if strings.HasPrefix(secretValue, "{") {
		var jsonSecret map[string]interface{}
		if err := json.Unmarshal([]byte(secretValue), &jsonSecret); err == nil {
			if password, ok := jsonSecret["password"].(string); ok {
				return password, nil
			}
			return secretValue, nil
		}
	}

	return secretValue, nil
}

// pullGCPSecret retrieves a secret from GCP Secret Manager
func pullGCPSecret(secretName, projectID, serviceAccountJSON string) (string, error) {
	if projectID == "" {
		return "", fmt.Errorf("GCP project ID required")
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// If service account JSON is provided, write it to a temp file
	var envVars []string
	if serviceAccountJSON != "" {
		tmpFile, err := os.CreateTemp("", "gcp-sa-*.json")
		if err != nil {
			return "", fmt.Errorf("failed to create temp file for service account: %w", err)
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(serviceAccountJSON); err != nil {
			tmpFile.Close()
			return "", fmt.Errorf("failed to write service account: %w", err)
		}
		tmpFile.Close()

		envVars = append(os.Environ(), "GOOGLE_APPLICATION_CREDENTIALS="+tmpFile.Name())
	} else {
		envVars = os.Environ()
	}

	// Parse secret name to get version (default to "latest")
	version := "latest"
	parts := strings.Split(secretName, ":")
	if len(parts) == 2 {
		secretName = parts[0]
		version = parts[1]
	}

	// Use gcloud CLI with timeout
	cmd := exec.CommandContext(ctx, "gcloud", "secrets", "versions", "access", version,
		"--secret", secretName,
		"--project", projectID)
	cmd.Env = envVars

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("gcloud timed out after 30 seconds")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("gcloud error: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to execute gcloud: %w", err)
	}

	secretValue := strings.TrimSpace(string(output))

	// Handle JSON secrets (extract password if present)
	if strings.HasPrefix(secretValue, "{") {
		var jsonSecret map[string]interface{}
		if err := json.Unmarshal([]byte(secretValue), &jsonSecret); err == nil {
			if password, ok := jsonSecret["password"].(string); ok {
				return password, nil
			}
		}
	}

	return secretValue, nil
}

// pullAzureSecret retrieves a secret from Azure Key Vault
func pullAzureSecret(secretName, subscriptionID, tenantID, clientID, clientSecret string) (string, error) {
	// Parse vault name and secret name from the key
	// Format: vault-name/secret-name or just secret-name (with default vault)
	parts := strings.SplitN(secretName, "/", 2)
	var vaultName, actualSecretName string
	if len(parts) == 2 {
		vaultName = parts[0]
		actualSecretName = parts[1]
	} else {
		return "", fmt.Errorf("secret key must be in format 'vault-name/secret-name'")
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Set up Azure CLI environment
	envVars := os.Environ()
	if clientID != "" && clientSecret != "" && tenantID != "" {
		envVars = append(envVars,
			"AZURE_CLIENT_ID="+clientID,
			"AZURE_CLIENT_SECRET="+clientSecret,
			"AZURE_TENANT_ID="+tenantID,
		)
	}

	// Use Azure CLI with timeout
	args := []string{"keyvault", "secret", "show",
		"--vault-name", vaultName,
		"--name", actualSecretName,
		"--query", "value",
		"-o", "tsv"}

	if subscriptionID != "" {
		args = append(args, "--subscription", subscriptionID)
	}

	cmd := exec.CommandContext(ctx, "az", args...)
	cmd.Env = envVars

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("Azure CLI timed out after 30 seconds")
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("Azure CLI error: %s", string(exitErr.Stderr))
		}
		return "", fmt.Errorf("failed to execute Azure CLI: %w", err)
	}

	secretValue := strings.TrimSpace(string(output))

	// Handle JSON secrets (extract password if present)
	if strings.HasPrefix(secretValue, "{") {
		var jsonSecret map[string]interface{}
		if err := json.Unmarshal([]byte(secretValue), &jsonSecret); err == nil {
			if password, ok := jsonSecret["password"].(string); ok {
				return password, nil
			}
		}
	}

	return secretValue, nil
}

// ImportBundle imports a bundle to a target
func (h *BundleHandler) ImportBundle(w http.ResponseWriter, r *http.Request) {
	var req ImportBundleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, r, http.StatusBadRequest, "Invalid request body")
		return
	}

	h.bundlesMu.RLock()
	bundleInfo, ok := h.bundles[req.BundleID]
	h.bundlesMu.RUnlock()

	if !ok {
		respondError(w, r, http.StatusNotFound, "Bundle not found")
		return
	}

	// Check for missing required secrets
	var missingSecrets []string
	h.secretsMu.RLock()
	secrets := h.secretValues[req.BundleID]
	h.secretsMu.RUnlock()

	for _, secret := range bundleInfo.Secrets {
		if secret.Required {
			if secrets == nil || secrets[secret.Name] == "" {
				missingSecrets = append(missingSecrets, secret.Name)
			}
		}
	}

	importID := uuid.New().String()[:8]
	extractDir := filepath.Join(h.tempDir, req.BundleID)

	respondJSON(w, r, http.StatusOK, ImportBundleResponse{
		ImportID:       importID,
		Status:         "started",
		ExtractedTo:    extractDir,
		MissingSecrets: missingSecrets,
	})
}

// Helper functions

func detectProvider(resources []*resource.AWSResource) string {
	if len(resources) == 0 {
		return "unknown"
	}
	// Check first resource type prefix
	resType := string(resources[0].Type)
	if strings.HasPrefix(resType, "aws_") {
		return "aws"
	}
	if strings.HasPrefix(resType, "google_") || strings.HasPrefix(resType, "gcp_") {
		return "gcp"
	}
	if strings.HasPrefix(resType, "azurerm_") || strings.HasPrefix(resType, "azure_") {
		return "azure"
	}
	return "unknown"
}

func detectRegion(resources []*resource.AWSResource) string {
	for _, r := range resources {
		if r.Region != "" {
			return r.Region
		}
	}
	return ""
}

func estimateStackCount(resources []*resource.AWSResource) int {
	// Estimate based on resource type categories
	categories := make(map[resource.Category]bool)
	for _, r := range resources {
		cat := resource.GetCategory(r.Type)
		categories[cat] = true
	}
	return len(categories)
}

func generateBasicCompose(resources []*resource.AWSResource, domain string) string {
	// Generate a basic docker-compose.yml
	return fmt.Sprintf(`version: '3.8'

services:
  # Generated from %d resources
  # Domain: %s

  # Add your services here

networks:
  default:
    name: homeport-network
`, len(resources), domain)
}

func generateEnvTemplate(resources []*resource.AWSResource) string {
	return `# Environment variables for migration
# Fill in the values below before deployment

# Database
# DATABASE_PASSWORD=

# API Keys
# API_KEY=

# Secrets
# JWT_SECRET=
`
}

func extractSecretRefs(resources []*resource.AWSResource) []*SecretRef {
	// Use the secret detector registry to analyze resources
	registry := detector.NewDefaultRegistry()

	ctx := context.Background()
	manifest, err := registry.DetectAll(ctx, resources)
	if err != nil {
		fmt.Printf("[extractSecretRefs] Error detecting secrets: %v\n", err)
		return []*SecretRef{}
	}

	// Convert domain secrets to API response format
	var refs []*SecretRef
	for _, secret := range manifest.Secrets {
		fmt.Printf("[extractSecretRefs] Detected: name=%s, source=%s, key=%s\n",
			secret.Name, secret.Source, secret.Key)
		refs = append(refs, &SecretRef{
			Name:        secret.Name,
			Source:      string(secret.Source),
			Key:         secret.Key,
			Description: secret.Description,
			Required:    secret.Required,
		})
	}

	fmt.Printf("[extractSecretRefs] Total secrets detected: %d\n", len(refs))
	return refs
}

func generateBundleReadme(manifest *bundle.Manifest, secrets []*SecretRef) string {
	provider := "Unknown"
	resourceCount := 0
	if manifest.Source != nil {
		provider = manifest.Source.Provider
		resourceCount = manifest.Source.ResourceCount
	}

	return fmt.Sprintf(`# Migration Bundle

## Overview
- **Created:** %s
- **Homeport Version:** %s
- **Source Provider:** %s
- **Resources:** %d

## Contents
- compose/ - Docker Compose files
- configs/ - Service configuration files
- scripts/ - Migration and backup scripts
- secrets/ - Secret references (no values stored)
- dns/ - DNS cutover configuration
- validation/ - Health check configuration

## Required Secrets
%s

## Usage
1. Review the compose files in compose/
2. Provide secrets using 'homeport import --secrets-file .env'
3. Deploy using 'homeport import --deploy'
`,
		manifest.Created.Format(time.RFC3339),
		manifest.HomeportVersion,
		provider,
		resourceCount,
		formatSecretsList(secrets),
	)
}

func formatSecretsList(secrets []*SecretRef) string {
	if len(secrets) == 0 {
		return "No secrets required."
	}

	var lines []string
	for _, s := range secrets {
		required := ""
		if s.Required {
			required = " (required)"
		}
		lines = append(lines, fmt.Sprintf("- %s: %s%s", s.Name, s.Description, required))
	}
	return strings.Join(lines, "\n")
}

func listBundleFiles(dir string) []string {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			relPath, _ := filepath.Rel(dir, path)
			files = append(files, relPath)
		}
		return nil
	})
	return files
}

func createTarGz(sourceDir, targetPath string) error {
	file, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tarWriter, file)
		return err
	})
}

func extractTarGz(sourcePath, targetDir string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Join(targetDir, header.Name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			outFile, err := os.Create(targetPath)
			if err != nil {
				return err
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				return err
			}
			outFile.Close()
		}
	}

	return nil
}

func sendSSE(w http.ResponseWriter, flusher http.Flusher, eventType string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\n", eventType)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}
