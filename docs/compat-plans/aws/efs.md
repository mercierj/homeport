# AWS EFS Compatibility Plan

## Goal

Expose the smallest AWS EFS-compatible surface needed to migrate the ledger resources to `NFS server` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: efs:CreateFileSystem, efs:DescribeFileSystems, efs:UpdateFileSystem, efs:DeleteFileSystem, efs:DescribeLifecycleConfiguration, efs:PutFileSystemPolicy, efs:DescribeFileSystemPolicy, efs:DeleteFileSystemPolicy, efs:CreateMountTarget, efs:DescribeMountTargets, efs:DeleteMountTarget, efs:CreateAccessPoint, efs:DescribeAccessPoints, efs:DeleteAccessPoint.
- Actions explicitly not supported first: EFS console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `efs:CreateFileSystem` and its paired read/list calls.
- Ledger resource types: `aws_efs_file_system`
- Provider errors: map EFS authorization failures to AWS access-denied codes, missing `aws_efs_file_system` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/efs` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_efs_file_system`.

## Backend

- Backend: NFS server.
- Storage and metadata: EFS state lives in `NFS server`; HomePort stores provider identifiers for `aws_efs_file_system`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `NFS server` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: efs:CreateFileSystem, efs:DescribeFileSystems, efs:UpdateFileSystem, efs:DeleteFileSystem, efs:DescribeLifecycleConfiguration, efs:PutFileSystemPolicy, efs:DescribeFileSystemPolicy, efs:DeleteFileSystemPolicy, efs:CreateMountTarget, efs:DescribeMountTargets, efs:DeleteMountTarget, efs:CreateAccessPoint, efs:DescribeAccessPoints, efs:DeleteAccessPoint.
- Resource: arn:aws:efs:{region}:{account}:efs/{id}.
- Context: evaluate EFS calls with tenant/project/account, provider region/location, `arn:aws:efs:{region}:{account}:efs/{id}`, source IP, request id, user agent, tags/labels on `aws_efs_file_system`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed EFS actions, `arn:aws:efs:{region}:{account}:efs/{id}` prefix checks, tag/label equality on `aws_efs_file_system`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/efs` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: EFS provider names, locations, tags/labels, and request bodies map to HomePort `aws_efs_file_system` records and `NFS server` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return EFS provider ids, `aws_efs_file_system` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/efs` backend auth, missing `aws_efs_file_system`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/efs/backend.yaml` for `NFS server` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/efs/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/efs/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-efs.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateFileSystem -> DescribeFileSystems -> UpdateFileSystem -> DeleteFileSystem against `/compat/aws/efs` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Terraform applies and destroys `aws_efs_file_system` with tags through a provider EFS endpoint override, including lifecycle-configuration read-back.
- Fixture import covers `aws_efs_file_system` from `aws/efs`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 seed - AWS SDK, AWS CLI, and Terraform endpoint-override checks cover the local EFS adapter, but NFS durability and full acceptance gates still block L4.
- Target level: L4 after `test/conformance/services/aws-efs.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-efs.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-efs.yaml`, then promote only when that manifest passes in CI.
