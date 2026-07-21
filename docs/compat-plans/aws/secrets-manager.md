# AWS Secrets Manager Local Compatibility Plan

## Goal

Provide a process-local Secrets Manager surface for endpoint-override checks without claiming Vault deployment or durable secret storage.

## Provider API Surface

- Supported operations cover secret creation, version reads and writes, deletion, description, resource policies, tags, pagination, quotas, authorization, and audit callbacks.
- State is held in memory and disappears when the adapter stops.
- Ledger resource types: `aws_secretsmanager_secret`

## Backend

- Backend: Vault.
- Local status: proposed migration seed only; Vault is not deployed and persistence, HA, backup, validation, cutover, and rollback are not proved.

## Authz Model

- Every supported operation calls the shared authorizer.
- Authorization derives the secret ARN from `SecretId`; creation derives it from `Name`.

## Adapter

- Endpoint: `/compat/aws/secretsmanager`.
- Requests and responses use the AWS JSON shapes exercised by official clients.
- Denied or invalid requests do not mutate process-local state.

## Generated Artifacts

- `artifacts/compat/aws/secrets-manager/backend.yaml` records the proposed Vault target.
- `artifacts/compat/aws/secrets-manager/adapter.yaml` records local actions, errors, pagination, and quota behavior.
- `artifacts/compat/aws/secrets-manager/migration.md` preserves source identifiers without asserting migration execution.
- `test/conformance/services/aws-secrets-manager.yaml` records the runnable local contract.

## Contract Tests

- AWS SDK for Go v2 exercises secret lifecycle, `PutSecretValue` `ClientRequestToken` as `VersionId`, version replay and mismatched-token rejection while preserving `AWSPREVIOUS`, resource-policy lifecycle, tag round-trip, policy/tag denials with audit and no mutation, pagination, quota, authorization, and audit behavior against the local endpoint.
- AWS CLI, Terraform, and Boto3 endpoint-override checks cover their documented local slices.

## Compatibility Level

- Current level: L3 local seed.
- Target level: L4 only after durable Vault integration and external migration gates are proved.
- Blocking gaps: durable Vault storage, production validation, cutover, and rollback.
