# GCP Memorystore Compatibility Plan

## Goal

Expose the smallest GCP Memorystore-compatible surface needed to migrate the ledger resources to `Redis or Valkey` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: redis.projects.locations.instances.create -> redis.projects.locations.instances.get -> redis.projects.locations.instances.list -> redis.projects.locations.instances.patch -> redis.projects.locations.instances.delete.
- Actions explicitly not supported first: Memorystore console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `redis.projects.locations.instances.create` and its paired read/list calls.
- Ledger resource types: `google_redis_instance`.
- Provider errors: map Memorystore authorization failures to GCP access-denied codes, missing `google_redis_instance` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/memorystore` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_redis_instance`.

## Backend

- Backend: Redis or Valkey.
- Storage and metadata: Memorystore state lives in `Redis or Valkey`; HomePort stores provider identifiers for `google_redis_instance`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Redis or Valkey` with generated `artifacts/compat/gcp/memorystore/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/memorystore`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: redis.projects.locations.instances.create -> redis.projects.locations.instances.get -> redis.projects.locations.instances.list -> redis.projects.locations.instances.patch -> redis.projects.locations.instances.delete.
- Resource: projects/{project}/locations/{location}/memorystore/{id}.
- Context: evaluate Memorystore calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/memorystore/{id}`, source IP, request id, user agent, tags/labels on `google_redis_instance`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Memorystore actions, `projects/{project}/locations/{location}/memorystore/{id}` prefix checks, tag/label equality on `google_redis_instance`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/memorystore` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Memorystore provider names, locations, tags/labels, and request bodies map to HomePort `google_redis_instance` records and `Redis or Valkey` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Memorystore provider ids, `google_redis_instance` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/memorystore` backend auth, missing `google_redis_instance`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/memorystore/backend.yaml` for `Redis or Valkey` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/memorystore/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/memorystore/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-memorystore.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises redis.projects.locations.instances.create -> redis.projects.locations.instances.get -> redis.projects.locations.instances.list -> redis.projects.locations.instances.patch -> redis.projects.locations.instances.delete against `/compat/gcp/memorystore` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_redis_instance` from `gcp/memorystore`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-memorystore.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-memorystore.yaml`, then promote only when that manifest passes in CI.
