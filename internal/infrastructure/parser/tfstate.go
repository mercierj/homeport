package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/homeport/homeport/internal/domain/resource"
)

// TerraformState represents the structure of a Terraform state file
type TerraformState struct {
	Version          int                `json:"version"`
	TerraformVersion string             `json:"terraform_version"`
	Resources        []StateResource    `json:"resources"`
	Outputs          map[string]Output  `json:"outputs,omitempty"`
}

// StateResource represents a resource in the Terraform state
type StateResource struct {
	Mode         string             `json:"mode"`
	Type         string             `json:"type"`
	Name         string             `json:"name"`
	Provider     string             `json:"provider"`
	Instances    []ResourceInstance `json:"instances"`
	Module       string             `json:"module,omitempty"`
}

// ResourceInstance represents an instance of a resource
type ResourceInstance struct {
	SchemaVersion   int                    `json:"schema_version"`
	Attributes      map[string]interface{} `json:"attributes"`
	Dependencies    []string               `json:"dependencies,omitempty"`
	Private         string                 `json:"private,omitempty"`
	IndexKey        interface{}            `json:"index_key,omitempty"`
}

// Output represents a Terraform output value
type Output struct {
	Value     interface{} `json:"value"`
	Type      interface{} `json:"type"`
	Sensitive bool        `json:"sensitive,omitempty"`
}

// ParseStateFile parses a Terraform state file and returns the state structure
func ParseStateFile(path string) (*TerraformState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	var state TerraformState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse state file: %w", err)
	}

	// Validate state version
	if state.Version < 3 || state.Version > 4 {
		return nil, fmt.Errorf("unsupported state version: %d (supported: 3-4)", state.Version)
	}

	return &state, nil
}

// BuildInfrastructureFromState converts a Terraform state to an Infrastructure model
func BuildInfrastructureFromState(state *TerraformState) (*resource.Infrastructure, error) {
	infra := resource.NewInfrastructure(resource.ProviderAWS)

	// Set metadata
	infra.Metadata["terraform_version"] = state.TerraformVersion
	infra.Metadata["state_version"] = fmt.Sprintf("%d", state.Version)

	// Process each resource
	for _, stateRes := range state.Resources {
		// Skip data sources (mode = "data")
		if stateRes.Mode != "managed" {
			continue
		}

		// Process each instance of the resource
		for idx, instance := range stateRes.Instances {
			res, err := convertStateResourceToResource(stateRes, instance, idx)
			if err != nil {
				// Log warning but continue processing other resources
				fmt.Fprintf(os.Stderr, "Warning: failed to convert resource %s.%s: %v\n",
					stateRes.Type, stateRes.Name, err)
				continue
			}

			infra.AddResource(res)
		}
	}

	return infra, nil
}

// convertStateResourceToResource converts a Terraform state resource to our Resource model
func convertStateResourceToResource(stateRes StateResource, instance ResourceInstance, idx int) (*resource.Resource, error) {
	// Generate resource ID
	resourceID := fmt.Sprintf("%s.%s", stateRes.Type, stateRes.Name)
	if instance.IndexKey != nil {
		resourceID = fmt.Sprintf("%s[%v]", resourceID, instance.IndexKey)
	}

	// Extract resource name from attributes or use Terraform name
	name := stateRes.Name
	if nameAttr, ok := instance.Attributes["name"].(string); ok && nameAttr != "" {
		name = nameAttr
	} else if tags, ok := instance.Attributes["tags"].(map[string]interface{}); ok {
		if nameTag, ok := tags["Name"].(string); ok && nameTag != "" {
			name = nameTag
		}
	}

	// Map Terraform type to our resource Type
	resourceType := mapTerraformTypeToResourceType(stateRes.Type)

	// Create the resource
	res := resource.NewAWSResource(resourceID, name, resourceType)

	// Extract ARN if available
	if arn, ok := instance.Attributes["arn"].(string); ok {
		res.ARN = arn
	}

	// Extract region
	if region, ok := instance.Attributes["region"].(string); ok {
		res.Region = region
	} else if az, ok := instance.Attributes["availability_zone"].(string); ok {
		// Extract region from availability zone (e.g., us-east-1a -> us-east-1)
		if len(az) > 0 {
			res.Region = az[:len(az)-1]
		}
	}

	// Copy all attributes
	res.Config = instance.Attributes

	// Extract tags
	if tags, ok := instance.Attributes["tags"].(map[string]interface{}); ok {
		for k, v := range tags {
			if strVal, ok := v.(string); ok {
				res.Tags[k] = strVal
			}
		}
	}

	// Process dependencies
	for _, dep := range instance.Dependencies {
		// Dependencies in state file are in format "aws_security_group.web" or module format
		// We need to extract the resource reference
		cleanDep := strings.TrimPrefix(dep, "module.")
		res.AddDependency(cleanDep)
	}

	return res, nil
}

// mapTerraformTypeToResourceType maps Terraform resource types to our Type enum
func mapTerraformTypeToResourceType(tfType string) resource.Type {
	// Direct mapping for known types
	switch tfType {
	// Compute
	case "aws_instance":
		return resource.TypeEC2Instance
	case "aws_lambda_function":
		return resource.TypeLambdaFunction
	case "aws_ecs_service":
		return resource.TypeECSService
	case "aws_ecs_task_definition":
		return resource.TypeECSTaskDef

	// Storage
	case "aws_s3_bucket":
		return resource.TypeS3Bucket
	case "aws_ebs_volume":
		return resource.TypeEBSVolume

	// Database
	case "aws_db_instance":
		return resource.TypeRDSInstance
	case "aws_rds_cluster":
		return resource.TypeRDSCluster
	case "aws_dynamodb_table":
		return resource.TypeDynamoDBTable
	case "aws_elasticache_cluster", "aws_elasticache_replication_group":
		return resource.TypeElastiCache

	// Networking
	case "aws_lb", "aws_alb", "aws_elb":
		return resource.TypeALB
	case "aws_api_gateway_rest_api", "aws_apigatewayv2_api":
		return resource.TypeAPIGateway
	case "aws_route53_zone":
		return resource.TypeRoute53Zone
	case "aws_cloudfront_distribution":
		return resource.TypeCloudFront

	// Security
	case "aws_cognito_user_pool":
		return resource.TypeCognitoPool
	case "aws_secretsmanager_secret":
		return resource.TypeSecretsManager
	case "aws_iam_role":
		return resource.TypeIAMRole

	// Messaging
	case "aws_sqs_queue":
		return resource.TypeSQSQueue
	case "aws_sns_topic":
		return resource.TypeSNSTopic
	case "aws_cloudwatch_event_rule", "aws_cloudwatch_event_bus":
		return resource.TypeEventBridge

	default:
		// Return the Terraform type as-is for unknown types
		return resource.Type(tfType)
	}
}

// ExtractResourceDependencies analyzes resource attributes to find implicit dependencies
func ExtractResourceDependencies(res *resource.Resource, allResources map[string]*resource.Resource) {
	// Common attribute patterns that indicate dependencies
	dependencyPatterns := []string{
		"vpc_id",
		"subnet_id",
		"subnet_ids",
		"security_group_ids",
		"security_groups",
		"db_subnet_group_name",
		"load_balancer_arn",
		"target_group_arn",
		"role_arn",
		"kms_key_id",
		"source_security_group_id",
	}

	for _, pattern := range dependencyPatterns {
		if val := res.Config[pattern]; val != nil {
			// Handle string values
			if strVal, ok := val.(string); ok {
				if depRes := findResourceByID(strVal, allResources); depRes != nil {
					res.AddDependency(depRes.ID)
				}
			}

			// Handle array values
			if arrVal, ok := val.([]interface{}); ok {
				for _, item := range arrVal {
					if strVal, ok := item.(string); ok {
						if depRes := findResourceByID(strVal, allResources); depRes != nil {
							res.AddDependency(depRes.ID)
						}
					}
				}
			}
		}
	}
}

// findResourceByID finds a resource by matching its ID or ARN
func findResourceByID(id string, resources map[string]*resource.Resource) *resource.Resource {
	// First try direct ID match
	if res, ok := resources[id]; ok {
		return res
	}

	// Try to match by attribute (e.g., VPC ID, subnet ID)
	for _, res := range resources {
		// Check if the ID matches any known attribute
		if res.Config["id"] == id || res.ARN == id {
			return res
		}
	}

	return nil
}
