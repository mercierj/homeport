# Azure PostgreSQL Compatibility Plan

## Goal

Expose the smallest Azure PostgreSQL-compatible surface needed to migrate the ledger resources to `PostgreSQL` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.DBforPostgreSQL/flexibleServers/read, Microsoft.DBforPostgreSQL/flexibleServers/write, Microsoft.DBforPostgreSQL/flexibleServers/delete.
- Actions explicitly not supported first: PostgreSQL console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.DBforPostgreSQL/flexibleServers/read` and its paired read/list calls.
- Ledger resource types: `azurerm_postgresql_flexible_server`.
- Provider errors: map PostgreSQL authorization failures to Azure access-denied codes, missing `azurerm_postgresql_flexible_server` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/postgresql` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_postgresql_flexible_server`.

## Backend

- Backend: PostgreSQL.
- Storage and metadata: PostgreSQL state lives in `PostgreSQL`; HomePort stores provider identifiers for `azurerm_postgresql_flexible_server`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `PostgreSQL` with generated `artifacts/compat/azure/postgresql/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/postgresql`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.DBforPostgreSQL/flexibleServers/read, Microsoft.DBforPostgreSQL/flexibleServers/write, Microsoft.DBforPostgreSQL/flexibleServers/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.DBforPostgreSQL/flexibleServers/{name}.
- Context: evaluate PostgreSQL calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.DBforPostgreSQL/flexibleServers/{name}`, source IP, request id, user agent, tags/labels on `azurerm_postgresql_flexible_server`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed PostgreSQL actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.DBforPostgreSQL/flexibleServers/{name}` prefix checks, tag/label equality on `azurerm_postgresql_flexible_server`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/postgresql` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: PostgreSQL provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_postgresql_flexible_server` records and `PostgreSQL` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return PostgreSQL provider ids, `azurerm_postgresql_flexible_server` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/postgresql` backend auth, missing `azurerm_postgresql_flexible_server`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/postgresql/backend.yaml` for `PostgreSQL` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/postgresql/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/postgresql/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-postgresql.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises FlexibleServersGet -> FlexibleServersCreateOrUpdate -> FlexibleServersList -> FlexibleServersDelete against `/compat/azure/postgresql` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_postgresql_flexible_server` from `azure/postgresql`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-postgresql.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-postgresql.yaml`, then promote only when that manifest passes in CI.
