# GCP Cloud Storage Compatibility Plan

## Goal

Expose the smallest GCP Cloud Storage-compatible surface needed to migrate the ledger resources to `MinIO` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: storage.buckets.insert -> storage.buckets.get -> storage.buckets.list -> storage.buckets.patch -> storage.buckets.delete.
- Actions explicitly not supported first: Cloud Storage console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `storage.buckets.insert` and its paired read/list calls.
- Ledger resource types: `google_storage_bucket`.
- Provider errors: map Cloud Storage authorization failures to GCP access-denied codes, missing `google_storage_bucket` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/cloud-storage` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_storage_bucket`.

## Backend

- Backend: MinIO.
- Storage and metadata: Cloud Storage state lives in `MinIO`; HomePort stores provider identifiers for `google_storage_bucket`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `MinIO` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/cloud-storage`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: storage.buckets.insert -> storage.buckets.get -> storage.buckets.list -> storage.buckets.patch -> storage.buckets.delete.
- Resource: projects/_/buckets/{bucket}/objects/{object}.
- Context: evaluate Cloud Storage calls with tenant/project/account, provider region/location, `projects/_/buckets/{bucket}/objects/{object}`, source IP, request id, user agent, tags/labels on `google_storage_bucket`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cloud Storage actions, `projects/_/buckets/{bucket}/objects/{object}` prefix checks, tag/label equality on `google_storage_bucket`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/cloud-storage` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Cloud Storage provider names, locations, tags/labels, and request bodies map to HomePort `google_storage_bucket` records and `MinIO` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cloud Storage provider ids, `google_storage_bucket` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/cloud-storage` backend auth, missing `google_storage_bucket`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/cloud-storage/backend.yaml` for `MinIO` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/cloud-storage/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/cloud-storage/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-cloud-storage.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises storage.buckets.insert -> storage.buckets.get -> storage.buckets.list -> storage.buckets.patch -> storage.buckets.delete against `/compat/gcp/cloud-storage` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_storage_bucket` from `gcp/cloud-storage`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: MinIO S3 endpoint is generated, but native GCS SDK compatibility needs an adapter or application SDK switch; `test/conformance/services/gcp-cloud-storage.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-cloud-storage.yaml`, then promote only when that manifest passes in CI.
