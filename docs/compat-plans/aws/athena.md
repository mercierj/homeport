# AWS Athena Compatibility Plan

## Goal

Expose the smallest AWS Athena-compatible surface needed to migrate the ledger resources to `Trino` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: athena:CreateWorkGroup, athena:GetWorkGroup, athena:ListWorkGroups, athena:UpdateWorkGroup, athena:DeleteWorkGroup.
- Actions explicitly not supported first: Athena console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `athena:CreateWorkGroup` and its paired read/list calls.
- Ledger resource types: `aws_athena_workgroup`.
- Provider errors: map Athena authorization failures to AWS access-denied codes, missing `aws_athena_workgroup` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/athena` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_athena_workgroup`.

## Backend

- Backend: Trino.
- Storage and metadata: Athena state lives in `Trino`; HomePort stores provider identifiers for `aws_athena_workgroup`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Trino` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: athena:CreateWorkGroup, athena:GetWorkGroup, athena:ListWorkGroups, athena:UpdateWorkGroup, athena:DeleteWorkGroup.
- Resource: arn:aws:athena:{region}:{account}:athena/{id}.
- Context: evaluate Athena calls with tenant/project/account, provider region/location, `arn:aws:athena:{region}:{account}:athena/{id}`, source IP, request id, user agent, tags/labels on `aws_athena_workgroup`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Athena actions, `arn:aws:athena:{region}:{account}:athena/{id}` prefix checks, tag/label equality on `aws_athena_workgroup`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/athena` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Athena provider names, locations, tags/labels, and request bodies map to HomePort `aws_athena_workgroup` records and `Trino` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Athena provider ids, `aws_athena_workgroup` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/athena` backend auth, missing `aws_athena_workgroup`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/athena/backend.yaml` for `Trino` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/athena/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/athena/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-athena.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateWorkGroup -> GetWorkGroup -> ListWorkGroups -> UpdateWorkGroup -> DeleteWorkGroup against `/compat/aws/athena` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_athena_workgroup` from `aws/athena`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-athena.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-athena.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-athena.yaml`, then promote only when that manifest passes in CI.
