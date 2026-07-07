# AWS Cognito Compatibility Plan

## Goal

Expose the smallest AWS Cognito-compatible surface needed to migrate the ledger resources to `Keycloak OIDC` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: cognito:CreateUserPool, cognito:DescribeUserPool, cognito:ListUserPools, cognito:UpdateUserPool, cognito:DeleteUserPool.
- Actions explicitly not supported first: Cognito console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `cognito:CreateUserPool` and its paired read/list calls.
- Ledger resource types: `aws_cognito_user_pool`.
- Provider errors: map Cognito authorization failures to AWS access-denied codes, missing `aws_cognito_user_pool` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/cognito` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_cognito_user_pool`.

## Backend

- Backend: Keycloak OIDC.
- Storage and metadata: Cognito state lives in `Keycloak OIDC`; HomePort stores provider identifiers for `aws_cognito_user_pool`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Keycloak OIDC` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: cognito:CreateUserPool, cognito:DescribeUserPool, cognito:ListUserPools, cognito:UpdateUserPool, cognito:DeleteUserPool.
- Resource: arn:aws:cognito:{region}:{account}:cognito/{id}.
- Context: evaluate Cognito calls with tenant/project/account, provider region/location, `arn:aws:cognito:{region}:{account}:cognito/{id}`, source IP, request id, user agent, tags/labels on `aws_cognito_user_pool`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cognito actions, `arn:aws:cognito:{region}:{account}:cognito/{id}` prefix checks, tag/label equality on `aws_cognito_user_pool`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/cognito` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Cognito provider names, locations, tags/labels, and request bodies map to HomePort `aws_cognito_user_pool` records and `Keycloak OIDC` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cognito provider ids, `aws_cognito_user_pool` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/cognito` backend auth, missing `aws_cognito_user_pool`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/cognito/backend.yaml` for `Keycloak OIDC` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/cognito/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/cognito/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-cognito.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateUserPool -> DescribeUserPool -> ListUserPools -> UpdateUserPool -> DeleteUserPool against `/compat/aws/cognito` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_cognito_user_pool` from `aws/cognito`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-cognito.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-cognito.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-cognito.yaml`, then promote only when that manifest passes in CI.
