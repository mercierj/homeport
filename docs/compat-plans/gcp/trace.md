# GCP Trace Compatibility Plan

## Goal

Expose the smallest GCP Trace-compatible surface needed to migrate the ledger resources to `Grafana Tempo` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: cloudtrace.projects.traces.patch -> cloudtrace.projects.traces.get -> cloudtrace.projects.traces.list; cloudtrace.projects.traces.batchWrite.
- Actions explicitly not supported first: Trace console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `cloudtrace.projects.traces.patch` and its paired read/list calls.
- Ledger resource types: source Trace resource model
- First concrete resource model to add: source Trace resource with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Trace authorization failures to GCP access-denied codes, missing `source Trace resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/trace` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the source Trace resource model.

## Backend

- Backend: Grafana Tempo.
- Storage and metadata: Trace state lives in `Grafana Tempo`; HomePort stores provider identifiers for `source Trace resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Grafana Tempo` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/trace`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: cloudtrace.projects.traces.patch -> cloudtrace.projects.traces.get -> cloudtrace.projects.traces.list; cloudtrace.projects.traces.batchWrite.
- Resource: projects/{project}/locations/{location}/trace/{id}.
- Context: evaluate Trace calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/trace/{id}`, source IP, request id, user agent, tags/labels on `source Trace resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Trace actions, `projects/{project}/locations/{location}/trace/{id}` prefix checks, tag/label equality on `source Trace resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/trace` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Trace provider names, locations, tags/labels, and request bodies map to HomePort `source Trace resource model` records and `Grafana Tempo` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Trace provider ids, `source Trace resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/trace` backend auth, missing `source Trace resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/trace/backend.yaml` for `Grafana Tempo` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/trace/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/trace/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-trace.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises cloudtrace.projects.traces.patch -> cloudtrace.projects.traces.get -> cloudtrace.projects.traces.list; cloudtrace.projects.traces.batchWrite against `/compat/gcp/trace` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the source Trace resource model from `gcp/trace`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-trace.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-trace.yaml`, then promote only when that manifest passes in CI.
