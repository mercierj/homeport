# Azure Event Hubs Compatibility Plan

## Goal

Expose the smallest Azure Event Hubs-compatible surface needed to migrate the ledger resources to `Redpanda Kafka-compatible cluster` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.EventHub/namespaces/eventhubs/read, Microsoft.EventHub/namespaces/eventhubs/write, Microsoft.EventHub/namespaces/eventhubs/delete.
- Actions explicitly not supported first: Event Hubs console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.EventHub/namespaces/eventhubs/read` and its paired read/list calls.
- Ledger resource types: `azurerm_eventhub`
- Provider errors: map Event Hubs authorization failures to Azure access-denied codes, missing `azurerm_eventhub` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/event-hubs` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_eventhub`.

## Backend

- Backend: Redpanda Kafka-compatible topic.
- Storage and metadata: Event Hubs state lives in `Redpanda Kafka-compatible cluster`; HomePort stores provider identifiers for `azurerm_eventhub`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Redpanda Kafka-compatible cluster` with generated `artifacts/compat/azure/event-hubs/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/event-hubs`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.EventHub/namespaces/eventhubs/read, Microsoft.EventHub/namespaces/eventhubs/write, Microsoft.EventHub/namespaces/eventhubs/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.EventHub/namespaces/eventhubs/{name}.
- Context: evaluate Event Hubs calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.EventHub/namespaces/eventhubs/{name}`, source IP, request id, user agent, tags/labels on `azurerm_eventhub`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Event Hubs actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.EventHub/namespaces/eventhubs/{name}` prefix checks, tag/label equality on `azurerm_eventhub`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/event-hubs` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Event Hubs provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_eventhub` records and `Redpanda Kafka-compatible cluster` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Event Hubs provider ids, `azurerm_eventhub` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/event-hubs` backend auth, missing `azurerm_eventhub`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/event-hubs/backend.yaml` for `Redpanda Kafka-compatible cluster` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/event-hubs/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/event-hubs/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-event-hubs.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises EventhubsGet -> EventhubsCreateOrUpdate -> EventhubsList -> EventhubsDelete against `/compat/azure/event-hubs` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_eventhub` from `azure/event-hubs`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: Kafka-compatible path needs explicit consumer-group, offset, retention, and replay validation; `test/conformance/services/azure-event-hubs.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-event-hubs.yaml`, then promote only when that manifest passes in CI.
