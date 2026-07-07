# Azure Service Bus Compatibility Plan

## Goal

Expose the smallest Azure Service Bus-compatible surface needed to migrate the ledger resources to `RabbitMQ with AMQP compatibility` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.ServiceBus/namespaces/queues/read, Microsoft.ServiceBus/namespaces/queues/write, Microsoft.ServiceBus/namespaces/queues/delete.
- Actions explicitly not supported first: Service Bus console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.ServiceBus/namespaces/queues/read` and its paired read/list calls.
- Ledger resource types: `azurerm_servicebus_namespace`, `azurerm_servicebus_queue`.
- Provider errors: map Service Bus authorization failures to Azure access-denied codes, missing `azurerm_servicebus_namespace` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/service-bus` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_servicebus_namespace`.

## Backend

- Backend: RabbitMQ with AMQP compatibility.
- Storage and metadata: Service Bus state lives in `RabbitMQ with AMQP compatibility`; HomePort stores provider identifiers for `azurerm_servicebus_namespace`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `RabbitMQ with AMQP compatibility` with generated `artifacts/compat/azure/service-bus/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/service-bus`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.ServiceBus/namespaces/queues/read, Microsoft.ServiceBus/namespaces/queues/write, Microsoft.ServiceBus/namespaces/queues/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.ServiceBus/namespaces/queues/{name}.
- Context: evaluate Service Bus calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.ServiceBus/namespaces/queues/{name}`, source IP, request id, user agent, tags/labels on `azurerm_servicebus_namespace`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Service Bus actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.ServiceBus/namespaces/queues/{name}` prefix checks, tag/label equality on `azurerm_servicebus_namespace`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/service-bus` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Service Bus provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_servicebus_namespace` records and `RabbitMQ with AMQP compatibility` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Service Bus provider ids, `azurerm_servicebus_namespace` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/service-bus` backend auth, missing `azurerm_servicebus_namespace`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/service-bus/backend.yaml` for `RabbitMQ with AMQP compatibility` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/service-bus/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/service-bus/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-service-bus.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises QueuesGet -> QueuesCreateOrUpdate -> QueuesList -> QueuesDelete against `/compat/azure/service-bus` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_servicebus_namespace`, `azurerm_servicebus_queue` from `azure/service-bus`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: no Azure Service Bus-compatible local API adapter exists yet; `test/conformance/services/azure-service-bus.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-service-bus.yaml`, then promote only when that manifest passes in CI.
