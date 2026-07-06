# GCP Dataproc Compatibility Plan

## Goal

Expose the smallest GCP Dataproc-compatible surface needed to migrate the ledger resources to `Apache Spark on Kubernetes` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: dataproc.projects.regions.clusters.create -> dataproc.projects.regions.clusters.get -> dataproc.projects.regions.clusters.list -> dataproc.projects.regions.clusters.patch -> dataproc.projects.regions.clusters.delete.
- Actions explicitly not supported first: Dataproc console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `dataproc.projects.regions.clusters.create` and its paired read/list calls.
- Ledger resource types: source Dataproc resource model
- First concrete resource model to add: source Dataproc resource with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Dataproc authorization failures to GCP access-denied codes, missing `source Dataproc resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/dataproc` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the source Dataproc resource model.

## Backend

- Backend: Apache Spark on Kubernetes.
- Storage and metadata: Dataproc state lives in `Apache Spark on Kubernetes`; HomePort stores provider identifiers for `source Dataproc resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Apache Spark on Kubernetes` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/dataproc`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: dataproc.projects.regions.clusters.create -> dataproc.projects.regions.clusters.get -> dataproc.projects.regions.clusters.list -> dataproc.projects.regions.clusters.patch -> dataproc.projects.regions.clusters.delete.
- Resource: projects/{project}/locations/{location}/dataproc/{id}.
- Context: evaluate Dataproc calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/dataproc/{id}`, source IP, request id, user agent, tags/labels on `source Dataproc resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Dataproc actions, `projects/{project}/locations/{location}/dataproc/{id}` prefix checks, tag/label equality on `source Dataproc resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/dataproc` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Dataproc provider names, locations, tags/labels, and request bodies map to HomePort `source Dataproc resource model` records and `Apache Spark on Kubernetes` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Dataproc provider ids, `source Dataproc resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/dataproc` backend auth, missing `source Dataproc resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/dataproc/backend.yaml` for `Apache Spark on Kubernetes` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/dataproc/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/dataproc/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-dataproc.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises dataproc.projects.regions.clusters.create -> dataproc.projects.regions.clusters.get -> dataproc.projects.regions.clusters.list -> dataproc.projects.regions.clusters.patch -> dataproc.projects.regions.clusters.delete against `/compat/gcp/dataproc` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the source Dataproc resource model from `gcp/dataproc`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-dataproc.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-dataproc.yaml`, then promote only when that manifest passes in CI.
