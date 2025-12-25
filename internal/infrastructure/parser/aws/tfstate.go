package aws

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

// TFStateParser parses Terraform state files for AWS resources.
type TFStateParser struct{}

// NewTFStateParser creates a new AWS TFState parser.
func NewTFStateParser() *TFStateParser {
	return &TFStateParser{}
}

// Provider returns the cloud provider.
func (p *TFStateParser) Provider() resource.Provider {
	return resource.ProviderAWS
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
		if !p.hasAWSResources(statePath) {
			return parser.ErrUnsupportedFormat
		}
		return nil
	}

	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".tfstate" {
		return parser.ErrUnsupportedFormat
	}

	if !p.hasAWSResources(path) {
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

	awsCount := 0
	totalCount := 0

	for _, res := range state.Resources {
		if res.Mode == "managed" {
			totalCount++
			if strings.HasPrefix(res.Type, "aws_") {
				awsCount++
			}
		}
	}

	if awsCount > 0 {
		confidence := float64(awsCount) / float64(totalCount)
		if confidence > 0.9 {
			return true, 0.95
		}
		return true, confidence * 0.85
	}

	return false, 0
}

// Parse parses Terraform state files and returns AWS infrastructure.
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

// hasAWSResources checks if a state file contains AWS resources.
func (p *TFStateParser) hasAWSResources(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}

	var state TerraformState
	if err := json.Unmarshal(data, &state); err != nil {
		return false
	}

	for _, res := range state.Resources {
		if strings.HasPrefix(res.Type, "aws_") {
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
	infra := resource.NewInfrastructure(resource.ProviderAWS)

	infra.Metadata["terraform_version"] = state.TerraformVersion
	infra.Metadata["state_version"] = fmt.Sprintf("%d", state.Version)

	for _, stateRes := range state.Resources {
		if stateRes.Mode != "managed" {
			continue
		}

		if !strings.HasPrefix(stateRes.Type, "aws_") {
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
	} else if tags, ok := instance.Attributes["tags"].(map[string]interface{}); ok {
		if nameTag, ok := tags["Name"].(string); ok && nameTag != "" {
			name = nameTag
		}
	}

	resourceType := mapAWSTerraformType(stateRes.Type)
	res := resource.NewAWSResource(resourceID, name, resourceType)

	if arn, ok := instance.Attributes["arn"].(string); ok {
		res.ARN = arn
	}

	if region, ok := instance.Attributes["region"].(string); ok {
		res.Region = region
	} else if az, ok := instance.Attributes["availability_zone"].(string); ok && len(az) > 1 {
		res.Region = az[:len(az)-1]
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

// mapAWSTerraformType maps AWS Terraform types to our resource types.
func mapAWSTerraformType(tfType string) resource.Type {
	mapping := map[string]resource.Type{
		// Compute
		"aws_instance":            resource.TypeEC2Instance,
		"aws_lambda_function":     resource.TypeLambdaFunction,
		"aws_ecs_service":         resource.TypeECSService,
		"aws_ecs_task_definition": resource.TypeECSTaskDef,
		"aws_eks_cluster":         resource.TypeEKSCluster,

		// Storage
		"aws_s3_bucket":       resource.TypeS3Bucket,
		"aws_ebs_volume":      resource.TypeEBSVolume,
		"aws_efs_file_system": resource.TypeEFSVolume,

		// Database
		"aws_db_instance":                 resource.TypeRDSInstance,
		"aws_rds_cluster":                 resource.TypeRDSCluster,
		"aws_dynamodb_table":              resource.TypeDynamoDBTable,
		"aws_elasticache_cluster":         resource.TypeElastiCache,
		"aws_elasticache_replication_group": resource.TypeElastiCache,

		// Networking
		"aws_lb":                    resource.TypeALB,
		"aws_alb":                   resource.TypeALB,
		"aws_elb":                   resource.TypeALB,
		"aws_api_gateway_rest_api":  resource.TypeAPIGateway,
		"aws_apigatewayv2_api":      resource.TypeAPIGateway,
		"aws_route53_zone":          resource.TypeRoute53Zone,
		"aws_cloudfront_distribution": resource.TypeCloudFront,
		"aws_vpc":                   resource.TypeVPC,

		// Security
		"aws_cognito_user_pool":    resource.TypeCognitoPool,
		"aws_secretsmanager_secret": resource.TypeSecretsManager,
		"aws_iam_role":             resource.TypeIAMRole,
		"aws_acm_certificate":      resource.TypeACMCertificate,

		// Messaging
		"aws_sqs_queue":             resource.TypeSQSQueue,
		"aws_sns_topic":             resource.TypeSNSTopic,
		"aws_cloudwatch_event_rule": resource.TypeEventBridge,
		"aws_cloudwatch_event_bus":  resource.TypeEventBridge,
		"aws_kinesis_stream":        resource.TypeKinesis,
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
