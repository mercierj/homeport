# Azure Azure SQL Compatibility Plan

## Goal

Expose the smallest Azure Azure SQL-compatible surface needed to migrate the ledger resources to `PostgreSQL with TDS compatibility review` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Sql/servers/databases/read, Microsoft.Sql/servers/databases/write, Microsoft.Sql/servers/databases/delete.
- Actions explicitly not supported first: Azure SQL console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Sql/servers/databases/read` and its paired read/list calls.
- Ledger resource types: `azurerm_mssql_database`
- Provider errors: map Azure SQL authorization failures to Azure access-denied codes, missing `azurerm_mssql_database` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/azure-sql` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_mssql_database`.

## Backend

- Backend: SQL Server container with generated BACPAC handoff.
- Storage and metadata: Azure SQL state lives in `PostgreSQL with TDS compatibility review`; HomePort stores provider identifiers for `azurerm_mssql_database`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `PostgreSQL with TDS compatibility review` with generated `artifacts/compat/azure/azure-sql/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/azure-sql`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Sql/servers/databases/read, Microsoft.Sql/servers/databases/write, Microsoft.Sql/servers/databases/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Sql/servers/databases/{name}.
- Context: evaluate Azure SQL calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Sql/servers/databases/{name}`, source IP, request id, user agent, tags/labels on `azurerm_mssql_database`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Azure SQL actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Sql/servers/databases/{name}` prefix checks, tag/label equality on `azurerm_mssql_database`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/azure-sql` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Azure SQL provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_mssql_database` records and `PostgreSQL with TDS compatibility review` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Azure SQL provider ids, `azurerm_mssql_database` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/azure-sql` backend auth, missing `azurerm_mssql_database`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/azure-sql/backend.yaml` for `PostgreSQL with TDS compatibility review` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/azure-sql/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/azure-sql/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-azure-sql.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises DatabasesGet -> DatabasesCreateOrUpdate -> DatabasesList -> DatabasesDelete against `/compat/azure/azure-sql` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_mssql_database` from `azure/azure-sql`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-azure-sql.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-azure-sql.yaml`, then promote only when that manifest passes in CI.
