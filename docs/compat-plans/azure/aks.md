# Azure AKS Compatibility Plan

## Goal

Expose the smallest Azure AKS-compatible surface needed to migrate the ledger resources to `K3s or upstream Kubernetes` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.ContainerService/managedClusters/read, Microsoft.ContainerService/managedClusters/write, Microsoft.ContainerService/managedClusters/delete.
- Actions explicitly not supported first: AKS console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.ContainerService/managedClusters/read` and its paired read/list calls.
- Ledger resource types: `azurerm_kubernetes_cluster`.
- Provider errors: map AKS authorization failures to Azure access-denied codes, missing `azurerm_kubernetes_cluster` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/aks` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_kubernetes_cluster`.

## Backend

- Backend: K3s or upstream Kubernetes.
- Storage and metadata: AKS state lives in `K3s or upstream Kubernetes`; HomePort stores provider identifiers for `azurerm_kubernetes_cluster`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `K3s or upstream Kubernetes` with generated `artifacts/compat/azure/aks/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/aks`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.ContainerService/managedClusters/read, Microsoft.ContainerService/managedClusters/write, Microsoft.ContainerService/managedClusters/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.ContainerService/managedClusters/{name}.
- Context: evaluate AKS calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.ContainerService/managedClusters/{name}`, source IP, request id, user agent, tags/labels on `azurerm_kubernetes_cluster`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed AKS actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.ContainerService/managedClusters/{name}` prefix checks, tag/label equality on `azurerm_kubernetes_cluster`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/aks` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: AKS provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_kubernetes_cluster` records and `K3s or upstream Kubernetes` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return AKS provider ids, `azurerm_kubernetes_cluster` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/aks` backend auth, missing `azurerm_kubernetes_cluster`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/aks/backend.yaml` for `K3s or upstream Kubernetes` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/aks/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/aks/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-aks.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises ManagedClustersGet -> ManagedClustersCreateOrUpdate -> ManagedClustersList -> ManagedClustersDelete against `/compat/azure/aks` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_kubernetes_cluster` from `azure/aks`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-aks.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-aks.yaml`, then promote only when that manifest passes in CI.
