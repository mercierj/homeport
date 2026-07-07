# Azure Azure Cache Compatibility Plan

## Goal

Expose the smallest Azure Azure Cache-compatible surface needed to migrate the ledger resources to `Redis or Valkey` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Cache/Redis/read, Microsoft.Cache/Redis/write, Microsoft.Cache/Redis/delete.
- Actions explicitly not supported first: Azure Cache console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Cache/Redis/read` and its paired read/list calls.
- Ledger resource types: `azurerm_redis_cache`.
- Provider errors: map Azure Cache authorization failures to Azure access-denied codes, missing `azurerm_redis_cache` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/azure-cache` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_redis_cache`.

## Backend

- Backend: Redis or Valkey.
- Storage and metadata: Azure Cache state lives in `Redis or Valkey`; HomePort stores provider identifiers for `azurerm_redis_cache`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Redis or Valkey` with generated `artifacts/compat/azure/azure-cache/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/azure-cache`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Cache/Redis/read, Microsoft.Cache/Redis/write, Microsoft.Cache/Redis/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Cache/Redis/{name}.
- Context: evaluate Azure Cache calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Cache/Redis/{name}`, source IP, request id, user agent, tags/labels on `azurerm_redis_cache`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Azure Cache actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Cache/Redis/{name}` prefix checks, tag/label equality on `azurerm_redis_cache`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/azure-cache` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Azure Cache provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_redis_cache` records and `Redis or Valkey` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Azure Cache provider ids, `azurerm_redis_cache` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/azure-cache` backend auth, missing `azurerm_redis_cache`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/azure-cache/backend.yaml` for `Redis or Valkey` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/azure-cache/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/azure-cache/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-azure-cache.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises RedisGet -> RedisCreateOrUpdate -> RedisList -> RedisDelete against `/compat/azure/azure-cache` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_redis_cache` from `azure/azure-cache`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-azure-cache.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-azure-cache.yaml`, then promote only when that manifest passes in CI.
