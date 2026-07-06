# GCP Profiler Compatibility Plan

## Goal

Expose the smallest GCP Profiler-compatible surface needed to migrate the ledger resources to `Pyroscope` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: cloudprofiler.projects.profiles.create -> profiles.patch -> profiles.list.
- Actions explicitly not supported first: Profiler console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `cloudprofiler.projects.profiles.create` and its paired read/list calls.
- Ledger resource types: source Profiler resource model
- First concrete resource model to add: source Profiler resource with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Profiler authorization failures to GCP access-denied codes, missing `source Profiler resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/profiler` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the source Profiler resource model.

## Backend

- Backend: Pyroscope.
- Storage and metadata: Profiler state lives in `Pyroscope`; HomePort stores provider identifiers for `source Profiler resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Pyroscope` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: cloudprofiler.projects.profiles.create -> profiles.patch -> profiles.list.
- Resource: projects/{project}/locations/{location}/profiler/{id}.
- Context: evaluate Profiler calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/profiler/{id}`, source IP, request id, user agent, tags/labels on `source Profiler resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Profiler actions, `projects/{project}/locations/{location}/profiler/{id}` prefix checks, tag/label equality on `source Profiler resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/profiler` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Profiler provider names, locations, tags/labels, and request bodies map to HomePort `source Profiler resource model` records and `Pyroscope` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Profiler provider ids, `source Profiler resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/profiler` backend auth, missing `source Profiler resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/profiler/backend.yaml` for `Pyroscope` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/profiler/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/profiler/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-profiler.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises cloudprofiler.projects.profiles.create -> profiles.patch -> profiles.list against `/compat/gcp/profiler` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the source Profiler resource model from `gcp/profiler`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-profiler.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-profiler.yaml`, then promote only when that manifest passes in CI.
