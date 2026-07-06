# Azure AI Search Compatibility Plan

## Goal

Expose the smallest Azure AI Search-compatible surface needed to migrate the ledger resources to `OpenSearch` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Search/searchServices/read, Microsoft.Search/searchServices/write, Microsoft.Search/searchServices/delete.
- Actions explicitly not supported first: AI Search console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `Microsoft.Search/searchServices/read` and its paired read/list calls.
- Ledger resource types: source AI Search resource model
- First concrete resource model to add: `homeport_azure_ai_search_resource` with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map AI Search authorization failures to Azure access-denied codes, missing `source AI Search resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/ai-search` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `homeport_azure_ai_search_resource`.

## Backend

- Backend: OpenSearch.
- Storage and metadata: AI Search state lives in `OpenSearch`; HomePort stores provider identifiers for `source AI Search resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `OpenSearch` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/ai-search`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Search/searchServices/read, Microsoft.Search/searchServices/write, Microsoft.Search/searchServices/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Search/searchServices/{name}.
- Context: evaluate AI Search calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Search/searchServices/{name}`, source IP, request id, user agent, tags/labels on `source AI Search resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed AI Search actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Search/searchServices/{name}` prefix checks, tag/label equality on `source AI Search resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/ai-search` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: AI Search provider names, locations, tags/labels, and request bodies map to HomePort `source AI Search resource model` records and `OpenSearch` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return AI Search provider ids, `source AI Search resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/ai-search` backend auth, missing `source AI Search resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/ai-search/backend.yaml` for `OpenSearch` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/ai-search/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/ai-search/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-ai-search.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises SearchServicesGet -> SearchServicesCreateOrUpdate -> SearchServicesList -> SearchServicesDelete against `/compat/azure/ai-search` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the new `homeport_azure_ai_search_resource` model from `azure/ai-search`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/azure-ai-search.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-ai-search.yaml`, then promote only when that manifest passes in CI.
