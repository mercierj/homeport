# GCP Dataplex Compatibility Plan

## Goal

Expose the smallest GCP Dataplex-compatible surface needed to migrate the ledger resources to `Apache Atlas` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: dataplex.projects.locations.lakes.create -> dataplex.projects.locations.lakes.get -> dataplex.projects.locations.lakes.list -> dataplex.projects.locations.lakes.patch -> dataplex.projects.locations.lakes.delete.
- Actions explicitly not supported first: Dataplex console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `dataplex.projects.locations.lakes.create` and its paired read/list calls.
- Ledger resource types: no resource type currently modeled in the ledger.
- First concrete resource model to add: service-specific model with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Dataplex authorization failures to GCP access-denied codes, missing `planned resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/dataplex` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the planned resource model.

## Backend

- Backend: Apache Atlas.
- Storage and metadata: Dataplex state lives in `Apache Atlas`; HomePort stores provider identifiers for `planned resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Apache Atlas` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: dataplex.projects.locations.lakes.create -> dataplex.projects.locations.lakes.get -> dataplex.projects.locations.lakes.list -> dataplex.projects.locations.lakes.patch -> dataplex.projects.locations.lakes.delete.
- Resource: projects/{project}/locations/{location}/dataplex/{id}.
- Context: evaluate Dataplex calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/dataplex/{id}`, source IP, request id, user agent, tags/labels on `planned resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Dataplex actions, `projects/{project}/locations/{location}/dataplex/{id}` prefix checks, tag/label equality on `planned resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/dataplex` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Dataplex provider names, locations, tags/labels, and request bodies map to HomePort `planned resource model` records and `Apache Atlas` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Dataplex provider ids, `planned resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/dataplex` backend auth, missing `planned resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/dataplex/backend.yaml` for `Apache Atlas` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/dataplex/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/dataplex/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-dataplex.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises dataplex.projects.locations.lakes.create -> dataplex.projects.locations.lakes.get -> dataplex.projects.locations.lakes.list -> dataplex.projects.locations.lakes.patch -> dataplex.projects.locations.lakes.delete against `/compat/gcp/dataplex` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the planned resource model from `gcp/dataplex`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-dataplex.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-dataplex.yaml`, then promote only when that manifest passes in CI.
