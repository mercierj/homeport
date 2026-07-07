# Azure App Service Compatibility Plan

## Goal

Expose the smallest Azure App Service-compatible surface needed to migrate the ledger resources to `Dokku or Cloud Foundry buildpacks` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Web/sites/read, Microsoft.Web/sites/write, Microsoft.Web/sites/delete.
- Actions explicitly not supported first: App Service console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Web/sites/read` and its paired read/list calls.
- Ledger resource types: `azurerm_app_service`.
- Provider errors: map App Service authorization failures to Azure access-denied codes, missing `azurerm_app_service` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/app-service` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_app_service`.

## Backend

- Backend: Dokku or Cloud Foundry buildpacks.
- Storage and metadata: App Service state lives in `Dokku or Cloud Foundry buildpacks`; HomePort stores provider identifiers for `azurerm_app_service`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Dokku or Cloud Foundry buildpacks` with generated `artifacts/compat/azure/app-service/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/app-service`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Web/sites/read, Microsoft.Web/sites/write, Microsoft.Web/sites/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Web/sites/{name}.
- Context: evaluate App Service calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Web/sites/{name}`, source IP, request id, user agent, tags/labels on `azurerm_app_service`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed App Service actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Web/sites/{name}` prefix checks, tag/label equality on `azurerm_app_service`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/app-service` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: App Service provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_app_service` records and `Dokku or Cloud Foundry buildpacks` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return App Service provider ids, `azurerm_app_service` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/app-service` backend auth, missing `azurerm_app_service`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/app-service/backend.yaml` for `Dokku or Cloud Foundry buildpacks` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/app-service/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/app-service/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-app-service.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises SitesGet -> SitesCreateOrUpdate -> SitesList -> SitesDelete against `/compat/azure/app-service` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_app_service` from `azure/app-service`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-app-service.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-app-service.yaml`, then promote only when that manifest passes in CI.
