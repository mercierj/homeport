# Azure DevOps Pipelines Compatibility Plan

## Goal

Expose the smallest Azure DevOps Pipelines-compatible surface needed to migrate the ledger resources to `GitLab CI or Tekton` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Azure DevOps Pipelines REST: list pipelines, get pipeline, run pipeline, get run.
- Actions explicitly not supported first: DevOps Pipelines console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `Azure DevOps Pipelines REST: list pipelines` and its paired read/list calls.
- Ledger resource types: no resource type currently modeled in the ledger.
- First concrete resource model to add: service-specific model with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map DevOps Pipelines authorization failures to Azure access-denied codes, missing `planned resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/devops-pipelines` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on planned resource model.

## Backend

- Backend: GitLab CI or Tekton.
- Storage and metadata: DevOps Pipelines state lives in `GitLab CI or Tekton`; HomePort stores provider identifiers for `planned resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `GitLab CI or Tekton` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Azure DevOps Pipelines REST: list pipelines, get pipeline, run pipeline, get run.
- Resource: https://dev.azure.com/{organization}/{project}/_apis/pipelines/{pipelineId}.
- Context: evaluate DevOps Pipelines calls with tenant/project/account, provider region/location, `https://dev.azure.com/{organization}/{project}/_apis/pipelines/{pipelineId}`, source IP, request id, user agent, tags/labels on `planned resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed DevOps Pipelines actions, `https://dev.azure.com/{organization}/{project}/_apis/pipelines/{pipelineId}` prefix checks, tag/label equality on `planned resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/devops-pipelines` for the actions above.
- SDK used in tests: Azure DevOps Pipelines REST configured with endpoint override and HomePort credentials.
- Request mapping: DevOps Pipelines provider names, locations, tags/labels, and request bodies map to HomePort `planned resource model` records and `GitLab CI or Tekton` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return DevOps Pipelines provider ids, `planned resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/devops-pipelines` backend auth, missing `planned resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/devops-pipelines/backend.yaml` for `GitLab CI or Tekton` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/azure/devops-pipelines/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/devops-pipelines/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-devops-pipelines.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure DevOps Pipelines REST exercises PipelinesGet -> PipelinesList -> RunsRunPipeline -> RunsGet against `/compat/azure/devops-pipelines` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the planned resource model from `azure/devops-pipelines`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/azure-devops-pipelines.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-devops-pipelines.yaml`, then promote only when that manifest passes in CI.
