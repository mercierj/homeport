# Azure Container Apps Compatibility Plan

## Goal

Expose the smallest Azure Container Apps-compatible surface needed to migrate the ledger resources to `Knative Serving` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.App/containerApps/read, Microsoft.App/containerApps/write, Microsoft.App/containerApps/delete.
- Actions explicitly not supported first: Container Apps console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `Microsoft.App/containerApps/read` and its paired read/list calls.
- Ledger resource types: source Container Apps resource model
- First concrete resource model to add: `homeport_azure_container_apps_resource` with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Container Apps authorization failures to Azure access-denied codes, missing `source Container Apps resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/container-apps` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `homeport_azure_container_apps_resource`.

## Backend

- Backend: Knative Serving.
- Storage and metadata: Container Apps state lives in `Knative Serving`; HomePort stores provider identifiers for `source Container Apps resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Knative Serving` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `azure/container-apps`.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.App/containerApps/read, Microsoft.App/containerApps/write, Microsoft.App/containerApps/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.App/containerApps/{name}.
- Context: evaluate Container Apps calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.App/containerApps/{name}`, source IP, request id, user agent, tags/labels on `source Container Apps resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Container Apps actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.App/containerApps/{name}` prefix checks, tag/label equality on `source Container Apps resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/container-apps` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Container Apps provider names, locations, tags/labels, and request bodies map to HomePort `source Container Apps resource model` records and `Knative Serving` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Container Apps provider ids, `source Container Apps resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/container-apps` backend auth, missing `source Container Apps resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/container-apps/backend.yaml` for `Knative Serving` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/azure/container-apps/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/container-apps/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-container-apps.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises ContainerAppsGet -> ContainerAppsCreateOrUpdate -> ContainerAppsList -> ContainerAppsDelete against `/compat/azure/container-apps` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the new `homeport_azure_container_apps_resource` model from `azure/container-apps`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/azure-container-apps.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-container-apps.yaml`, then promote only when that manifest passes in CI.
