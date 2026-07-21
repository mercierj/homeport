# Azure Front Door Compatibility Plan

## Goal

Expose the smallest Azure Front Door-compatible surface needed to migrate the ledger resources to `Caddy with Varnish cache` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Network/frontDoors/read, Microsoft.Network/frontDoors/write, Microsoft.Network/frontDoors/delete.
- Actions explicitly not supported first: Front Door console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Network/frontDoors/read` and its paired read/list calls.
- Ledger resource types: `azurerm_frontdoor`
- Provider errors: map Front Door authorization failures to Azure access-denied codes, missing `azurerm_frontdoor` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/front-door` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_frontdoor`.

## Backend

- Backend: Traefik and Varnish edge cache.
- Storage and metadata: Front Door state lives in `Caddy with Varnish cache`; HomePort stores provider identifiers for `azurerm_frontdoor`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Caddy with Varnish cache` with generated `artifacts/compat/azure/front-door/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/front-door`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Network/frontDoors/read, Microsoft.Network/frontDoors/write, Microsoft.Network/frontDoors/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/frontDoors/{name}.
- Context: evaluate Front Door calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/frontDoors/{name}`, source IP, request id, user agent, tags/labels on `azurerm_frontdoor`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Front Door actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/frontDoors/{name}` prefix checks, tag/label equality on `azurerm_frontdoor`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/front-door` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Front Door provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_frontdoor` records and `Caddy with Varnish cache` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Front Door provider ids, `azurerm_frontdoor` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/front-door` backend auth, missing `azurerm_frontdoor`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/front-door/backend.yaml` for `Caddy with Varnish cache` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/front-door/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/front-door/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-front-door.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises FrontDoorsGet -> FrontDoorsCreateOrUpdate -> FrontDoorsList -> FrontDoorsDelete against `/compat/azure/front-door` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_frontdoor` from `azure/front-door`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-front-door.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-front-door.yaml`, then promote only when that manifest passes in CI.
