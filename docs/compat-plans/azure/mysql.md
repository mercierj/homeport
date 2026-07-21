# Azure MySQL Compatibility Plan

## Goal

Expose the smallest Azure MySQL-compatible surface needed to migrate the ledger resources to `MySQL or MariaDB` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.DBforMySQL/flexibleServers/read, Microsoft.DBforMySQL/flexibleServers/write, Microsoft.DBforMySQL/flexibleServers/delete.
- Actions explicitly not supported first: MySQL console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.DBforMySQL/flexibleServers/read` and its paired read/list calls.
- Ledger resource types: `azurerm_mysql_flexible_server`
- Provider errors: map MySQL authorization failures to Azure access-denied codes, missing `azurerm_mysql_flexible_server` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/mysql` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_mysql_flexible_server`.

## Backend

- Backend: Not selected in `docs/coverage/services.yaml`.
- Storage and metadata: MySQL state lives in `MySQL or MariaDB`; HomePort stores provider identifiers for `azurerm_mysql_flexible_server`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `MySQL or MariaDB` with generated `artifacts/compat/azure/mysql/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/mysql`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.DBforMySQL/flexibleServers/read, Microsoft.DBforMySQL/flexibleServers/write, Microsoft.DBforMySQL/flexibleServers/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.DBforMySQL/flexibleServers/{name}.
- Context: evaluate MySQL calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.DBforMySQL/flexibleServers/{name}`, source IP, request id, user agent, tags/labels on `azurerm_mysql_flexible_server`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed MySQL actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.DBforMySQL/flexibleServers/{name}` prefix checks, tag/label equality on `azurerm_mysql_flexible_server`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/mysql` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: MySQL provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_mysql_flexible_server` records and `MySQL or MariaDB` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return MySQL provider ids, `azurerm_mysql_flexible_server` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/mysql` backend auth, missing `azurerm_mysql_flexible_server`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/mysql/backend.yaml` for `MySQL or MariaDB` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/mysql/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/mysql/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-mysql.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises FlexibleServersGet -> FlexibleServersCreateOrUpdate -> FlexibleServersList -> FlexibleServersDelete against `/compat/azure/mysql` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_mysql_flexible_server` from `azure/mysql`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-mysql.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-mysql.yaml`, then promote only when that manifest passes in CI.
