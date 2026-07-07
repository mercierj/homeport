# GCP TPU Compatibility Plan

## Goal

Expose the smallest GCP TPU-compatible surface needed to migrate the ledger resources to `Kubernetes GPU/accelerator scheduling` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: tpu.projects.locations.nodes.create -> tpu.projects.locations.nodes.get -> tpu.projects.locations.nodes.list -> tpu.projects.locations.nodes.patch -> tpu.projects.locations.nodes.delete.
- Actions explicitly not supported first: TPU console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `tpu.projects.locations.nodes.create` and its paired read/list calls.
- Ledger resource types: no resource type currently modeled in the ledger.
- First concrete resource model to add: service-specific model with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map TPU authorization failures to GCP access-denied codes, missing `planned resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/tpu` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the planned resource model.

## Backend

- Backend: Kubernetes GPU/accelerator scheduling.
- Storage and metadata: TPU state lives in `Kubernetes GPU/accelerator scheduling`; HomePort stores provider identifiers for `planned resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Kubernetes GPU/accelerator scheduling` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: tpu.projects.locations.nodes.create -> tpu.projects.locations.nodes.get -> tpu.projects.locations.nodes.list -> tpu.projects.locations.nodes.patch -> tpu.projects.locations.nodes.delete.
- Resource: projects/{project}/locations/{location}/tpu/{id}.
- Context: evaluate TPU calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/tpu/{id}`, source IP, request id, user agent, tags/labels on `planned resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed TPU actions, `projects/{project}/locations/{location}/tpu/{id}` prefix checks, tag/label equality on `planned resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/tpu` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: TPU provider names, locations, tags/labels, and request bodies map to HomePort `planned resource model` records and `Kubernetes GPU/accelerator scheduling` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return TPU provider ids, `planned resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/tpu` backend auth, missing `planned resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/tpu/backend.yaml` for `Kubernetes GPU/accelerator scheduling` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/tpu/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/tpu/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-tpu.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises tpu.projects.locations.nodes.create -> tpu.projects.locations.nodes.get -> tpu.projects.locations.nodes.list -> tpu.projects.locations.nodes.patch -> tpu.projects.locations.nodes.delete against `/compat/gcp/tpu` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the planned resource model from `gcp/tpu`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-tpu.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-tpu.yaml`, then promote only when that manifest passes in CI.
