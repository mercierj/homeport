# Azure Container Instances Compatibility Plan

## Goal

Expose the smallest Azure Container Instances-compatible surface needed to migrate the ledger resources to `Kubernetes Jobs and Pods` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.ContainerInstance/containerGroups/read, Microsoft.ContainerInstance/containerGroups/write, Microsoft.ContainerInstance/containerGroups/delete.
- Actions explicitly not supported first: Container Instances console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.ContainerInstance/containerGroups/read` and its paired read/list calls.
- Ledger resource types: `azurerm_container_group`.
- Provider errors: map Container Instances authorization failures to Azure access-denied codes, missing `azurerm_container_group` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/container-instances` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_container_group`.

## Backend

- Backend: Kubernetes Jobs and Pods.
- Storage and metadata: Container Instances state lives in `Kubernetes Jobs and Pods`; HomePort stores provider identifiers for `azurerm_container_group`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Kubernetes Jobs and Pods` with generated `artifacts/compat/azure/container-instances/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/container-instances`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.ContainerInstance/containerGroups/read, Microsoft.ContainerInstance/containerGroups/write, Microsoft.ContainerInstance/containerGroups/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.ContainerInstance/containerGroups/{name}.
- Context: evaluate Container Instances calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.ContainerInstance/containerGroups/{name}`, source IP, request id, user agent, tags/labels on `azurerm_container_group`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Container Instances actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.ContainerInstance/containerGroups/{name}` prefix checks, tag/label equality on `azurerm_container_group`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/container-instances` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Container Instances provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_container_group` records and `Kubernetes Jobs and Pods` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Container Instances provider ids, `azurerm_container_group` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/container-instances` backend auth, missing `azurerm_container_group`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/container-instances/backend.yaml` for `Kubernetes Jobs and Pods` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/container-instances/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/container-instances/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-container-instances.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises ContainerGroupsGet -> ContainerGroupsCreateOrUpdate -> ContainerGroupsList -> ContainerGroupsDelete against `/compat/azure/container-instances` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_container_group` from `azure/container-instances`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-container-instances.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-container-instances.yaml`, then promote only when that manifest passes in CI.
