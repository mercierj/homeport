# AWS IAM Compatibility Plan

## Goal

Expose the smallest AWS IAM-compatible surface needed to migrate the ledger resources to `Keycloak` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: iam roles, inline and managed policies with versions, instance profiles, and role tags.
- Actions explicitly not supported first: IAM console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `iam:CreateRole` and its paired read/list calls.
- Ledger resource types: `aws_iam_role`
- Provider errors: map IAM authorization failures to AWS access-denied codes, missing `aws_iam_role` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/iam` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_iam_role`.

## Backend

- Backend: Keycloak.
- Storage and metadata: IAM state lives in `Keycloak`; HomePort stores provider identifiers for `aws_iam_role`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Keycloak` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: iam:CreateRole, iam:GetRole, iam:ListRoles, iam:UpdateRole, iam:DeleteRole, role policy operations, managed policy/version operations, instance-profile operations, iam:ListRoleTags, iam:TagRole, iam:UntagRole.
- Resource: arn:aws:iam:{region}:{account}:iam/{id}.
- Context: evaluate IAM calls with tenant/project/account, provider region/location, `arn:aws:iam:{region}:{account}:iam/{id}`, source IP, request id, user agent, tags/labels on `aws_iam_role`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed IAM actions, `arn:aws:iam:{region}:{account}:iam/{id}` prefix checks, tag/label equality on `aws_iam_role`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/iam` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: IAM provider names, locations, tags/labels, and request bodies map to HomePort `aws_iam_role` records and `Keycloak` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return IAM provider ids, `aws_iam_role` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/iam` backend auth, missing `aws_iam_role`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/iam/backend.yaml` for `Keycloak` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/iam/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/iam/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-iam.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateRole -> GetRole -> ListRoles -> UpdateRole -> DeleteRole against `/compat/aws/iam` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Terraform applies and destroys `aws_iam_role` with tags through a provider IAM endpoint override, including empty inline, attached-policy, and instance-profile read-back.
- Fixture import covers `aws_iam_role` from `aws/iam`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 seed - AWS SDK, AWS CLI, and Terraform endpoint-override checks cover the local IAM adapter, but Keycloak durability and full acceptance gates still block L4.
- Target level: L4 after `test/conformance/services/aws-iam.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-iam.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-iam.yaml`, then promote only when that manifest passes in CI.
