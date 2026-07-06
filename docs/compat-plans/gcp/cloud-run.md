# GCP Cloud Run Compatibility Plan

## Goal

Expose the smallest GCP Cloud Run-compatible surface needed to migrate the ledger resources to `Knative Serving` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: run.projects.locations.services.create -> run.projects.locations.services.get -> run.projects.locations.services.list -> run.projects.locations.services.update -> run.projects.locations.services.delete.
- Actions explicitly not supported first: Cloud Run console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `run.projects.locations.services.create` and its paired read/list calls.
- Ledger resource types: `google_cloud_run_service`.
- Provider errors: map Cloud Run authorization failures to GCP access-denied codes, missing `google_cloud_run_service` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/cloud-run` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_cloud_run_service`.

## Backend

- Backend: Knative Serving.
- Storage and metadata: Cloud Run state lives in `Knative Serving`; HomePort stores provider identifiers for `google_cloud_run_service`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Knative Serving` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/cloud-run`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: run.projects.locations.services.create -> run.projects.locations.services.get -> run.projects.locations.services.list -> run.projects.locations.services.update -> run.projects.locations.services.delete.
- Resource: projects/{project}/locations/{location}/cloud-run/{id}.
- Context: evaluate Cloud Run calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/cloud-run/{id}`, source IP, request id, user agent, tags/labels on `google_cloud_run_service`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cloud Run actions, `projects/{project}/locations/{location}/cloud-run/{id}` prefix checks, tag/label equality on `google_cloud_run_service`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/cloud-run` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Cloud Run provider names, locations, tags/labels, and request bodies map to HomePort `google_cloud_run_service` records and `Knative Serving` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cloud Run provider ids, `google_cloud_run_service` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/cloud-run` backend auth, missing `google_cloud_run_service`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/cloud-run/backend.yaml` for `Knative Serving` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/cloud-run/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/cloud-run/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-cloud-run.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises run.projects.locations.services.create -> run.projects.locations.services.get -> run.projects.locations.services.list -> run.projects.locations.services.update -> run.projects.locations.services.delete against `/compat/gcp/cloud-run` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_cloud_run_service` from `gcp/cloud-run`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-cloud-run.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-cloud-run.yaml`, then promote only when that manifest passes in CI.
