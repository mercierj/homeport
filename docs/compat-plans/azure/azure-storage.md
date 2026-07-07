# Azure Azure Storage Compatibility Plan

## Goal

Expose the smallest Azure Azure Storage-compatible surface needed to migrate the ledger resources to `MinIO and Azurite` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Storage/storageAccounts/read, Microsoft.Storage/storageAccounts/write, Microsoft.Storage/storageAccounts/delete.
- Actions explicitly not supported first: Azure Storage console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Storage/storageAccounts/read` and its paired read/list calls.
- Ledger resource types: `azurerm_storage_container`, `azurerm_storage_account`, `azurerm_storage_share`.
- Provider errors: map Azure Storage authorization failures to Azure access-denied codes, missing `azurerm_storage_container` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/azure-storage` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_storage_container`.

## Backend

- Backend: MinIO and Azurite.
- Storage and metadata: Azure Storage state lives in `MinIO and Azurite`; HomePort stores provider identifiers for `azurerm_storage_container`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `MinIO and Azurite` with generated `artifacts/compat/azure/azure-storage/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/azure-storage`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Storage/storageAccounts/read, Microsoft.Storage/storageAccounts/write, Microsoft.Storage/storageAccounts/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Storage/storageAccounts/{name}.
- Context: evaluate Azure Storage calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Storage/storageAccounts/{name}`, source IP, request id, user agent, tags/labels on `azurerm_storage_container`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Azure Storage actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Storage/storageAccounts/{name}` prefix checks, tag/label equality on `azurerm_storage_container`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/azure-storage` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Azure Storage provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_storage_container` records and `MinIO and Azurite` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Azure Storage provider ids, `azurerm_storage_container` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/azure-storage` backend auth, missing `azurerm_storage_container`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/azure-storage/backend.yaml` for `MinIO and Azurite` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/azure-storage/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/azure-storage/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-azure-storage.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises StorageAccountsGet -> StorageAccountsCreateOrUpdate -> StorageAccountsList -> StorageAccountsDelete against `/compat/azure/azure-storage` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_storage_container`, `azurerm_storage_account`, `azurerm_storage_share` from `azure/azure-storage`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: MinIO/Azurite targets are generated, but native Azure Storage SDK compatibility needs adapter validation or application SDK switch; `test/conformance/services/azure-azure-storage.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-azure-storage.yaml`, then promote only when that manifest passes in CI.
