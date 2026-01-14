// Package stacks provides stack-specific merger implementations for consolidating
// cloud resources into self-hosted equivalents.
package stacks

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/policy"
	"github.com/homeport/homeport/internal/domain/resource"
	"github.com/homeport/homeport/internal/domain/stack"
	"github.com/homeport/homeport/internal/infrastructure/consolidator"
)

// SecretsMerger consolidates secret management resources into HashiCorp Vault.
// It handles AWS Secrets Manager, AWS KMS, GCP Secret Manager, Azure Key Vault,
// and similar services, mapping them to a unified Vault deployment.
type SecretsMerger struct {
	*consolidator.BaseMerger
}

// NewSecretsMerger creates a new SecretsMerger instance.
func NewSecretsMerger() *SecretsMerger {
	return &SecretsMerger{
		BaseMerger: consolidator.NewBaseMerger(stack.StackTypeSecrets),
	}
}

// StackType returns the stack type this merger handles.
func (m *SecretsMerger) StackType() stack.StackType {
	return stack.StackTypeSecrets
}

// CanMerge checks if this merger can handle the given results.
// Returns true if there are any secret management resources present.
func (m *SecretsMerger) CanMerge(results []*mapper.MappingResult) bool {
	if len(results) == 0 {
		return false
	}

	for _, result := range results {
		if result == nil {
			continue
		}
		resourceType := strings.ToLower(result.SourceResourceType)
		if isSecretResource(resourceType) {
			return true
		}
	}

	return false
}

// Merge creates a consolidated secrets stack with HashiCorp Vault.
// It generates:
// - Vault server configuration
// - Secret path mappings from each cloud provider
// - Vault policies based on IAM policies
// - Migration scripts for secret values
// - Unseal/init scripts for production setup
func (m *SecretsMerger) Merge(ctx context.Context, results []*mapper.MappingResult, opts *consolidator.MergeOptions) (*stack.Stack, error) {
	if opts == nil {
		opts = consolidator.DefaultOptions()
	}

	// Create the stack
	name := "secrets"
	if opts.NamePrefix != "" {
		name = opts.NamePrefix + "-secrets"
	}

	stk := stack.NewStack(stack.StackTypeSecrets, name)
	stk.Description = "Secret management with HashiCorp Vault"

	// Create Vault service
	vaultService := m.createVaultService(opts)
	stk.AddService(vaultService)

	// Add data volume
	stk.AddVolume(stack.Volume{
		Name:   "vault-data",
		Driver: "local",
		Labels: map[string]string{
			"homeport.io/stack": "secrets",
			"homeport.io/role":  "data",
		},
	})

	// Add logs volume
	stk.AddVolume(stack.Volume{
		Name:   "vault-logs",
		Driver: "local",
		Labels: map[string]string{
			"homeport.io/stack": "secrets",
			"homeport.io/role":  "logs",
		},
	})

	// Add config volume
	stk.AddVolume(stack.Volume{
		Name:   "vault-config",
		Driver: "local",
		Labels: map[string]string{
			"homeport.io/stack": "secrets",
			"homeport.io/role":  "config",
		},
	})

	// Collect secret paths and generate policies
	var secretPaths []SecretPath
	var vaultPolicies []VaultPolicy

	for _, result := range results {
		if result == nil {
			continue
		}

		// Add source resource
		res := &resource.Resource{
			Type: resource.Type(result.SourceResourceType),
			Name: result.SourceResourceName,
		}
		stk.AddSourceResource(res)

		// Convert to Vault path based on source type
		path := m.convertToVaultPath(result)
		if path.Path != "" {
			secretPaths = append(secretPaths, path)
		}

		// Generate Vault policies from source IAM policies
		for _, p := range result.Policies {
			vaultPolicy := m.convertPolicyToVaultPolicy(p, path.Path)
			if vaultPolicy != nil {
				vaultPolicies = append(vaultPolicies, *vaultPolicy)
			}
		}

		// Merge warnings
		for _, warning := range result.Warnings {
			stk.Metadata["warning_"+result.SourceResourceName] = warning
		}
	}

	// Generate Vault configuration file
	vaultConfig := m.generateVaultConfig()
	stk.AddConfig("vault.hcl", vaultConfig)

	// Generate Vault policies file
	if len(vaultPolicies) > 0 {
		policiesContent := m.generateVaultPoliciesHCL(vaultPolicies)
		stk.AddConfig("policies.hcl", policiesContent)
	}

	// Generate migration script for importing secrets
	if len(secretPaths) > 0 {
		migrationScript := m.generateMigrationScript(secretPaths)
		stk.AddScript("migrate-secrets.sh", migrationScript)
	}

	// Generate init/unseal script
	unsealScript := m.generateUnsealScript()
	stk.AddScript("init-vault.sh", unsealScript)

	// Generate secrets path mapping documentation
	pathMappingDoc := m.generatePathMappingDoc(secretPaths)
	stk.AddConfig("secret-paths.md", pathMappingDoc)

	// Add manual steps for secret migration
	stk.Metadata["manual_step_1"] = "Export secrets from cloud providers (secrets cannot be auto-exported for security)"
	stk.Metadata["manual_step_2"] = "Run init-vault.sh to initialize and unseal Vault"
	stk.Metadata["manual_step_3"] = "Run migrate-secrets.sh to import secrets into Vault"
	stk.Metadata["manual_step_4"] = "Apply policies.hcl to Vault"
	stk.Metadata["manual_step_5"] = "Update application configurations to use Vault addresses"

	// Add network
	stk.AddNetwork(stack.Network{
		Name:   "secrets-net",
		Driver: "bridge",
	})

	return stk, nil
}

// createVaultService creates the main Vault service configuration.
func (m *SecretsMerger) createVaultService(opts *consolidator.MergeOptions) *stack.Service {
	svc := stack.NewService("vault", "hashicorp/vault:latest")

	svc.Ports = []string{"8200:8200"}
	svc.Command = []string{"server"}

	svc.Environment = map[string]string{
		"VAULT_ADDR":              "http://0.0.0.0:8200",
		"VAULT_API_ADDR":          "http://0.0.0.0:8200",
		"VAULT_LOCAL_CONFIG":      `{"ui": true, "listener": {"tcp": {"address": "0.0.0.0:8200", "tls_disable": 1}}}`,
		"VAULT_DEV_ROOT_TOKEN_ID": "dev-token-${VAULT_DEV_TOKEN:-homeport}",
	}

	svc.Volumes = []string{
		"vault-data:/vault/data",
		"vault-logs:/vault/logs",
		"vault-config:/vault/config",
	}

	svc.Labels = map[string]string{
		"homeport.io/stack":     "secrets",
		"homeport.io/role":      "primary",
		"homeport.io/component": "vault",
	}

	// Add IPC_LOCK capability for mlock
	svc.HealthCheck = &stack.HealthCheck{
		Test:        []string{"CMD", "vault", "status"},
		Interval:    "30s",
		Timeout:     "10s",
		Retries:     3,
		StartPeriod: "10s",
	}

	svc.Restart = "unless-stopped"
	svc.Networks = []string{"secrets-net"}

	// Add deployment config
	svc.Deploy = &stack.DeployConfig{
		Replicas: 1,
		Resources: &stack.ResourceConfig{
			Limits: &stack.ResourceSpec{
				CPUs:   "1",
				Memory: "512M",
			},
			Reservations: &stack.ResourceSpec{
				CPUs:   "0.25",
				Memory: "128M",
			},
		},
	}

	return svc
}

// VaultConfig represents the Vault server configuration.
type VaultConfig struct {
	Storage  StorageConfig  `hcl:"storage,block"`
	Listener ListenerConfig `hcl:"listener,block"`
	UI       bool           `hcl:"ui"`
	APIAddr  string         `hcl:"api_addr"`
}

// StorageConfig represents Vault storage backend configuration.
type StorageConfig struct {
	Type string `hcl:"type,label"`
	Path string `hcl:"path"`
}

// ListenerConfig represents Vault listener configuration.
type ListenerConfig struct {
	Type       string `hcl:"type,label"`
	Address    string `hcl:"address"`
	TLSDisable bool   `hcl:"tls_disable"`
}

// VaultPolicy represents a Vault policy definition.
type VaultPolicy struct {
	Name  string
	Path  string
	Rules []PolicyRule
}

// PolicyRule represents a rule within a Vault policy.
type PolicyRule struct {
	Path         string
	Capabilities []string
}

// SecretPath represents a mapping from cloud secret to Vault path.
type SecretPath struct {
	SourceProvider string
	SourceType     string
	SourceName     string
	SourceARN      string
	Path           string
	Description    string
}

// generateVaultConfig generates the Vault server configuration file.
func (m *SecretsMerger) generateVaultConfig() []byte {
	config := `# Vault Server Configuration
# Generated by Homeport

# Storage backend - file-based for single node
storage "file" {
  path = "/vault/data"
}

# Listener configuration
listener "tcp" {
  address     = "0.0.0.0:8200"
  tls_disable = true  # Enable TLS in production!
}

# Enable UI
ui = true

# API address for redirection
api_addr = "http://127.0.0.1:8200"

# Disable mlock for development (enable in production)
disable_mlock = true

# Logging
log_level = "info"
log_format = "standard"

# Telemetry for monitoring
telemetry {
  disable_hostname = true
  prometheus_retention_time = "60s"
}
`
	return []byte(config)
}

// convertToVaultPath converts a cloud secret resource to a Vault path.
func (m *SecretsMerger) convertToVaultPath(result *mapper.MappingResult) SecretPath {
	resourceType := strings.ToLower(result.SourceResourceType)
	name := consolidator.NormalizeName(result.SourceResourceName)

	path := SecretPath{
		SourceName: result.SourceResourceName,
		SourceType: result.SourceResourceType,
	}

	switch {
	// AWS Secrets Manager
	case strings.Contains(resourceType, "secretsmanager"):
		path.SourceProvider = "aws"
		path.Path = fmt.Sprintf("secret/data/aws/secrets-manager/%s", name)
		path.Description = "AWS Secrets Manager secret"

	// AWS KMS
	case strings.Contains(resourceType, "kms"):
		path.SourceProvider = "aws"
		path.Path = fmt.Sprintf("transit/keys/aws/kms/%s", name)
		path.Description = "AWS KMS key (mapped to Vault Transit)"

	// AWS SSM Parameter Store
	case strings.Contains(resourceType, "ssm_parameter"):
		path.SourceProvider = "aws"
		path.Path = fmt.Sprintf("secret/data/aws/ssm/%s", name)
		path.Description = "AWS SSM Parameter Store parameter"

	// GCP Secret Manager
	case strings.Contains(resourceType, "google_secret_manager"):
		path.SourceProvider = "gcp"
		path.Path = fmt.Sprintf("secret/data/gcp/secret-manager/%s", name)
		path.Description = "GCP Secret Manager secret"

	// GCP KMS
	case strings.Contains(resourceType, "google_kms"):
		path.SourceProvider = "gcp"
		path.Path = fmt.Sprintf("transit/keys/gcp/kms/%s", name)
		path.Description = "GCP Cloud KMS key (mapped to Vault Transit)"

	// Azure Key Vault Secrets
	case strings.Contains(resourceType, "azurerm_key_vault_secret"):
		path.SourceProvider = "azure"
		path.Path = fmt.Sprintf("secret/data/azure/key-vault/secrets/%s", name)
		path.Description = "Azure Key Vault secret"

	// Azure Key Vault Keys
	case strings.Contains(resourceType, "azurerm_key_vault_key"):
		path.SourceProvider = "azure"
		path.Path = fmt.Sprintf("transit/keys/azure/key-vault/%s", name)
		path.Description = "Azure Key Vault key (mapped to Vault Transit)"

	// Azure Key Vault Certificates
	case strings.Contains(resourceType, "azurerm_key_vault_certificate"):
		path.SourceProvider = "azure"
		path.Path = fmt.Sprintf("pki/certs/azure/key-vault/%s", name)
		path.Description = "Azure Key Vault certificate (mapped to Vault PKI)"

	// Generic Key Vault
	case strings.Contains(resourceType, "key_vault"):
		path.SourceProvider = "azure"
		path.Path = fmt.Sprintf("secret/data/azure/key-vault/%s", name)
		path.Description = "Azure Key Vault item"

	default:
		// Generic secret path
		path.SourceProvider = "unknown"
		path.Path = fmt.Sprintf("secret/data/migrated/%s", name)
		path.Description = "Migrated secret"
	}

	return path
}

// convertPolicyToVaultPolicy converts a cloud IAM policy to Vault policy.
func (m *SecretsMerger) convertPolicyToVaultPolicy(p *policy.Policy, basePath string) *VaultPolicy {
	if p == nil || p.NormalizedPolicy == nil {
		return nil
	}

	vaultPolicy := &VaultPolicy{
		Name:  consolidator.NormalizeName(p.Name),
		Path:  basePath,
		Rules: make([]PolicyRule, 0),
	}

	for _, stmt := range p.NormalizedPolicy.Statements {
		if stmt.Effect != policy.EffectAllow {
			continue // Vault policies are allow-only; deny is handled by absence of capability
		}

		capabilities := m.mapActionsToCapabilities(stmt.Actions)
		if len(capabilities) == 0 {
			continue
		}

		// Create rule for the base path and subpaths
		rule := PolicyRule{
			Path:         basePath + "/*",
			Capabilities: capabilities,
		}
		vaultPolicy.Rules = append(vaultPolicy.Rules, rule)
	}

	if len(vaultPolicy.Rules) == 0 {
		return nil
	}

	return vaultPolicy
}

// mapActionsToCapabilities maps cloud IAM actions to Vault capabilities.
func (m *SecretsMerger) mapActionsToCapabilities(actions []string) []string {
	capabilitySet := make(map[string]bool)

	for _, action := range actions {
		action = strings.ToLower(action)

		switch {
		// Read operations
		case strings.Contains(action, "get") ||
			strings.Contains(action, "read") ||
			strings.Contains(action, "describe") ||
			strings.Contains(action, "list"):
			capabilitySet["read"] = true
			capabilitySet["list"] = true

		// Write operations
		case strings.Contains(action, "put") ||
			strings.Contains(action, "create") ||
			strings.Contains(action, "write") ||
			strings.Contains(action, "update"):
			capabilitySet["create"] = true
			capabilitySet["update"] = true

		// Delete operations
		case strings.Contains(action, "delete") ||
			strings.Contains(action, "remove"):
			capabilitySet["delete"] = true

		// Full access
		case strings.Contains(action, "*") ||
			strings.Contains(action, "admin") ||
			strings.Contains(action, "fullaccess"):
			capabilitySet["create"] = true
			capabilitySet["read"] = true
			capabilitySet["update"] = true
			capabilitySet["delete"] = true
			capabilitySet["list"] = true

		// Encryption operations (for KMS)
		case strings.Contains(action, "encrypt"):
			capabilitySet["update"] = true

		case strings.Contains(action, "decrypt"):
			capabilitySet["update"] = true

		case strings.Contains(action, "sign"):
			capabilitySet["update"] = true

		case strings.Contains(action, "verify"):
			capabilitySet["read"] = true
		}
	}

	capabilities := make([]string, 0, len(capabilitySet))
	for cap := range capabilitySet {
		capabilities = append(capabilities, cap)
	}

	return capabilities
}

// generateVaultPoliciesHCL generates Vault policy HCL files.
func (m *SecretsMerger) generateVaultPoliciesHCL(policies []VaultPolicy) []byte {
	var buf bytes.Buffer

	buf.WriteString("# Vault Policies\n")
	buf.WriteString("# Generated by Homeport - Review and customize before applying\n\n")

	for _, p := range policies {
		buf.WriteString(fmt.Sprintf("# Policy: %s\n", p.Name))
		buf.WriteString(fmt.Sprintf("path \"%s\" {\n", p.Path))
		buf.WriteString(fmt.Sprintf("  capabilities = [%s]\n", formatCapabilities(p.Rules)))
		buf.WriteString("}\n\n")

		for _, rule := range p.Rules {
			if rule.Path != p.Path {
				buf.WriteString(fmt.Sprintf("path \"%s\" {\n", rule.Path))
				buf.WriteString(fmt.Sprintf("  capabilities = [%s]\n", formatCapabilitiesList(rule.Capabilities)))
				buf.WriteString("}\n\n")
			}
		}
	}

	return buf.Bytes()
}

// formatCapabilities formats policy rules into Vault capabilities string.
func formatCapabilities(rules []PolicyRule) string {
	allCaps := make(map[string]bool)
	for _, rule := range rules {
		for _, cap := range rule.Capabilities {
			allCaps[cap] = true
		}
	}

	caps := make([]string, 0, len(allCaps))
	for cap := range allCaps {
		caps = append(caps, fmt.Sprintf("\"%s\"", cap))
	}

	return strings.Join(caps, ", ")
}

// formatCapabilitiesList formats a capabilities slice to HCL format.
func formatCapabilitiesList(caps []string) string {
	quoted := make([]string, len(caps))
	for i, cap := range caps {
		quoted[i] = fmt.Sprintf("\"%s\"", cap)
	}
	return strings.Join(quoted, ", ")
}

// generateMigrationScript generates a script to migrate secrets to Vault.
func (m *SecretsMerger) generateMigrationScript(paths []SecretPath) []byte {
	tmpl := `#!/bin/bash
# Secret Migration Script
# Generated by Homeport
#
# IMPORTANT: This script provides the structure for secret migration.
# You must manually export secrets from cloud providers and update this script.
# Cloud provider secrets cannot be automatically exported for security reasons.

set -e

VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-}"

if [ -z "$VAULT_TOKEN" ]; then
    echo "Error: VAULT_TOKEN environment variable is required"
    exit 1
fi

echo "Starting secret migration to Vault..."
echo "Vault Address: $VAULT_ADDR"

# Enable required secrets engines
echo "Enabling secrets engines..."
vault secrets enable -path=secret kv-v2 2>/dev/null || true
vault secrets enable transit 2>/dev/null || true
vault secrets enable pki 2>/dev/null || true

{{range .Paths}}
# Migrate: {{.SourceName}} ({{.SourceProvider}}/{{.SourceType}})
# Description: {{.Description}}
# Target Path: {{.Path}}
#
# TODO: Replace the placeholder with actual secret value
# Example:
# vault kv put {{.Path}} value="YOUR_SECRET_VALUE_HERE"
echo "Placeholder for {{.SourceName}} - update with actual secret value"

{{end}}

echo ""
echo "Migration script completed."
echo "IMPORTANT: Update this script with actual secret values before running."
echo "You can retrieve secrets from:"
echo "  - AWS: aws secretsmanager get-secret-value --secret-id <secret-name>"
echo "  - GCP: gcloud secrets versions access latest --secret=<secret-name>"
echo "  - Azure: az keyvault secret show --vault-name <vault> --name <secret>"
`

	t := template.Must(template.New("migration").Parse(tmpl))

	var buf bytes.Buffer
	data := struct {
		Paths []SecretPath
	}{
		Paths: paths,
	}

	if err := t.Execute(&buf, data); err != nil {
		return []byte("#!/bin/bash\n# Error generating migration script\n")
	}

	return buf.Bytes()
}

// generateUnsealScript generates the Vault initialization and unseal script.
func (m *SecretsMerger) generateUnsealScript() []byte {
	script := `#!/bin/bash
# Vault Initialization and Unseal Script
# Generated by Homeport
#
# This script initializes Vault and stores unseal keys securely.
# For production, use auto-unseal with cloud KMS or HSM.

set -e

VAULT_ADDR="${VAULT_ADDR:-http://localhost:8200}"
KEYS_FILE="${KEYS_FILE:-./vault-keys.json}"

echo "Vault Initialization Script"
echo "==========================="
echo ""

# Wait for Vault to be ready
echo "Waiting for Vault to be ready..."
until curl -s "$VAULT_ADDR/v1/sys/health" > /dev/null 2>&1; do
    echo "  Waiting..."
    sleep 2
done
echo "Vault is ready!"

# Check if already initialized
INIT_STATUS=$(curl -s "$VAULT_ADDR/v1/sys/init" | jq -r '.initialized')

if [ "$INIT_STATUS" = "true" ]; then
    echo "Vault is already initialized."

    # Check if sealed
    SEAL_STATUS=$(curl -s "$VAULT_ADDR/v1/sys/seal-status" | jq -r '.sealed')

    if [ "$SEAL_STATUS" = "true" ]; then
        echo "Vault is sealed. Attempting to unseal..."

        if [ -f "$KEYS_FILE" ]; then
            # Read unseal keys and unseal
            for i in 0 1 2; do
                KEY=$(jq -r ".unseal_keys_b64[$i]" "$KEYS_FILE")
                curl -s -X PUT "$VAULT_ADDR/v1/sys/unseal" \
                    -d "{\"key\": \"$KEY\"}" > /dev/null
            done

            echo "Vault unsealed successfully!"
        else
            echo "ERROR: Keys file not found at $KEYS_FILE"
            echo "Manual unseal required."
            exit 1
        fi
    else
        echo "Vault is already unsealed."
    fi
else
    echo "Initializing Vault..."

    # Initialize Vault with 5 key shares and 3 key threshold
    INIT_RESPONSE=$(curl -s -X PUT "$VAULT_ADDR/v1/sys/init" \
        -d '{"secret_shares": 5, "secret_threshold": 3}')

    # Save keys to file
    echo "$INIT_RESPONSE" > "$KEYS_FILE"
    chmod 600 "$KEYS_FILE"

    echo "Vault initialized! Keys saved to $KEYS_FILE"
    echo ""
    echo "IMPORTANT: Store these keys securely!"
    echo "  - Root token and unseal keys are in $KEYS_FILE"
    echo "  - For production, use Vault's auto-unseal feature"
    echo ""

    # Unseal with first 3 keys
    echo "Unsealing Vault..."
    for i in 0 1 2; do
        KEY=$(echo "$INIT_RESPONSE" | jq -r ".keys_base64[$i]")
        curl -s -X PUT "$VAULT_ADDR/v1/sys/unseal" \
            -d "{\"key\": \"$KEY\"}" > /dev/null
    done

    echo "Vault unsealed successfully!"

    # Extract root token
    ROOT_TOKEN=$(echo "$INIT_RESPONSE" | jq -r '.root_token')
    echo ""
    echo "Root Token: $ROOT_TOKEN"
    echo ""
    echo "Set environment variable:"
    echo "  export VAULT_TOKEN=$ROOT_TOKEN"
fi

echo ""
echo "Vault is ready to use!"
echo "  Address: $VAULT_ADDR"
echo "  UI:      $VAULT_ADDR/ui"
`
	return []byte(script)
}

// generatePathMappingDoc generates documentation of secret path mappings.
func (m *SecretsMerger) generatePathMappingDoc(paths []SecretPath) []byte {
	var buf bytes.Buffer

	buf.WriteString("# Secret Path Mappings\n\n")
	buf.WriteString("This document shows how cloud secrets are mapped to Vault paths.\n\n")

	// Group by provider
	byProvider := make(map[string][]SecretPath)
	for _, p := range paths {
		byProvider[p.SourceProvider] = append(byProvider[p.SourceProvider], p)
	}

	for provider, providerPaths := range byProvider {
		buf.WriteString(fmt.Sprintf("## %s\n\n", strings.ToUpper(provider)))
		buf.WriteString("| Source Name | Source Type | Vault Path | Description |\n")
		buf.WriteString("|-------------|-------------|------------|-------------|\n")

		for _, p := range providerPaths {
			buf.WriteString(fmt.Sprintf("| %s | %s | `%s` | %s |\n",
				p.SourceName, p.SourceType, p.Path, p.Description))
		}
		buf.WriteString("\n")
	}

	buf.WriteString("## Usage Examples\n\n")
	buf.WriteString("### Reading a secret\n")
	buf.WriteString("```bash\n")
	buf.WriteString("vault kv get secret/data/aws/secrets-manager/my-secret\n")
	buf.WriteString("```\n\n")
	buf.WriteString("### Writing a secret\n")
	buf.WriteString("```bash\n")
	buf.WriteString("vault kv put secret/data/aws/secrets-manager/my-secret value=\"secret-value\"\n")
	buf.WriteString("```\n\n")
	buf.WriteString("### Using Transit for encryption (KMS replacement)\n")
	buf.WriteString("```bash\n")
	buf.WriteString("# Encrypt\n")
	buf.WriteString("vault write transit/encrypt/my-key plaintext=$(echo -n \"secret\" | base64)\n")
	buf.WriteString("# Decrypt\n")
	buf.WriteString("vault write transit/decrypt/my-key ciphertext=\"vault:v1:...\"\n")
	buf.WriteString("```\n")

	return buf.Bytes()
}

// isSecretResource checks if a resource type is a secret management resource.
func isSecretResource(resourceType string) bool {
	secretTypes := []string{
		"secretsmanager",
		"kms",
		"ssm_parameter",
		"secret_manager",
		"key_vault",
		"keyvault",
	}

	for _, t := range secretTypes {
		if strings.Contains(resourceType, t) {
			return true
		}
	}

	return false
}
