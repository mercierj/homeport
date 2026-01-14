// Package secrets provides secure secret management with encryption at rest.
package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/homeport/homeport/internal/pkg/logger"
)

// Config holds the secret service configuration.
type Config struct {
	// DataDir is the directory for storing encrypted secrets
	DataDir string
	// EncryptionKey is the master key for encrypting secrets (32 bytes for AES-256)
	EncryptionKey string
	// EnableVersioning enables secret versioning
	EnableVersioning bool
	// MaxVersions is the maximum number of versions to keep per secret
	MaxVersions int
}

// Secret represents a secret with its encrypted value.
type Secret struct {
	Name     string            `json:"name"`
	Value    string            `json:"value,omitempty"` // Only populated when explicitly requested
	Metadata SecretMetadata    `json:"metadata"`
	Labels   map[string]string `json:"labels,omitempty"`
}

// SecretMetadata contains metadata about a secret (without the value).
type SecretMetadata struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	CreatedBy   string            `json:"created_by,omitempty"`
	Version     int               `json:"version"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// SecretVersion represents a historical version of a secret.
type SecretVersion struct {
	Version   int       `json:"version"`
	Value     string    `json:"value,omitempty"` // Only populated when explicitly requested
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by,omitempty"`
}

// CreateSecretRequest contains parameters for creating a secret.
type CreateSecretRequest struct {
	Name        string            `json:"name"`
	Value       string            `json:"value"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedBy   string            `json:"created_by,omitempty"`
}

// storedSecret is the internal representation stored on disk.
type storedSecret struct {
	Name           string            `json:"name"`
	EncryptedValue string            `json:"encrypted_value"`
	Description    string            `json:"description,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      time.Time         `json:"updated_at"`
	CreatedBy      string            `json:"created_by,omitempty"`
	Version        int               `json:"version"`
	Labels         map[string]string `json:"labels,omitempty"`
}

// storedVersion is the internal representation of a version stored on disk.
type storedVersion struct {
	Version        int       `json:"version"`
	EncryptedValue string    `json:"encrypted_value"`
	CreatedAt      time.Time `json:"created_at"`
	CreatedBy      string    `json:"created_by,omitempty"`
}

// Service manages secrets with encryption at rest.
type Service struct {
	config Config
	gcm    cipher.AEAD
	mu     sync.RWMutex
}

// NewService creates a new secret management service.
func NewService(cfg Config) (*Service, error) {
	if cfg.DataDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		cfg.DataDir = filepath.Join(homeDir, ".homeport", "secrets")
	}

	// Create data directory with restrictive permissions
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Set default max versions
	if cfg.MaxVersions <= 0 {
		cfg.MaxVersions = 10
	}

	// Derive AES-256 key from the provided encryption key
	var key []byte
	if cfg.EncryptionKey != "" {
		hash := sha256.Sum256([]byte(cfg.EncryptionKey))
		key = hash[:]
	} else {
		// Generate or load a key if none provided
		keyPath := filepath.Join(cfg.DataDir, ".encryption_key")
		keyData, err := os.ReadFile(keyPath)
		if err != nil {
			if os.IsNotExist(err) {
				// Generate new key
				key = make([]byte, 32)
				if _, err := io.ReadFull(rand.Reader, key); err != nil {
					return nil, fmt.Errorf("failed to generate encryption key: %w", err)
				}
				// Save the key
				if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0600); err != nil {
					return nil, fmt.Errorf("failed to save encryption key: %w", err)
				}
			} else {
				return nil, fmt.Errorf("failed to read encryption key: %w", err)
			}
		} else {
			key, err = hex.DecodeString(string(keyData))
			if err != nil {
				return nil, fmt.Errorf("failed to decode encryption key: %w", err)
			}
		}
	}

	// Create AES cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	// Create GCM mode for authenticated encryption
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &Service{
		config: cfg,
		gcm:    gcm,
	}, nil
}

// getStackDir returns the directory path for a stack's secrets.
func (s *Service) getStackDir(stackID string) string {
	if stackID == "" {
		stackID = "default"
	}
	return filepath.Join(s.config.DataDir, stackID)
}

// getSecretPath returns the file path for a secret.
func (s *Service) getSecretPath(stackID, name string) string {
	return filepath.Join(s.getStackDir(stackID), name+".json")
}

// getVersionsDir returns the directory path for secret versions.
func (s *Service) getVersionsDir(stackID, name string) string {
	return filepath.Join(s.getStackDir(stackID), ".versions", name)
}

// encrypt encrypts a plaintext value.
func (s *Service) encrypt(plaintext string) (string, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := s.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// decrypt decrypts an encrypted value.
func (s *Service) decrypt(ciphertext string) (string, error) {
	data, err := hex.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	if len(data) < s.gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce := data[:s.gcm.NonceSize()]
	encrypted := data[s.gcm.NonceSize():]

	plaintext, err := s.gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %w", err)
	}

	return string(plaintext), nil
}

// logAudit logs an audit event for secret access.
func (s *Service) logAudit(ctx context.Context, action, stackID, secretName string, success bool) {
	logger.Info("Secret audit",
		"action", action,
		"stack_id", stackID,
		"secret_name", secretName,
		"success", success,
		"timestamp", time.Now().UTC().Format(time.RFC3339),
	)
}

// ListSecrets returns metadata for all secrets in a stack (without values).
func (s *Service) ListSecrets(ctx context.Context, stackID string) ([]SecretMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stackDir := s.getStackDir(stackID)
	entries, err := os.ReadDir(stackDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SecretMetadata{}, nil
		}
		return nil, fmt.Errorf("failed to read secrets directory: %w", err)
	}

	var secrets []SecretMetadata
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		name := entry.Name()[:len(entry.Name())-5] // Remove .json extension
		metadata, err := s.getSecretMetadataInternal(stackID, name)
		if err != nil {
			continue // Skip invalid secrets
		}
		secrets = append(secrets, *metadata)
	}

	// Sort by name
	sort.Slice(secrets, func(i, j int) bool {
		return secrets[i].Name < secrets[j].Name
	})

	s.logAudit(ctx, "list", stackID, "*", true)
	return secrets, nil
}

// GetSecret retrieves a secret with its value (triggers audit log).
func (s *Service) GetSecret(ctx context.Context, stackID, name string) (*Secret, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stored, err := s.loadSecret(stackID, name)
	if err != nil {
		s.logAudit(ctx, "get", stackID, name, false)
		return nil, err
	}

	// Decrypt the value
	value, err := s.decrypt(stored.EncryptedValue)
	if err != nil {
		s.logAudit(ctx, "get", stackID, name, false)
		return nil, fmt.Errorf("failed to decrypt secret: %w", err)
	}

	s.logAudit(ctx, "get", stackID, name, true)

	return &Secret{
		Name:  stored.Name,
		Value: value,
		Metadata: SecretMetadata{
			Name:        stored.Name,
			Description: stored.Description,
			CreatedAt:   stored.CreatedAt,
			UpdatedAt:   stored.UpdatedAt,
			CreatedBy:   stored.CreatedBy,
			Version:     stored.Version,
			Labels:      stored.Labels,
		},
		Labels: stored.Labels,
	}, nil
}

// GetSecretMetadata retrieves secret metadata without the value.
func (s *Service) GetSecretMetadata(ctx context.Context, stackID, name string) (*SecretMetadata, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metadata, err := s.getSecretMetadataInternal(stackID, name)
	if err != nil {
		return nil, err
	}

	// Metadata access doesn't trigger full audit (no value exposed)
	logger.Debug("Secret metadata accessed",
		"stack_id", stackID,
		"secret_name", name,
	)

	return metadata, nil
}

// getSecretMetadataInternal loads metadata without locking (caller must hold lock).
func (s *Service) getSecretMetadataInternal(stackID, name string) (*SecretMetadata, error) {
	stored, err := s.loadSecret(stackID, name)
	if err != nil {
		return nil, err
	}

	return &SecretMetadata{
		Name:        stored.Name,
		Description: stored.Description,
		CreatedAt:   stored.CreatedAt,
		UpdatedAt:   stored.UpdatedAt,
		CreatedBy:   stored.CreatedBy,
		Version:     stored.Version,
		Labels:      stored.Labels,
	}, nil
}

// loadSecret loads a secret from disk (caller must hold lock).
func (s *Service) loadSecret(stackID, name string) (*storedSecret, error) {
	path := s.getSecretPath(stackID, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("secret not found: %s", name)
		}
		return nil, fmt.Errorf("failed to read secret: %w", err)
	}

	var stored storedSecret
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("failed to parse secret: %w", err)
	}

	return &stored, nil
}

// CreateSecret creates a new secret.
func (s *Service) CreateSecret(ctx context.Context, stackID string, req CreateSecretRequest) (*SecretMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if secret already exists
	path := s.getSecretPath(stackID, req.Name)
	if _, err := os.Stat(path); err == nil {
		s.logAudit(ctx, "create", stackID, req.Name, false)
		return nil, fmt.Errorf("secret already exists: %s", req.Name)
	}

	// Create stack directory if needed
	stackDir := s.getStackDir(stackID)
	if err := os.MkdirAll(stackDir, 0700); err != nil {
		s.logAudit(ctx, "create", stackID, req.Name, false)
		return nil, fmt.Errorf("failed to create stack directory: %w", err)
	}

	// Encrypt the value
	encryptedValue, err := s.encrypt(req.Value)
	if err != nil {
		s.logAudit(ctx, "create", stackID, req.Name, false)
		return nil, fmt.Errorf("failed to encrypt secret: %w", err)
	}

	now := time.Now().UTC()
	stored := storedSecret{
		Name:           req.Name,
		EncryptedValue: encryptedValue,
		Description:    req.Description,
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      req.CreatedBy,
		Version:        1,
		Labels:         req.Labels,
	}

	// Save the secret
	if err := s.saveSecret(stackID, &stored); err != nil {
		s.logAudit(ctx, "create", stackID, req.Name, false)
		return nil, err
	}

	s.logAudit(ctx, "create", stackID, req.Name, true)

	return &SecretMetadata{
		Name:        stored.Name,
		Description: stored.Description,
		CreatedAt:   stored.CreatedAt,
		UpdatedAt:   stored.UpdatedAt,
		CreatedBy:   stored.CreatedBy,
		Version:     stored.Version,
		Labels:      stored.Labels,
	}, nil
}

// UpdateSecret updates an existing secret's value.
func (s *Service) UpdateSecret(ctx context.Context, stackID, name, value string) (*SecretMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load existing secret
	stored, err := s.loadSecret(stackID, name)
	if err != nil {
		s.logAudit(ctx, "update", stackID, name, false)
		return nil, err
	}

	// Save version if versioning is enabled
	if s.config.EnableVersioning {
		if err := s.saveVersion(stackID, name, stored); err != nil {
			logger.Warn("Failed to save secret version",
				"stack_id", stackID,
				"secret_name", name,
				"error", err,
			)
		}
	}

	// Encrypt the new value
	encryptedValue, err := s.encrypt(value)
	if err != nil {
		s.logAudit(ctx, "update", stackID, name, false)
		return nil, fmt.Errorf("failed to encrypt secret: %w", err)
	}

	// Update the secret
	stored.EncryptedValue = encryptedValue
	stored.UpdatedAt = time.Now().UTC()
	stored.Version++

	// Save the updated secret
	if err := s.saveSecret(stackID, stored); err != nil {
		s.logAudit(ctx, "update", stackID, name, false)
		return nil, err
	}

	s.logAudit(ctx, "update", stackID, name, true)

	return &SecretMetadata{
		Name:        stored.Name,
		Description: stored.Description,
		CreatedAt:   stored.CreatedAt,
		UpdatedAt:   stored.UpdatedAt,
		CreatedBy:   stored.CreatedBy,
		Version:     stored.Version,
		Labels:      stored.Labels,
	}, nil
}

// DeleteSecret deletes a secret.
func (s *Service) DeleteSecret(ctx context.Context, stackID, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.getSecretPath(stackID, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		s.logAudit(ctx, "delete", stackID, name, false)
		return fmt.Errorf("secret not found: %s", name)
	}

	// Remove the secret file
	if err := os.Remove(path); err != nil {
		s.logAudit(ctx, "delete", stackID, name, false)
		return fmt.Errorf("failed to delete secret: %w", err)
	}

	// Remove versions directory if it exists
	versionsDir := s.getVersionsDir(stackID, name)
	if err := os.RemoveAll(versionsDir); err != nil && !os.IsNotExist(err) {
		logger.Warn("Failed to remove secret versions",
			"stack_id", stackID,
			"secret_name", name,
			"error", err,
		)
	}

	s.logAudit(ctx, "delete", stackID, name, true)
	return nil
}

// ListVersions returns all versions of a secret.
func (s *Service) ListVersions(ctx context.Context, stackID, name string) ([]SecretVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.config.EnableVersioning {
		return nil, fmt.Errorf("versioning is not enabled")
	}

	// Check if secret exists
	if _, err := s.loadSecret(stackID, name); err != nil {
		return nil, err
	}

	versionsDir := s.getVersionsDir(stackID, name)
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SecretVersion{}, nil
		}
		return nil, fmt.Errorf("failed to read versions directory: %w", err)
	}

	var versions []SecretVersion
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		versionPath := filepath.Join(versionsDir, entry.Name())
		data, err := os.ReadFile(versionPath)
		if err != nil {
			continue
		}

		var stored storedVersion
		if err := json.Unmarshal(data, &stored); err != nil {
			continue
		}

		versions = append(versions, SecretVersion{
			Version:   stored.Version,
			CreatedAt: stored.CreatedAt,
			CreatedBy: stored.CreatedBy,
			// Value not included in list
		})
	}

	// Sort by version descending
	sort.Slice(versions, func(i, j int) bool {
		return versions[i].Version > versions[j].Version
	})

	return versions, nil
}

// GetSecretVersion retrieves a specific version of a secret.
func (s *Service) GetSecretVersion(ctx context.Context, stackID, name string, version int) (*SecretVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.config.EnableVersioning {
		return nil, fmt.Errorf("versioning is not enabled")
	}

	versionPath := filepath.Join(s.getVersionsDir(stackID, name), fmt.Sprintf("v%d.json", version))
	data, err := os.ReadFile(versionPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("version not found: %d", version)
		}
		return nil, fmt.Errorf("failed to read version: %w", err)
	}

	var stored storedVersion
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("failed to parse version: %w", err)
	}

	// Decrypt the value
	value, err := s.decrypt(stored.EncryptedValue)
	if err != nil {
		s.logAudit(ctx, "get_version", stackID, name, false)
		return nil, fmt.Errorf("failed to decrypt version: %w", err)
	}

	s.logAudit(ctx, "get_version", stackID, name, true)

	return &SecretVersion{
		Version:   stored.Version,
		Value:     value,
		CreatedAt: stored.CreatedAt,
		CreatedBy: stored.CreatedBy,
	}, nil
}

// saveSecret saves a secret to disk (caller must hold lock).
func (s *Service) saveSecret(stackID string, stored *storedSecret) error {
	path := s.getSecretPath(stackID, stored.Name)

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal secret: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to save secret: %w", err)
	}

	return nil
}

// saveVersion saves a version of a secret (caller must hold lock).
func (s *Service) saveVersion(stackID, name string, stored *storedSecret) error {
	versionsDir := s.getVersionsDir(stackID, name)
	if err := os.MkdirAll(versionsDir, 0700); err != nil {
		return fmt.Errorf("failed to create versions directory: %w", err)
	}

	version := storedVersion{
		Version:        stored.Version,
		EncryptedValue: stored.EncryptedValue,
		CreatedAt:      stored.UpdatedAt,
		CreatedBy:      stored.CreatedBy,
	}

	versionPath := filepath.Join(versionsDir, fmt.Sprintf("v%d.json", stored.Version))
	data, err := json.MarshalIndent(version, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal version: %w", err)
	}

	if err := os.WriteFile(versionPath, data, 0600); err != nil {
		return fmt.Errorf("failed to save version: %w", err)
	}

	// Clean up old versions
	s.cleanupOldVersions(versionsDir)

	return nil
}

// cleanupOldVersions removes old versions beyond the max limit.
func (s *Service) cleanupOldVersions(versionsDir string) {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		return
	}

	if len(entries) <= s.config.MaxVersions {
		return
	}

	// Sort by name (v1.json, v2.json, etc.)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	// Remove oldest versions
	toRemove := len(entries) - s.config.MaxVersions
	for i := 0; i < toRemove; i++ {
		_ = os.Remove(filepath.Join(versionsDir, entries[i].Name()))
	}
}

// UpdateMetadata updates secret metadata without changing the value.
func (s *Service) UpdateMetadata(ctx context.Context, stackID, name string, description string, labels map[string]string) (*SecretMetadata, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.loadSecret(stackID, name)
	if err != nil {
		return nil, err
	}

	// Update metadata fields
	if description != "" {
		stored.Description = description
	}
	if labels != nil {
		stored.Labels = labels
	}
	stored.UpdatedAt = time.Now().UTC()

	if err := s.saveSecret(stackID, stored); err != nil {
		return nil, err
	}

	return &SecretMetadata{
		Name:        stored.Name,
		Description: stored.Description,
		CreatedAt:   stored.CreatedAt,
		UpdatedAt:   stored.UpdatedAt,
		CreatedBy:   stored.CreatedBy,
		Version:     stored.Version,
		Labels:      stored.Labels,
	}, nil
}
