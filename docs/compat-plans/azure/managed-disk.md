# Azure Managed Disk Compatibility Plan

## Goal

Expose the smallest Azure Managed Disk-compatible surface needed to migrate the ledger resources to `Longhorn block volumes` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.Compute/disks/read, Microsoft.Compute/disks/write, Microsoft.Compute/disks/delete.
- Actions explicitly not supported first: Managed Disk console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Microsoft.Compute/disks/read` and its paired read/list calls.
- Ledger resource types: `azurerm_managed_disk`
- Provider errors: map Managed Disk authorization failures to Azure access-denied codes, missing `azurerm_managed_disk` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/managed-disk` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `azurerm_managed_disk`.

## Backend

- Backend: Not selected in `docs/coverage/services.yaml`.
- Storage and metadata: Managed Disk state lives in `Longhorn block volumes`; HomePort stores provider identifiers for `azurerm_managed_disk`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Longhorn block volumes` with generated `artifacts/compat/azure/managed-disk/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/managed-disk`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.Compute/disks/read, Microsoft.Compute/disks/write, Microsoft.Compute/disks/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Compute/disks/{name}.
- Context: evaluate Managed Disk calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Compute/disks/{name}`, source IP, request id, user agent, tags/labels on `azurerm_managed_disk`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Managed Disk actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.Compute/disks/{name}` prefix checks, tag/label equality on `azurerm_managed_disk`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/managed-disk` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Managed Disk provider names, locations, tags/labels, and request bodies map to HomePort `azurerm_managed_disk` records and `Longhorn block volumes` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Managed Disk provider ids, `azurerm_managed_disk` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/managed-disk` backend auth, missing `azurerm_managed_disk`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/managed-disk/backend.yaml` for `Longhorn block volumes` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/managed-disk/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/managed-disk/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-managed-disk.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises DisksGet -> DisksCreateOrUpdate -> DisksList -> DisksDelete against `/compat/azure/managed-disk` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `azurerm_managed_disk` from `azure/managed-disk`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/azure-managed-disk.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-managed-disk.yaml`, then promote only when that manifest passes in CI.
