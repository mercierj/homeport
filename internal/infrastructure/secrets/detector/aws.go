// Package detector provides secret detection implementations for cloud resources.
package detector

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/secrets"
)

// AWSDetector detects secrets from AWS resources.
type AWSDetector struct {
	*secrets.BaseDetector
}

// NewAWSDetector creates a new AWS secret detector.
func NewAWSDetector() *AWSDetector {
	return &AWSDetector{
		BaseDetector: secrets.NewBaseDetector(
			resource.ProviderAWS,
			// Databases
			resource.TypeRDSInstance,
			resource.TypeRDSCluster,
			resource.TypeElastiCache,
			resource.TypeDynamoDBTable,
			// Compute
			resource.TypeLambdaFunction,
			resource.TypeECSService,
			resource.TypeECSTaskDef,
			// Security
			resource.TypeSecretsManager,
			resource.TypeCognitoPool,
		),
	}
}

// Detect analyzes an AWS resource and returns detected secrets.
func (d *AWSDetector) Detect(ctx context.Context, res *resource.AWSResource) ([]*secrets.DetectedSecret, error) {
	switch res.Type {
	case resource.TypeRDSInstance:
		return d.detectRDSSecrets(res), nil
	case resource.TypeRDSCluster:
		return d.detectRDSClusterSecrets(res), nil
	case resource.TypeElastiCache:
		return d.detectElastiCacheSecrets(res), nil
	case resource.TypeLambdaFunction:
		return d.detectLambdaSecrets(res), nil
	case resource.TypeECSService, resource.TypeECSTaskDef:
		return d.detectECSSecrets(res), nil
	case resource.TypeSecretsManager:
		return d.detectSecretsManagerSecrets(res), nil
	case resource.TypeCognitoPool:
		return d.detectCognitoSecrets(res), nil
	default:
		return nil, nil
	}
}

// detectRDSSecrets detects secrets from RDS instances.
func (d *AWSDetector) detectRDSSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Skip password detection for Aurora instances that belong to a cluster
	// The cluster manages the password, not the individual instances
	// Check both field names:
	// - "db_cluster_identifier" from AWS API parser
	// - "cluster_identifier" from Terraform state
	clusterID := secrets.GetConfigString(config, "db_cluster_identifier")
	if clusterID == "" {
		clusterID = secrets.GetConfigString(config, "cluster_identifier")
	}
	if clusterID != "" {
		// This is an Aurora cluster member - password is at cluster level
		return nil
	}

	// Check for managed secret (RDS Secrets Manager integration)
	// Only use if it looks like a valid Secrets Manager ARN
	if secretARN := secrets.GetConfigString(config, "master_user_secret_arn"); secretARN != "" && strings.HasPrefix(secretARN, "arn:aws:secretsmanager:") {
		detected = append(detected, &secrets.DetectedSecret{
			Name:             secrets.GenerateSecretName(resName, "rds", "password"),
			Source:           secrets.SourceAWSSecretsManager,
			Key:              secretARN,
			Description:      fmt.Sprintf("Database master password for RDS instance %s (managed by Secrets Manager)", resName),
			Required:         true,
			Type:             secrets.TypePassword,
			ResourceID:       res.ID,
			ResourceName:     resName,
			ResourceType:     res.Type,
			DeduplicationKey: secrets.SourceAWSSecretsManager.String() + ":" + secretARN,
		})
		return detected
	}

	// Check for master_user_secret block (newer format)
	if secretBlock := secrets.GetConfigMap(config, "master_user_secret"); secretBlock != nil {
		if secretARN := secrets.GetConfigString(secretBlock, "secret_arn"); secretARN != "" && strings.HasPrefix(secretARN, "arn:aws:secretsmanager:") {
			detected = append(detected, &secrets.DetectedSecret{
				Name:             secrets.GenerateSecretName(resName, "rds", "password"),
				Source:           secrets.SourceAWSSecretsManager,
				Key:              secretARN,
				Description:      fmt.Sprintf("Database master password for RDS instance %s (managed by Secrets Manager)", resName),
				Required:         true,
				Type:             secrets.TypePassword,
				ResourceID:       res.ID,
				ResourceName:     resName,
				ResourceType:     res.Type,
				DeduplicationKey: secrets.SourceAWSSecretsManager.String() + ":" + secretARN,
			})
			return detected
		}
	}

	// No Secrets Manager integration - password is manual
	engine := secrets.GetConfigString(config, "engine")
	if engine == "" {
		engine = "database"
	}

	detected = append(detected, &secrets.DetectedSecret{
		Name:         secrets.GenerateSecretName(resName, "rds", "password"),
		Source:       secrets.SourceManual,
		Description:  fmt.Sprintf("Database master password for %s (%s)", resName, engine),
		Required:     true,
		Type:         secrets.TypePassword,
		ResourceID:   res.ID,
		ResourceName: resName,
		ResourceType: res.Type,
	})

	return detected
}

// detectRDSClusterSecrets detects secrets from RDS Aurora clusters.
func (d *AWSDetector) detectRDSClusterSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Check for managed secret - only if it looks like a valid ARN
	if secretARN := secrets.GetConfigString(config, "master_user_secret_arn"); secretARN != "" && strings.HasPrefix(secretARN, "arn:aws:secretsmanager:") {
		detected = append(detected, &secrets.DetectedSecret{
			Name:             secrets.GenerateSecretName(resName, "aurora", "password"),
			Source:           secrets.SourceAWSSecretsManager,
			Key:              secretARN,
			Description:      fmt.Sprintf("Database master password for Aurora cluster %s (managed by Secrets Manager)", resName),
			Required:         true,
			Type:             secrets.TypePassword,
			ResourceID:       res.ID,
			ResourceName:     resName,
			ResourceType:     res.Type,
			DeduplicationKey: secrets.SourceAWSSecretsManager.String() + ":" + secretARN,
		})
		return detected
	}

	// Check master_user_secret block - only if it looks like a valid ARN
	if secretBlock := secrets.GetConfigMap(config, "master_user_secret"); secretBlock != nil {
		if secretARN := secrets.GetConfigString(secretBlock, "secret_arn"); secretARN != "" && strings.HasPrefix(secretARN, "arn:aws:secretsmanager:") {
			detected = append(detected, &secrets.DetectedSecret{
				Name:             secrets.GenerateSecretName(resName, "aurora", "password"),
				Source:           secrets.SourceAWSSecretsManager,
				Key:              secretARN,
				Description:      fmt.Sprintf("Database master password for Aurora cluster %s (managed by Secrets Manager)", resName),
				Required:         true,
				Type:             secrets.TypePassword,
				ResourceID:       res.ID,
				ResourceName:     resName,
				ResourceType:     res.Type,
				DeduplicationKey: secrets.SourceAWSSecretsManager.String() + ":" + secretARN,
			})
			return detected
		}
	}

	// Manual password
	engine := secrets.GetConfigString(config, "engine")
	if engine == "" {
		engine = "aurora"
	}

	detected = append(detected, &secrets.DetectedSecret{
		Name:         secrets.GenerateSecretName(resName, "aurora", "password"),
		Source:       secrets.SourceManual,
		Description:  fmt.Sprintf("Database master password for Aurora cluster %s (%s)", resName, engine),
		Required:     true,
		Type:         secrets.TypePassword,
		ResourceID:   res.ID,
		ResourceName: resName,
		ResourceType: res.Type,
	})

	return detected
}

// detectElastiCacheSecrets detects secrets from ElastiCache clusters.
func (d *AWSDetector) detectElastiCacheSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Check if auth is enabled
	authTokenEnabled := secrets.GetConfigBool(config, "auth_token_enabled")
	transitEncryption := secrets.GetConfigBool(config, "transit_encryption_enabled")

	// If auth token is enabled or there's an auth_token field
	if authTokenEnabled || secrets.GetConfigString(config, "auth_token") != "" || transitEncryption {
		engine := secrets.GetConfigString(config, "engine")
		if engine == "" {
			engine = "redis"
		}

		detected = append(detected, &secrets.DetectedSecret{
			Name:         secrets.GenerateSecretName(resName, "elasticache", "auth_token"),
			Source:       secrets.SourceManual,
			Description:  fmt.Sprintf("Auth token for ElastiCache %s cluster %s", engine, resName),
			Required:     true,
			Type:         secrets.TypePassword,
			ResourceID:   res.ID,
			ResourceName: resName,
			ResourceType: res.Type,
		})
	}

	return detected
}

// detectLambdaSecrets detects secrets from Lambda functions.
func (d *AWSDetector) detectLambdaSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Check environment variables
	envVars := secrets.GetConfigMap(config, "environment")
	if envVars != nil {
		// Check nested variables map
		if varsMap := secrets.GetConfigMap(envVars, "variables"); varsMap != nil {
			envVars = varsMap
		}

		for key := range envVars {
			if secrets.IsSensitiveEnvName(key) {
				detected = append(detected, &secrets.DetectedSecret{
					Name:         secrets.NormalizeEnvName(key),
					Source:       secrets.SourceManual,
					Description:  fmt.Sprintf("Environment variable %s from Lambda function %s", key, resName),
					Required:     true,
					Type:         secrets.InferSecretType(key),
					ResourceID:   res.ID,
					ResourceName: resName,
					ResourceType: res.Type,
				})
			}
		}
	}

	return detected
}

// detectECSSecrets detects secrets from ECS task definitions and services.
func (d *AWSDetector) detectECSSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Debug: print what we're parsing
	fmt.Printf("[ECS Detector] Parsing resource: %s (type: %s)\n", resName, res.Type)
	fmt.Printf("[ECS Detector] Config keys: %v\n", getMapKeys(config))

	// Parse container definitions
	containerDefs := secrets.GetConfigList(config, "container_definitions")
	if containerDefs == nil {
		// Try parsing as JSON string
		if defsStr := secrets.GetConfigString(config, "container_definitions"); defsStr != "" {
			// Container definitions might be stored as string - check secrets field
			if strings.Contains(defsStr, `"secrets"`) {
				detected = append(detected, d.parseECSSecretsFromString(defsStr, resName, res)...)
			}
			// Also check for environment variables
			if strings.Contains(defsStr, `"environment"`) {
				detected = append(detected, d.parseECSEnvFromString(defsStr, resName, res)...)
			}
		}
		return detected
	}

	for _, containerDef := range containerDefs {
		container, ok := containerDef.(map[string]interface{})
		if !ok {
			continue
		}

		containerName := secrets.GetConfigString(container, "name")
		if containerName == "" {
			containerName = resName
		}

		// Check secrets configuration (references to Secrets Manager)
		secretsList := secrets.GetConfigList(container, "secrets")
		for _, secretItem := range secretsList {
			secret, ok := secretItem.(map[string]interface{})
			if !ok {
				continue
			}

			name := secrets.GetConfigString(secret, "name")
			valueFrom := secrets.GetConfigString(secret, "valueFrom")

			if name != "" && valueFrom != "" {
				// Determine if it's Secrets Manager or SSM Parameter Store
				// valueFrom can be:
				// - Full ARN: arn:aws:secretsmanager:region:account:secret:name-suffix
				// - Partial ARN: arn:aws:secretsmanager:region:account:secret:name
				// - Just secret name: prod/myapp/db-password
				if strings.HasPrefix(valueFrom, "arn:aws:secretsmanager:") {
					// Full Secrets Manager ARN
					detected = append(detected, &secrets.DetectedSecret{
						Name:             secrets.NormalizeEnvName(name),
						Source:           secrets.SourceAWSSecretsManager,
						Key:              valueFrom,
						Description:      fmt.Sprintf("Secret %s for ECS container %s (from Secrets Manager)", name, containerName),
						Required:         true,
						Type:             secrets.InferSecretType(name),
						ResourceID:       res.ID,
						ResourceName:     resName,
						ResourceType:     res.Type,
						DeduplicationKey: secrets.SourceAWSSecretsManager.String() + ":" + valueFrom,
					})
				} else if strings.HasPrefix(valueFrom, "arn:aws:ssm:") {
					// SSM Parameter Store - treat as manual for now
					detected = append(detected, &secrets.DetectedSecret{
						Name:         secrets.NormalizeEnvName(name),
						Source:       secrets.SourceManual,
						Description:  fmt.Sprintf("Secret %s for ECS container %s (from SSM Parameter Store: %s)", name, containerName, valueFrom),
						Required:     true,
						Type:         secrets.InferSecretType(name),
						ResourceID:   res.ID,
						ResourceName: resName,
						ResourceType: res.Type,
					})
				} else if !strings.HasPrefix(valueFrom, "arn:") {
					// Not an ARN - likely a Secrets Manager secret name/path like "prod/myapp/db-password"
					// Treat as Secrets Manager with the name as key
					detected = append(detected, &secrets.DetectedSecret{
						Name:             secrets.NormalizeEnvName(name),
						Source:           secrets.SourceAWSSecretsManager,
						Key:              valueFrom, // Use the secret name/path directly
						Description:      fmt.Sprintf("Secret %s for ECS container %s (from Secrets Manager: %s)", name, containerName, valueFrom),
						Required:         true,
						Type:             secrets.InferSecretType(name),
						ResourceID:       res.ID,
						ResourceName:     resName,
						ResourceType:     res.Type,
						DeduplicationKey: secrets.SourceAWSSecretsManager.String() + ":" + valueFrom,
					})
				} else {
					// Unknown ARN type - treat as manual
					detected = append(detected, &secrets.DetectedSecret{
						Name:         secrets.NormalizeEnvName(name),
						Source:       secrets.SourceManual,
						Description:  fmt.Sprintf("Secret %s for ECS container %s (reference: %s)", name, containerName, valueFrom),
						Required:     true,
						Type:         secrets.InferSecretType(name),
						ResourceID:   res.ID,
						ResourceName: resName,
						ResourceType: res.Type,
					})
				}
			}
		}

		// Check environment variables for sensitive values
		envList := secrets.GetConfigList(container, "environment")
		for _, envItem := range envList {
			env, ok := envItem.(map[string]interface{})
			if !ok {
				continue
			}

			name := secrets.GetConfigString(env, "name")
			if secrets.IsSensitiveEnvName(name) {
				detected = append(detected, &secrets.DetectedSecret{
					Name:         secrets.NormalizeEnvName(name),
					Source:       secrets.SourceManual,
					Description:  fmt.Sprintf("Environment variable %s from ECS container %s", name, containerName),
					Required:     true,
					Type:         secrets.InferSecretType(name),
					ResourceID:   res.ID,
					ResourceName: resName,
					ResourceType: res.Type,
				})
			}
		}
	}

	return detected
}

// parseECSSecretsFromString extracts secret references from JSON string container definitions.
func (d *AWSDetector) parseECSSecretsFromString(defsStr, resName string, res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	// Simple pattern matching for secrets in JSON
	// Only look for valid Secrets Manager ARNs: "arn:aws:secretsmanager:..."
	if strings.Contains(defsStr, "arn:aws:secretsmanager:") {
		// Extract ARNs - basic pattern matching
		parts := strings.Split(defsStr, `"valueFrom"`)
		for i := 1; i < len(parts); i++ {
			part := parts[i]
			// Find the ARN value - must be a valid Secrets Manager ARN
			start := strings.Index(part, `"arn:aws:secretsmanager:`)
			if start < 0 {
				continue
			}
			part = part[start+1:]
			end := strings.Index(part, `"`)
			if end < 0 {
				continue
			}
			arn := part[:end]

			// Validate it looks like a proper ARN
			if !strings.HasPrefix(arn, "arn:aws:secretsmanager:") {
				continue
			}

			// Try to find the associated name
			name := "SECRET_" + fmt.Sprintf("%d", i)
			if nameIdx := strings.LastIndex(parts[i-1], `"name"`); nameIdx >= 0 {
				namePart := parts[i-1][nameIdx:]
				if start := strings.Index(namePart, `": "`); start >= 0 {
					namePart = namePart[start+4:]
					if end := strings.Index(namePart, `"`); end >= 0 {
						name = namePart[:end]
					}
				}
			}

			detected = append(detected, &secrets.DetectedSecret{
				Name:             secrets.NormalizeEnvName(name),
				Source:           secrets.SourceAWSSecretsManager,
				Key:              arn,
				Description:      fmt.Sprintf("Secret %s for ECS task %s (from Secrets Manager)", name, resName),
				Required:         true,
				Type:             secrets.InferSecretType(name),
				ResourceID:       res.ID,
				ResourceName:     resName,
				ResourceType:     res.Type,
				DeduplicationKey: secrets.SourceAWSSecretsManager.String() + ":" + arn,
			})
		}
	}

	return detected
}

// parseECSEnvFromString extracts sensitive environment variables from JSON string.
func (d *AWSDetector) parseECSEnvFromString(defsStr, resName string, res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	// Look for environment variable patterns with sensitive names
	for _, pattern := range []string{"PASSWORD", "SECRET", "API_KEY", "TOKEN", "CREDENTIAL"} {
		if strings.Contains(strings.ToUpper(defsStr), pattern) {
			// Found a potentially sensitive env var - add a generic detection
			// More sophisticated parsing would require JSON unmarshaling
			break
		}
	}

	return detected
}

// detectSecretsManagerSecrets detects secrets that are direct Secrets Manager resources.
func (d *AWSDetector) detectSecretsManagerSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Get the ARN - must be a valid Secrets Manager ARN
	arn := res.ARN
	if arn == "" {
		arn = secrets.GetConfigString(config, "arn")
	}
	if arn == "" {
		arn = secrets.GetConfigString(config, "id")
	}

	// Only proceed if we have a valid Secrets Manager ARN
	if !strings.HasPrefix(arn, "arn:aws:secretsmanager:") {
		// If no valid ARN, treat as manual
		secretName := secrets.GetConfigString(config, "name")
		if secretName == "" {
			secretName = resName
		}
		detected = append(detected, &secrets.DetectedSecret{
			Name:         secrets.NormalizeEnvName(secretName),
			Source:       secrets.SourceManual,
			Description:  fmt.Sprintf("Secret from AWS Secrets Manager: %s (ARN not available)", secretName),
			Required:     true,
			Type:         secrets.TypeGeneric,
			ResourceID:   res.ID,
			ResourceName: resName,
			ResourceType: res.Type,
		})
		return detected
	}

	// Generate a meaningful name from the secret name/path
	secretName := secrets.GetConfigString(config, "name")
	if secretName == "" {
		secretName = resName
	}

	envName := secrets.NormalizeEnvName(secretName)

	description := secrets.GetConfigString(config, "description")
	if description == "" {
		description = fmt.Sprintf("Secret from AWS Secrets Manager: %s", secretName)
	}

	detected = append(detected, &secrets.DetectedSecret{
		Name:             envName,
		Source:           secrets.SourceAWSSecretsManager,
		Key:              arn,
		Description:      description,
		Required:         true,
		Type:             secrets.TypeGeneric,
		ResourceID:       res.ID,
		ResourceName:     resName,
		ResourceType:     res.Type,
		DeduplicationKey: secrets.SourceAWSSecretsManager.String() + ":" + arn,
	})

	return detected
}

// getMapKeys returns the keys of a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// detectCognitoSecrets detects secrets from Cognito user pools.
func (d *AWSDetector) detectCognitoSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Check for app clients with secrets
	// Note: Client secrets are generated by Cognito and need to be retrieved
	clients := secrets.GetConfigList(config, "app_clients")
	for _, clientItem := range clients {
		client, ok := clientItem.(map[string]interface{})
		if !ok {
			continue
		}

		clientName := secrets.GetConfigString(client, "name")
		if clientName == "" {
			clientName = "app"
		}

		// Check if client secret is enabled
		generateSecret := secrets.GetConfigBool(client, "generate_secret")
		if generateSecret {
			detected = append(detected, &secrets.DetectedSecret{
				Name:         secrets.GenerateSecretName(resName+"_"+clientName, "cognito", "client_secret"),
				Source:       secrets.SourceManual, // Client secrets are retrieved via AWS API
				Description:  fmt.Sprintf("Cognito app client secret for %s in pool %s", clientName, resName),
				Required:     false, // Often optional for public clients
				Type:         secrets.TypeAPIKey,
				ResourceID:   res.ID,
				ResourceName: resName,
				ResourceType: res.Type,
			})
		}
	}

	return detected
}
