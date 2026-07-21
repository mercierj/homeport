# Azure Azure Functions Compatibility Plan

## Goal

Expose the smallest Azure Azure Functions-compatible surface needed to migrate the ledger resources to `OpenFaaS` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Web/sites/functions/read, Microsoft.Web/sites/functions/write, Microsoft.Web/sites/functions/delete.
- Actions explicitly not supported first: Azure Functions console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Web/sites/functions/read` and its paired read/list calls.
- Ledger resource types: `azurerm_function_app`
- Provider errors: map Azure Functions authorization failures to Azure access-denied codes, missing `azurerm_function_app` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/azure-functions` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_function_app`.

## Backend

- Backend: OpenFaaS or Docker Compose function runtime.
- Storage and metadata: Azure Functions state lives in `OpenFaaS`; HomePort stores provider identifiers for `azurerm_function_app`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `OpenFaaS` with generated `artifacts/compat/azure/azure-functions/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/azure-functions`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Web/sites/functions/read, Microsoft.Web/sites/functions/write, Microsoft.Web/sites/functions/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Web/sites/functions/{name}.
- Context: evaluate Azure Functions calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Web/sites/functions/{name}`, source IP, request id, user agent, tags/labels on `azurerm_function_app`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Azure Functions actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Web/sites/functions/{name}` prefix checks, tag/label equality on `azurerm_function_app`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/azure-functions` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Azure Functions provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_function_app` records and `OpenFaaS` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Azure Functions provider ids, `azurerm_function_app` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/azure-functions` backend auth, missing `azurerm_function_app`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/azure-functions/backend.yaml` for `OpenFaaS` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/azure-functions/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/azure-functions/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-azure-functions.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises FunctionsGet -> FunctionsCreateOrUpdate -> FunctionsList -> FunctionsDelete against `/compat/azure/azure-functions` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_function_app` from `azure/azure-functions`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-azure-functions.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-azure-functions.yaml`, then promote only when that manifest passes in CI.
