# Azure Azure CDN Compatibility Plan

## Goal

Expose the smallest Azure Azure CDN-compatible surface needed to migrate the ledger resources to `Caddy with Varnish cache` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Cdn/profiles/endpoints/read, Microsoft.Cdn/profiles/endpoints/write, Microsoft.Cdn/profiles/endpoints/delete.
- Actions explicitly not supported first: Azure CDN console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Cdn/profiles/endpoints/read` and its paired read/list calls.
- Ledger resource types: `azurerm_cdn_profile`
- Provider errors: map Azure CDN authorization failures to Azure access-denied codes, missing `azurerm_cdn_profile` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/azure-cdn` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_cdn_profile`.

## Backend

- Backend: Caddy with Varnish cache.
- Storage and metadata: Azure CDN state lives in `Caddy with Varnish cache`; HomePort stores provider identifiers for `azurerm_cdn_profile`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Caddy with Varnish cache` with generated `artifacts/compat/azure/azure-cdn/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/azure-cdn`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Cdn/profiles/endpoints/read, Microsoft.Cdn/profiles/endpoints/write, Microsoft.Cdn/profiles/endpoints/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Cdn/profiles/endpoints/{name}.
- Context: evaluate Azure CDN calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Cdn/profiles/endpoints/{name}`, source IP, request id, user agent, tags/labels on `azurerm_cdn_profile`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Azure CDN actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Cdn/profiles/endpoints/{name}` prefix checks, tag/label equality on `azurerm_cdn_profile`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/azure-cdn` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Azure CDN provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_cdn_profile` records and `Caddy with Varnish cache` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Azure CDN provider ids, `azurerm_cdn_profile` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/azure-cdn` backend auth, missing `azurerm_cdn_profile`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/azure-cdn/backend.yaml` for `Caddy with Varnish cache` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/azure-cdn/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/azure-cdn/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-azure-cdn.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises EndpointsGet -> EndpointsCreateOrUpdate -> EndpointsList -> EndpointsDelete against `/compat/azure/azure-cdn` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_cdn_profile` from `azure/azure-cdn`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-azure-cdn.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-azure-cdn.yaml`, then promote only when that manifest passes in CI.
