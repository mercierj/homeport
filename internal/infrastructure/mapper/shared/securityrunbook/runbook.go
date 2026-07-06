package securityrunbook

import domainrunbook "github.com/homeport/homeport/internal/domain/runbook"

func SecretsManager(secretName string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                            "secrets",
		"secret":                          secretName,
		"AWS_ENDPOINT_URL_SECRETSMANAGER": "http://homeport:8080/api/v1/compat/aws/secretsmanager",
		"HOMEPORT_COMPAT_BACKEND":         "vault",
	}
	return []domainrunbook.Step{
		command("init-vault-kv", "Initialize Vault KV", "Provision", []string{"sh", "init_vault.sh"}, "Vault KV v2 engine and policies are configured", metadata),
		command("import-secret-value", "Import secret value", "Sync", []string{"sh", "migrate_secrets.sh"}, "secret value exists in Vault KV", metadata),
		input("provide-unreadable-secret", "Provide unreadable secret value", "Sync", "secret value supplied through encrypted operator input when provider API cannot return it", metadata),
		command("validate-secretsmanager-compat", "Validate Secrets Manager compatibility", "Validate", []string{"sh", "-c", "echo validate GetSecretValue against Vault-backed adapter"}, "GetSecretValue returns the imported Vault value", metadata),
		rollback("rollback-secrets-source-authority", "Keep AWS Secrets Manager authoritative", metadata),
	}
}

func KMSTransit(keyID string) []domainrunbook.Step {
	metadata := map[string]string{
		"kind":                     "kms",
		"key":                      keyID,
		"AWS_ENDPOINT_URL_KMS":     "http://homeport:8080/api/v1/compat/aws/kms",
		"HOMEPORT_COMPAT_BACKEND":  "vault-transit",
		"HOMEPORT_COMPAT_PROTOCOL": "kms",
	}
	return []domainrunbook.Step{
		command("export-kms-metadata", "Export KMS metadata", "Discovery", []string{"sh", "scripts/kms-export.sh"}, "key metadata, aliases, policy, grants, rotation, and tags are exported", metadata),
		command("setup-vault-transit", "Set up Vault Transit key", "Provision", []string{"sh", "scripts/vault/setup-transit.sh"}, "Vault Transit key exists with mapped metadata", metadata),
		command("reencrypt-kms-ciphertexts", "Re-encrypt KMS ciphertexts", "Sync", []string{"sh", "scripts/kms-reencrypt.sh"}, "ciphertext manifest is decrypted with source KMS and re-encrypted with Vault Transit", metadata),
		command("validate-vault-transit-roundtrip", "Validate Vault Transit roundtrip", "Validate", []string{"sh", "scripts/vault/test-transit.sh"}, "encrypt/decrypt roundtrip and old ciphertext checks pass", metadata),
		command("backup-kms-vault", "Backup Vault Transit config", "Backup", []string{"sh", "scripts/kms-backup.sh"}, "Vault Transit config and migration plan are archived", metadata),
		apiCall("cutover-kms-adapter", "Cut over KMS clients to HomePort adapter", "Cutover", []string{"sh", "scripts/kms-cutover.sh"}, "KMS clients use the HomePort compatibility endpoint backed by Vault Transit", metadata),
		rollback("rollback-kms-source-authority", "Keep AWS KMS authoritative", metadata),
	}
}

func command(id, name, group string, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             domainrunbook.StepTypeCommand,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		SuccessCondition: success,
		Command:          command,
		Metadata:         clone(metadata),
	}
}

func input(id, name, group, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             domainrunbook.StepTypeInput,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "user",
		SuccessCondition: success,
		Metadata:         clone(metadata),
	}
}

func apiCall(id, name, group string, command []string, success string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            group,
		Type:             domainrunbook.StepTypeAPICall,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "shell",
		SuccessCondition: success,
		Command:          command,
		Metadata:         clone(metadata),
	}
}

func rollback(id, name string, metadata map[string]string) domainrunbook.Step {
	return domainrunbook.Step{
		ID:               id,
		Name:             name,
		Group:            "Rollback",
		Type:             domainrunbook.StepTypeRollback,
		Status:           domainrunbook.StepStatusPending,
		Executor:         "noop",
		SuccessCondition: "source remains authoritative until cutover passes",
		Metadata:         clone(metadata),
	}
}

func clone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
