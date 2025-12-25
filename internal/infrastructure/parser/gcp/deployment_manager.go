package gcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/agnostech/agnostech/internal/domain/parser"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// DeploymentManagerParser parses GCP Deployment Manager templates.
type DeploymentManagerParser struct{}

// NewDeploymentManagerParser creates a new GCP Deployment Manager parser.
func NewDeploymentManagerParser() *DeploymentManagerParser {
	return &DeploymentManagerParser{}
}

// Provider returns the cloud provider.
func (p *DeploymentManagerParser) Provider() resource.Provider {
	return resource.ProviderGCP
}

// SupportedFormats returns the supported formats.
func (p *DeploymentManagerParser) SupportedFormats() []parser.Format {
	return []parser.Format{parser.FormatDeploymentManager}
}

// Validate checks if the path contains valid Deployment Manager templates.
func (p *DeploymentManagerParser) Validate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return parser.ErrInvalidPath
	}

	if info.IsDir() {
		found := false
		err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if !info.IsDir() && p.isDeploymentManagerFile(filePath) {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if err != nil {
			return err
		}
		if !found {
			return parser.ErrNoFilesFound
		}
		return nil
	}

	if !p.isDeploymentManagerFile(path) {
		return parser.ErrUnsupportedFormat
	}

	return nil
}

// AutoDetect checks if this parser can handle the given path.
func (p *DeploymentManagerParser) AutoDetect(path string) (bool, float64) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}

	if info.IsDir() {
		dmCount := 0
		filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if p.isDeploymentManagerFile(filePath) {
				dmCount++
			}
			return nil
		})

		if dmCount > 0 {
			return true, 0.85
		}
		return false, 0
	}

	if p.isDeploymentManagerFile(path) {
		return true, 0.9
	}

	return false, 0
}

// Parse parses Deployment Manager templates and returns GCP infrastructure.
func (p *DeploymentManagerParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	if opts == nil {
		opts = parser.NewParseOptions()
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	infra := resource.NewInfrastructure(resource.ProviderGCP)
	infra.Metadata["format"] = "deployment_manager"

	if info.IsDir() {
		err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if p.isDeploymentManagerFile(filePath) {
				if parseErr := p.parseDeploymentFile(filePath, infra, opts); parseErr != nil {
					if opts.IgnoreErrors {
						return nil
					}
					return parseErr
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		if err := p.parseDeploymentFile(path, infra, opts); err != nil {
			return nil, err
		}
	}

	return infra, nil
}

// isDeploymentManagerFile checks if a file is a Deployment Manager template.
func (p *DeploymentManagerParser) isDeploymentManagerFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".yaml" && ext != ".yml" {
		return false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	content := string(data)

	// Check for Deployment Manager markers
	return strings.Contains(content, "resources:") &&
		(strings.Contains(content, "type: ") || strings.Contains(content, "properties:")) &&
		(strings.Contains(content, "compute.v1") ||
			strings.Contains(content, "storage.v1") ||
			strings.Contains(content, "sqladmin.v1") ||
			strings.Contains(content, "$(ref.") ||
			strings.Contains(content, "gcp-types/"))
}

// parseDeploymentFile parses a single Deployment Manager file.
func (p *DeploymentManagerParser) parseDeploymentFile(path string, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var template DeploymentManagerTemplate
	if err := yaml.Unmarshal(data, &template); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	for _, dmRes := range template.Resources {
		res := p.convertResource(dmRes, path)

		if !p.shouldIncludeResource(res, opts) {
			continue
		}

		infra.AddResource(res)
	}

	// Store outputs in metadata
	for _, output := range template.Outputs {
		key := fmt.Sprintf("output.%s", output.Name)
		infra.Metadata[key] = fmt.Sprintf("%v", output.Value)
	}

	return nil
}

// convertResource converts a Deployment Manager resource to our Resource model.
func (p *DeploymentManagerParser) convertResource(dmRes DMResource, sourcePath string) *resource.Resource {
	resourceID := dmRes.Name
	resourceType := mapDMTypeToResourceType(dmRes.Type)

	res := resource.NewAWSResource(resourceID, dmRes.Name, resourceType)
	res.Config = dmRes.Properties
	res.Config["dm_type"] = dmRes.Type
	res.Config["source_file"] = filepath.Base(sourcePath)

	// Extract region/zone from properties
	if zone, ok := dmRes.Properties["zone"].(string); ok {
		parts := strings.Split(zone, "-")
		if len(parts) >= 3 {
			res.Region = strings.Join(parts[:len(parts)-1], "-")
		}
	} else if region, ok := dmRes.Properties["region"].(string); ok {
		res.Region = region
	} else if location, ok := dmRes.Properties["location"].(string); ok {
		res.Region = location
	}

	// Extract labels
	if labels, ok := dmRes.Properties["labels"].(map[string]interface{}); ok {
		for k, v := range labels {
			if strVal, ok := v.(string); ok {
				res.Tags[k] = strVal
			}
		}
	}

	// Extract dependencies from $(ref.resourceName) syntax
	deps := p.extractDependencies(dmRes.Properties)
	for _, dep := range deps {
		res.AddDependency(dep)
	}

	return res
}

// extractDependencies extracts resource references from properties.
func (p *DeploymentManagerParser) extractDependencies(props map[string]interface{}) []string {
	var deps []string
	refPattern := regexp.MustCompile(`\$\(ref\.([^.]+)\.`)

	var extractFromValue func(interface{})
	extractFromValue = func(val interface{}) {
		switch v := val.(type) {
		case string:
			matches := refPattern.FindAllStringSubmatch(v, -1)
			for _, match := range matches {
				if len(match) > 1 {
					deps = append(deps, match[1])
				}
			}
		case map[string]interface{}:
			for _, nestedVal := range v {
				extractFromValue(nestedVal)
			}
		case []interface{}:
			for _, item := range v {
				extractFromValue(item)
			}
		}
	}

	for _, val := range props {
		extractFromValue(val)
	}

	// Deduplicate
	seen := make(map[string]bool)
	unique := make([]string, 0)
	for _, dep := range deps {
		if !seen[dep] {
			seen[dep] = true
			unique = append(unique, dep)
		}
	}

	return unique
}

// shouldIncludeResource checks if a resource matches the filter criteria.
func (p *DeploymentManagerParser) shouldIncludeResource(res *resource.Resource, opts *parser.ParseOptions) bool {
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

// mapDMTypeToResourceType maps Deployment Manager types to our resource types.
func mapDMTypeToResourceType(dmType string) resource.Type {
	// Normalize the type
	dmType = strings.ToLower(dmType)

	mapping := map[string]resource.Type{
		// Compute
		"compute.v1.instance":        resource.TypeGCEInstance,
		"compute.v1.instancetemplate": resource.TypeGCEInstance,
		"compute.beta.instance":      resource.TypeGCEInstance,

		// Storage
		"storage.v1.bucket":         resource.TypeGCSBucket,
		"compute.v1.disk":           resource.TypePersistentDisk,

		// Database
		"sqladmin.v1beta4.instance": resource.TypeCloudSQL,
		"sqladmin.v1.instance":      resource.TypeCloudSQL,
		"spanner.v1.instance":       resource.TypeSpanner,
		"redis.v1.instance":         resource.TypeMemorystore,

		// Networking
		"compute.v1.network":          resource.TypeGCPVPCNetwork,
		"compute.v1.subnetwork":       resource.TypeGCPVPCNetwork,
		"compute.v1.globaladdress":    resource.TypeCloudLB,
		"compute.v1.backendservice":   resource.TypeCloudLB,
		"compute.v1.forwardingrule":   resource.TypeCloudLB,
		"dns.v1.managedzone":          resource.TypeCloudDNS,

		// Security
		"iam.v1.serviceaccount":      resource.TypeGCPIAM,
		"compute.v1.firewall":        resource.TypeCloudArmor,
		"secretmanager.v1.secret":    resource.TypeSecretManager,

		// Messaging
		"pubsub.v1.topic":            resource.TypePubSubTopic,
		"pubsub.v1.subscription":     resource.TypePubSubSubscription,
		"cloudscheduler.v1.job":      resource.TypeCloudScheduler,
		"cloudtasks.v2.queue":        resource.TypeCloudTasks,

		// Containers
		"container.v1.cluster":       resource.TypeGKE,
	}

	// Try direct match
	if resType, ok := mapping[dmType]; ok {
		return resType
	}

	// Try prefix match for gcp-types format
	for pattern, resType := range mapping {
		if strings.Contains(dmType, pattern) {
			return resType
		}
	}

	return resource.Type(dmType)
}

// DeploymentManagerTemplate represents a Deployment Manager template.
type DeploymentManagerTemplate struct {
	Imports   []DMImport   `yaml:"imports,omitempty"`
	Resources []DMResource `yaml:"resources"`
	Outputs   []DMOutput   `yaml:"outputs,omitempty"`
}

// DMImport represents an import in a Deployment Manager template.
type DMImport struct {
	Path string `yaml:"path"`
	Name string `yaml:"name,omitempty"`
}

// DMResource represents a resource in a Deployment Manager template.
type DMResource struct {
	Name       string                 `yaml:"name"`
	Type       string                 `yaml:"type"`
	Properties map[string]interface{} `yaml:"properties,omitempty"`
	Metadata   map[string]interface{} `yaml:"metadata,omitempty"`
}

// DMOutput represents an output in a Deployment Manager template.
type DMOutput struct {
	Name  string      `yaml:"name"`
	Value interface{} `yaml:"value"`
}
