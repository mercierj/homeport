# GCP Cloud Functions Compatibility Plan

## Goal

Expose the smallest GCP Cloud Functions-compatible surface needed to migrate the ledger resources to `OpenFaaS` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: cloudfunctions.projects.locations.functions.create -> cloudfunctions.projects.locations.functions.get -> cloudfunctions.projects.locations.functions.list -> cloudfunctions.projects.locations.functions.patch -> cloudfunctions.projects.locations.functions.delete.
- Actions explicitly not supported first: Cloud Functions console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `cloudfunctions.projects.locations.functions.create` and its paired read/list calls.
- Ledger resource types: `google_cloudfunctions_function`.
- Provider errors: map Cloud Functions authorization failures to GCP access-denied codes, missing `google_cloudfunctions_function` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/cloud-functions` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_cloudfunctions_function`.

## Backend

- Backend: Docker function runtime.
- Storage and metadata: Cloud Functions state lives in `OpenFaaS`; HomePort stores provider identifiers for `google_cloudfunctions_function`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `OpenFaaS` with generated `artifacts/compat/gcp/cloud-functions/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/cloud-functions`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: cloudfunctions.projects.locations.functions.create -> cloudfunctions.projects.locations.functions.get -> cloudfunctions.projects.locations.functions.list -> cloudfunctions.projects.locations.functions.patch -> cloudfunctions.projects.locations.functions.delete.
- Resource: projects/{project}/locations/{location}/cloud-functions/{id}.
- Context: evaluate Cloud Functions calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/cloud-functions/{id}`, source IP, request id, user agent, tags/labels on `google_cloudfunctions_function`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cloud Functions actions, `projects/{project}/locations/{location}/cloud-functions/{id}` prefix checks, tag/label equality on `google_cloudfunctions_function`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/cloud-functions` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Cloud Functions provider names, locations, tags/labels, and request bodies map to HomePort `google_cloudfunctions_function` records and `OpenFaaS` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cloud Functions provider ids, `google_cloudfunctions_function` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/cloud-functions` backend auth, missing `google_cloudfunctions_function`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/cloud-functions/backend.yaml` for `OpenFaaS` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/cloud-functions/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/cloud-functions/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-cloud-functions.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises cloudfunctions.projects.locations.functions.create -> cloudfunctions.projects.locations.functions.get -> cloudfunctions.projects.locations.functions.list -> cloudfunctions.projects.locations.functions.patch -> cloudfunctions.projects.locations.functions.delete against `/compat/gcp/cloud-functions` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_cloudfunctions_function` from `gcp/cloud-functions`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-cloud-functions.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-cloud-functions.yaml`, then promote only when that manifest passes in CI.
