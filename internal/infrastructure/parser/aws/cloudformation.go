package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/cloudexit/cloudexit/internal/domain/parser"
	"github.com/cloudexit/cloudexit/internal/domain/resource"
)

// CloudFormationParser parses AWS CloudFormation templates.
type CloudFormationParser struct{}

// NewCloudFormationParser creates a new CloudFormation parser.
func NewCloudFormationParser() *CloudFormationParser {
	return &CloudFormationParser{}
}

// Provider returns the cloud provider.
func (p *CloudFormationParser) Provider() resource.Provider {
	return resource.ProviderAWS
}

// SupportedFormats returns the supported formats.
func (p *CloudFormationParser) SupportedFormats() []parser.Format {
	return []parser.Format{parser.FormatCloudFormation}
}

// Validate checks if the path contains valid CloudFormation templates.
func (p *CloudFormationParser) Validate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	if info.IsDir() {
		// Check for CloudFormation files in directory
		found := false
		err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && isCloudFormationFile(path) {
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

	if !isCloudFormationFile(path) {
		return parser.ErrUnsupportedFormat
	}

	return nil
}

// AutoDetect checks if the path contains CloudFormation templates.
func (p *CloudFormationParser) AutoDetect(path string) (bool, float64) {
	info, err := os.Stat(path)
	if err != nil {
		return false, 0
	}

	if info.IsDir() {
		count := 0
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() && isCloudFormationFile(p) {
				// Verify it's actually CloudFormation
				if isActualCloudFormation(p) {
					count++
				}
			}
			return nil
		})
		if count > 0 {
			return true, 0.8
		}
		return false, 0
	}

	if isCloudFormationFile(path) && isActualCloudFormation(path) {
		return true, 0.9
	}

	return false, 0
}

// Parse parses CloudFormation templates and returns infrastructure.
func (p *CloudFormationParser) Parse(ctx context.Context, path string, opts *parser.ParseOptions) (*resource.Infrastructure, error) {
	if opts == nil {
		opts = parser.NewParseOptions()
	}

	infra := resource.NewInfrastructure(resource.ProviderAWS)

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}

	if info.IsDir() {
		err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if isCloudFormationFile(filePath) {
				if err := p.parseFile(ctx, filePath, infra, opts); err != nil {
					if opts.IgnoreErrors {
						return nil
					}
					return err
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		if err := p.parseFile(ctx, path, infra, opts); err != nil {
			return nil, err
		}
	}

	// Build dependencies from template references
	p.buildDependencies(infra)

	return infra, nil
}

// parseFile parses a single CloudFormation file.
func (p *CloudFormationParser) parseFile(ctx context.Context, path string, infra *resource.Infrastructure, opts *parser.ParseOptions) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	var template CloudFormationTemplate

	// Try YAML first, then JSON
	if strings.HasSuffix(strings.ToLower(path), ".json") {
		if err := json.Unmarshal(data, &template); err != nil {
			return fmt.Errorf("failed to parse JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &template); err != nil {
			return fmt.Errorf("failed to parse YAML: %w", err)
		}
	}

	// Extract metadata
	if template.Description != "" {
		infra.Metadata["description"] = template.Description
	}
	if template.AWSTemplateFormatVersion != "" {
		infra.Metadata["template_version"] = template.AWSTemplateFormatVersion
	}

	// Parse resources
	for logicalID, cfnResource := range template.Resources {
		resourceType := mapCFNTypeToResourceType(cfnResource.Type)
		if resourceType == "" {
			continue // Skip unknown resource types
		}

		// Check filters
		if !p.shouldIncludeType(resourceType, opts) {
			continue
		}

		res := resource.NewAWSResource(logicalID, logicalID, resourceType)
		res.Config["cfn_type"] = cfnResource.Type
		res.Config["source_file"] = path

		// Extract properties
		if cfnResource.Properties != nil {
			for key, value := range cfnResource.Properties {
				res.Config[toSnakeCase(key)] = p.resolveValue(value, template.Parameters)
			}
		}

		// Handle condition
		if cfnResource.Condition != "" {
			res.Config["condition"] = cfnResource.Condition
		}

		// Handle DependsOn
		if len(cfnResource.DependsOn) > 0 {
			for _, dep := range cfnResource.DependsOn {
				res.AddDependency(dep)
			}
		}

		// Extract tags if present
		if tags, ok := cfnResource.Properties["Tags"].([]interface{}); ok {
			for _, tag := range tags {
				if tagMap, ok := tag.(map[string]interface{}); ok {
					key := fmt.Sprintf("%v", tagMap["Key"])
					value := fmt.Sprintf("%v", tagMap["Value"])
					res.Tags[key] = value
				}
			}
		}

		infra.AddResource(res)
	}

	return nil
}

// buildDependencies extracts implicit dependencies from resource references.
func (p *CloudFormationParser) buildDependencies(infra *resource.Infrastructure) {
	for _, res := range infra.Resources {
		// Check config values for references to other resources
		for _, value := range res.Config {
			refs := p.extractReferences(value)
			for _, ref := range refs {
				// Only add if the referenced resource exists
				if _, err := infra.GetResource(ref); err == nil {
					res.AddDependency(ref)
				}
			}
		}
	}
}

// extractReferences finds resource references in a value.
func (p *CloudFormationParser) extractReferences(value interface{}) []string {
	var refs []string

	switch v := value.(type) {
	case map[string]interface{}:
		// Check for Ref
		if ref, ok := v["Ref"].(string); ok {
			refs = append(refs, ref)
		}
		// Check for GetAtt
		if getAtt, ok := v["Fn::GetAtt"].([]interface{}); ok && len(getAtt) > 0 {
			if logicalID, ok := getAtt[0].(string); ok {
				refs = append(refs, logicalID)
			}
		}
		if getAtt, ok := v["!GetAtt"].(string); ok {
			parts := strings.Split(getAtt, ".")
			if len(parts) > 0 {
				refs = append(refs, parts[0])
			}
		}
		// Recurse into nested maps
		for _, nestedValue := range v {
			refs = append(refs, p.extractReferences(nestedValue)...)
		}
	case []interface{}:
		for _, item := range v {
			refs = append(refs, p.extractReferences(item)...)
		}
	case string:
		// Check for !Ref shorthand
		if strings.HasPrefix(v, "!Ref ") {
			refs = append(refs, strings.TrimPrefix(v, "!Ref "))
		}
	}

	return refs
}

// resolveValue resolves intrinsic function values where possible.
func (p *CloudFormationParser) resolveValue(value interface{}, params map[string]CFNParameter) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		// Handle Ref to parameters
		if ref, ok := v["Ref"].(string); ok {
			if param, exists := params[ref]; exists {
				if param.Default != nil {
					return param.Default
				}
				return fmt.Sprintf("${%s}", ref)
			}
			return fmt.Sprintf("!Ref %s", ref)
		}
		// Handle Sub
		if sub, ok := v["Fn::Sub"].(string); ok {
			return sub
		}
		// Handle Join
		if join, ok := v["Fn::Join"].([]interface{}); ok && len(join) == 2 {
			if delimiter, ok := join[0].(string); ok {
				if items, ok := join[1].([]interface{}); ok {
					var strItems []string
					for _, item := range items {
						strItems = append(strItems, fmt.Sprintf("%v", item))
					}
					return strings.Join(strItems, delimiter)
				}
			}
		}
		// Recurse
		resolved := make(map[string]interface{})
		for k, val := range v {
			resolved[k] = p.resolveValue(val, params)
		}
		return resolved
	case []interface{}:
		resolved := make([]interface{}, len(v))
		for i, val := range v {
			resolved[i] = p.resolveValue(val, params)
		}
		return resolved
	default:
		return value
	}
}

// shouldIncludeType checks if a resource type should be included based on filters.
func (p *CloudFormationParser) shouldIncludeType(t resource.Type, opts *parser.ParseOptions) bool {
	if opts == nil {
		return true
	}

	if len(opts.FilterTypes) > 0 {
		for _, ft := range opts.FilterTypes {
			if ft == t {
				return true
			}
		}
		return false
	}

	if len(opts.FilterCategories) > 0 {
		category := t.GetCategory()
		for _, fc := range opts.FilterCategories {
			if fc == category {
				return true
			}
		}
		return false
	}

	return true
}

// CloudFormationTemplate represents an AWS CloudFormation template.
type CloudFormationTemplate struct {
	AWSTemplateFormatVersion string                    `yaml:"AWSTemplateFormatVersion" json:"AWSTemplateFormatVersion"`
	Description              string                    `yaml:"Description" json:"Description"`
	Parameters               map[string]CFNParameter   `yaml:"Parameters" json:"Parameters"`
	Mappings                 map[string]interface{}    `yaml:"Mappings" json:"Mappings"`
	Conditions               map[string]interface{}    `yaml:"Conditions" json:"Conditions"`
	Resources                map[string]CFNResource    `yaml:"Resources" json:"Resources"`
	Outputs                  map[string]CFNOutput      `yaml:"Outputs" json:"Outputs"`
}

// CFNParameter represents a CloudFormation parameter.
type CFNParameter struct {
	Type          string      `yaml:"Type" json:"Type"`
	Default       interface{} `yaml:"Default" json:"Default"`
	Description   string      `yaml:"Description" json:"Description"`
	AllowedValues []string    `yaml:"AllowedValues" json:"AllowedValues"`
	ConstraintDescription string `yaml:"ConstraintDescription" json:"ConstraintDescription"`
}

// CFNResource represents a CloudFormation resource.
type CFNResource struct {
	Type       string                 `yaml:"Type" json:"Type"`
	Properties map[string]interface{} `yaml:"Properties" json:"Properties"`
	DependsOn  []string               `yaml:"DependsOn" json:"DependsOn"`
	Condition  string                 `yaml:"Condition" json:"Condition"`
	Metadata   map[string]interface{} `yaml:"Metadata" json:"Metadata"`
}

// UnmarshalYAML handles both string and []string for DependsOn.
func (r *CFNResource) UnmarshalYAML(node *yaml.Node) error {
	type rawResource struct {
		Type       string                 `yaml:"Type"`
		Properties map[string]interface{} `yaml:"Properties"`
		DependsOn  interface{}            `yaml:"DependsOn"`
		Condition  string                 `yaml:"Condition"`
		Metadata   map[string]interface{} `yaml:"Metadata"`
	}

	var raw rawResource
	if err := node.Decode(&raw); err != nil {
		return err
	}

	r.Type = raw.Type
	r.Properties = raw.Properties
	r.Condition = raw.Condition
	r.Metadata = raw.Metadata

	// Handle DependsOn as string or []string
	switch v := raw.DependsOn.(type) {
	case string:
		r.DependsOn = []string{v}
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				r.DependsOn = append(r.DependsOn, s)
			}
		}
	}

	return nil
}

// CFNOutput represents a CloudFormation output.
type CFNOutput struct {
	Description string      `yaml:"Description" json:"Description"`
	Value       interface{} `yaml:"Value" json:"Value"`
	Export      struct {
		Name interface{} `yaml:"Name" json:"Name"`
	} `yaml:"Export" json:"Export"`
}

// isCloudFormationFile checks if a file is a potential CloudFormation template.
func isCloudFormationFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	base := strings.ToLower(filepath.Base(path))

	// Check for common CloudFormation file patterns
	if ext == ".yaml" || ext == ".yml" || ext == ".json" {
		// Check for common CFN naming patterns
		if strings.Contains(base, "template") ||
			strings.Contains(base, "cloudformation") ||
			strings.Contains(base, "cfn") ||
			strings.Contains(base, "stack") {
			return true
		}
		return true // Accept all YAML/JSON files for further inspection
	}

	if ext == ".template" {
		return true
	}

	return false
}

// isActualCloudFormation verifies a file is actually CloudFormation by checking content.
func isActualCloudFormation(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	content := string(data)

	// Check for CloudFormation markers
	if strings.Contains(content, "AWSTemplateFormatVersion") ||
		strings.Contains(content, "AWS::") ||
		(strings.Contains(content, "Resources:") && strings.Contains(content, "Type:")) {
		return true
	}

	return false
}

// mapCFNTypeToResourceType maps CloudFormation resource types to our resource types.
func mapCFNTypeToResourceType(cfnType string) resource.Type {
	mapping := map[string]resource.Type{
		// Compute
		"AWS::EC2::Instance":                  resource.TypeEC2Instance,
		"AWS::Lambda::Function":               resource.TypeLambdaFunction,
		"AWS::ECS::Service":                   resource.TypeECSService,
		"AWS::ECS::TaskDefinition":            resource.TypeECSTaskDef,
		"AWS::EKS::Cluster":                   resource.TypeEKSCluster,

		// Storage
		"AWS::S3::Bucket":                     resource.TypeS3Bucket,
		"AWS::EC2::Volume":                    resource.TypeEBSVolume,
		"AWS::EFS::FileSystem":                resource.TypeEFSVolume,

		// Database
		"AWS::RDS::DBInstance":                resource.TypeRDSInstance,
		"AWS::RDS::DBCluster":                 resource.TypeRDSCluster,
		"AWS::DynamoDB::Table":                resource.TypeDynamoDBTable,
		"AWS::ElastiCache::CacheCluster":      resource.TypeElastiCache,
		"AWS::ElastiCache::ReplicationGroup":  resource.TypeElastiCache,

		// Networking
		"AWS::ElasticLoadBalancingV2::LoadBalancer": resource.TypeALB,
		"AWS::ApiGateway::RestApi":                  resource.TypeAPIGateway,
		"AWS::Route53::HostedZone":                  resource.TypeRoute53Zone,
		"AWS::CloudFront::Distribution":             resource.TypeCloudFront,
		"AWS::EC2::VPC":                             resource.TypeVPC,

		// Security
		"AWS::Cognito::UserPool":              resource.TypeCognitoPool,
		"AWS::SecretsManager::Secret":         resource.TypeSecretsManager,
		"AWS::IAM::Role":                      resource.TypeIAMRole,
		"AWS::CertificateManager::Certificate": resource.TypeACMCertificate,

		// Messaging
		"AWS::SQS::Queue":                     resource.TypeSQSQueue,
		"AWS::SNS::Topic":                     resource.TypeSNSTopic,
		"AWS::Events::Rule":                   resource.TypeEventBridge,
		"AWS::Kinesis::Stream":                resource.TypeKinesis,
	}

	if resType, ok := mapping[cfnType]; ok {
		return resType
	}

	return ""
}

// toSnakeCase converts PascalCase to snake_case.
func toSnakeCase(s string) string {
	var result strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			result.WriteByte('_')
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}
