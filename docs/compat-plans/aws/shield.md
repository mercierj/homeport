# AWS Shield Compatibility Plan

## Goal

Expose the smallest AWS Shield-compatible surface needed to migrate the ledger resources to `edge WAF and DDoS provider controls` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: shield:CreateProtection, shield:DescribeProtection, shield:ListProtections, shield:DeleteProtection.
- Actions explicitly not supported first: Shield console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `shield:CreateProtection` and its paired read/list calls.
- Ledger resource types: `aws_shield_protection`.
- Provider errors: map Shield authorization failures to AWS access-denied codes, missing `aws_shield_protection` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/shield` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_shield_protection`.

## Backend

- Backend: edge WAF and DDoS provider controls.
- Storage and metadata: Shield state lives in `edge WAF and DDoS provider controls`; HomePort stores provider identifiers for `aws_shield_protection`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `edge WAF and DDoS provider controls` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: shield:CreateProtection, shield:DescribeProtection, shield:ListProtections, shield:DeleteProtection.
- Resource: arn:aws:shield:{region}:{account}:shield/{id}.
- Context: evaluate Shield calls with tenant/project/account, provider region/location, `arn:aws:shield:{region}:{account}:shield/{id}`, source IP, request id, user agent, tags/labels on `aws_shield_protection`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Shield actions, `arn:aws:shield:{region}:{account}:shield/{id}` prefix checks, tag/label equality on `aws_shield_protection`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/shield` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Shield provider names, locations, tags/labels, and request bodies map to HomePort `aws_shield_protection` records and `edge WAF and DDoS provider controls` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Shield provider ids, `aws_shield_protection` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/shield` backend auth, missing `aws_shield_protection`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/shield/backend.yaml` for `edge WAF and DDoS provider controls` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/shield/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/shield/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-shield.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateProtection -> DescribeProtection -> ListProtections -> DeleteProtection against `/compat/aws/shield` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_shield_protection` from `aws/shield`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-shield.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-shield.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-shield.yaml`, then promote only when that manifest passes in CI.
