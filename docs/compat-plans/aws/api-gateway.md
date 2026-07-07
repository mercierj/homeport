# AWS API Gateway Compatibility Plan

## Goal

Expose the smallest AWS API Gateway-compatible surface needed to migrate the ledger resources to `Kong` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: apigateway:CreateRestApi, apigateway:GetRestApi, apigateway:GetRestApis, apigateway:UpdateRestApi, apigateway:DeleteRestApi.
- Actions explicitly not supported first: API Gateway console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `apigateway:CreateRestApi` and its paired read/list calls.
- Ledger resource types: `aws_api_gateway_rest_api`.
- Provider errors: map API Gateway authorization failures to AWS access-denied codes, missing `aws_api_gateway_rest_api` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/api-gateway` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_api_gateway_rest_api`.

## Backend

- Backend: Kong.
- Storage and metadata: API Gateway state lives in `Kong`; HomePort stores provider identifiers for `aws_api_gateway_rest_api`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Kong` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: apigateway:CreateRestApi, apigateway:GetRestApi, apigateway:GetRestApis, apigateway:UpdateRestApi, apigateway:DeleteRestApi.
- Resource: arn:aws:apigateway:{region}:{account}:api-gateway/{id}.
- Context: evaluate API Gateway calls with tenant/project/account, provider region/location, `arn:aws:apigateway:{region}:{account}:api-gateway/{id}`, source IP, request id, user agent, tags/labels on `aws_api_gateway_rest_api`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed API Gateway actions, `arn:aws:apigateway:{region}:{account}:api-gateway/{id}` prefix checks, tag/label equality on `aws_api_gateway_rest_api`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/api-gateway` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: API Gateway provider names, locations, tags/labels, and request bodies map to HomePort `aws_api_gateway_rest_api` records and `Kong` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return API Gateway provider ids, `aws_api_gateway_rest_api` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/api-gateway` backend auth, missing `aws_api_gateway_rest_api`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/api-gateway/backend.yaml` for `Kong` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/api-gateway/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/api-gateway/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-api-gateway.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateRestApi -> GetRestApi -> GetRestApis -> UpdateRestApi -> DeleteRestApi against `/compat/aws/api-gateway` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_api_gateway_rest_api` from `aws/api-gateway`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-api-gateway.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-api-gateway.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-api-gateway.yaml`, then promote only when that manifest passes in CI.
