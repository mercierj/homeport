# Azure Foundry/OpenAI Compatibility Plan

## Goal

Expose the smallest Azure Foundry/OpenAI-compatible surface needed to migrate the ledger resources to `Ollama with OpenAI-compatible gateway` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.CognitiveServices/accounts/read, Microsoft.CognitiveServices/accounts/write, Microsoft.CognitiveServices/accounts/delete.
- Actions explicitly not supported first: Foundry/OpenAI console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.CognitiveServices/accounts/read` and its paired read/list calls.
- Ledger resource types: `azurerm_cognitive_account`
- First concrete resource model to add: service-specific model with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Foundry/OpenAI authorization failures to Azure access-denied codes, missing `planned resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/foundry-openai` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on planned resource model.

## Backend

- Backend: vLLM OpenAI-compatible API.
- Storage and metadata: Foundry/OpenAI state lives in `Ollama with OpenAI-compatible gateway`; HomePort stores provider identifiers for `planned resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Ollama with OpenAI-compatible gateway` with generated `artifacts/compat/azure/foundry-openai/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/foundry-openai`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.CognitiveServices/accounts/read, Microsoft.CognitiveServices/accounts/write, Microsoft.CognitiveServices/accounts/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.CognitiveServices/accounts/{name}.
- Context: evaluate Foundry/OpenAI calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.CognitiveServices/accounts/{name}`, source IP, request id, user agent, tags/labels on `planned resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Foundry/OpenAI actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.CognitiveServices/accounts/{name}` prefix checks, tag/label equality on `planned resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/foundry-openai` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Foundry/OpenAI provider names, locations, tags/labels, and request bodies map to HomePort `planned resource model` records and `Ollama with OpenAI-compatible gateway` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Foundry/OpenAI provider ids, `planned resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/foundry-openai` backend auth, missing `planned resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/foundry-openai/backend.yaml` for `Ollama with OpenAI-compatible gateway` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/foundry-openai/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/foundry-openai/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-foundry-openai.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises AccountsGet -> AccountsCreateOrUpdate -> AccountsList -> AccountsDelete against `/compat/azure/foundry-openai` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the planned resource model from `azure/foundry-openai`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/azure-foundry-openai.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-foundry-openai.yaml`, then promote only when that manifest passes in CI.
