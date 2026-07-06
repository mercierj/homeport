# GCP Firestore Compatibility Plan

## Goal

Expose the smallest GCP Firestore-compatible surface needed to migrate the ledger resources to `FerretDB with document-store compatibility review` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: firestore.projects.databases.create -> firestore.projects.databases.get -> firestore.projects.databases.list -> firestore.projects.databases.patch -> firestore.projects.databases.delete.
- Actions explicitly not supported first: Firestore console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `firestore.projects.databases.create` and its paired read/list calls.
- Ledger resource types: `google_firestore_database`.
- Provider errors: map Firestore authorization failures to GCP access-denied codes, missing `google_firestore_database` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/firestore` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_firestore_database`.

## Backend

- Backend: FerretDB with document-store compatibility review.
- Storage and metadata: Firestore state lives in `FerretDB with document-store compatibility review`; HomePort stores provider identifiers for `google_firestore_database`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `FerretDB with document-store compatibility review` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/firestore`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: firestore.projects.databases.create -> firestore.projects.databases.get -> firestore.projects.databases.list -> firestore.projects.databases.patch -> firestore.projects.databases.delete.
- Resource: projects/{project}/locations/{location}/firestore/{id}.
- Context: evaluate Firestore calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/firestore/{id}`, source IP, request id, user agent, tags/labels on `google_firestore_database`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Firestore actions, `projects/{project}/locations/{location}/firestore/{id}` prefix checks, tag/label equality on `google_firestore_database`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/firestore` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Firestore provider names, locations, tags/labels, and request bodies map to HomePort `google_firestore_database` records and `FerretDB with document-store compatibility review` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Firestore provider ids, `google_firestore_database` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/firestore` backend auth, missing `google_firestore_database`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/firestore/backend.yaml` for `FerretDB with document-store compatibility review` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/firestore/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/firestore/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-firestore.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises firestore.projects.databases.create -> firestore.projects.databases.get -> firestore.projects.databases.list -> firestore.projects.databases.patch -> firestore.projects.databases.delete against `/compat/gcp/firestore` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_firestore_database` from `gcp/firestore`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: no Firestore-compatible local API adapter exists yet; `test/conformance/services/gcp-firestore.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-firestore.yaml`, then promote only when that manifest passes in CI.
