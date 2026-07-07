# AWS Lambda Compatibility Plan

## Goal

Expose the smallest AWS Lambda-compatible surface needed to migrate the ledger resources to `OpenFaaS-compatible containers with the HomePort Lambda adapter` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: lambda:CreateFunction, lambda:GetFunction, lambda:Invoke, lambda:UpdateFunctionCode, lambda:DeleteFunction.
- Actions explicitly not supported first: Lambda console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `lambda:CreateFunction` and its paired read/list calls.
- Ledger resource types: `aws_lambda_function`.
- Provider errors: map Lambda authorization failures to AWS access-denied codes, missing `aws_lambda_function` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/lambda` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_lambda_function`.

## Backend

- Backend: OpenFaaS-compatible container with HomePort Lambda adapter.
- Storage and metadata: Lambda state lives in `OpenFaaS-compatible containers with the HomePort Lambda adapter`; HomePort stores provider identifiers for `aws_lambda_function`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision OpenFaaS-compatible containers and the HomePort Lambda adapter with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: lambda:CreateFunction, lambda:GetFunction, lambda:Invoke, lambda:UpdateFunctionCode, lambda:DeleteFunction.
- Resource: arn:aws:lambda:{region}:{account}:lambda/{id}.
- Context: evaluate Lambda calls with tenant/project/account, provider region/location, `arn:aws:lambda:{region}:{account}:lambda/{id}`, source IP, request id, user agent, tags/labels on `aws_lambda_function`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Lambda actions, `arn:aws:lambda:{region}:{account}:lambda/{id}` prefix checks, tag/label equality on `aws_lambda_function`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/lambda` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Lambda provider names, locations, tags/labels, and request bodies map to HomePort `aws_lambda_function` records and `OpenFaaS-compatible containers with the HomePort Lambda adapter` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Lambda provider ids, `aws_lambda_function` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/lambda` backend auth, missing `aws_lambda_function`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/lambda/backend.yaml` for `OpenFaaS-compatible containers with the HomePort Lambda adapter` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/lambda/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/lambda/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-lambda.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateFunction -> GetFunction -> Invoke -> UpdateFunctionCode -> DeleteFunction against `/compat/aws/lambda` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_lambda_function` from `aws/lambda`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-lambda.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-lambda.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-lambda.yaml`, then promote only when that manifest passes in CI.
