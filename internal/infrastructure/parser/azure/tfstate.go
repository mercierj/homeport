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

// TFStateParser parses Terraform state files for Azure resources.
type TFStateParser struct{}

// NewTFStateParser creates a new Azure TFState parser.
func NewTFStateParser() *TFStateParser {
	return &TFStateParser{}
}

// Provider returns the cloud provider.
func (p *TFStateParser) Provider() resource.Provider {
	return resource.ProviderAzure
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
		if !p.hasAzureResources(statePath) {
			return parser.ErrUnsupportedFormat
		}
		return nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".tfstate" {
		return parser.ErrUnsupportedFormat
	}

	if !p.hasAzureResources(path) {
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

	azureCount := 0
	totalCount := 0

	for _, res := range state.Resources {
		if res.Mode == "managed" {
			totalCount++
			if strings.HasPrefix(res.Type, "azurerm_") {
				azureCount++
			}
		}
	}

	if azureCount > 0 {
		confidence := float64(azureCount) / float64(totalCount)
		if confidence > 0.9 {
			return true, 0.95
		}
		return true, confidence * 0.85
	}

	return false, 0
}

// Parse parses Terraform state files and returns Azure infrastructure.
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

// hasAzureResources checks if a state file contains Azure resources.
func (p *TFStateParser) hasAzureResources(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var state TerraformState
	if err := json.Unmarshal(data, &state); err != nil {
		return false
	}

	for _, res := range state.Resources {
		if strings.HasPrefix(res.Type, "azurerm_") {
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
	infra := resource.NewInfrastructure(resource.ProviderAzure)

	infra.Metadata["terraform_version"] = state.TerraformVersion
	infra.Metadata["state_version"] = fmt.Sprintf("%d", state.Version)

	for _, stateRes := range state.Resources {
		if stateRes.Mode != "managed" {
			continue
		}

		if !strings.HasPrefix(stateRes.Type, "azurerm_") {
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
	}

	resourceType := mapAzureTerraformType(stateRes.Type)
	res := resource.NewAWSResource(resourceID, name, resourceType)

	if azureID, ok := instance.Attributes["id"].(string); ok {
		res.ARN = azureID
	}

	if location, ok := instance.Attributes["location"].(string); ok {
		res.Region = location
	}

	res.Config = instance.Attributes

	if tags, ok := instance.Attributes["tags"].(map[string]interface{}); ok {
		for k, v := range tags {
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

// mapAzureTerraformType maps Azure Terraform types to our resource types.
func mapAzureTerraformType(tfType string) resource.Type {
	mapping := map[string]resource.Type{
		// Compute
		"azurerm_linux_virtual_machine":   resource.TypeAzureVM,
		"azurerm_windows_virtual_machine": resource.TypeAzureVMWindows,
		"azurerm_virtual_machine":         resource.TypeAzureVM,
		"azurerm_function_app":            resource.TypeAzureFunction,
		"azurerm_linux_function_app":      resource.TypeAzureFunction,
		"azurerm_windows_function_app":    resource.TypeAzureFunction,
		"azurerm_container_group":         resource.TypeContainerInstance,
		"azurerm_kubernetes_cluster":      resource.TypeAKS,
		"azurerm_app_service":             resource.TypeAppService,
		"azurerm_linux_web_app":           resource.TypeAppService,
		"azurerm_windows_web_app":         resource.TypeAppService,

		// Storage
		"azurerm_storage_account":   resource.TypeAzureStorageAcct,
		"azurerm_storage_container": resource.TypeBlobStorage,
		"azurerm_managed_disk":      resource.TypeManagedDisk,
		"azurerm_storage_share":     resource.TypeAzureFiles,

		// Database
		"azurerm_mssql_database":             resource.TypeAzureSQL,
		"azurerm_sql_database":               resource.TypeAzureSQL,
		"azurerm_postgresql_flexible_server": resource.TypeAzurePostgres,
		"azurerm_mysql_flexible_server":      resource.TypeAzureMySQL,
		"azurerm_cosmosdb_account":           resource.TypeCosmosDB,
		"azurerm_redis_cache":                resource.TypeAzureCache,

		// Networking
		"azurerm_lb":                  resource.TypeAzureLB,
		"azurerm_application_gateway": resource.TypeAppGateway,
		"azurerm_dns_zone":            resource.TypeAzureDNS,
		"azurerm_cdn_profile":         resource.TypeAzureCDN,
		"azurerm_frontdoor":           resource.TypeFrontDoor,
		"azurerm_virtual_network":     resource.TypeAzureVNet,

		// Security
		"azurerm_key_vault":        resource.TypeKeyVault,
		"azurerm_aadb2c_directory": resource.TypeAzureADB2C,
		"azurerm_firewall":         resource.TypeAzureFirewall,

		// Messaging
		"azurerm_servicebus_namespace": resource.TypeServiceBus,
		"azurerm_servicebus_queue":     resource.TypeServiceBusQueue,
		"azurerm_eventhub":             resource.TypeEventHub,
		"azurerm_eventhub_namespace":   resource.TypeEventHub,
		"azurerm_eventgrid_topic":      resource.TypeEventGrid,
		"azurerm_logic_app_workflow":   resource.TypeLogicApp,
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
