package detector

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/secrets"
)

// AzureDetector detects secrets from Azure resources.
type AzureDetector struct {
	*secrets.BaseDetector
}

// NewAzureDetector creates a new Azure secret detector.
func NewAzureDetector() *AzureDetector {
	return &AzureDetector{
		BaseDetector: secrets.NewBaseDetector(
			resource.ProviderAzure,
			// Databases
			resource.TypeAzureSQL,
			resource.TypeAzurePostgres,
			resource.TypeAzureMySQL,
			resource.TypeCosmosDB,
			resource.TypeAzureCache,
			// Compute
			resource.TypeAppService,
			resource.TypeAzureFunction,
			resource.TypeContainerInstance,
			// Security
			resource.TypeKeyVault,
		),
	}
}

// Detect analyzes an Azure resource and returns detected secrets.
func (d *AzureDetector) Detect(ctx context.Context, res *resource.AWSResource) ([]*secrets.DetectedSecret, error) {
	switch res.Type {
	case resource.TypeAzureSQL:
		return d.detectAzureSQLSecrets(res), nil
	case resource.TypeAzurePostgres:
		return d.detectAzurePostgresSecrets(res), nil
	case resource.TypeAzureMySQL:
		return d.detectAzureMySQLSecrets(res), nil
	case resource.TypeCosmosDB:
		return d.detectCosmosDBSecrets(res), nil
	case resource.TypeAzureCache:
		return d.detectAzureCacheSecrets(res), nil
	case resource.TypeAppService, resource.TypeAzureFunction:
		return d.detectAppServiceSecrets(res), nil
	case resource.TypeContainerInstance:
		return d.detectContainerInstanceSecrets(res), nil
	case resource.TypeKeyVault:
		return d.detectKeyVaultSecrets(res), nil
	default:
		return nil, nil
	}
}

// detectAzureSQLSecrets detects secrets from Azure SQL databases.
// Password is at server level, so we deduplicate by server_name to avoid
// generating duplicate secrets for each database on the same server.
func (d *AzureDetector) detectAzureSQLSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Get server name - password is at server level, not database level
	serverName := secrets.GetConfigString(config, "server_name")
	if serverName == "" {
		// Fall back to resource name (might be server-only resource)
		serverName = resName
	}

	// Use server name for deduplication - all databases on same server share password
	dedupeKey := "azuresql:server:" + serverName

	// Azure SQL requires admin password (at server level)
	detected = append(detected, &secrets.DetectedSecret{
		Name:             secrets.GenerateSecretName(serverName, "azuresql", "password"),
		Source:           secrets.SourceManual,
		Description:      fmt.Sprintf("Administrator password for Azure SQL Server %s", serverName),
		Required:         true,
		Type:             secrets.TypePassword,
		ResourceID:       res.ID,
		ResourceName:     resName,
		ResourceType:     res.Type,
		DeduplicationKey: dedupeKey,
	})

	// Check for Key Vault reference
	if kvRef := secrets.GetConfigString(config, "administrator_login_password_key_vault_secret_id"); kvRef != "" {
		// Override with Key Vault source
		detected[0].Source = secrets.SourceAzureKeyVault
		detected[0].Key = kvRef
		detected[0].Description = fmt.Sprintf("Administrator password for Azure SQL Server %s (from Key Vault)", serverName)
		detected[0].DeduplicationKey = secrets.SourceAzureKeyVault.String() + ":" + kvRef
	}

	return detected
}

// detectAzurePostgresSecrets detects secrets from Azure PostgreSQL.
func (d *AzureDetector) detectAzurePostgresSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Use server name for deduplication
	dedupeKey := "azurepostgres:server:" + resName

	// PostgreSQL flexible server requires admin password
	detected = append(detected, &secrets.DetectedSecret{
		Name:             secrets.GenerateSecretName(resName, "postgres", "password"),
		Source:           secrets.SourceManual,
		Description:      fmt.Sprintf("Administrator password for Azure PostgreSQL %s", resName),
		Required:         true,
		Type:             secrets.TypePassword,
		ResourceID:       res.ID,
		ResourceName:     resName,
		ResourceType:     res.Type,
		DeduplicationKey: dedupeKey,
	})

	// Check for Key Vault reference
	if kvRef := secrets.GetConfigString(config, "administrator_password_key_vault_secret_id"); kvRef != "" {
		detected[0].Source = secrets.SourceAzureKeyVault
		detected[0].Key = kvRef
		detected[0].Description = fmt.Sprintf("Administrator password for Azure PostgreSQL %s (from Key Vault)", resName)
		detected[0].DeduplicationKey = secrets.SourceAzureKeyVault.String() + ":" + kvRef
	}

	return detected
}

// detectAzureMySQLSecrets detects secrets from Azure MySQL.
func (d *AzureDetector) detectAzureMySQLSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Use server name for deduplication
	dedupeKey := "azuremysql:server:" + resName

	// MySQL flexible server requires admin password
	detected = append(detected, &secrets.DetectedSecret{
		Name:             secrets.GenerateSecretName(resName, "mysql", "password"),
		Source:           secrets.SourceManual,
		Description:      fmt.Sprintf("Administrator password for Azure MySQL %s", resName),
		Required:         true,
		Type:             secrets.TypePassword,
		ResourceID:       res.ID,
		ResourceName:     resName,
		ResourceType:     res.Type,
		DeduplicationKey: dedupeKey,
	})

	// Check for Key Vault reference
	if kvRef := secrets.GetConfigString(config, "administrator_password_key_vault_secret_id"); kvRef != "" {
		detected[0].Source = secrets.SourceAzureKeyVault
		detected[0].Key = kvRef
		detected[0].Description = fmt.Sprintf("Administrator password for Azure MySQL %s (from Key Vault)", resName)
		detected[0].DeduplicationKey = secrets.SourceAzureKeyVault.String() + ":" + kvRef
	}

	return detected
}

// detectCosmosDBSecrets detects secrets from CosmosDB.
func (d *AzureDetector) detectCosmosDBSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// CosmosDB has primary and secondary keys (at account level)
	detected = append(detected, &secrets.DetectedSecret{
		Name:             secrets.GenerateSecretName(resName, "cosmosdb", "primary_key"),
		Source:           secrets.SourceManual,
		Description:      fmt.Sprintf("Primary key for CosmosDB account %s", resName),
		Required:         true,
		Type:             secrets.TypeAPIKey,
		ResourceID:       res.ID,
		ResourceName:     resName,
		ResourceType:     res.Type,
		DeduplicationKey: "cosmosdb:account:" + resName + ":primary_key",
	})

	// Connection string is also commonly needed
	detected = append(detected, &secrets.DetectedSecret{
		Name:             secrets.GenerateSecretName(resName, "cosmosdb", "connection_string"),
		Source:           secrets.SourceManual,
		Description:      fmt.Sprintf("Connection string for CosmosDB account %s", resName),
		Required:         false,
		Type:             secrets.TypeConnectionString,
		ResourceID:       res.ID,
		ResourceName:     resName,
		ResourceType:     res.Type,
		DeduplicationKey: "cosmosdb:account:" + resName + ":connection_string",
	})

	return detected
}

// detectAzureCacheSecrets detects secrets from Azure Cache for Redis.
func (d *AzureDetector) detectAzureCacheSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Redis cache has access keys
	detected = append(detected, &secrets.DetectedSecret{
		Name:             secrets.GenerateSecretName(resName, "redis", "primary_access_key"),
		Source:           secrets.SourceManual,
		Description:      fmt.Sprintf("Primary access key for Azure Cache for Redis %s", resName),
		Required:         true,
		Type:             secrets.TypeAPIKey,
		ResourceID:       res.ID,
		ResourceName:     resName,
		ResourceType:     res.Type,
		DeduplicationKey: "azurecache:redis:" + resName,
	})

	return detected
}

// detectAppServiceSecrets detects secrets from App Service and Function Apps.
func (d *AzureDetector) detectAppServiceSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Check app_settings
	appSettings := secrets.GetConfigMap(config, "app_settings")
	if appSettings == nil {
		// Try site_config -> app_settings
		if siteConfig := secrets.GetConfigMap(config, "site_config"); siteConfig != nil {
			appSettings = secrets.GetConfigMap(siteConfig, "app_settings")
		}
	}

	if appSettings != nil {
		for key, value := range appSettings {
			// Check for Key Vault references
			if strValue, ok := value.(string); ok && strings.HasPrefix(strValue, "@Microsoft.KeyVault") {
				// Extract Key Vault reference
				// Format: @Microsoft.KeyVault(SecretUri=https://myvault.vault.azure.net/secrets/mysecret/)
				kvRef := extractKeyVaultReference(strValue)
				if kvRef != "" {
					detected = append(detected, &secrets.DetectedSecret{
						Name:             secrets.NormalizeEnvName(key),
						Source:           secrets.SourceAzureKeyVault,
						Key:              kvRef,
						Description:      fmt.Sprintf("App setting %s for %s (from Key Vault)", key, resName),
						Required:         true,
						Type:             secrets.InferSecretType(key),
						ResourceID:       res.ID,
						ResourceName:     resName,
						ResourceType:     res.Type,
						DeduplicationKey: secrets.SourceAzureKeyVault.String() + ":" + kvRef,
					})
					continue
				}
			}

			// Check for sensitive names
			if secrets.IsSensitiveEnvName(key) {
				detected = append(detected, &secrets.DetectedSecret{
					Name:         secrets.NormalizeEnvName(key),
					Source:       secrets.SourceManual,
					Description:  fmt.Sprintf("App setting %s from %s", key, resName),
					Required:     true,
					Type:         secrets.InferSecretType(key),
					ResourceID:   res.ID,
					ResourceName: resName,
					ResourceType: res.Type,
				})
			}
		}
	}

	// Check connection_strings
	connStrings := secrets.GetConfigList(config, "connection_string")
	for _, connItem := range connStrings {
		conn, ok := connItem.(map[string]interface{})
		if !ok {
			continue
		}

		name := secrets.GetConfigString(conn, "name")
		value := secrets.GetConfigString(conn, "value")

		if name != "" {
			// Check for Key Vault reference
			if strings.HasPrefix(value, "@Microsoft.KeyVault") {
				kvRef := extractKeyVaultReference(value)
				if kvRef != "" {
					detected = append(detected, &secrets.DetectedSecret{
						Name:             secrets.NormalizeEnvName(name + "_CONNECTION_STRING"),
						Source:           secrets.SourceAzureKeyVault,
						Key:              kvRef,
						Description:      fmt.Sprintf("Connection string %s for %s (from Key Vault)", name, resName),
						Required:         true,
						Type:             secrets.TypeConnectionString,
						ResourceID:       res.ID,
						ResourceName:     resName,
						ResourceType:     res.Type,
						DeduplicationKey: secrets.SourceAzureKeyVault.String() + ":" + kvRef,
					})
					continue
				}
			}

			// Manual connection string
			detected = append(detected, &secrets.DetectedSecret{
				Name:         secrets.NormalizeEnvName(name + "_CONNECTION_STRING"),
				Source:       secrets.SourceManual,
				Description:  fmt.Sprintf("Connection string %s for %s", name, resName),
				Required:     true,
				Type:         secrets.TypeConnectionString,
				ResourceID:   res.ID,
				ResourceName: resName,
				ResourceType: res.Type,
			})
		}
	}

	return detected
}

// detectContainerInstanceSecrets detects secrets from Container Instances.
func (d *AzureDetector) detectContainerInstanceSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Check containers
	containers := secrets.GetConfigList(config, "container")
	for _, containerItem := range containers {
		container, ok := containerItem.(map[string]interface{})
		if !ok {
			continue
		}

		containerName := secrets.GetConfigString(container, "name")
		if containerName == "" {
			containerName = resName
		}

		// Check secure_environment_variables
		secureEnv := secrets.GetConfigMap(container, "secure_environment_variables")
		for key := range secureEnv {
			detected = append(detected, &secrets.DetectedSecret{
				Name:         secrets.NormalizeEnvName(key),
				Source:       secrets.SourceManual,
				Description:  fmt.Sprintf("Secure environment variable %s from container %s", key, containerName),
				Required:     true,
				Type:         secrets.InferSecretType(key),
				ResourceID:   res.ID,
				ResourceName: resName,
				ResourceType: res.Type,
			})
		}

		// Check regular environment variables for sensitive names
		envVars := secrets.GetConfigMap(container, "environment_variables")
		for key := range envVars {
			if secrets.IsSensitiveEnvName(key) {
				detected = append(detected, &secrets.DetectedSecret{
					Name:         secrets.NormalizeEnvName(key),
					Source:       secrets.SourceManual,
					Description:  fmt.Sprintf("Environment variable %s from container %s", key, containerName),
					Required:     true,
					Type:         secrets.InferSecretType(key),
					ResourceID:   res.ID,
					ResourceName: resName,
					ResourceType: res.Type,
				})
			}
		}
	}

	// Check image_registry_credential for container registry access
	registryCreds := secrets.GetConfigList(config, "image_registry_credential")
	for _, credItem := range registryCreds {
		cred, ok := credItem.(map[string]interface{})
		if !ok {
			continue
		}

		server := secrets.GetConfigString(cred, "server")
		if server != "" {
			serverName := strings.ReplaceAll(server, ".", "_")
			detected = append(detected, &secrets.DetectedSecret{
				Name:         secrets.NormalizeEnvName(serverName + "_REGISTRY_PASSWORD"),
				Source:       secrets.SourceManual,
				Description:  fmt.Sprintf("Container registry password for %s", server),
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

// detectKeyVaultSecrets detects secrets that are direct Key Vault resources.
func (d *AzureDetector) detectKeyVaultSecrets(res *resource.AWSResource) []*secrets.DetectedSecret {
	var detected []*secrets.DetectedSecret

	config := res.Config
	resName := res.Name
	if resName == "" {
		resName = res.ID
	}

	// Get vault name
	vaultName := secrets.GetConfigString(config, "name")
	if vaultName == "" {
		vaultName = resName
	}

	// For Key Vault resources, we detect them as a reference point
	// The actual secrets within would need to be enumerated separately
	envName := secrets.NormalizeEnvName(vaultName)

	detected = append(detected, &secrets.DetectedSecret{
		Name:             envName + "_VAULT_URL",
		Source:           secrets.SourceAzureKeyVault,
		Key:              vaultName,
		Description:      fmt.Sprintf("Azure Key Vault reference: %s", vaultName),
		Required:         false,
		Type:             secrets.TypeGeneric,
		ResourceID:       res.ID,
		ResourceName:     resName,
		ResourceType:     res.Type,
		DeduplicationKey: secrets.SourceAzureKeyVault.String() + ":vault:" + vaultName,
	})

	return detected
}

// extractKeyVaultReference extracts the Key Vault URI from an App Service Key Vault reference.
// Input format: @Microsoft.KeyVault(SecretUri=https://myvault.vault.azure.net/secrets/mysecret/)
func extractKeyVaultReference(ref string) string {
	// Look for SecretUri=
	if idx := strings.Index(ref, "SecretUri="); idx >= 0 {
		ref = ref[idx+10:]
		// Remove trailing )
		if endIdx := strings.Index(ref, ")"); endIdx >= 0 {
			ref = ref[:endIdx]
		}
		return strings.TrimSpace(ref)
	}

	// Look for VaultName and SecretName format
	// @Microsoft.KeyVault(VaultName=myvault;SecretName=mysecret)
	if strings.Contains(ref, "VaultName=") && strings.Contains(ref, "SecretName=") {
		var vaultName, secretName string
		parts := strings.Split(ref, ";")
		for _, part := range parts {
			if strings.HasPrefix(part, "VaultName=") {
				vaultName = strings.TrimPrefix(part, "VaultName=")
				vaultName = strings.TrimSuffix(vaultName, ")")
			}
			if strings.HasPrefix(part, "SecretName=") {
				secretName = strings.TrimPrefix(part, "SecretName=")
				secretName = strings.TrimSuffix(secretName, ")")
			}
		}
		if vaultName != "" && secretName != "" {
			return vaultName + "/" + secretName
		}
	}

	return ""
}
