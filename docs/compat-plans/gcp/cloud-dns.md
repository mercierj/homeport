# GCP Cloud DNS Compatibility Plan

## Goal

Expose the smallest GCP Cloud DNS-compatible surface needed to migrate the ledger resources to `CoreDNS` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: dns.managedZones.create -> dns.managedZones.get -> dns.managedZones.list -> dns.managedZones.delete; dns.changes.create -> dns.changes.get.
- Actions explicitly not supported first: Cloud DNS console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `dns.managedZones.create` and its paired read/list calls.
- Ledger resource types: `google_dns_managed_zone`.
- Provider errors: map Cloud DNS authorization failures to GCP access-denied codes, missing `google_dns_managed_zone` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/cloud-dns` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_dns_managed_zone`.

## Backend

- Backend: CoreDNS.
- Storage and metadata: Cloud DNS state lives in `CoreDNS`; HomePort stores provider identifiers for `google_dns_managed_zone`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `CoreDNS` with generated `artifacts/compat/gcp/cloud-dns/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/cloud-dns`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: dns.managedZones.create -> dns.managedZones.get -> dns.managedZones.list -> dns.managedZones.delete; dns.changes.create -> dns.changes.get.
- Resource: projects/{project}/locations/{location}/cloud-dns/{id}.
- Context: evaluate Cloud DNS calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/cloud-dns/{id}`, source IP, request id, user agent, tags/labels on `google_dns_managed_zone`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cloud DNS actions, `projects/{project}/locations/{location}/cloud-dns/{id}` prefix checks, tag/label equality on `google_dns_managed_zone`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/cloud-dns` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Cloud DNS provider names, locations, tags/labels, and request bodies map to HomePort `google_dns_managed_zone` records and `CoreDNS` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cloud DNS provider ids, `google_dns_managed_zone` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/cloud-dns` backend auth, missing `google_dns_managed_zone`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/cloud-dns/backend.yaml` for `CoreDNS` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/cloud-dns/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/cloud-dns/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-cloud-dns.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises dns.managedZones.create -> dns.managedZones.get -> dns.managedZones.list -> dns.managedZones.delete; dns.changes.create -> dns.changes.get against `/compat/gcp/cloud-dns` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_dns_managed_zone` from `gcp/cloud-dns`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-cloud-dns.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-cloud-dns.yaml`, then promote only when that manifest passes in CI.
