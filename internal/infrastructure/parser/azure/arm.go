package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeport/homeport/internal/domain/parser"
	"github.com/homeport/homeport/internal/domain/resource"
)

// ARMParser parses Azure Resource Manager templates.
type ARMParser struct{}

// NewARMParser creates a new ARM template parser.
func NewARMParser() *ARMParser {
	return &ARMParser{}
}

// Provider returns the cloud provider.
func (p *ARMParser) Provider() resource.Provider {
	return resource.ProviderAzure
}

// SupportedFormats returns the supported formats.
func (p *ARMParser) SupportedFormats() []parser.Format {
	return []parser.Format{parser.FormatARM}
}

// Validate checks if the path contains valid ARM templates.
func (p *ARMParser) Validate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return parser.ErrInvalidPath
	}

	if info.IsDir() {
		found := false
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if isARMTemplate(p) {
				found = true
				return filepath.SkipAll
			}
			return nil
		})
		if !found {
			return parser.ErrNoFilesFound
		}
		return nil
	}

	if !isARMTemplate(path) {
		return parser.ErrUnsupportedFormat
	}

	return nil
}

// AutoDetect checks if this parser can handle the given path.
func (p *ARMParser) AutoDetect(path string) (bool, float64) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}

	if info.IsDir() {
		armCount := 0
		totalJSON := 0
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext == ".json" {
				totalJSON++
				if isARMTemplate(p) {
					armCount++
				}
			}
			return nil
		})

		if armCount > 0 {
			confidence := float64(armCount) / float64(totalJSON) * 0.9
			return true, confidence
		}
		return false, 0
	}

	if isARMTemplate(path) {
		return true, 0.9
	}

	return false, 0
}

// Parse parses ARM templates and returns infrastructure.
func (p *ARMParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	infra := resource.NewInfrastructure(resource.ProviderAzure)

	if info.IsDir() {
		filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if isARMTemplate(filePath) {
				templateInfra, err := p.parseARMFile(filePath, opts)
				if err == nil {
					for id, res := range templateInfra.Resources {
						infra.Resources[id] = res
					}
				}
			}
			return nil
		})
	} else {
		return p.parseARMFile(path, opts)
	}

	return infra, nil
}

// parseARMFile parses a single ARM template file.
func (p *ARMParser) parseARMFile(path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var template ARMTemplate
	if err := json.Unmarshal(data, &template); err != nil {
		return nil, fmt.Errorf("failed to parse ARM template: %w", err)
	}

	infra := resource.NewInfrastructure(resource.ProviderAzure)
	infra.Metadata["source_file"] = path

	for _, res := range template.Resources {
		armRes, err := p.convertARMResource(res, opts)
		if err != nil {
			if opts != nil && opts.IgnoreErrors {
				continue
			}
			return nil, err
		}
		if armRes != nil {
			infra.AddResource(armRes)
		}
	}

	return infra, nil
}

// convertARMResource converts an ARM template resource to a domain resource.
func (p *ARMParser) convertARMResource(armRes ARMResource, opts *parser.ParseOptions) (*resource.Resource, error) {
	resType := mapARMTypeToResourceType(armRes.Type)
	if resType == "" {
		return nil, nil // Unsupported resource type
	}

	// Check filters
	if opts != nil && len(opts.FilterTypes) > 0 {
		found := false
		for _, ft := range opts.FilterTypes {
			if ft == resType {
				found = true
				break
			}
		}
		if !found {
			return nil, nil
		}
	}

	res := resource.NewAWSResource(armRes.Name, armRes.Name, resType)
	res.Region = armRes.Location

	// Copy properties to config
	if armRes.Properties != nil {
		for k, v := range armRes.Properties {
			res.Config[k] = v
		}
	}

	// Copy tags
	if armRes.Tags != nil {
		for k, v := range armRes.Tags {
			if str, ok := v.(string); ok {
				res.Tags[k] = str
			}
		}
	}

	// Handle dependencies
	for _, dep := range armRes.DependsOn {
		// Extract resource name from dependency string
		parts := strings.Split(dep, "/")
		if len(parts) > 0 {
			res.AddDependency(parts[len(parts)-1])
		}
	}

	return res, nil
}

// isARMTemplate checks if a file is an ARM template.
func isARMTemplate(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".json" {
		return false
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var template ARMTemplate
	if err := json.Unmarshal(data, &template); err != nil {
		return false
	}

	// Check for ARM template markers
	return template.Schema != "" &&
		strings.Contains(template.Schema, "deploymentTemplate.json") ||
		len(template.Resources) > 0 && template.ContentVersion != ""
}

// mapARMTypeToResourceType maps ARM resource types to our domain types.
func mapARMTypeToResourceType(armType string) resource.Type {
	armType = strings.ToLower(armType)

	mapping := map[string]resource.Type{
		// Compute
		"microsoft.compute/virtualmachines":         resource.TypeAzureVM,
		"microsoft.web/sites":                       resource.TypeAppService,
		"microsoft.containerinstance/containergroups": resource.TypeContainerInstance,
		"microsoft.containerservice/managedclusters": resource.TypeAKS,

		// Storage
		"microsoft.storage/storageaccounts": resource.TypeAzureStorageAcct,

		// Database
		"microsoft.sql/servers/databases":         resource.TypeAzureSQL,
		"microsoft.dbforpostgresql/flexibleservers": resource.TypeAzurePostgres,
		"microsoft.dbformysql/flexibleservers":    resource.TypeAzureMySQL,
		"microsoft.documentdb/databaseaccounts":   resource.TypeCosmosDB,
		"microsoft.cache/redis":                   resource.TypeAzureCache,

		// Networking
		"microsoft.network/loadbalancers":         resource.TypeAzureLB,
		"microsoft.network/applicationgateways":   resource.TypeAppGateway,
		"microsoft.network/dnszones":              resource.TypeAzureDNS,
		"microsoft.cdn/profiles":                  resource.TypeAzureCDN,
		"microsoft.network/frontdoors":            resource.TypeFrontDoor,
		"microsoft.network/virtualnetworks":       resource.TypeAzureVNet,

		// Security
		"microsoft.keyvault/vaults": resource.TypeKeyVault,

		// Messaging
		"microsoft.servicebus/namespaces":     resource.TypeServiceBus,
		"microsoft.eventhub/namespaces":       resource.TypeEventHub,
		"microsoft.eventgrid/topics":          resource.TypeEventGrid,
		"microsoft.logic/workflows":           resource.TypeLogicApp,
	}

	for prefix, resType := range mapping {
		if strings.HasPrefix(armType, prefix) {
			return resType
		}
	}

	return ""
}

// ARMTemplate represents an ARM template structure.
type ARMTemplate struct {
	Schema         string                 `json:"$schema"`
	ContentVersion string                 `json:"contentVersion"`
	Parameters     map[string]interface{} `json:"parameters"`
	Variables      map[string]interface{} `json:"variables"`
	Resources      []ARMResource          `json:"resources"`
	Outputs        map[string]interface{} `json:"outputs"`
}

// ARMResource represents a resource in an ARM template.
type ARMResource struct {
	Type       string                 `json:"type"`
	APIVersion string                 `json:"apiVersion"`
	Name       string                 `json:"name"`
	Location   string                 `json:"location"`
	DependsOn  []string               `json:"dependsOn"`
	Tags       map[string]interface{} `json:"tags"`
	Properties map[string]interface{} `json:"properties"`
	Resources  []ARMResource          `json:"resources"` // Nested resources
	SKU        *ARMSKU                `json:"sku"`
	Kind       string                 `json:"kind"`
}

// ARMSKU represents SKU information in ARM.
type ARMSKU struct {
	Name     string `json:"name"`
	Tier     string `json:"tier"`
	Size     string `json:"size"`
	Family   string `json:"family"`
	Capacity int    `json:"capacity"`
}
