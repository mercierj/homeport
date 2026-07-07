# GCP Cloud SQL Compatibility Plan

## Goal

Expose the smallest GCP Cloud SQL-compatible surface needed to migrate the ledger resources to `PostgreSQL or MySQL` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: sqladmin.instances.insert -> sqladmin.instances.get -> sqladmin.instances.list -> sqladmin.instances.patch -> sqladmin.instances.delete.
- Actions explicitly not supported first: Cloud SQL console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `sqladmin.instances.insert` and its paired read/list calls.
- Ledger resource types: `google_sql_database_instance`.
- Provider errors: map Cloud SQL authorization failures to GCP access-denied codes, missing `google_sql_database_instance` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/cloud-sql` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_sql_database_instance`.

## Backend

- Backend: PostgreSQL or MySQL.
- Storage and metadata: Cloud SQL state lives in `PostgreSQL or MySQL`; HomePort stores provider identifiers for `google_sql_database_instance`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `PostgreSQL or MySQL` with generated `artifacts/compat/gcp/cloud-sql/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/cloud-sql`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: sqladmin.instances.insert -> sqladmin.instances.get -> sqladmin.instances.list -> sqladmin.instances.patch -> sqladmin.instances.delete.
- Resource: projects/{project}/locations/{location}/cloud-sql/{id}.
- Context: evaluate Cloud SQL calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/cloud-sql/{id}`, source IP, request id, user agent, tags/labels on `google_sql_database_instance`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cloud SQL actions, `projects/{project}/locations/{location}/cloud-sql/{id}` prefix checks, tag/label equality on `google_sql_database_instance`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/cloud-sql` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Cloud SQL provider names, locations, tags/labels, and request bodies map to HomePort `google_sql_database_instance` records and `PostgreSQL or MySQL` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cloud SQL provider ids, `google_sql_database_instance` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/cloud-sql` backend auth, missing `google_sql_database_instance`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/cloud-sql/backend.yaml` for `PostgreSQL or MySQL` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/cloud-sql/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/cloud-sql/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-cloud-sql.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises sqladmin.instances.insert -> sqladmin.instances.get -> sqladmin.instances.list -> sqladmin.instances.patch -> sqladmin.instances.delete against `/compat/gcp/cloud-sql` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_sql_database_instance` from `gcp/cloud-sql`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-cloud-sql.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-cloud-sql.yaml`, then promote only when that manifest passes in CI.
