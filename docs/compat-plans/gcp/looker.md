# GCP Looker Compatibility Plan

## Goal

Expose the smallest GCP Looker-compatible surface needed to migrate the ledger resources to `Apache Superset` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: looker.projects.locations.instances.create -> looker.projects.locations.instances.get -> looker.projects.locations.instances.list -> looker.projects.locations.instances.patch -> looker.projects.locations.instances.delete.
- Actions explicitly not supported first: Looker console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `looker.projects.locations.instances.create` and its paired read/list calls.
- Ledger resource types: source Looker resource model
- First concrete resource model to add: source Looker resource with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Looker authorization failures to GCP access-denied codes, missing `source Looker resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/looker` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the source Looker resource model.

## Backend

- Backend: Apache Superset.
- Storage and metadata: Looker state lives in `Apache Superset`; HomePort stores provider identifiers for `source Looker resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Apache Superset` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: looker.projects.locations.instances.create -> looker.projects.locations.instances.get -> looker.projects.locations.instances.list -> looker.projects.locations.instances.patch -> looker.projects.locations.instances.delete.
- Resource: projects/{project}/locations/{location}/looker/{id}.
- Context: evaluate Looker calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/looker/{id}`, source IP, request id, user agent, tags/labels on `source Looker resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Looker actions, `projects/{project}/locations/{location}/looker/{id}` prefix checks, tag/label equality on `source Looker resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/looker` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Looker provider names, locations, tags/labels, and request bodies map to HomePort `source Looker resource model` records and `Apache Superset` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Looker provider ids, `source Looker resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/looker` backend auth, missing `source Looker resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/looker/backend.yaml` for `Apache Superset` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/looker/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/looker/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-looker.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises looker.projects.locations.instances.create -> looker.projects.locations.instances.get -> looker.projects.locations.instances.list -> looker.projects.locations.instances.patch -> looker.projects.locations.instances.delete against `/compat/gcp/looker` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the source Looker resource model from `gcp/looker`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-looker.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-looker.yaml`, then promote only when that manifest passes in CI.
