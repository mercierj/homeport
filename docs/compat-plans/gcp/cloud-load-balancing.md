# GCP Cloud Load Balancing Compatibility Plan

## Goal

Expose the smallest GCP Cloud Load Balancing-compatible surface needed to migrate the ledger resources to `Traefik ingress gateway` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: compute.backendServices.insert -> compute.backendServices.get -> compute.backendServices.list -> compute.backendServices.patch -> compute.backendServices.delete.
- Actions explicitly not supported first: Cloud Load Balancing console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `compute.backendServices.insert` and its paired read/list calls.
- Ledger resource types: `google_compute_backend_service`
- Provider errors: map Cloud Load Balancing authorization failures to GCP access-denied codes, missing `google_compute_backend_service` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/cloud-load-balancing` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_compute_backend_service`.

## Backend

- Backend: Traefik.
- Storage and metadata: Cloud Load Balancing state lives in `Traefik ingress gateway`; HomePort stores provider identifiers for `google_compute_backend_service`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Traefik ingress gateway` with generated `artifacts/compat/gcp/cloud-load-balancing/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/cloud-load-balancing`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: compute.backendServices.insert -> compute.backendServices.get -> compute.backendServices.list -> compute.backendServices.patch -> compute.backendServices.delete.
- Resource: projects/{project}/locations/{location}/cloud-load-balancing/{id}.
- Context: evaluate Cloud Load Balancing calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/cloud-load-balancing/{id}`, source IP, request id, user agent, tags/labels on `google_compute_backend_service`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cloud Load Balancing actions, `projects/{project}/locations/{location}/cloud-load-balancing/{id}` prefix checks, tag/label equality on `google_compute_backend_service`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/cloud-load-balancing` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Cloud Load Balancing provider names, locations, tags/labels, and request bodies map to HomePort `google_compute_backend_service` records and `Traefik ingress gateway` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cloud Load Balancing provider ids, `google_compute_backend_service` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/cloud-load-balancing` backend auth, missing `google_compute_backend_service`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/cloud-load-balancing/backend.yaml` for `Traefik ingress gateway` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/cloud-load-balancing/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/cloud-load-balancing/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-cloud-load-balancing.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises compute.backendServices.insert -> compute.backendServices.get -> compute.backendServices.list -> compute.backendServices.patch -> compute.backendServices.delete against `/compat/gcp/cloud-load-balancing` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_compute_backend_service` from `gcp/cloud-load-balancing`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-cloud-load-balancing.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-cloud-load-balancing.yaml`, then promote only when that manifest passes in CI.
