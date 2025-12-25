package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudexit/cloudexit/internal/domain/parser"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// TFStateParser parses Terraform state files for GCP resources.
type TFStateParser struct{}

// NewTFStateParser creates a new GCP TFState parser.
func NewTFStateParser() *TFStateParser {
	return &TFStateParser{}
}

// Provider returns the cloud provider.
func (p *TFStateParser) Provider() resource.Provider {
	return resource.ProviderGCP
}

// SupportedFormats returns the supported formats.
func (p *TFStateParser) SupportedFormats() []parser.Format {
	return []parser.Format{parser.FormatTFState}
}

// Validate checks if the path contains valid Terraform state files.
func (p *TFStateParser) Validate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return parser.ErrInvalidPath
	}

	if info.IsDir() {
		statePath := filepath.Join(path, "terraform.tfstate")
		if _, err := os.Stat(statePath); err != nil {
			return parser.ErrNoFilesFound
		}
		if !p.hasGCPResources(statePath) {
			return parser.ErrUnsupportedFormat
		}
		return nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".tfstate" {
		return parser.ErrUnsupportedFormat
	}

	if !p.hasGCPResources(path) {
		return parser.ErrUnsupportedFormat
	}

	return nil
}

// AutoDetect checks if this parser can handle the given path.
func (p *TFStateParser) AutoDetect(path string) (bool, float64) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}

	var stateFile string
	if info.IsDir() {
		stateFile = filepath.Join(path, "terraform.tfstate")
		if _, err := os.Stat(stateFile); err != nil {
			return false, 0
		}
	} else {
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".tfstate" {
			return false, 0
		}
		stateFile = path
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		return false, 0
	}

	var state TerraformState
	if err := json.Unmarshal(data, &state); err != nil {
		return false, 0
	}

	gcpCount := 0
	totalCount := 0

	for _, res := range state.Resources {
		if res.Mode == "managed" {
			totalCount++
			if strings.HasPrefix(res.Type, "google_") {
				gcpCount++
			}
		}
	}

	if gcpCount > 0 {
		confidence := float64(gcpCount) / float64(totalCount)
		if confidence > 0.9 {
			return true, 0.95
		}
		return true, confidence * 0.85
	}

	return false, 0
}

// Parse parses Terraform state files and returns GCP infrastructure.
func (p *TFStateParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	if opts == nil {
		opts = parser.NewParseOptions()
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	var stateFile string
	if info.IsDir() {
		stateFile = filepath.Join(path, "terraform.tfstate")
	} else {
		stateFile = path
	}

	state, err := p.parseStateFile(stateFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	infra := p.buildInfrastructure(state, opts)

	return infra, nil
}

// hasGCPResources checks if a state file contains GCP resources.
func (p *TFStateParser) hasGCPResources(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var state TerraformState
	if err := json.Unmarshal(data, &state); err != nil {
		return false
	}

	for _, res := range state.Resources {
		if strings.HasPrefix(res.Type, "google_") {
			return true
		}
	}

	return false
}

// parseStateFile parses a Terraform state file.
func (p *TFStateParser) parseStateFile(path string) (*TerraformState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state TerraformState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	if state.Version < 3 || state.Version > 4 {
		return nil, fmt.Errorf("unsupported state version: %d (supported: 3-4)", state.Version)
	}

	return &state, nil
}

// buildInfrastructure converts a Terraform state to Infrastructure.
func (p *TFStateParser) buildInfrastructure(state *TerraformState, opts *parser.ParseOptions) *resource.Infrastructure {
	infra := resource.NewInfrastructure(resource.ProviderGCP)

	infra.Metadata["terraform_version"] = state.TerraformVersion
	infra.Metadata["state_version"] = fmt.Sprintf("%d", state.Version)

	for _, stateRes := range state.Resources {
		if stateRes.Mode != "managed" {
			continue
		}

		if !strings.HasPrefix(stateRes.Type, "google_") {
			continue
		}

		for _, instance := range stateRes.Instances {
			res := p.convertResource(stateRes, instance)

			if !p.shouldIncludeResource(res, opts) {
				continue
			}

			infra.AddResource(res)
		}
	}

	return infra
}

// convertResource converts a state resource to our Resource model.
func (p *TFStateParser) convertResource(stateRes StateResource, instance ResourceInstance) *resource.Resource {
	resourceID := fmt.Sprintf("%s.%s", stateRes.Type, stateRes.Name)
	if instance.IndexKey != nil {
		resourceID = fmt.Sprintf("%s[%v]", resourceID, instance.IndexKey)
	}

	name := stateRes.Name
	if nameAttr, ok := instance.Attributes["name"].(string); ok && nameAttr != "" {
		name = nameAttr
	} else if labels, ok := instance.Attributes["labels"].(map[string]interface{}); ok {
		if nameLabel, ok := labels["name"].(string); ok && nameLabel != "" {
			name = nameLabel
		}
	}

	resourceType := mapGCPTerraformType(stateRes.Type)
	res := resource.NewAWSResource(resourceID, name, resourceType)

	if selfLink, ok := instance.Attributes["self_link"].(string); ok {
		res.ARN = selfLink
	} else if id, ok := instance.Attributes["id"].(string); ok {
		res.ARN = id
	}

	if region, ok := instance.Attributes["region"].(string); ok {
		res.Region = region
	} else if zone, ok := instance.Attributes["zone"].(string); ok {
		parts := strings.Split(zone, "-")
		if len(parts) >= 3 {
			res.Region = strings.Join(parts[:len(parts)-1], "-")
		}
	} else if location, ok := instance.Attributes["location"].(string); ok {
		res.Region = location
	}

	res.Config = instance.Attributes

	if labels, ok := instance.Attributes["labels"].(map[string]interface{}); ok {
		for k, v := range labels {
			if strVal, ok := v.(string); ok {
				res.Tags[k] = strVal
			}
		}
	}

	for _, dep := range instance.Dependencies {
		cleanDep := strings.TrimPrefix(dep, "module.")
		res.AddDependency(cleanDep)
	}

	return res
}

// shouldIncludeResource checks if a resource matches the filter criteria.
func (p *TFStateParser) shouldIncludeResource(res *resource.Resource, opts *parser.ParseOptions) bool {
	if len(opts.FilterTypes) > 0 {
		found := false
		for _, ft := range opts.FilterTypes {
			if ft == res.Type {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	if len(opts.FilterCategories) > 0 {
		category := res.Type.GetCategory()
		found := false
		for _, fc := range opts.FilterCategories {
			if fc == category {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// mapGCPTerraformType maps GCP Terraform types to our resource types.
func mapGCPTerraformType(tfType string) resource.Type {
	mapping := map[string]resource.Type{
		// Compute
		"google_compute_instance":          resource.TypeGCEInstance,
		"google_cloud_run_service":         resource.TypeCloudRun,
		"google_cloudfunctions_function":   resource.TypeCloudFunction,
		"google_container_cluster":         resource.TypeGKE,
		"google_app_engine_application":    resource.TypeAppEngine,

		// Storage
		"google_storage_bucket":    resource.TypeGCSBucket,
		"google_compute_disk":      resource.TypePersistentDisk,
		"google_filestore_instance": resource.TypeFilestore,

		// Database
		"google_sql_database_instance": resource.TypeCloudSQL,
		"google_firestore_database":    resource.TypeFirestore,
		"google_bigtable_instance":     resource.TypeBigtable,
		"google_redis_instance":        resource.TypeMemorystore,
		"google_spanner_instance":      resource.TypeSpanner,

		// Networking
		"google_compute_backend_service":  resource.TypeCloudLB,
		"google_dns_managed_zone":         resource.TypeCloudDNS,
		"google_compute_backend_bucket":   resource.TypeCloudCDN,
		"google_compute_security_policy":  resource.TypeCloudArmor,
		"google_compute_network":          resource.TypeGCPVPCNetwork,

		// Security
		"google_identity_platform_config": resource.TypeIdentityPlatform,
		"google_secret_manager_secret":    resource.TypeSecretManager,
		"google_project_iam_member":       resource.TypeGCPIAM,

		// Messaging
		"google_pubsub_topic":         resource.TypePubSubTopic,
		"google_pubsub_subscription":  resource.TypePubSubSubscription,
		"google_cloud_tasks_queue":    resource.TypeCloudTasks,
		"google_cloud_scheduler_job":  resource.TypeCloudScheduler,
	}

	if resType, ok := mapping[tfType]; ok {
		return resType
	}
	return resource.Type(tfType)
}

// TerraformState represents a Terraform state file.
type TerraformState struct {
	Version          int                   `json:"version"`
	TerraformVersion string                `json:"terraform_version"`
	Resources        []StateResource       `json:"resources"`
	Outputs          map[string]StateOutput `json:"outputs,omitempty"`
}

// StateResource represents a resource in Terraform state.
type StateResource struct {
	Mode      string             `json:"mode"`
	Type      string             `json:"type"`
	Name      string             `json:"name"`
	Provider  string             `json:"provider"`
	Instances []ResourceInstance `json:"instances"`
	Module    string             `json:"module,omitempty"`
}

// ResourceInstance represents an instance of a resource.
type ResourceInstance struct {
	SchemaVersion int                    `json:"schema_version"`
	Attributes    map[string]interface{} `json:"attributes"`
	Dependencies  []string               `json:"dependencies,omitempty"`
	Private       string                 `json:"private,omitempty"`
	IndexKey      interface{}            `json:"index_key,omitempty"`
}

// StateOutput represents a Terraform output.
type StateOutput struct {
	Value     interface{} `json:"value"`
	Type      interface{} `json:"type"`
	Sensitive bool        `json:"sensitive,omitempty"`
}
