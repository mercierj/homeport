# AWS Secrets Manager local compatibility seed

The adapter provides process-local secret lifecycle and version operations for endpoint-override checks. Vault is a migration target only: this artifact does not deploy it or prove persistence, backup, cutover, or rollback.

## Source identifiers

- `aws_secretsmanager_secret`: preserve account, region, name, ARN, version metadata, tags, and source import ID when a real migration is authorized.

## Local verification

Run `go test ./test/compat -run SecretsManager` for the supported contract.
