package detector

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/secrets"
)

// GCPDetector detects secrets from GCP resources.
type GCPDetector struct {
	*secrets.BaseDetector
}

// NewGCPDetector creates a new GCP secret detector.
func NewGCPDetector() *GCPDetector {
	return &GCPDetector{
		BaseDetector: secrets.NewBaseDetector(
			resource.ProviderGCP,
			// Databases
			resource.TypeCloudSQL,
			resource.TypeMemorystore,
			resource.TypeFirestore,
			resource.TypeSpanner,
			resource.TypeBigtable,
			// Compute
			resource.TypeCloudRun,
			resource.TypeCloudFunction,
			// Security
			resource.TypeSecretManager,
		),
	}
}

// Detect analyzes a GCP resource and returns detected secrets.
func (d *GCPDetector) Detect(ctx context.Context, res *resource.AWSResource) ([]*secrets.DetectedSecret, error) {
	switch res.Type {
	case resource.TypeCloudSQL:
		return d.detectCloudSQLSecrets(res), nil
	case resource.TypeMemorystore:
		return d.detectMemorystoreSecrets(res), nil
	case resource.TypeCloudRun:
		return d.detectCloudRunSecrets(res), nil
	case resource.TypeCloudFunction:
		return d.detectCloudFunctionSecrets(res), nil
	case resource.TypeSecretManager:
		return d.detectSecretManagerSecrets(res), nil
	default:
		return nil, nil
	}
}

// detectCloudSQLSecrets detects secrets from Cloud SQL instances.
func (d *GCPDetector) detectCloudSQLSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Cloud SQL always needs a root/admin password
	databaseVersion := secrets.GetConfigString(config, "database_version")
	dbType := "database"
	if strings.HasPrefix(databaseVersion, "POSTGRES") {
		dbType = "postgres"
	} else if strings.HasPrefix(databaseVersion, "MYSQL") {
		dbType = "mysql"
	} else if strings.HasPrefix(databaseVersion, "SQLSERVER") {
		dbType = "sqlserver"
	}

	detected = append(detected, &secrets.DetectedSecret{
		Name:         secrets.GenerateSecretName(resName, "cloudsql", "password"),
		Source:       secrets.SourceManual,
		Description:  fmt.Sprintf("Root password for Cloud SQL instance %s (%s)", resName, dbType),
		Required:     true,
		Type:         secrets.TypePassword,
		ResourceID:   res.ID,
		ResourceName: resName,
		ResourceType: res.Type,
	})

	// Check for additional users
	users := secrets.GetConfigList(config, "users")
	for _, userItem := range users {
		user, ok := userItem.(map[string]interface{})
		if !ok {
			continue
		}

		userName := secrets.GetConfigString(user, "name")
		if userName != "" && userName != "root" {
			detected = append(detected, &secrets.DetectedSecret{
				Name:         secrets.GenerateSecretName(resName+"_"+userName, "cloudsql", "password"),
				Source:       secrets.SourceManual,
				Description:  fmt.Sprintf("Password for Cloud SQL user %s in instance %s", userName, resName),
				Required:     true,
				Type:         secrets.TypePassword,
				ResourceID:   res.ID,
				ResourceName: resName,
				ResourceType: res.Type,
			})
		}
	}

	return detected
}

// detectMemorystoreSecrets detects secrets from Memorystore (Redis) instances.
func (d *GCPDetector) detectMemorystoreSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Check if auth is enabled
	authEnabled := secrets.GetConfigBool(config, "auth_enabled")
	if authEnabled {
		detected = append(detected, &secrets.DetectedSecret{
			Name:         secrets.GenerateSecretName(resName, "memorystore", "auth_string"),
			Source:       secrets.SourceManual,
			Description:  fmt.Sprintf("Auth string for Memorystore Redis instance %s", resName),
			Required:     true,
			Type:         secrets.TypePassword,
			ResourceID:   res.ID,
			ResourceName: resName,
			ResourceType: res.Type,
		})
	}

	return detected
}

// detectCloudRunSecrets detects secrets from Cloud Run services.
func (d *GCPDetector) detectCloudRunSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Check template -> spec -> containers -> env
	template := secrets.GetConfigMap(config, "template")
	if template == nil {
		return detected
	}

	spec := secrets.GetConfigMap(template, "spec")
	if spec == nil {
		return detected
	}

	containers := secrets.GetConfigList(spec, "containers")
	for _, containerItem := range containers {
		container, ok := containerItem.(map[string]interface{})
		if !ok {
			continue
		}

		// Check environment variables
		envList := secrets.GetConfigList(container, "env")
		for _, envItem := range envList {
			env, ok := envItem.(map[string]interface{})
			if !ok {
				continue
			}

			name := secrets.GetConfigString(env, "name")

			// Check for secretKeyRef
			if valueFrom := secrets.GetConfigMap(env, "value_from"); valueFrom != nil {
				if secretRef := secrets.GetConfigMap(valueFrom, "secret_key_ref"); secretRef != nil {
					secretName := secrets.GetConfigString(secretRef, "name")
					secretKey := secrets.GetConfigString(secretRef, "key")

					if secretName != "" {
						// This is a reference to GCP Secret Manager
						gcpSecretPath := secretName
						if secretKey != "" {
							gcpSecretPath += "/" + secretKey
						}

						detected = append(detected, &secrets.DetectedSecret{
							Name:             secrets.NormalizeEnvName(name),
							Source:           secrets.SourceGCPSecretManager,
							Key:              gcpSecretPath,
							Description:      fmt.Sprintf("Secret %s for Cloud Run service %s (from Secret Manager)", name, resName),
							Required:         true,
							Type:             secrets.InferSecretType(name),
							ResourceID:       res.ID,
							ResourceName:     resName,
							ResourceType:     res.Type,
							DeduplicationKey: secrets.SourceGCPSecretManager.String() + ":" + gcpSecretPath,
						})
						continue
					}
				}
			}

			// Check for sensitive env var names
			if secrets.IsSensitiveEnvName(name) {
				detected = append(detected, &secrets.DetectedSecret{
					Name:         secrets.NormalizeEnvName(name),
					Source:       secrets.SourceManual,
					Description:  fmt.Sprintf("Environment variable %s from Cloud Run service %s", name, resName),
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

// detectCloudFunctionSecrets detects secrets from Cloud Functions.
func (d *GCPDetector) detectCloudFunctionSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Check environment_variables
	envVars := secrets.GetConfigMap(config, "environment_variables")
	for key := range envVars {
		if secrets.IsSensitiveEnvName(key) {
			detected = append(detected, &secrets.DetectedSecret{
				Name:         secrets.NormalizeEnvName(key),
				Source:       secrets.SourceManual,
				Description:  fmt.Sprintf("Environment variable %s from Cloud Function %s", key, resName),
				Required:     true,
				Type:         secrets.InferSecretType(key),
				ResourceID:   res.ID,
				ResourceName: resName,
				ResourceType: res.Type,
			})
		}
	}

	// Check secret_environment_variables (direct Secret Manager references)
	secretEnvVars := secrets.GetConfigList(config, "secret_environment_variables")
	for _, secretItem := range secretEnvVars {
		secret, ok := secretItem.(map[string]interface{})
		if !ok {
			continue
		}

		key := secrets.GetConfigString(secret, "key")
		projectID := secrets.GetConfigString(secret, "project_id")
		secretID := secrets.GetConfigString(secret, "secret")
		version := secrets.GetConfigString(secret, "version")

		if secretID != "" {
			// Construct Secret Manager path
			gcpSecretPath := secretID
			if projectID != "" {
				gcpSecretPath = fmt.Sprintf("projects/%s/secrets/%s", projectID, secretID)
			}
			if version != "" {
				gcpSecretPath += "/versions/" + version
			}

			detected = append(detected, &secrets.DetectedSecret{
				Name:             secrets.NormalizeEnvName(key),
				Source:           secrets.SourceGCPSecretManager,
				Key:              gcpSecretPath,
				Description:      fmt.Sprintf("Secret %s for Cloud Function %s (from Secret Manager)", key, resName),
				Required:         true,
				Type:             secrets.InferSecretType(key),
				ResourceID:       res.ID,
				ResourceName:     resName,
				ResourceType:     res.Type,
				DeduplicationKey: secrets.SourceGCPSecretManager.String() + ":" + gcpSecretPath,
			})
		}
	}

	return detected
}

// detectSecretManagerSecrets detects secrets that are direct GCP Secret Manager resources.
func (d *GCPDetector) detectSecretManagerSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Get the secret ID
	secretID := secrets.GetConfigString(config, "secret_id")
	if secretID == "" {
		secretID = resName
	}

	// Get project if available
	project := secrets.GetConfigString(config, "project")
	gcpSecretPath := secretID
	if project != "" {
		gcpSecretPath = fmt.Sprintf("projects/%s/secrets/%s", project, secretID)
	}

	envName := secrets.NormalizeEnvName(secretID)

	detected = append(detected, &secrets.DetectedSecret{
		Name:             envName,
		Source:           secrets.SourceGCPSecretManager,
		Key:              gcpSecretPath,
		Description:      fmt.Sprintf("Secret from GCP Secret Manager: %s", secretID),
		Required:         true,
		Type:             secrets.TypeGeneric,
		ResourceID:       res.ID,
		ResourceName:     resName,
		ResourceType:     res.Type,
		DeduplicationKey: secrets.SourceGCPSecretManager.String() + ":" + gcpSecretPath,
	})

	return detected
}
