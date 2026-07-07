# Azure Event Grid Compatibility Plan

## Goal

Expose the smallest Azure Event Grid-compatible surface needed to migrate the ledger resources to `NATS JetStream event bus` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.EventGrid/topics/read, Microsoft.EventGrid/topics/write, Microsoft.EventGrid/topics/delete.
- Actions explicitly not supported first: Event Grid console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.EventGrid/topics/read` and its paired read/list calls.
- Ledger resource types: `azurerm_eventgrid_topic`.
- Provider errors: map Event Grid authorization failures to Azure access-denied codes, missing `azurerm_eventgrid_topic` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/event-grid` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_eventgrid_topic`.

## Backend

- Backend: NATS JetStream event bus.
- Storage and metadata: Event Grid state lives in `NATS JetStream event bus`; HomePort stores provider identifiers for `azurerm_eventgrid_topic`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `NATS JetStream event bus` with generated `artifacts/compat/azure/event-grid/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/event-grid`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.EventGrid/topics/read, Microsoft.EventGrid/topics/write, Microsoft.EventGrid/topics/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.EventGrid/topics/{name}.
- Context: evaluate Event Grid calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.EventGrid/topics/{name}`, source IP, request id, user agent, tags/labels on `azurerm_eventgrid_topic`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Event Grid actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.EventGrid/topics/{name}` prefix checks, tag/label equality on `azurerm_eventgrid_topic`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/event-grid` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Event Grid provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_eventgrid_topic` records and `NATS JetStream event bus` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Event Grid provider ids, `azurerm_eventgrid_topic` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/event-grid` backend auth, missing `azurerm_eventgrid_topic`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/event-grid/backend.yaml` for `NATS JetStream event bus` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/event-grid/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/event-grid/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-event-grid.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises TopicsGet -> TopicsCreateOrUpdate -> TopicsList -> TopicsDelete against `/compat/azure/event-grid` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_eventgrid_topic` from `azure/event-grid`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: Event Grid mapping exists, but executable routing delivery needs generated targets and end-to-end validation; `test/conformance/services/azure-event-grid.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-event-grid.yaml`, then promote only when that manifest passes in CI.
