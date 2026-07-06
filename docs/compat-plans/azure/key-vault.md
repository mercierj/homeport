# Azure Key Vault Compatibility Plan

## Goal

Expose the smallest Azure Key Vault-compatible surface needed to migrate the ledger resources to `HashiCorp Vault` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.KeyVault/vaults/read, Microsoft.KeyVault/vaults/write, Microsoft.KeyVault/vaults/delete.
- Actions explicitly not supported first: Key Vault console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `Microsoft.KeyVault/vaults/read` and its paired read/list calls.
- Ledger resource types: `azurerm_key_vault`.
- Provider errors: map Key Vault authorization failures to Azure access-denied codes, missing `azurerm_key_vault` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/key-vault` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_key_vault`.

## Backend

- Backend: HashiCorp Vault.
- Storage and metadata: Key Vault state lives in `HashiCorp Vault`; HomePort stores provider identifiers for `azurerm_key_vault`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `HashiCorp Vault` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/key-vault`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.KeyVault/vaults/read, Microsoft.KeyVault/vaults/write, Microsoft.KeyVault/vaults/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.KeyVault/vaults/{name}.
- Context: evaluate Key Vault calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.KeyVault/vaults/{name}`, source IP, request id, user agent, tags/labels on `azurerm_key_vault`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Key Vault actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.KeyVault/vaults/{name}` prefix checks, tag/label equality on `azurerm_key_vault`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/key-vault` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Key Vault provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_key_vault` records and `HashiCorp Vault` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Key Vault provider ids, `azurerm_key_vault` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/key-vault` backend auth, missing `azurerm_key_vault`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/key-vault/backend.yaml` for `HashiCorp Vault` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/key-vault/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/key-vault/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-key-vault.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises VaultsGet -> VaultsCreateOrUpdate -> VaultsList -> VaultsDelete against `/compat/azure/key-vault` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_key_vault` from `azure/key-vault`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-key-vault.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-key-vault.yaml`, then promote only when that manifest passes in CI.
