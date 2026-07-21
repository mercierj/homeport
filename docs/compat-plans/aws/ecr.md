# AWS ECR Compatibility Plan

## Goal

Expose the smallest AWS ECR-compatible surface needed to migrate the ledger resources to `OCI Distribution registry` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: ecr:CreateRepository, ecr:DescribeRepositories, ecr:PutImage, ecr:DescribeImages, ecr:ListImages, ecr:BatchGetImage, ecr:BatchDeleteImage, ecr:DeleteRepository.
- Actions explicitly not supported first: ECR console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `ecr:CreateRepository` and its paired read/list calls.
- Ledger resource types: `aws_ecr_repository`
- Provider errors: map ECR authorization failures to access-denied, missing `aws_ecr_repository` records to not-found, duplicate repositories to already-exists, invalid repository names and pagination fields to validation, configured repository limits to `LimitExceededException`, and authorization failures to `ServerException`.
- Pagination: `DescribeRepositories` and `ListImages` expose `nextToken`; create-time tags plus `TagResource`, `ListTagsForResource`, and `UntagResource` are supported. Idempotency tokens are not supported by this seed.

## Backend

- Backend: OCI Distribution registry.
- Storage and metadata: generated artifacts target an `OCI Distribution registry`; the local adapter keeps repository and image-manifest metadata in memory, but does not retain OCI blobs.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `OCI Distribution registry` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: ecr:CreateRepository, ecr:DescribeRepositories, ecr:ListImages, ecr:DeleteRepository.
- Resource: arn:aws:ecr:{region}:{account}:repository/{id}.
- Context: evaluate ECR calls with tenant/project/account, provider region/location, `arn:aws:ecr:{region}:{account}:repository/{id}`, source IP, request id, user agent, tags/labels on `aws_ecr_repository`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed ECR actions, `arn:aws:ecr:{region}:{account}:repository/{id}` prefix checks, tag/label equality on `aws_ecr_repository`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/ecr` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: ECR repository names and request bodies map to local `aws_ecr_repository` records; backend-only knobs are omitted from provider responses.
- Response mapping: return repository IDs, ARNs, names, URIs, creation timestamps, and `DescribeRepositories` pagination tokens without backend-only fields.
- Error mapping: return provider-shaped access-denied, not-found, already-exists, validation, configured-quota, and internal-error responses; backend timeout and dependency mapping are outside this local seed.

## Generated Artifacts

- `artifacts/compat/aws/ecr/backend.yaml` for `OCI Distribution registry` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/ecr/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/ecr/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-ecr.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateRepository -> DescribeRepositories -> ListImages -> DeleteRepository against `/compat/aws/ecr`, including named-resource authorization, pagination, repository-name validation, and a configured repository quota.
- Fixture import, credential expiry, backend timeout, real OCI registry validation, and cross-service IAM parity remain outside this local seed.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-ecr.yaml` passes in CI.
- Blocking gaps: idempotency, credential expiry, durable backend integration, and provider-wide error parity remain unproven.
- Path to close gaps: wire a real OCI backend, add the missing provider contracts, then promote only when those contracts pass in CI.
