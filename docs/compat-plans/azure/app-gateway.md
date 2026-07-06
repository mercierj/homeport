# Azure App Gateway Compatibility Plan

## Goal

Expose the smallest Azure App Gateway-compatible surface needed to migrate the ledger resources to `Traefik ingress gateway` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Network/applicationGateways/read, Microsoft.Network/applicationGateways/write, Microsoft.Network/applicationGateways/delete.
- Actions explicitly not supported first: App Gateway console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `Microsoft.Network/applicationGateways/read` and its paired read/list calls.
- Ledger resource types: `azurerm_application_gateway`.
- Provider errors: map App Gateway authorization failures to Azure access-denied codes, missing `azurerm_application_gateway` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/app-gateway` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_application_gateway`.

## Backend

- Backend: Traefik ingress gateway.
- Storage and metadata: App Gateway state lives in `Traefik ingress gateway`; HomePort stores provider identifiers for `azurerm_application_gateway`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Traefik ingress gateway` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/app-gateway`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Network/applicationGateways/read, Microsoft.Network/applicationGateways/write, Microsoft.Network/applicationGateways/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/applicationGateways/{name}.
- Context: evaluate App Gateway calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/applicationGateways/{name}`, source IP, request id, user agent, tags/labels on `azurerm_application_gateway`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed App Gateway actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/applicationGateways/{name}` prefix checks, tag/label equality on `azurerm_application_gateway`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/app-gateway` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: App Gateway provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_application_gateway` records and `Traefik ingress gateway` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return App Gateway provider ids, `azurerm_application_gateway` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/app-gateway` backend auth, missing `azurerm_application_gateway`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/app-gateway/backend.yaml` for `Traefik ingress gateway` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/app-gateway/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/app-gateway/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-app-gateway.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises ApplicationGatewaysGet -> ApplicationGatewaysCreateOrUpdate -> ApplicationGatewaysList -> ApplicationGatewaysDelete against `/compat/azure/app-gateway` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_application_gateway` from `azure/app-gateway`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-app-gateway.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-app-gateway.yaml`, then promote only when that manifest passes in CI.
