# Azure Azure AD B2C Compatibility Plan

## Goal

Expose the smallest Azure Azure AD B2C-compatible surface needed to migrate the ledger resources to `Keycloak with realm-per-tenant mapping` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.AzureActiveDirectory/b2cDirectories/read, Microsoft.AzureActiveDirectory/b2cDirectories/write, Microsoft.AzureActiveDirectory/b2cDirectories/delete.
- Actions explicitly not supported first: Azure AD B2C console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.AzureActiveDirectory/b2cDirectories/read` and its paired read/list calls.
- Ledger resource types: `azurerm_aadb2c_directory`.
- Provider errors: map Azure AD B2C authorization failures to Azure access-denied codes, missing `azurerm_aadb2c_directory` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/azure-ad-b2c` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_aadb2c_directory`.

## Backend

- Backend: Keycloak with realm-per-tenant mapping.
- Storage and metadata: Azure AD B2C state lives in `Keycloak with realm-per-tenant mapping`; HomePort stores provider identifiers for `azurerm_aadb2c_directory`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Keycloak with realm-per-tenant mapping` with generated `artifacts/compat/azure/azure-ad-b2c/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/azure-ad-b2c`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.AzureActiveDirectory/b2cDirectories/read, Microsoft.AzureActiveDirectory/b2cDirectories/write, Microsoft.AzureActiveDirectory/b2cDirectories/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.AzureActiveDirectory/b2cDirectories/{name}.
- Context: evaluate Azure AD B2C calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.AzureActiveDirectory/b2cDirectories/{name}`, source IP, request id, user agent, tags/labels on `azurerm_aadb2c_directory`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Azure AD B2C actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.AzureActiveDirectory/b2cDirectories/{name}` prefix checks, tag/label equality on `azurerm_aadb2c_directory`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/azure-ad-b2c` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Azure AD B2C provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_aadb2c_directory` records and `Keycloak with realm-per-tenant mapping` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Azure AD B2C provider ids, `azurerm_aadb2c_directory` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/azure-ad-b2c` backend auth, missing `azurerm_aadb2c_directory`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/azure-ad-b2c/backend.yaml` for `Keycloak with realm-per-tenant mapping` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/azure-ad-b2c/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/azure-ad-b2c/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-azure-ad-b2c.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises B2cDirectoriesGet -> B2cDirectoriesCreateOrUpdate -> B2cDirectoriesList -> B2cDirectoriesDelete against `/compat/azure/azure-ad-b2c` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_aadb2c_directory` from `azure/azure-ad-b2c`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-azure-ad-b2c.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-azure-ad-b2c.yaml`, then promote only when that manifest passes in CI.
