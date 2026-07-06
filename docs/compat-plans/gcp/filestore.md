# GCP Filestore Compatibility Plan

## Goal

Expose the smallest GCP Filestore-compatible surface needed to migrate the ledger resources to `NFS-Ganesha` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: file.projects.locations.instances.create -> file.projects.locations.instances.get -> file.projects.locations.instances.list -> file.projects.locations.instances.patch -> file.projects.locations.instances.delete.
- Actions explicitly not supported first: Filestore console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `file.projects.locations.instances.create` and its paired read/list calls.
- Ledger resource types: `google_filestore_instance`.
- Provider errors: map Filestore authorization failures to GCP access-denied codes, missing `google_filestore_instance` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/filestore` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_filestore_instance`.

## Backend

- Backend: NFS-Ganesha.
- Storage and metadata: Filestore state lives in `NFS-Ganesha`; HomePort stores provider identifiers for `google_filestore_instance`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `NFS-Ganesha` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/filestore`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: file.projects.locations.instances.create -> file.projects.locations.instances.get -> file.projects.locations.instances.list -> file.projects.locations.instances.patch -> file.projects.locations.instances.delete.
- Resource: projects/{project}/locations/{location}/filestore/{id}.
- Context: evaluate Filestore calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/filestore/{id}`, source IP, request id, user agent, tags/labels on `google_filestore_instance`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Filestore actions, `projects/{project}/locations/{location}/filestore/{id}` prefix checks, tag/label equality on `google_filestore_instance`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/filestore` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Filestore provider names, locations, tags/labels, and request bodies map to HomePort `google_filestore_instance` records and `NFS-Ganesha` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Filestore provider ids, `google_filestore_instance` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/filestore` backend auth, missing `google_filestore_instance`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/filestore/backend.yaml` for `NFS-Ganesha` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/filestore/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/filestore/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-filestore.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises file.projects.locations.instances.create -> file.projects.locations.instances.get -> file.projects.locations.instances.list -> file.projects.locations.instances.patch -> file.projects.locations.instances.delete against `/compat/gcp/filestore` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_filestore_instance` from `gcp/filestore`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-filestore.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-filestore.yaml`, then promote only when that manifest passes in CI.
