# GCP Spanner Compatibility Plan

## Goal

Expose the smallest GCP Spanner-compatible surface needed to migrate the ledger resources to `CockroachDB` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: spanner.projects.instances.create -> spanner.projects.instances.get -> spanner.projects.instances.list -> spanner.projects.instances.update -> spanner.projects.instances.delete.
- Actions explicitly not supported first: Spanner console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `spanner.projects.instances.create` and its paired read/list calls.
- Ledger resource types: `google_spanner_instance`.
- Provider errors: map Spanner authorization failures to GCP access-denied codes, missing `google_spanner_instance` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/spanner` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_spanner_instance`.

## Backend

- Backend: CockroachDB.
- Storage and metadata: Spanner state lives in `CockroachDB`; HomePort stores provider identifiers for `google_spanner_instance`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `CockroachDB` with generated `artifacts/compat/gcp/spanner/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/spanner`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: spanner.projects.instances.create -> spanner.projects.instances.get -> spanner.projects.instances.list -> spanner.projects.instances.update -> spanner.projects.instances.delete.
- Resource: projects/{project}/instances/{instance}/databases/{database}.
- Context: evaluate Spanner calls with tenant/project/account, provider region/location, `projects/{project}/instances/{instance}/databases/{database}`, source IP, request id, user agent, tags/labels on `google_spanner_instance`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Spanner actions, `projects/{project}/instances/{instance}/databases/{database}` prefix checks, tag/label equality on `google_spanner_instance`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/spanner` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Spanner provider names, locations, tags/labels, and request bodies map to HomePort `google_spanner_instance` records and `CockroachDB` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Spanner provider ids, `google_spanner_instance` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/spanner` backend auth, missing `google_spanner_instance`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/spanner/backend.yaml` for `CockroachDB` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/spanner/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/spanner/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-spanner.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises spanner.projects.instances.create -> spanner.projects.instances.get -> spanner.projects.instances.list -> spanner.projects.instances.update -> spanner.projects.instances.delete against `/compat/gcp/spanner` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_spanner_instance` from `gcp/spanner`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: Postgres or Cockroach target requires query and driver review; no Spanner-compatible API adapter exists yet; `test/conformance/services/gcp-spanner.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-spanner.yaml`, then promote only when that manifest passes in CI.
