package awsoperations

import (
	"sort"
	"strings"
	"unicode"

	"github.com/homeport/homeport/internal/app/coverage"
)

// ServiceMetadata is the canonical, server-owned description of one AWS
// service. It is derived from the embedded coverage catalogue; the browser
// never supplies a target, resource type, capability, or local identity.
type ServiceMetadata struct {
	Key           ServiceKey `json:"key"`
	DisplayName   string     `json:"display_name"`
	ResourceTypes []string   `json:"resource_types"`
	Target        string     `json:"target"`
	Family        string     `json:"family"`
	PanelKind     string     `json:"panel_kind"`
	Driver        string     `json:"driver"`
}

var serviceCatalog = buildServiceCatalog()

// RegisteredServices returns a stable, detached catalogue projection.
func RegisteredServices() []ServiceMetadata {
	services := make([]ServiceMetadata, len(serviceCatalog.services))
	for i, service := range serviceCatalog.services {
		service.ResourceTypes = append([]string(nil), service.ResourceTypes...)
		services[i] = service
	}
	return services
}

func ServiceMetadataFor(key ServiceKey) (ServiceMetadata, bool) {
	service, found := serviceCatalog.byKey[key]
	if found {
		service.ResourceTypes = append([]string(nil), service.ResourceTypes...)
	}
	return service, found
}

func ServiceForResource(resourceType string) (ServiceKey, bool) {
	key, found := serviceCatalog.byResourceType[resourceType]
	return key, found
}

// NormalizeServiceKey normalizes catalogue display names including App Mesh and
// CloudFormation full import to stable API and route keys.
func NormalizeServiceKey(name string) ServiceKey {
	var builder strings.Builder
	lastDash := false
	for _, character := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(character)
			lastDash = false
			continue
		}
		if !lastDash && builder.Len() > 0 {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return ServiceKey(strings.Trim(builder.String(), "-"))
}

type catalogIndex struct {
	services       []ServiceMetadata
	byKey          map[ServiceKey]ServiceMetadata
	byResourceType map[string]ServiceKey
}

func buildServiceCatalog() catalogIndex {
	coverageCatalog, err := coverage.LoadDefaultCatalog()
	if err != nil {
		panic("load embedded AWS coverage catalogue: " + err.Error())
	}
	index := catalogIndex{byKey: make(map[ServiceKey]ServiceMetadata), byResourceType: make(map[string]ServiceKey)}
	for _, entry := range coverageCatalog.Services {
		if entry.Provider != "aws" {
			continue
		}
		key := NormalizeServiceKey(entry.Service)
		if key == "" || len(entry.ResourceTypes) == 0 {
			panic("invalid AWS coverage entry: " + entry.Service)
		}
		if _, exists := index.byKey[key]; exists {
			panic("duplicate AWS coverage key: " + string(key))
		}
		family := serviceFamily(key)
		metadata := ServiceMetadata{
			Key:           key,
			DisplayName:   entry.Service,
			ResourceTypes: append([]string(nil), entry.ResourceTypes...),
			Target:        entry.Target,
			Family:        family,
			PanelKind:     family,
			Driver:        declaredDriver(key),
		}
		if metadata.Target == "" || metadata.Family == "" || metadata.Driver == "" {
			panic("incomplete AWS service metadata: " + entry.Service)
		}
		for _, resourceType := range metadata.ResourceTypes {
			if previous, exists := index.byResourceType[resourceType]; exists {
				panic("AWS resource type " + resourceType + " is declared by both " + string(previous) + " and " + string(key))
			}
			index.byResourceType[resourceType] = key
		}
		index.byKey[key] = metadata
		index.services = append(index.services, metadata)
	}
	sort.Slice(index.services, func(i, j int) bool { return index.services[i].Key < index.services[j].Key })
	return index
}

func declaredDriver(key ServiceKey) string {
	switch key {
	case ServiceLambda:
		return "lambda-local"
	case ServiceSQS:
		return "sqs-local"
	default:
		return "unavailable-local"
	}
}

func serviceFamily(key ServiceKey) string {
	switch key {
	case "acm", "alb", "api-gateway", "app-mesh", "cloudfront", "route-53", "vpc", "waf", "shield":
		return "edge-networking-delivery"
	case "lambda", "ecs", "eks", "ec2", "ecr", "codebuild", "codedeploy", "codepipeline", "step-functions", "cloudformation-full-import":
		return "compute-orchestration"
	case "s3", "ebs", "efs", "dynamodb", "rds", "elasticache", "redshift", "opensearch", "athena", "emr", "glue", "lake-formation", "quicksight":
		return "storage-database-analytics"
	case "sqs", "sns", "kinesis", "eventbridge", "mq", "msk", "iot-core":
		return "messaging-events"
	case "iam", "cognito", "kms", "secrets-manager", "config", "control-tower", "guardduty", "organizations", "security-hub":
		return "identity-security-governance"
	case "appsync", "bedrock", "comprehend", "rekognition", "sagemaker", "textract", "transcribe", "translate", "ses":
		return "ai-application-services"
	case "cloudwatch", "x-ray":
		return "observability"
	default:
		return ""
	}
}
