# AWS CodeBuild Compatibility Plan

## Goal

Expose the smallest AWS CodeBuild-compatible surface needed to migrate the ledger resources to `GitLab Runner and GitLab CI` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: codebuild:CreateProject, codebuild:BatchGetProjects, codebuild:ListProjects, codebuild:UpdateProject, codebuild:DeleteProject.
- Actions explicitly not supported first: CodeBuild console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `codebuild:CreateProject` and its paired read/list calls.
- Ledger resource types: `aws_codebuild_project`.
- Provider errors: map CodeBuild authorization failures to AWS access-denied codes, missing `aws_codebuild_project` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/codebuild` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_codebuild_project`.

## Backend

- Backend: GitLab Runner and GitLab CI.
- Storage and metadata: CodeBuild state lives in `GitLab Runner and GitLab CI`; HomePort stores provider identifiers for `aws_codebuild_project`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `GitLab Runner and GitLab CI` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: codebuild:CreateProject, codebuild:BatchGetProjects, codebuild:ListProjects, codebuild:UpdateProject, codebuild:DeleteProject.
- Resource: arn:aws:codebuild:{region}:{account}:codebuild/{id}.
- Context: evaluate CodeBuild calls with tenant/project/account, provider region/location, `arn:aws:codebuild:{region}:{account}:codebuild/{id}`, source IP, request id, user agent, tags/labels on `aws_codebuild_project`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed CodeBuild actions, `arn:aws:codebuild:{region}:{account}:codebuild/{id}` prefix checks, tag/label equality on `aws_codebuild_project`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/codebuild` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: CodeBuild provider names, locations, tags/labels, and request bodies map to HomePort `aws_codebuild_project` records and `GitLab Runner and GitLab CI` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return CodeBuild provider ids, `aws_codebuild_project` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/codebuild` backend auth, missing `aws_codebuild_project`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/codebuild/backend.yaml` for `GitLab Runner and GitLab CI` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/codebuild/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/codebuild/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-codebuild.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateProject -> BatchGetProjects -> ListProjects -> UpdateProject -> DeleteProject against `/compat/aws/codebuild` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_codebuild_project` from `aws/codebuild`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-codebuild.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-codebuild.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-codebuild.yaml`, then promote only when that manifest passes in CI.
