# AWS Secrets Manager Compatibility Plan

## Goal

Expose the smallest AWS Secrets Manager-compatible surface needed to migrate the ledger resources to `Vault` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: secretsmanager:CreateSecret, secretsmanager:DescribeSecret, secretsmanager:ListSecrets, secretsmanager:UpdateSecret, secretsmanager:DeleteSecret.
- Actions explicitly not supported first: Secrets Manager console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `secretsmanager:CreateSecret` and its paired read/list calls.
- Ledger resource types: `aws_secretsmanager_secret`.
- Provider errors: map Secrets Manager authorization failures to AWS access-denied codes, missing `aws_secretsmanager_secret` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/secrets-manager` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_secretsmanager_secret`.

## Backend

- Backend: Vault.
- Storage and metadata: Secrets Manager state lives in `Vault`; HomePort stores provider identifiers for `aws_secretsmanager_secret`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Vault` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: secretsmanager:CreateSecret, secretsmanager:DescribeSecret, secretsmanager:ListSecrets, secretsmanager:UpdateSecret, secretsmanager:DeleteSecret.
- Resource: arn:aws:secretsmanager:{region}:{account}:secrets-manager/{id}.
- Context: evaluate Secrets Manager calls with tenant/project/account, provider region/location, `arn:aws:secretsmanager:{region}:{account}:secrets-manager/{id}`, source IP, request id, user agent, tags/labels on `aws_secretsmanager_secret`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Secrets Manager actions, `arn:aws:secretsmanager:{region}:{account}:secrets-manager/{id}` prefix checks, tag/label equality on `aws_secretsmanager_secret`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/secrets-manager` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Secrets Manager provider names, locations, tags/labels, and request bodies map to HomePort `aws_secretsmanager_secret` records and `Vault` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Secrets Manager provider ids, `aws_secretsmanager_secret` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/secrets-manager` backend auth, missing `aws_secretsmanager_secret`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/secrets-manager/backend.yaml` for `Vault` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/secrets-manager/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/secrets-manager/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-secrets-manager.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateSecret -> DescribeSecret -> ListSecrets -> UpdateSecret -> DeleteSecret against `/compat/aws/secrets-manager` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_secretsmanager_secret` from `aws/secrets-manager`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-secrets-manager.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-secrets-manager.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-secrets-manager.yaml`, then promote only when that manifest passes in CI.
