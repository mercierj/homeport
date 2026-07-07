# GCP BigQuery Compatibility Plan

## Goal

Expose the smallest GCP BigQuery-compatible surface needed to migrate the ledger resources to `Trino with Iceberg catalog` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: bigquery.datasets.insert -> bigquery.datasets.get -> bigquery.datasets.list -> bigquery.datasets.patch -> bigquery.datasets.delete.
- Actions explicitly not supported first: BigQuery console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `bigquery.datasets.insert` and its paired read/list calls.
- Ledger resource types: `google_bigquery_dataset`.
- Provider errors: map BigQuery authorization failures to GCP access-denied codes, missing `google_bigquery_dataset` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/bigquery` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_bigquery_dataset`.

## Backend

- Backend: Trino with Iceberg catalog.
- Storage and metadata: BigQuery state lives in `Trino with Iceberg catalog`; HomePort stores provider identifiers for `google_bigquery_dataset`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Trino with Iceberg catalog` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: bigquery.datasets.insert -> bigquery.datasets.get -> bigquery.datasets.list -> bigquery.datasets.patch -> bigquery.datasets.delete.
- Resource: projects/{project}/datasets/{dataset}/tables/{table}.
- Context: evaluate BigQuery calls with tenant/project/account, provider region/location, `projects/{project}/datasets/{dataset}/tables/{table}`, source IP, request id, user agent, tags/labels on `google_bigquery_dataset`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed BigQuery actions, `projects/{project}/datasets/{dataset}/tables/{table}` prefix checks, tag/label equality on `google_bigquery_dataset`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/bigquery` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: BigQuery provider names, locations, tags/labels, and request bodies map to HomePort `google_bigquery_dataset` records and `Trino with Iceberg catalog` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return BigQuery provider ids, `google_bigquery_dataset` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/bigquery` backend auth, missing `google_bigquery_dataset`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/bigquery/backend.yaml` for `Trino with Iceberg catalog` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/bigquery/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/bigquery/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-bigquery.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises bigquery.datasets.insert -> bigquery.datasets.get -> bigquery.datasets.list -> bigquery.datasets.patch -> bigquery.datasets.delete against `/compat/gcp/bigquery` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_bigquery_dataset` from `gcp/bigquery`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/gcp-bigquery.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/gcp-bigquery.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-bigquery.yaml`, then promote only when that manifest passes in CI.
