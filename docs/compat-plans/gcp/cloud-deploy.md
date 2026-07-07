# GCP Cloud Deploy Compatibility Plan

## Goal

Expose the smallest GCP Cloud Deploy-compatible surface needed to migrate the ledger resources to `Argo CD` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: clouddeploy.projects.locations.deliveryPipelines.create -> clouddeploy.projects.locations.deliveryPipelines.get -> clouddeploy.projects.locations.deliveryPipelines.list -> clouddeploy.projects.locations.deliveryPipelines.patch -> clouddeploy.projects.locations.deliveryPipelines.delete.
- Actions explicitly not supported first: Cloud Deploy console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `clouddeploy.projects.locations.deliveryPipelines.create` and its paired read/list calls.
- Ledger resource types: no resource type currently modeled in the ledger.
- First concrete resource model to add: service-specific model with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Cloud Deploy authorization failures to GCP access-denied codes, missing `planned resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/cloud-deploy` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the planned resource model.

## Backend

- Backend: Argo CD.
- Storage and metadata: Cloud Deploy state lives in `Argo CD`; HomePort stores provider identifiers for `planned resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Argo CD` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: clouddeploy.projects.locations.deliveryPipelines.create -> clouddeploy.projects.locations.deliveryPipelines.get -> clouddeploy.projects.locations.deliveryPipelines.list -> clouddeploy.projects.locations.deliveryPipelines.patch -> clouddeploy.projects.locations.deliveryPipelines.delete.
- Resource: projects/{project}/locations/{location}/cloud-deploy/{id}.
- Context: evaluate Cloud Deploy calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/cloud-deploy/{id}`, source IP, request id, user agent, tags/labels on `planned resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cloud Deploy actions, `projects/{project}/locations/{location}/cloud-deploy/{id}` prefix checks, tag/label equality on `planned resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/cloud-deploy` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Cloud Deploy provider names, locations, tags/labels, and request bodies map to HomePort `planned resource model` records and `Argo CD` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cloud Deploy provider ids, `planned resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/cloud-deploy` backend auth, missing `planned resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/cloud-deploy/backend.yaml` for `Argo CD` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/cloud-deploy/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/cloud-deploy/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-cloud-deploy.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises clouddeploy.projects.locations.deliveryPipelines.create -> clouddeploy.projects.locations.deliveryPipelines.get -> clouddeploy.projects.locations.deliveryPipelines.list -> clouddeploy.projects.locations.deliveryPipelines.patch -> clouddeploy.projects.locations.deliveryPipelines.delete against `/compat/gcp/cloud-deploy` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the planned resource model from `gcp/cloud-deploy`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-cloud-deploy.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-cloud-deploy.yaml`, then promote only when that manifest passes in CI.
