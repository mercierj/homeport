# Azure Cosmos DB Compatibility Plan

## Goal

Expose the smallest Azure Cosmos DB-compatible surface needed to migrate the ledger resources to `MongoDB-compatible FerretDB or PostgreSQL JSONB` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.DocumentDB/databaseAccounts/read, Microsoft.DocumentDB/databaseAccounts/write, Microsoft.DocumentDB/databaseAccounts/delete.
- Actions explicitly not supported first: Cosmos DB console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `Microsoft.DocumentDB/databaseAccounts/read` and its paired read/list calls.
- Ledger resource types: `azurerm_cosmosdb_account`.
- Provider errors: map Cosmos DB authorization failures to Azure access-denied codes, missing `azurerm_cosmosdb_account` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/cosmos-db` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_cosmosdb_account`.

## Backend

- Backend: MongoDB-compatible FerretDB or PostgreSQL JSONB.
- Storage and metadata: Cosmos DB state lives in `MongoDB-compatible FerretDB or PostgreSQL JSONB`; HomePort stores provider identifiers for `azurerm_cosmosdb_account`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `MongoDB-compatible FerretDB or PostgreSQL JSONB` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/cosmos-db`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.DocumentDB/databaseAccounts/read, Microsoft.DocumentDB/databaseAccounts/write, Microsoft.DocumentDB/databaseAccounts/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.DocumentDB/databaseAccounts/{name}.
- Context: evaluate Cosmos DB calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.DocumentDB/databaseAccounts/{name}`, source IP, request id, user agent, tags/labels on `azurerm_cosmosdb_account`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cosmos DB actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.DocumentDB/databaseAccounts/{name}` prefix checks, tag/label equality on `azurerm_cosmosdb_account`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/cosmos-db` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Cosmos DB provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_cosmosdb_account` records and `MongoDB-compatible FerretDB or PostgreSQL JSONB` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cosmos DB provider ids, `azurerm_cosmosdb_account` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/cosmos-db` backend auth, missing `azurerm_cosmosdb_account`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/cosmos-db/backend.yaml` for `MongoDB-compatible FerretDB or PostgreSQL JSONB` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/cosmos-db/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/cosmos-db/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-cosmos-db.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises DatabaseAccountsGet -> DatabaseAccountsCreateOrUpdate -> DatabaseAccountsList -> DatabaseAccountsDelete against `/compat/azure/cosmos-db` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_cosmosdb_account` from `azure/cosmos-db`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: Mongo API mode can use Mongo-compatible targets; SQL, Gremlin, and Table APIs need adapters or guided migration; `test/conformance/services/azure-cosmos-db.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-cosmos-db.yaml`, then promote only when that manifest passes in CI.
