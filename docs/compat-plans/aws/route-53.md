# AWS Route 53 Compatibility Plan

## Goal

Expose the smallest AWS Route 53-compatible surface needed to migrate the ledger resources to `CoreDNS` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: route53:CreateHostedZone, route53:GetHostedZone, route53:ListHostedZones, route53:ChangeResourceRecordSets, route53:DeleteHostedZone.
- Actions explicitly not supported first: Route 53 console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `route53:CreateHostedZone` and its paired read/list calls.
- Ledger resource types: `aws_route53_zone`.
- Provider errors: map Route 53 authorization failures to AWS access-denied codes, missing `aws_route53_zone` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/route-53` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_route53_zone`.

## Backend

- Backend: CoreDNS.
- Storage and metadata: Route 53 state lives in `CoreDNS`; HomePort stores provider identifiers for `aws_route53_zone`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `CoreDNS` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: route53:CreateHostedZone, route53:GetHostedZone, route53:ListHostedZones, route53:ChangeResourceRecordSets, route53:DeleteHostedZone.
- Resource: arn:aws:route53:{region}:{account}:route-53/{id}.
- Context: evaluate Route 53 calls with tenant/project/account, provider region/location, `arn:aws:route53:{region}:{account}:route-53/{id}`, source IP, request id, user agent, tags/labels on `aws_route53_zone`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Route 53 actions, `arn:aws:route53:{region}:{account}:route-53/{id}` prefix checks, tag/label equality on `aws_route53_zone`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/route-53` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Route 53 provider names, locations, tags/labels, and request bodies map to HomePort `aws_route53_zone` records and `CoreDNS` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Route 53 provider ids, `aws_route53_zone` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/route-53` backend auth, missing `aws_route53_zone`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/route-53/backend.yaml` for `CoreDNS` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/route-53/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/route-53/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-route-53.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateHostedZone -> GetHostedZone -> ListHostedZones -> ChangeResourceRecordSets -> DeleteHostedZone against `/compat/aws/route-53` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_route53_zone` from `aws/route-53`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-route-53.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-route-53.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-route-53.yaml`, then promote only when that manifest passes in CI.
