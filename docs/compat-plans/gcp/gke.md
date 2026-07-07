# GCP GKE Compatibility Plan

## Goal

Expose the smallest GCP GKE-compatible surface needed to migrate the ledger resources to `K3s or upstream Kubernetes` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: container.projects.locations.clusters.create -> container.projects.locations.clusters.get -> container.projects.locations.clusters.list -> container.projects.locations.clusters.update -> container.projects.locations.clusters.delete.
- Actions explicitly not supported first: GKE console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `container.projects.locations.clusters.create` and its paired read/list calls.
- Ledger resource types: `google_container_cluster`.
- Provider errors: map GKE authorization failures to GCP access-denied codes, missing `google_container_cluster` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/gke` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_container_cluster`.

## Backend

- Backend: K3s or upstream Kubernetes.
- Storage and metadata: GKE state lives in `K3s or upstream Kubernetes`; HomePort stores provider identifiers for `google_container_cluster`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `K3s or upstream Kubernetes` with generated `artifacts/compat/gcp/gke/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/gke`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: container.projects.locations.clusters.create -> container.projects.locations.clusters.get -> container.projects.locations.clusters.list -> container.projects.locations.clusters.update -> container.projects.locations.clusters.delete.
- Resource: projects/{project}/locations/{location}/gke/{id}.
- Context: evaluate GKE calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/gke/{id}`, source IP, request id, user agent, tags/labels on `google_container_cluster`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed GKE actions, `projects/{project}/locations/{location}/gke/{id}` prefix checks, tag/label equality on `google_container_cluster`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/gke` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: GKE provider names, locations, tags/labels, and request bodies map to HomePort `google_container_cluster` records and `K3s or upstream Kubernetes` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return GKE provider ids, `google_container_cluster` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/gke` backend auth, missing `google_container_cluster`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/gke/backend.yaml` for `K3s or upstream Kubernetes` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/gke/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/gke/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-gke.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises container.projects.locations.clusters.create -> container.projects.locations.clusters.get -> container.projects.locations.clusters.list -> container.projects.locations.clusters.update -> container.projects.locations.clusters.delete against `/compat/gcp/gke` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_container_cluster` from `gcp/gke`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-gke.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-gke.yaml`, then promote only when that manifest passes in CI.
