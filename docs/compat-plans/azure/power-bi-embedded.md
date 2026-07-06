# Azure Power BI Embedded Compatibility Plan

## Goal

Expose the smallest Azure Power BI Embedded-compatible surface needed to migrate the ledger resources to `Apache Superset` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Power BI REST: list groups, list datasets, list reports, import PBIX.
- Actions explicitly not supported first: Power BI Embedded console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `Power BI REST: list groups` and its paired read/list calls.
- Ledger resource types: source Power BI Embedded resource model
- First concrete resource model to add: `homeport_azure_power_bi_embedded_resource` with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Power BI Embedded authorization failures to Azure access-denied codes, missing `source Power BI Embedded resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/power-bi-embedded` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `homeport_azure_power_bi_embedded_resource`.

## Backend

- Backend: Apache Superset.
- Storage and metadata: Power BI Embedded state lives in `Apache Superset`; HomePort stores provider identifiers for `source Power BI Embedded resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Apache Superset` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Power BI REST: list groups, list datasets, list reports, import PBIX.
- Resource: https://api.powerbi.com/v1.0/myorg/groups/{workspaceId}.
- Context: evaluate Power BI Embedded calls with tenant/project/account, provider region/location, `https://api.powerbi.com/v1.0/myorg/groups/{workspaceId}`, source IP, request id, user agent, tags/labels on `source Power BI Embedded resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Power BI Embedded actions, `https://api.powerbi.com/v1.0/myorg/groups/{workspaceId}` prefix checks, tag/label equality on `source Power BI Embedded resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/power-bi-embedded` for the actions above.
- SDK used in tests: Power BI REST configured with endpoint override and HomePort credentials.
- Request mapping: Power BI Embedded provider names, locations, tags/labels, and request bodies map to HomePort `source Power BI Embedded resource model` records and `Apache Superset` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Power BI Embedded provider ids, `source Power BI Embedded resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/power-bi-embedded` backend auth, missing `source Power BI Embedded resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/power-bi-embedded/backend.yaml` for `Apache Superset` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/azure/power-bi-embedded/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/power-bi-embedded/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-power-bi-embedded.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Power BI REST exercises GroupsGetGroups -> DatasetsGetDatasets -> ReportsGetReports -> ImportsPostImport against `/compat/azure/power-bi-embedded` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the new `homeport_azure_power_bi_embedded_resource` model from `azure/power-bi-embedded`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/azure-power-bi-embedded.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-power-bi-embedded.yaml`, then promote only when that manifest passes in CI.
