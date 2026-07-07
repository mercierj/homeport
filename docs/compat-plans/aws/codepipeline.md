# AWS CodePipeline Compatibility Plan

## Goal

Expose the smallest AWS CodePipeline-compatible surface needed to migrate the ledger resources to `GitLab Runner and GitLab CI` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: codepipeline:CreatePipeline, codepipeline:GetPipeline, codepipeline:ListPipelines, codepipeline:UpdatePipeline, codepipeline:DeletePipeline.
- Actions explicitly not supported first: CodePipeline console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `codepipeline:CreatePipeline` and its paired read/list calls.
- Ledger resource types: `aws_codepipeline`.
- Provider errors: map CodePipeline authorization failures to AWS access-denied codes, missing `aws_codepipeline` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/codepipeline` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_codepipeline`.

## Backend

- Backend: GitLab Runner and GitLab CI.
- Storage and metadata: CodePipeline state lives in `GitLab Runner and GitLab CI`; HomePort stores provider identifiers for `aws_codepipeline`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `GitLab Runner and GitLab CI` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: codepipeline:CreatePipeline, codepipeline:GetPipeline, codepipeline:ListPipelines, codepipeline:UpdatePipeline, codepipeline:DeletePipeline.
- Resource: arn:aws:codepipeline:{region}:{account}:codepipeline/{id}.
- Context: evaluate CodePipeline calls with tenant/project/account, provider region/location, `arn:aws:codepipeline:{region}:{account}:codepipeline/{id}`, source IP, request id, user agent, tags/labels on `aws_codepipeline`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed CodePipeline actions, `arn:aws:codepipeline:{region}:{account}:codepipeline/{id}` prefix checks, tag/label equality on `aws_codepipeline`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/codepipeline` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: CodePipeline provider names, locations, tags/labels, and request bodies map to HomePort `aws_codepipeline` records and `GitLab Runner and GitLab CI` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return CodePipeline provider ids, `aws_codepipeline` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/codepipeline` backend auth, missing `aws_codepipeline`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/codepipeline/backend.yaml` for `GitLab Runner and GitLab CI` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/codepipeline/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/codepipeline/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-codepipeline.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreatePipeline -> GetPipeline -> ListPipelines -> UpdatePipeline -> DeletePipeline against `/compat/aws/codepipeline` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_codepipeline` from `aws/codepipeline`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-codepipeline.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-codepipeline.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-codepipeline.yaml`, then promote only when that manifest passes in CI.
