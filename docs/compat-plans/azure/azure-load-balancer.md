# Azure Azure Load Balancer Compatibility Plan

## Goal

Expose the smallest Azure Azure Load Balancer-compatible surface needed to migrate the ledger resources to `HAProxy or MetalLB` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Network/loadBalancers/read, Microsoft.Network/loadBalancers/write, Microsoft.Network/loadBalancers/delete.
- Actions explicitly not supported first: Azure Load Balancer console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Network/loadBalancers/read` and its paired read/list calls.
- Ledger resource types: `azurerm_lb`
- Provider errors: map Azure Load Balancer authorization failures to Azure access-denied codes, missing `azurerm_lb` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/azure-load-balancer` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_lb`.

## Backend

- Backend: HAProxy or MetalLB.
- Storage and metadata: Azure Load Balancer state lives in `HAProxy or MetalLB`; HomePort stores provider identifiers for `azurerm_lb`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `HAProxy or MetalLB` with generated `artifacts/compat/azure/azure-load-balancer/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/azure-load-balancer`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Network/loadBalancers/read, Microsoft.Network/loadBalancers/write, Microsoft.Network/loadBalancers/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/loadBalancers/{name}.
- Context: evaluate Azure Load Balancer calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/loadBalancers/{name}`, source IP, request id, user agent, tags/labels on `azurerm_lb`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Azure Load Balancer actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Network/loadBalancers/{name}` prefix checks, tag/label equality on `azurerm_lb`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/azure-load-balancer` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Azure Load Balancer provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_lb` records and `HAProxy or MetalLB` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Azure Load Balancer provider ids, `azurerm_lb` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/azure-load-balancer` backend auth, missing `azurerm_lb`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/azure-load-balancer/backend.yaml` for `HAProxy or MetalLB` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/azure-load-balancer/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/azure-load-balancer/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-azure-load-balancer.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises LoadBalancersGet -> LoadBalancersCreateOrUpdate -> LoadBalancersList -> LoadBalancersDelete against `/compat/azure/azure-load-balancer` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_lb` from `azure/azure-load-balancer`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-azure-load-balancer.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-azure-load-balancer.yaml`, then promote only when that manifest passes in CI.
