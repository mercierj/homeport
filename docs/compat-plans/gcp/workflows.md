# GCP Workflows Compatibility Plan

## Goal

Expose the smallest GCP Workflows-compatible surface needed to migrate the ledger resources to `Temporal workflows` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: workflows.projects.locations.workflows.create -> workflows.projects.locations.workflows.get -> workflows.projects.locations.workflows.list -> workflows.projects.locations.workflows.patch -> workflows.projects.locations.workflows.delete; executions.create/list.
- Actions explicitly not supported first: Workflows console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `workflows.projects.locations.workflows.create` and its paired read/list calls.
- Ledger resource types: source Workflows resource model
- First concrete resource model to add: source Workflows resource with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Workflows authorization failures to GCP access-denied codes, missing `source Workflows resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/workflows` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the source Workflows resource model.

## Backend

- Backend: Temporal workflows.
- Storage and metadata: Workflows state lives in `Temporal workflows`; HomePort stores provider identifiers for `source Workflows resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Temporal workflows` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/workflows`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: workflows.projects.locations.workflows.create -> workflows.projects.locations.workflows.get -> workflows.projects.locations.workflows.list -> workflows.projects.locations.workflows.patch -> workflows.projects.locations.workflows.delete; executions.create/list.
- Resource: projects/{project}/locations/{location}/workflows/{id}.
- Context: evaluate Workflows calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/workflows/{id}`, source IP, request id, user agent, tags/labels on `source Workflows resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Workflows actions, `projects/{project}/locations/{location}/workflows/{id}` prefix checks, tag/label equality on `source Workflows resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/workflows` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Workflows provider names, locations, tags/labels, and request bodies map to HomePort `source Workflows resource model` records and `Temporal workflows` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Workflows provider ids, `source Workflows resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/workflows` backend auth, missing `source Workflows resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/workflows/backend.yaml` for `Temporal workflows` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/workflows/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/workflows/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-workflows.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises workflows.projects.locations.workflows.create -> workflows.projects.locations.workflows.get -> workflows.projects.locations.workflows.list -> workflows.projects.locations.workflows.patch -> workflows.projects.locations.workflows.delete; executions.create/list against `/compat/gcp/workflows` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the source Workflows resource model from `gcp/workflows`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-workflows.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-workflows.yaml`, then promote only when that manifest passes in CI.
