# AWS Control Tower Compatibility Plan

## Goal

Expose the smallest AWS Control Tower-compatible surface needed to migrate the ledger resources to `Crossplane and Open Policy Agent` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: controltower:EnableControl, controltower:GetEnabledControl, controltower:ListEnabledControls, controltower:DisableControl.
- Actions explicitly not supported first: Control Tower console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `controltower:EnableControl` and its paired read/list calls.
- Ledger resource types: `aws_controltower_control`
- Provider errors: map Control Tower authorization failures to AWS access-denied codes, missing `aws_controltower_control` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/control-tower` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_controltower_control`.

## Backend

- Backend: Crossplane and Open Policy Agent.
- Storage and metadata: Control Tower state lives in `Crossplane and Open Policy Agent`; HomePort stores provider identifiers for `aws_controltower_control`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Crossplane and Open Policy Agent` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: controltower:EnableControl, controltower:GetEnabledControl, controltower:ListEnabledControls, controltower:DisableControl.
- Resource: arn:aws:controltower:{region}:{account}:control-tower/{id}.
- Context: evaluate Control Tower calls with tenant/project/account, provider region/location, `arn:aws:controltower:{region}:{account}:control-tower/{id}`, source IP, request id, user agent, tags/labels on `aws_controltower_control`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Control Tower actions, `arn:aws:controltower:{region}:{account}:control-tower/{id}` prefix checks, tag/label equality on `aws_controltower_control`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/control-tower` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Control Tower provider names, locations, tags/labels, and request bodies map to HomePort `aws_controltower_control` records and `Crossplane and Open Policy Agent` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Control Tower provider ids, `aws_controltower_control` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/control-tower` backend auth, missing `aws_controltower_control`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/control-tower/backend.yaml` for `Crossplane and Open Policy Agent` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/control-tower/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/control-tower/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-control-tower.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises EnableControl -> GetEnabledControl -> ListEnabledControls -> DisableControl against `/compat/aws/control-tower` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_controltower_control` from `aws/control-tower`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-control-tower.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-control-tower.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-control-tower.yaml`, then promote only when that manifest passes in CI.
