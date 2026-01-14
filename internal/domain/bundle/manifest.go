package bundle

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Manifest contains bundle metadata, checksums, and deployment information.
type Manifest struct {
	Version         string            `json:"version"`
	Format          string            `json:"format"`
	Created         time.Time         `json:"created"`
	HomeportVersion string            `json:"homeport_version"`
	Source          *SourceInfo       `json:"source"`
	Target          *TargetInfo       `json:"target"`
	Stacks          []*StackInfo      `json:"stacks"`
	Checksums       map[string]string `json:"checksums"`
	DataSync        *DataSyncInfo     `json:"data_sync,omitempty"`
	Dependencies    *Dependencies     `json:"dependencies,omitempty"`
	Rollback        *RollbackInfo     `json:"rollback,omitempty"`
	Secrets         *SecretsManifest  `json:"secrets,omitempty"`
}

// SourceInfo describes the cloud source being migrated.
type SourceInfo struct {
	Provider      string    `json:"provider"`
	Region        string    `json:"region,omitempty"`
	AccountID     string    `json:"account_id,omitempty"`
	ProjectID     string    `json:"project_id,omitempty"`
	Subscription  string    `json:"subscription,omitempty"`
	ResourceCount int       `json:"resource_count"`
	AnalyzedAt    time.Time `json:"analyzed_at"`
}

// TargetInfo describes the target deployment.
type TargetInfo struct {
	Type          string `json:"type"`
	Consolidation bool   `json:"consolidation"`
	StackCount    int    `json:"stack_count"`
	Host          string `json:"host,omitempty"`
	SSHUser       string `json:"ssh_user,omitempty"`
	Domain        string `json:"domain,omitempty"`
}

// StackInfo describes a consolidated stack in the bundle.
type StackInfo struct {
	Name                  string   `json:"name"`
	Type                  string   `json:"type"`
	Services              []string `json:"services"`
	ResourcesConsolidated int      `json:"resources_consolidated"`
	DataSyncRequired      bool     `json:"data_sync_required"`
	EstimatedSyncSize     string   `json:"estimated_sync_size,omitempty"`
	DependsOn             []string `json:"depends_on,omitempty"`
	ComposeFile           string   `json:"compose_file,omitempty"`
}

// DataSyncInfo describes data synchronization requirements.
type DataSyncInfo struct {
	TotalEstimatedSize string       `json:"total_estimated_size"`
	Databases          []string     `json:"databases,omitempty"`
	Storage            []string     `json:"storage,omitempty"`
	Caches             []string     `json:"caches,omitempty"`
	EstimatedDuration  string       `json:"estimated_duration,omitempty"`
	Tasks              []*SyncTask  `json:"tasks,omitempty"`
}

// SyncTask describes a single data synchronization task.
type SyncTask struct {
	ID             string    `json:"id"`
	Type           string    `json:"type"`
	SourceEndpoint *Endpoint `json:"source_endpoint"`
	TargetEndpoint *Endpoint `json:"target_endpoint"`
	Strategy       string    `json:"strategy"`
	Priority       int       `json:"priority"`
	Dependencies   []string  `json:"dependencies,omitempty"`
}

// Endpoint describes a connection endpoint for sync.
type Endpoint struct {
	Type      string `json:"type"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	Database  string `json:"database,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Path      string `json:"path,omitempty"`
	SecretRef string `json:"secret_ref,omitempty"`
}

// Dependencies lists required tools and versions.
type Dependencies struct {
	Docker        string            `json:"docker,omitempty"`
	DockerCompose string            `json:"docker-compose,omitempty"`
	Tools         map[string]string `json:"tools,omitempty"`
}

// RollbackInfo describes rollback capabilities.
type RollbackInfo struct {
	Supported        bool   `json:"supported"`
	SnapshotRequired bool   `json:"snapshot_required"`
	MaxRollbackTime  string `json:"max_rollback_time,omitempty"`
	PreserveData     bool   `json:"preserve_data"`
}

// SecretsManifest describes secret references (NO VALUES).
type SecretsManifest struct {
	Secrets     []*SecretReference `json:"secrets"`
	EnvTemplate string             `json:"env_template,omitempty"`
}

// SecretReference describes a secret that must be provided at import time.
// IMPORTANT: This contains references only, never actual secret values.
type SecretReference struct {
	Name        string   `json:"name"`
	Source      string   `json:"source"`
	Key         string   `json:"key,omitempty"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required"`
	UsedBy      []string `json:"used_by,omitempty"`
}

// SecretSource represents the source type for a secret.
type SecretSource string

const (
	// SecretSourceManual requires user to provide the value.
	SecretSourceManual SecretSource = "manual"

	// SecretSourceEnv reads from environment variable.
	SecretSourceEnv SecretSource = "env"

	// SecretSourceFile reads from a file.
	SecretSourceFile SecretSource = "file"

	// SecretSourceAWSSecretsManager pulls from AWS Secrets Manager.
	SecretSourceAWSSecretsManager SecretSource = "aws-secrets-manager"

	// SecretSourceGCPSecretManager pulls from GCP Secret Manager.
	SecretSourceGCPSecretManager SecretSource = "gcp-secret-manager"

	// SecretSourceAzureKeyVault pulls from Azure Key Vault.
	SecretSourceAzureKeyVault SecretSource = "azure-key-vault"

	// SecretSourceHashiCorpVault pulls from HashiCorp Vault.
	SecretSourceHashiCorpVault SecretSource = "hashicorp-vault"
)

// ManifestVersion is the current manifest schema version.
const ManifestVersion = "1.0.0"

// BundleFormat is the bundle format identifier.
const BundleFormat = "hprt"

// NewManifest creates a new manifest with defaults.
func NewManifest() *Manifest {
	return &Manifest{
		Version:         "1.0.0",
		Format:          "hprt",
		Created:         time.Now().UTC(),
		HomeportVersion: "0.1.0",
		Checksums:       make(map[string]string),
		Stacks:          make([]*StackInfo, 0),
	}
}

// Validate checks if the manifest is valid.
func (m *Manifest) Validate() error {
	if m.Version == "" {
		return errors.New("manifest version is required")
	}
	if m.Format != "hprt" {
		return fmt.Errorf("invalid format: expected 'hprt', got '%s'", m.Format)
	}
	if m.Source == nil {
		return errors.New("source info is required")
	}
	if m.Source.Provider == "" {
		return errors.New("source provider is required")
	}
	if m.Target == nil {
		return errors.New("target info is required")
	}
	return nil
}

// ToJSON serializes the manifest to JSON.
func (m *Manifest) ToJSON() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// FromJSON deserializes a manifest from JSON.
func FromJSON(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	return &m, nil
}

// AddStack adds a stack to the manifest.
func (m *Manifest) AddStack(stack *StackInfo) {
	m.Stacks = append(m.Stacks, stack)
}

// AddSecretReference adds a secret reference to the manifest.
func (m *Manifest) AddSecretReference(ref *SecretReference) {
	if m.Secrets == nil {
		m.Secrets = &SecretsManifest{
			Secrets: make([]*SecretReference, 0),
		}
	}
	m.Secrets.Secrets = append(m.Secrets.Secrets, ref)
}

// GetRequiredSecrets returns all required secret references.
func (m *Manifest) GetRequiredSecrets() []*SecretReference {
	if m.Secrets == nil {
		return nil
	}
	var required []*SecretReference
	for _, s := range m.Secrets.Secrets {
		if s.Required {
			required = append(required, s)
		}
	}
	return required
}

// GetStack returns a stack by name.
func (m *Manifest) GetStack(name string) (*StackInfo, bool) {
	for _, s := range m.Stacks {
		if s.Name == name {
			return s, true
		}
	}
	return nil, false
}

// HasDataSync returns true if any stack requires data synchronization.
func (m *Manifest) HasDataSync() bool {
	for _, s := range m.Stacks {
		if s.DataSyncRequired {
			return true
		}
	}
	return false
}

// SetDependencies sets the tool dependencies.
func (m *Manifest) SetDependencies(deps *Dependencies) {
	m.Dependencies = deps
}

// SetRollback sets the rollback configuration.
func (m *Manifest) SetRollback(rb *RollbackInfo) {
	m.Rollback = rb
}

// SetDataSync sets the data sync configuration.
func (m *Manifest) SetDataSync(ds *DataSyncInfo) {
	m.DataSync = ds
}
