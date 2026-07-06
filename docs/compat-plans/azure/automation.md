# Azure Automation Compatibility Plan

## Goal

Expose the smallest Azure Automation-compatible surface needed to migrate the ledger resources to `Rundeck` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Automation/automationAccounts/read, Microsoft.Automation/automationAccounts/write, Microsoft.Automation/automationAccounts/delete.
- Actions explicitly not supported first: Automation console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `Microsoft.Automation/automationAccounts/read` and its paired read/list calls.
- Ledger resource types: source Automation resource model
- First concrete resource model to add: `homeport_azure_automation_resource` with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Automation authorization failures to Azure access-denied codes, missing `source Automation resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/automation` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `homeport_azure_automation_resource`.

## Backend

- Backend: Rundeck.
- Storage and metadata: Automation state lives in `Rundeck`; HomePort stores provider identifiers for `source Automation resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Rundeck` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Automation/automationAccounts/read, Microsoft.Automation/automationAccounts/write, Microsoft.Automation/automationAccounts/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Automation/automationAccounts/{name}.
- Context: evaluate Automation calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Automation/automationAccounts/{name}`, source IP, request id, user agent, tags/labels on `source Automation resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Automation actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Automation/automationAccounts/{name}` prefix checks, tag/label equality on `source Automation resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/automation` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Automation provider names, locations, tags/labels, and request bodies map to HomePort `source Automation resource model` records and `Rundeck` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Automation provider ids, `source Automation resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/automation` backend auth, missing `source Automation resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/automation/backend.yaml` for `Rundeck` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/azure/automation/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/automation/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-automation.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises AutomationAccountsGet -> AutomationAccountsCreateOrUpdate -> AutomationAccountsList -> AutomationAccountsDelete against `/compat/azure/automation` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the new `homeport_azure_automation_resource` model from `azure/automation`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/azure-automation.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-automation.yaml`, then promote only when that manifest passes in CI.
