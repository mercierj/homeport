# Azure Azure VM Compatibility Plan

## Goal

Expose the smallest Azure Azure VM-compatible surface needed to migrate the ledger resources to `Incus or libvirt virtual machines` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Compute/virtualMachines/read, Microsoft.Compute/virtualMachines/write, Microsoft.Compute/virtualMachines/delete.
- Actions explicitly not supported first: Azure VM console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Compute/virtualMachines/read` and its paired read/list calls.
- Ledger resource types: `azurerm_linux_virtual_machine`, `azurerm_windows_virtual_machine`
- Provider errors: map Azure VM authorization failures to Azure access-denied codes, missing `azurerm_linux_virtual_machine` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/azure-vm` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_linux_virtual_machine`.

## Backend

- Backend: Docker Compose container runtime.
- Storage and metadata: Azure VM state lives in `Incus or libvirt virtual machines`; HomePort stores provider identifiers for `azurerm_linux_virtual_machine`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Incus or libvirt virtual machines` with generated `artifacts/compat/azure/azure-vm/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/azure-vm`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Compute/virtualMachines/read, Microsoft.Compute/virtualMachines/write, Microsoft.Compute/virtualMachines/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Compute/virtualMachines/{name}.
- Context: evaluate Azure VM calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Compute/virtualMachines/{name}`, source IP, request id, user agent, tags/labels on `azurerm_linux_virtual_machine`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Azure VM actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Compute/virtualMachines/{name}` prefix checks, tag/label equality on `azurerm_linux_virtual_machine`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/azure-vm` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Azure VM provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_linux_virtual_machine` records and `Incus or libvirt virtual machines` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Azure VM provider ids, `azurerm_linux_virtual_machine` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/azure-vm` backend auth, missing `azurerm_linux_virtual_machine`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/azure-vm/backend.yaml` for `Incus or libvirt virtual machines` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/azure-vm/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/azure-vm/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-azure-vm.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises VirtualMachinesGet -> VirtualMachinesCreateOrUpdate -> VirtualMachinesList -> VirtualMachinesDelete against `/compat/azure/azure-vm` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_linux_virtual_machine`, `azurerm_windows_virtual_machine` from `azure/azure-vm`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-azure-vm.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-azure-vm.yaml`, then promote only when that manifest passes in CI.
