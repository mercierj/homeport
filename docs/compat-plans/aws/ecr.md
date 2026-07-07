# AWS ECR Compatibility Plan

## Goal

Expose the smallest AWS ECR-compatible surface needed to migrate the ledger resources to `OCI Distribution registry` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: ecr:CreateRepository, ecr:DescribeRepositories, ecr:ListImages, ecr:DeleteRepository.
- Actions explicitly not supported first: ECR console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `ecr:CreateRepository` and its paired read/list calls.
- Ledger resource types: `aws_ecr_repository`.
- Provider errors: map ECR authorization failures to AWS access-denied codes, missing `aws_ecr_repository` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/ecr` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_ecr_repository`.

## Backend

- Backend: OCI Distribution registry.
- Storage and metadata: ECR state lives in `OCI Distribution registry`; HomePort stores provider identifiers for `aws_ecr_repository`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `OCI Distribution registry` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: ecr:CreateRepository, ecr:DescribeRepositories, ecr:ListImages, ecr:DeleteRepository.
- Resource: arn:aws:ecr:{region}:{account}:ecr/{id}.
- Context: evaluate ECR calls with tenant/project/account, provider region/location, `arn:aws:ecr:{region}:{account}:ecr/{id}`, source IP, request id, user agent, tags/labels on `aws_ecr_repository`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed ECR actions, `arn:aws:ecr:{region}:{account}:ecr/{id}` prefix checks, tag/label equality on `aws_ecr_repository`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/ecr` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: ECR provider names, locations, tags/labels, and request bodies map to HomePort `aws_ecr_repository` records and `OCI Distribution registry` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return ECR provider ids, `aws_ecr_repository` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/ecr` backend auth, missing `aws_ecr_repository`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/ecr/backend.yaml` for `OCI Distribution registry` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/ecr/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/ecr/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-ecr.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateRepository -> DescribeRepositories -> ListImages -> DeleteRepository against `/compat/aws/ecr` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_ecr_repository` from `aws/ecr`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-ecr.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-ecr.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-ecr.yaml`, then promote only when that manifest passes in CI.
