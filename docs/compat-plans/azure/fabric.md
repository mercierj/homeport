# Azure Fabric Compatibility Plan

## Goal

Expose the smallest Azure Fabric-compatible surface needed to migrate the ledger resources to `Apache Superset and Trino` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Fabric REST: list workspaces, list lakehouses, import notebook, validate pipeline run.
- Actions explicitly not supported first: Fabric console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `Fabric REST: list workspaces` and its paired read/list calls.
- Ledger resource types: source Fabric resource model
- First concrete resource model to add: `homeport_azure_fabric_resource` with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Fabric authorization failures to Azure access-denied codes, missing `source Fabric resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/fabric` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `homeport_azure_fabric_resource`.

## Backend

- Backend: Apache Superset and Trino.
- Storage and metadata: Fabric state lives in `Apache Superset and Trino`; HomePort stores provider identifiers for `source Fabric resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Apache Superset and Trino` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Fabric REST: list workspaces, list lakehouses, import notebook, validate pipeline run.
- Resource: https://api.fabric.microsoft.com/v1/workspaces/{workspaceId}.
- Context: evaluate Fabric calls with tenant/project/account, provider region/location, `https://api.fabric.microsoft.com/v1/workspaces/{workspaceId}`, source IP, request id, user agent, tags/labels on `source Fabric resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Fabric actions, `https://api.fabric.microsoft.com/v1/workspaces/{workspaceId}` prefix checks, tag/label equality on `source Fabric resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/fabric` for the actions above.
- SDK used in tests: Fabric REST configured with endpoint override and HomePort credentials.
- Request mapping: Fabric provider names, locations, tags/labels, and request bodies map to HomePort `source Fabric resource model` records and `Apache Superset and Trino` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Fabric provider ids, `source Fabric resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/fabric` backend auth, missing `source Fabric resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/fabric/backend.yaml` for `Apache Superset and Trino` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/azure/fabric/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/fabric/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-fabric.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Fabric REST exercises WorkspacesList -> LakehousesList -> ItemsCreateNotebook -> PipelineRunsGet against `/compat/azure/fabric` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the new `homeport_azure_fabric_resource` model from `azure/fabric`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/azure-fabric.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-fabric.yaml`, then promote only when that manifest passes in CI.
