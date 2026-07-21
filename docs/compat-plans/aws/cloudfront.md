# AWS CloudFront Compatibility Plan

## Goal

Expose the smallest AWS CloudFront-compatible surface needed to migrate the ledger resources to `Caddy and Varnish CDN` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: cloudfront:CreateDistribution, cloudfront:GetDistribution, cloudfront:ListDistributions, cloudfront:UpdateDistribution, cloudfront:DeleteDistribution.
- Actions explicitly not supported first: CloudFront console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `cloudfront:CreateDistribution` and its paired read/list calls.
- Ledger resource types: `aws_cloudfront_distribution`
- Provider errors: map CloudFront authorization failures to AWS access-denied codes, missing `aws_cloudfront_distribution` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/cloudfront` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_cloudfront_distribution`.

## Backend

- Backend: Caddy and Varnish CDN.
- Storage and metadata: CloudFront state lives in `Caddy and Varnish CDN`; HomePort stores provider identifiers for `aws_cloudfront_distribution`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Caddy and Varnish CDN` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: cloudfront:CreateDistribution, cloudfront:GetDistribution, cloudfront:ListDistributions, cloudfront:UpdateDistribution, cloudfront:DeleteDistribution.
- Resource: arn:aws:cloudfront:{region}:{account}:cloudfront/{id}.
- Context: evaluate CloudFront calls with tenant/project/account, provider region/location, `arn:aws:cloudfront:{region}:{account}:cloudfront/{id}`, source IP, request id, user agent, tags/labels on `aws_cloudfront_distribution`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed CloudFront actions, `arn:aws:cloudfront:{region}:{account}:cloudfront/{id}` prefix checks, tag/label equality on `aws_cloudfront_distribution`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/cloudfront` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: CloudFront provider names, locations, tags/labels, and request bodies map to HomePort `aws_cloudfront_distribution` records and `Caddy and Varnish CDN` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return CloudFront provider ids, `aws_cloudfront_distribution` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/cloudfront` backend auth, missing `aws_cloudfront_distribution`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/cloudfront/backend.yaml` for `Caddy and Varnish CDN` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/cloudfront/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/cloudfront/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-cloudfront.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateDistribution -> GetDistribution -> ListDistributions -> UpdateDistribution -> DeleteDistribution against `/compat/aws/cloudfront` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_cloudfront_distribution` from `aws/cloudfront`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-cloudfront.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-cloudfront.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-cloudfront.yaml`, then promote only when that manifest passes in CI.
