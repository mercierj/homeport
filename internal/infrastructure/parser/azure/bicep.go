package azure

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/agnostech/agnostech/internal/domain/parser"
	"github.com/agnostech/agnostech/internal/domain/resource"
)

// BicepParser parses Azure Bicep (.bicep) files.
type BicepParser struct{}

// NewBicepParser creates a new Azure Bicep parser.
func NewBicepParser() *BicepParser {
	return &BicepParser{}
}

// Provider returns the cloud provider.
func (p *BicepParser) Provider() resource.Provider {
	return resource.ProviderAzure
}

// SupportedFormats returns the supported formats.
func (p *BicepParser) SupportedFormats() []parser.Format {
	return []parser.Format{parser.FormatBicep}
}

// Validate checks if the path contains valid Bicep files.
func (p *BicepParser) Validate(path string) error {
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
			if !info.IsDir() && strings.HasSuffix(strings.ToLower(filePath), ".bicep") {
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

	if !strings.HasSuffix(strings.ToLower(path), ".bicep") {
		return parser.ErrUnsupportedFormat
	}

	return nil
}

// AutoDetect checks if this parser can handle the given path.
func (p *BicepParser) AutoDetect(path string) (bool, float64) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}

	if info.IsDir() {
		bicepCount := 0
		filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(filePath), ".bicep") {
				bicepCount++
			}
			return nil
		})

		if bicepCount > 0 {
			return true, 0.9
		}
		return false, 0
	}

	if strings.HasSuffix(strings.ToLower(path), ".bicep") {
		return true, 0.95
	}

	return false, 0
}

// Parse parses Bicep files and returns Azure infrastructure.
func (p *BicepParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	if opts == nil {
		opts = parser.NewParseOptions()
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	infra := resource.NewInfrastructure(resource.ProviderAzure)
	infra.Metadata["format"] = "bicep"

	if info.IsDir() {
		err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(filePath), ".bicep") {
				if parseErr := p.parseBicepFile(filePath, infra, opts); parseErr != nil {
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
		if err := p.parseBicepFile(path, infra, opts); err != nil {
			return nil, err
		}
	}

	return infra, nil
}

// parseBicepFile parses a single Bicep file.
func (p *BicepParser) parseBicepFile(path string, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	content := string(data)

	// Parse parameters
	params := p.parseParameters(content)
	for name, value := range params {
		infra.Metadata[fmt.Sprintf("param.%s", name)] = value
	}

	// Parse variables
	vars := p.parseVariables(content)
	for name, value := range vars {
		infra.Metadata[fmt.Sprintf("var.%s", name)] = value
	}

	// Parse resources
	resources := p.parseResources(content, path)
	for _, res := range resources {
		if !p.shouldIncludeResource(res, opts) {
			continue
		}
		infra.AddResource(res)
	}

	// Parse outputs
	outputs := p.parseOutputs(content)
	for name, value := range outputs {
		infra.Metadata[fmt.Sprintf("output.%s", name)] = value
	}

	return nil
}

// parseParameters parses param declarations from Bicep content.
func (p *BicepParser) parseParameters(content string) map[string]string {
	params := make(map[string]string)

	// Pattern: param <name> <type> = <default>
	// Or: param <name> <type>
	paramPattern := regexp.MustCompile(`(?m)^param\s+(\w+)\s+(\w+)(?:\s*=\s*(.+?))?$`)
	matches := paramPattern.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) >= 3 {
			name := match[1]
			paramType := match[2]
			defaultValue := ""
			if len(match) >= 4 && match[3] != "" {
				defaultValue = strings.TrimSpace(match[3])
			}
			params[name] = fmt.Sprintf("type=%s, default=%s", paramType, defaultValue)
		}
	}

	return params
}

// parseVariables parses var declarations from Bicep content.
func (p *BicepParser) parseVariables(content string) map[string]string {
	vars := make(map[string]string)

	// Pattern: var <name> = <expression>
	varPattern := regexp.MustCompile(`(?m)^var\s+(\w+)\s*=\s*(.+?)$`)
	matches := varPattern.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) >= 3 {
			name := match[1]
			value := strings.TrimSpace(match[2])
			vars[name] = value
		}
	}

	return vars
}

// parseResources parses resource declarations from Bicep content.
func (p *BicepParser) parseResources(content, sourcePath string) []*resource.Resource {
	var resources []*resource.Resource

	// Pattern: resource <symbolic-name> '<type>@<api-version>' = { ... }
	// This is a simplified parser - Bicep is complex and a full parser would be more involved
	resourcePattern := regexp.MustCompile(`resource\s+(\w+)\s+'([^']+)'\s*=\s*\{`)
	matches := resourcePattern.FindAllStringSubmatchIndex(content, -1)

	for _, match := range matches {
		if len(match) >= 6 {
			symbolicName := content[match[2]:match[3]]
			resourceTypeWithVersion := content[match[4]:match[5]]

			// Extract type and API version
			parts := strings.Split(resourceTypeWithVersion, "@")
			resourceType := parts[0]

			res := p.createResource(symbolicName, resourceType, content, match[0], sourcePath)
			resources = append(resources, res)
		}
	}

	return resources
}

// createResource creates a Resource from parsed Bicep data.
func (p *BicepParser) createResource(symbolicName, bicepType, content string, startPos int, sourcePath string) *resource.Resource {
	resourceID := symbolicName
	resType := mapBicepTypeToResourceType(bicepType)

	res := resource.NewAWSResource(resourceID, symbolicName, resType)
	res.Config["bicep_type"] = bicepType
	res.Config["source_file"] = filepath.Base(sourcePath)

	// Try to extract properties from the resource block
	properties := p.extractResourceProperties(content, startPos)

	// Extract name
	if name, ok := properties["name"]; ok {
		res.Name = name
	}

	// Extract location
	if location, ok := properties["location"]; ok {
		res.Region = location
	}

	// Store all properties in config
	for k, v := range properties {
		res.Config[k] = v
	}

	// Extract dependencies from resource references
	deps := p.extractDependencies(content, startPos)
	for _, dep := range deps {
		res.AddDependency(dep)
	}

	return res
}

// extractResourceProperties extracts properties from a resource block.
func (p *BicepParser) extractResourceProperties(content string, startPos int) map[string]string {
	properties := make(map[string]string)

	// Find the opening brace
	bracePos := strings.Index(content[startPos:], "{")
	if bracePos == -1 {
		return properties
	}

	// Find matching closing brace (simplified - doesn't handle nested braces fully)
	depth := 1
	endPos := startPos + bracePos + 1
	for i := endPos; i < len(content) && depth > 0; i++ {
		if content[i] == '{' {
			depth++
		} else if content[i] == '}' {
			depth--
		}
		endPos = i
	}

	block := content[startPos+bracePos+1 : endPos]

	// Extract simple property assignments: name: 'value' or name: value
	propPattern := regexp.MustCompile(`(?m)^\s*(\w+)\s*:\s*'?([^'\n]+)'?\s*$`)
	matches := propPattern.FindAllStringSubmatch(block, -1)

	for _, match := range matches {
		if len(match) >= 3 {
			key := strings.TrimSpace(match[1])
			value := strings.TrimSpace(match[2])
			// Clean up quotes
			value = strings.Trim(value, "'\"")
			properties[key] = value
		}
	}

	return properties
}

// extractDependencies finds resource references in a block.
func (p *BicepParser) extractDependencies(content string, startPos int) []string {
	var deps []string
	seen := make(map[string]bool)

	// Find the resource block
	bracePos := strings.Index(content[startPos:], "{")
	if bracePos == -1 {
		return deps
	}

	depth := 1
	endPos := startPos + bracePos + 1
	for i := endPos; i < len(content) && depth > 0; i++ {
		if content[i] == '{' {
			depth++
		} else if content[i] == '}' {
			depth--
		}
		endPos = i
	}

	block := content[startPos+bracePos+1 : endPos]

	// Find resource references: resourceName.id, resourceName.properties.xxx
	refPattern := regexp.MustCompile(`(\w+)\.(id|name|properties|outputs)`)
	matches := refPattern.FindAllStringSubmatch(block, -1)

	for _, match := range matches {
		if len(match) >= 2 {
			dep := match[1]
			// Skip common keywords
			if dep != "resourceGroup" && dep != "subscription" && dep != "deployment" && dep != "environment" {
				if !seen[dep] {
					seen[dep] = true
					deps = append(deps, dep)
				}
			}
		}
	}

	return deps
}

// parseOutputs parses output declarations from Bicep content.
func (p *BicepParser) parseOutputs(content string) map[string]string {
	outputs := make(map[string]string)

	// Pattern: output <name> <type> = <expression>
	outputPattern := regexp.MustCompile(`(?m)^output\s+(\w+)\s+(\w+)\s*=\s*(.+?)$`)
	matches := outputPattern.FindAllStringSubmatch(content, -1)

	for _, match := range matches {
		if len(match) >= 4 {
			name := match[1]
			outputType := match[2]
			value := strings.TrimSpace(match[3])
			outputs[name] = fmt.Sprintf("type=%s, value=%s", outputType, value)
		}
	}

	return outputs
}

// shouldIncludeResource checks if a resource matches the filter criteria.
func (p *BicepParser) shouldIncludeResource(res *resource.Resource, opts *parser.ParseOptions) bool {
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

// mapBicepTypeToResourceType maps Bicep resource types to our resource types.
func mapBicepTypeToResourceType(bicepType string) resource.Type {
	// Normalize: Microsoft.Compute/virtualMachines -> lowercase for matching
	lowerType := strings.ToLower(bicepType)

	mapping := map[string]resource.Type{
		// Compute
		"microsoft.compute/virtualmachines":                          resource.TypeAzureVM,
		"microsoft.web/sites":                                        resource.TypeAppService,
		"microsoft.web/serverfarms":                                  resource.TypeAppService,
		"microsoft.containerinstance/containergroups":                resource.TypeContainerInstance,
		"microsoft.containerservice/managedclusters":                 resource.TypeAKS,

		// Storage
		"microsoft.storage/storageaccounts":                          resource.TypeAzureStorageAcct,
		"microsoft.storage/storageaccounts/blobservices/containers":  resource.TypeBlobStorage,
		"microsoft.compute/disks":                                    resource.TypeManagedDisk,

		// Database
		"microsoft.sql/servers/databases":                            resource.TypeAzureSQL,
		"microsoft.sql/servers":                                      resource.TypeAzureSQL,
		"microsoft.dbforpostgresql/flexibleservers":                  resource.TypeAzurePostgres,
		"microsoft.dbformysql/flexibleservers":                       resource.TypeAzureMySQL,
		"microsoft.documentdb/databaseaccounts":                      resource.TypeCosmosDB,
		"microsoft.cache/redis":                                      resource.TypeAzureCache,

		// Networking
		"microsoft.network/loadbalancers":                            resource.TypeAzureLB,
		"microsoft.network/applicationgateways":                      resource.TypeAppGateway,
		"microsoft.network/dnszones":                                 resource.TypeAzureDNS,
		"microsoft.cdn/profiles":                                     resource.TypeAzureCDN,
		"microsoft.network/frontdoors":                               resource.TypeFrontDoor,
		"microsoft.network/virtualnetworks":                          resource.TypeAzureVNet,

		// Security
		"microsoft.keyvault/vaults":                                  resource.TypeKeyVault,
		"microsoft.network/firewallpolicies":                         resource.TypeAzureFirewall,
		"microsoft.network/azurefirewalls":                           resource.TypeAzureFirewall,

		// Messaging
		"microsoft.servicebus/namespaces":                            resource.TypeServiceBus,
		"microsoft.servicebus/namespaces/queues":                     resource.TypeServiceBusQueue,
		"microsoft.eventhub/namespaces":                              resource.TypeEventHub,
		"microsoft.eventgrid/topics":                                 resource.TypeEventGrid,
		"microsoft.logic/workflows":                                  resource.TypeLogicApp,
	}

	if resType, ok := mapping[lowerType]; ok {
		return resType
	}

	// Try partial matching for versioned types
	for pattern, resType := range mapping {
		if strings.Contains(lowerType, pattern) {
			return resType
		}
	}

	return resource.Type(bicepType)
}
