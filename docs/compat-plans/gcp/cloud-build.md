# GCP Cloud Build Compatibility Plan

## Goal

Expose the smallest GCP Cloud Build-compatible surface needed to migrate the ledger resources to `Tekton Pipelines` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: cloudbuild.projects.locations.triggers.create -> cloudbuild.projects.locations.triggers.get -> cloudbuild.projects.locations.triggers.list -> cloudbuild.projects.locations.triggers.patch -> cloudbuild.projects.locations.triggers.delete.
- Actions explicitly not supported first: Cloud Build console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `cloudbuild.projects.locations.triggers.create` and its paired read/list calls.
- Ledger resource types: source Cloud Build resource model
- First concrete resource model to add: source Cloud Build resource with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Cloud Build authorization failures to GCP access-denied codes, missing `source Cloud Build resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/cloud-build` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the source Cloud Build resource model.

## Backend

- Backend: Tekton Pipelines.
- Storage and metadata: Cloud Build state lives in `Tekton Pipelines`; HomePort stores provider identifiers for `source Cloud Build resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Tekton Pipelines` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: cloudbuild.projects.locations.triggers.create -> cloudbuild.projects.locations.triggers.get -> cloudbuild.projects.locations.triggers.list -> cloudbuild.projects.locations.triggers.patch -> cloudbuild.projects.locations.triggers.delete.
- Resource: projects/{project}/locations/{location}/cloud-build/{id}.
- Context: evaluate Cloud Build calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/cloud-build/{id}`, source IP, request id, user agent, tags/labels on `source Cloud Build resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cloud Build actions, `projects/{project}/locations/{location}/cloud-build/{id}` prefix checks, tag/label equality on `source Cloud Build resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/cloud-build` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Cloud Build provider names, locations, tags/labels, and request bodies map to HomePort `source Cloud Build resource model` records and `Tekton Pipelines` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cloud Build provider ids, `source Cloud Build resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/cloud-build` backend auth, missing `source Cloud Build resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/cloud-build/backend.yaml` for `Tekton Pipelines` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/cloud-build/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/cloud-build/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-cloud-build.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises cloudbuild.projects.locations.triggers.create -> cloudbuild.projects.locations.triggers.get -> cloudbuild.projects.locations.triggers.list -> cloudbuild.projects.locations.triggers.patch -> cloudbuild.projects.locations.triggers.delete against `/compat/gcp/cloud-build` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the source Cloud Build resource model from `gcp/cloud-build`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/gcp-cloud-build.yaml` passes in CI.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-cloud-build.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-cloud-build.yaml`, then promote only when that manifest passes in CI.
