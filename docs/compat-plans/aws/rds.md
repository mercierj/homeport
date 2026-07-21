# AWS RDS Compatibility Plan

## Goal

Expose the smallest AWS RDS-compatible surface needed to migrate the ledger resources to `PostgreSQL or MySQL self-hosted database` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: rds:CreateDBInstance, rds:DescribeDBInstances, rds:ModifyDBInstance, rds:DeleteDBInstance.
- Actions explicitly not supported first: RDS console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `rds:CreateDBInstance` and its paired read/list calls.
- Ledger resource types: `aws_db_instance`, `aws_rds_cluster`
- Provider errors: map RDS authorization failures to AWS access-denied codes, missing `aws_db_instance` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/rds` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_db_instance`.

## Backend

- Backend: PostgreSQL or MySQL self-hosted database.
- Storage and metadata: RDS state lives in `PostgreSQL or MySQL self-hosted database`; HomePort stores provider identifiers for `aws_db_instance`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `PostgreSQL or MySQL self-hosted database` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: rds:CreateDBInstance, rds:DescribeDBInstances, rds:ModifyDBInstance, rds:DeleteDBInstance.
- Resource: arn:aws:rds:{region}:{account}:rds/{id}.
- Context: evaluate RDS calls with tenant/project/account, provider region/location, `arn:aws:rds:{region}:{account}:rds/{id}`, source IP, request id, user agent, tags/labels on `aws_db_instance`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed RDS actions, `arn:aws:rds:{region}:{account}:rds/{id}` prefix checks, tag/label equality on `aws_db_instance`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/rds` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: RDS provider names, locations, tags/labels, and request bodies map to HomePort `aws_db_instance` records and `PostgreSQL or MySQL self-hosted database` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return RDS provider ids, `aws_db_instance` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/rds` backend auth, missing `aws_db_instance`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/rds/backend.yaml` for `PostgreSQL or MySQL self-hosted database` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/rds/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/rds/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-rds.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateDBInstance -> DescribeDBInstances -> ModifyDBInstance -> DeleteDBInstance against `/compat/aws/rds` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_db_instance`, `aws_rds_cluster` from `aws/rds`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-rds.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-rds.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-rds.yaml`, then promote only when that manifest passes in CI.
