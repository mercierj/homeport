# AWS ACM Compatibility Plan

## Goal

Expose the smallest AWS ACM-compatible surface needed to migrate the ledger resources to `Traefik ACME` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: acm:RequestCertificate, acm:DescribeCertificate, acm:ListCertificates, acm:DeleteCertificate, acm:ListTagsForCertificate, acm:AddTagsToCertificate, acm:RemoveTagsFromCertificate.
- Actions explicitly not supported first: ACM console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `acm:RequestCertificate` and its paired read/list calls.
- Ledger resource types: `aws_acm_certificate`
- Provider errors: map ACM authorization failures to AWS access-denied codes, missing `aws_acm_certificate` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/acm` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_acm_certificate`.

## Backend

- Backend: Traefik ACME.
- Storage and metadata: ACM state lives in `Traefik ACME`; HomePort stores provider identifiers for `aws_acm_certificate`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Traefik ACME` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: acm:RequestCertificate, acm:DescribeCertificate, acm:ListCertificates, acm:DeleteCertificate, acm:ListTagsForCertificate, acm:AddTagsToCertificate, acm:RemoveTagsFromCertificate.
- Resource: arn:aws:acm:{region}:{account}:acm/{id}.
- Context: evaluate ACM calls with tenant/project/account, provider region/location, `arn:aws:acm:{region}:{account}:acm/{id}`, source IP, request id, user agent, tags/labels on `aws_acm_certificate`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed ACM actions, `arn:aws:acm:{region}:{account}:acm/{id}` prefix checks, tag/label equality on `aws_acm_certificate`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/acm` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: ACM provider names, locations, tags/labels, and request bodies map to HomePort `aws_acm_certificate` records and `Traefik ACME` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return ACM provider ids, `aws_acm_certificate` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/acm` backend auth, missing `aws_acm_certificate`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/acm/backend.yaml` for `Traefik ACME` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/acm/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/acm/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-acm.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises RequestCertificate -> DescribeCertificate -> ListCertificates -> DeleteCertificate against `/compat/aws/acm` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Terraform applies and destroys `aws_acm_certificate` with tags through a provider ACM endpoint override.
- Fixture import covers `aws_acm_certificate` from `aws/acm`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 seed - AWS SDK, AWS CLI, and Terraform endpoint-override checks cover the local ACM adapter, including certificate/tag lifecycle, authorization/audit, list pagination, and certificate quotas; Traefik ACME durability and full acceptance gates still block L4.
- Target level: L4 after `test/conformance/services/aws-acm.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-acm.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-acm.yaml`, then promote only when that manifest passes in CI.
