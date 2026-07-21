# GCP Bigtable Compatibility Plan

## Goal

Expose the smallest GCP Bigtable-compatible surface needed to migrate the ledger resources to Cassandra with Bigtable API adapter without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: bigtableadmin.projects.instances.create -> bigtableadmin.projects.instances.get -> bigtableadmin.projects.instances.list -> bigtableadmin.projects.instances.partialUpdateInstance -> bigtableadmin.projects.instances.delete.
- Actions explicitly not supported first: Bigtable console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `bigtableadmin.projects.instances.create` and its paired read/list calls.
- Ledger resource types: `google_bigtable_instance`
- Provider errors: map Bigtable authorization failures to GCP access-denied codes, missing `google_bigtable_instance` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/bigtable` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_bigtable_instance`.

## Backend

- Backend: Cassandra with Bigtable API adapter.
- Storage and metadata: Bigtable state lives in `Cassandra with Bigtable API adapter`; HomePort stores provider identifiers for `google_bigtable_instance`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision Cassandra with Bigtable API adapter with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: bigtableadmin.projects.instances.create -> bigtableadmin.projects.instances.get -> bigtableadmin.projects.instances.list -> bigtableadmin.projects.instances.partialUpdateInstance -> bigtableadmin.projects.instances.delete.
- Resource: projects/{project}/locations/{location}/bigtable/{id}.
- Context: evaluate Bigtable calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/bigtable/{id}`, source IP, request id, user agent, tags/labels on `google_bigtable_instance`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Bigtable actions, `projects/{project}/locations/{location}/bigtable/{id}` prefix checks, tag/label equality on `google_bigtable_instance`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/bigtable` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Bigtable provider names, locations, tags/labels, and request bodies map to HomePort `google_bigtable_instance` records and `Cassandra with Bigtable API adapter` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Bigtable provider ids, `google_bigtable_instance` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/bigtable` backend auth, missing `google_bigtable_instance`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/bigtable/backend.yaml` for Cassandra with Bigtable API adapter configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/bigtable/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/bigtable/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-bigtable.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises bigtableadmin.projects.instances.create -> bigtableadmin.projects.instances.get -> bigtableadmin.projects.instances.list -> bigtableadmin.projects.instances.partialUpdateInstance -> bigtableadmin.projects.instances.delete against `/compat/gcp/bigtable` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_bigtable_instance` from `gcp/bigtable`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/gcp-bigtable.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/gcp-bigtable.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-bigtable.yaml`, then promote only when that manifest passes in CI.
