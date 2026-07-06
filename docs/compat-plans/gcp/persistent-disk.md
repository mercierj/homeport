# GCP Persistent Disk Compatibility Plan

## Goal

Expose the smallest GCP Persistent Disk-compatible surface needed to migrate the ledger resources to `Longhorn block volumes` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: compute.disks.insert -> compute.disks.get -> compute.disks.list -> compute.disks.resize -> compute.disks.delete.
- Actions explicitly not supported first: Persistent Disk console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `compute.disks.insert` and its paired read/list calls.
- Ledger resource types: `google_compute_disk`.
- Provider errors: map Persistent Disk authorization failures to GCP access-denied codes, missing `google_compute_disk` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/persistent-disk` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_compute_disk`.

## Backend

- Backend: Longhorn block volumes.
- Storage and metadata: Persistent Disk state lives in `Longhorn block volumes`; HomePort stores provider identifiers for `google_compute_disk`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Longhorn block volumes` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/persistent-disk`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: compute.disks.insert -> compute.disks.get -> compute.disks.list -> compute.disks.resize -> compute.disks.delete.
- Resource: projects/{project}/zones/{zone}/disks/{disk}.
- Context: evaluate Persistent Disk calls with tenant/project/account, provider region/location, `projects/{project}/zones/{zone}/disks/{disk}`, source IP, request id, user agent, tags/labels on `google_compute_disk`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Persistent Disk actions, `projects/{project}/zones/{zone}/disks/{disk}` prefix checks, tag/label equality on `google_compute_disk`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/persistent-disk` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Persistent Disk provider names, locations, tags/labels, and request bodies map to HomePort `google_compute_disk` records and `Longhorn block volumes` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Persistent Disk provider ids, `google_compute_disk` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/persistent-disk` backend auth, missing `google_compute_disk`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/persistent-disk/backend.yaml` for `Longhorn block volumes` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/persistent-disk/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/persistent-disk/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-persistent-disk.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises compute.disks.insert -> compute.disks.get -> compute.disks.list -> compute.disks.resize -> compute.disks.delete against `/compat/gcp/persistent-disk` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_compute_disk` from `gcp/persistent-disk`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-persistent-disk.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-persistent-disk.yaml`, then promote only when that manifest passes in CI.
