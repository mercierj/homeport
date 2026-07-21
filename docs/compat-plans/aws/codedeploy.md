# AWS CodeDeploy Compatibility Plan

## Goal

Expose the smallest AWS CodeDeploy-compatible surface needed to migrate the ledger resources to `Argo Rollouts` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: codedeploy:CreateApplication, codedeploy:GetApplication, codedeploy:ListApplications, codedeploy:DeleteApplication.
- Actions explicitly not supported first: CodeDeploy console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `codedeploy:CreateApplication` and its paired read/list calls.
- Ledger resource types: `aws_codedeploy_app`
- Provider errors: map CodeDeploy authorization failures to AWS access-denied codes, missing `aws_codedeploy_app` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/codedeploy` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_codedeploy_app`.

## Backend

- Backend: Argo Rollouts.
- Storage and metadata: CodeDeploy state lives in `Argo Rollouts`; HomePort stores provider identifiers for `aws_codedeploy_app`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Argo Rollouts` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: codedeploy:CreateApplication, codedeploy:GetApplication, codedeploy:ListApplications, codedeploy:DeleteApplication.
- Resource: arn:aws:codedeploy:{region}:{account}:codedeploy/{id}.
- Context: evaluate CodeDeploy calls with tenant/project/account, provider region/location, `arn:aws:codedeploy:{region}:{account}:codedeploy/{id}`, source IP, request id, user agent, tags/labels on `aws_codedeploy_app`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed CodeDeploy actions, `arn:aws:codedeploy:{region}:{account}:codedeploy/{id}` prefix checks, tag/label equality on `aws_codedeploy_app`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/codedeploy` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: CodeDeploy provider names, locations, tags/labels, and request bodies map to HomePort `aws_codedeploy_app` records and `Argo Rollouts` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return CodeDeploy provider ids, `aws_codedeploy_app` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/codedeploy` backend auth, missing `aws_codedeploy_app`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/codedeploy/backend.yaml` for `Argo Rollouts` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/codedeploy/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/codedeploy/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-codedeploy.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateApplication -> GetApplication -> ListApplications -> DeleteApplication against `/compat/aws/codedeploy` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_codedeploy_app` from `aws/codedeploy`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-codedeploy.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-codedeploy.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-codedeploy.yaml`, then promote only when that manifest passes in CI.
