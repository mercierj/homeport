# GCP Compute Engine Compatibility Plan

## Goal

Expose the smallest GCP Compute Engine-compatible surface needed to migrate the ledger resources to `Incus or libvirt virtual machines` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: compute.instances.insert -> compute.instances.get -> compute.instances.list -> compute.instances.setMetadata -> compute.instances.delete.
- Actions explicitly not supported first: Compute Engine console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `compute.instances.insert` and its paired read/list calls.
- Ledger resource types: `google_compute_instance`.
- Provider errors: map Compute Engine authorization failures to GCP access-denied codes, missing `google_compute_instance` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/compute-engine` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_compute_instance`.

## Backend

- Backend: Incus or libvirt virtual machines.
- Storage and metadata: Compute Engine state lives in `Incus or libvirt virtual machines`; HomePort stores provider identifiers for `google_compute_instance`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Incus or libvirt virtual machines` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/compute-engine`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: compute.instances.insert -> compute.instances.get -> compute.instances.list -> compute.instances.setMetadata -> compute.instances.delete.
- Resource: projects/{project}/zones/{zone}/instances/{instance}.
- Context: evaluate Compute Engine calls with tenant/project/account, provider region/location, `projects/{project}/zones/{zone}/instances/{instance}`, source IP, request id, user agent, tags/labels on `google_compute_instance`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Compute Engine actions, `projects/{project}/zones/{zone}/instances/{instance}` prefix checks, tag/label equality on `google_compute_instance`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/compute-engine` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Compute Engine provider names, locations, tags/labels, and request bodies map to HomePort `google_compute_instance` records and `Incus or libvirt virtual machines` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Compute Engine provider ids, `google_compute_instance` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/compute-engine` backend auth, missing `google_compute_instance`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/compute-engine/backend.yaml` for `Incus or libvirt virtual machines` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/compute-engine/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/compute-engine/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-compute-engine.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises compute.instances.insert -> compute.instances.get -> compute.instances.list -> compute.instances.setMetadata -> compute.instances.delete against `/compat/gcp/compute-engine` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_compute_instance` from `gcp/compute-engine`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-compute-engine.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-compute-engine.yaml`, then promote only when that manifest passes in CI.
