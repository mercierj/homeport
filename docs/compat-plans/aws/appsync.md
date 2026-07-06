# AWS AppSync Compatibility Plan

## Goal

Expose the smallest AWS AppSync-compatible surface needed to migrate the ledger resources to `Hasura GraphQL Engine` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: appsync:CreateGraphqlApi, appsync:GetGraphqlApi, appsync:ListGraphqlApis, appsync:UpdateGraphqlApi, appsync:DeleteGraphqlApi.
- Actions explicitly not supported first: AppSync console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `appsync:CreateGraphqlApi` and its paired read/list calls.
- Ledger resource types: `aws_appsync_graphql_api`.
- Provider errors: map AppSync authorization failures to AWS access-denied codes, missing `aws_appsync_graphql_api` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/appsync` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_appsync_graphql_api`.

## Backend

- Backend: Hasura GraphQL Engine.
- Storage and metadata: AppSync state lives in `Hasura GraphQL Engine`; HomePort stores provider identifiers for `aws_appsync_graphql_api`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Hasura GraphQL Engine` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: appsync:CreateGraphqlApi, appsync:GetGraphqlApi, appsync:ListGraphqlApis, appsync:UpdateGraphqlApi, appsync:DeleteGraphqlApi.
- Resource: arn:aws:appsync:{region}:{account}:appsync/{id}.
- Context: evaluate AppSync calls with tenant/project/account, provider region/location, `arn:aws:appsync:{region}:{account}:appsync/{id}`, source IP, request id, user agent, tags/labels on `aws_appsync_graphql_api`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed AppSync actions, `arn:aws:appsync:{region}:{account}:appsync/{id}` prefix checks, tag/label equality on `aws_appsync_graphql_api`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/appsync` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: AppSync provider names, locations, tags/labels, and request bodies map to HomePort `aws_appsync_graphql_api` records and `Hasura GraphQL Engine` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return AppSync provider ids, `aws_appsync_graphql_api` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/appsync` backend auth, missing `aws_appsync_graphql_api`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/appsync/backend.yaml` for `Hasura GraphQL Engine` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/appsync/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/appsync/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-appsync.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateGraphqlApi -> GetGraphqlApi -> ListGraphqlApis -> UpdateGraphqlApi -> DeleteGraphqlApi against `/compat/aws/appsync` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_appsync_graphql_api` from `aws/appsync`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-appsync.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-appsync.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-appsync.yaml`, then promote only when that manifest passes in CI.
