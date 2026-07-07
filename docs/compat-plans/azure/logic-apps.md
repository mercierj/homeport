# Azure Logic Apps Compatibility Plan

## Goal

Expose the smallest Azure Logic Apps-compatible surface needed to migrate the ledger resources to `Temporal workflows` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Logic/workflows/read, Microsoft.Logic/workflows/write, Microsoft.Logic/workflows/delete.
- Actions explicitly not supported first: Logic Apps console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Logic/workflows/read` and its paired read/list calls.
- Ledger resource types: `azurerm_logic_app_workflow`.
- Provider errors: map Logic Apps authorization failures to Azure access-denied codes, missing `azurerm_logic_app_workflow` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/logic-apps` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_logic_app_workflow`.

## Backend

- Backend: Temporal workflows.
- Storage and metadata: Logic Apps state lives in `Temporal workflows`; HomePort stores provider identifiers for `azurerm_logic_app_workflow`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Temporal workflows` with generated `artifacts/compat/azure/logic-apps/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/logic-apps`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Logic/workflows/read, Microsoft.Logic/workflows/write, Microsoft.Logic/workflows/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Logic/workflows/{name}.
- Context: evaluate Logic Apps calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Logic/workflows/{name}`, source IP, request id, user agent, tags/labels on `azurerm_logic_app_workflow`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Logic Apps actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Logic/workflows/{name}` prefix checks, tag/label equality on `azurerm_logic_app_workflow`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/logic-apps` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Logic Apps provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_logic_app_workflow` records and `Temporal workflows` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Logic Apps provider ids, `azurerm_logic_app_workflow` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/logic-apps` backend auth, missing `azurerm_logic_app_workflow`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/logic-apps/backend.yaml` for `Temporal workflows` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/logic-apps/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/logic-apps/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-logic-apps.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises WorkflowsGet -> WorkflowsCreateOrUpdate -> WorkflowsList -> WorkflowsDelete against `/compat/azure/logic-apps` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_logic_app_workflow` from `azure/logic-apps`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: only workflows representable as generated routing config can be automated; others need review checklist; `test/conformance/services/azure-logic-apps.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-logic-apps.yaml`, then promote only when that manifest passes in CI.
