# AWS Cognito Compatibility Plan

## Goal

Expose the smallest AWS Cognito-compatible surface needed to migrate the ledger resources to `Keycloak OIDC` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: cognito:CreateUserPool, cognito:DescribeUserPool, cognito:ListUserPools, cognito:UpdateUserPool, cognito:DeleteUserPool, cognito:GetUserPoolMfaConfig, user-pool clients/domains, administrative users/groups, and resource tags.
- Actions explicitly not supported first: Cognito console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `cognito:CreateUserPool` and its paired read/list calls.
- Ledger resource types: `aws_cognito_user_pool`
- Provider errors: map authorization to `NotAuthorizedException`, missing user-pool resources to `ResourceNotFoundException`, invalid fields and pagination tokens to `InvalidParameterException`, a configured user-pool limit to `LimitExceededException`, and authorizer failures to `InternalErrorException`.
- Pagination/tags: list calls expose provider tokens and user-pool tags round-trip; idempotency tokens and operation IDs are not supported by this local seed.

## Backend

- Backend: Keycloak OIDC.
- Storage and metadata: generated artifacts target `Keycloak OIDC`; the local adapter keeps user-pool state in memory and emits provider identifiers and audit decisions.
- Secrets/keys/tokens: the local adapter accepts compatibility credentials only; credential issuance and encrypted source-input storage are outside this seed.
- Runtime/provisioning: `backend.yaml` records a Keycloak target, health path, persistence volume, and backup command; provisioning and teardown are outside this local seed.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: `cognito-idp:CreateUserPool`, `cognito-idp:DescribeUserPool`, `cognito-idp:ListUserPools`, `cognito-idp:UpdateUserPool`, `cognito-idp:DeleteUserPool`, `cognito-idp:GetUserPoolMfaConfig`, supported client/domain/user/group actions, and resource-tag actions.
- Resource: arn:aws:cognito-idp:{region}:{account}:userpool/{id}.
- Context: evaluate Cognito calls with tenant/project/account, provider region/location, `arn:aws:cognito-idp:{region}:{account}:userpool/{id}`, source IP, request id, user agent, tags/labels on `aws_cognito_user_pool`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cognito IDP actions, `arn:aws:cognito-idp:{region}:{account}:userpool/{id}` prefix checks, tag/label equality on `aws_cognito_user_pool`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/cognito` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Cognito request bodies map to in-memory user-pool, client, domain, user, group, and tag records; no Keycloak configuration is applied by the adapter.
- Response mapping: return the local Cognito-compatible IDs, ARNs, lifecycle shapes, tags, and pagination tokens; operation IDs, ETags, audit timestamps, and retry hints are not emitted.
- Error mapping: return the local provider-shaped authorization, not-found, duplicate-user/group, validation, configured-quota, unsupported-operation, and authorizer-failure errors; backend timeout and dependency mapping are outside this seed.

## Generated Artifacts

- `artifacts/compat/aws/cognito/backend.yaml` for `Keycloak OIDC` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/cognito/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/cognito/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-cognito.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises user-pool lifecycle, MFA validation, clients, domains, users, groups, tags, pagination, authorization/audit, and a configurable quota against `/compat/aws/cognito`.
- AWS CLI, Terraform, and boto3 smoke tests cover the supported endpoint override path.
- Fixture import, credential-expiry enforcement, backend timeout/retry behavior, and cross-service IAM parity remain outside this seed.

## Compatibility Level

- Current level: L3 seed - AWS SDK, AWS CLI, and Terraform endpoint-override checks cover the local Cognito adapter. SDK contracts also cover centralized authz/audit, a configurable user-pool quota, and pagination for user pools, clients, users, and groups, but Keycloak persistence and full acceptance gates still block L4.
- Target level: L4 after `test/conformance/services/aws-cognito.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-cognito.yaml` must still prove broader provider-error parity, idempotency, and real Keycloak persistence before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-cognito.yaml`, then promote only when that manifest passes in CI.
