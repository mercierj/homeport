# AWS EventBridge Compatibility Plan

## Goal

Expose the smallest AWS EventBridge-compatible surface needed to migrate the ledger resources to `n8n` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: events:PutRule, events:DescribeRule, events:ListRules, events:PutEvents, events:PutTargets, events:ListTargetsByRule, events:ListRuleNamesByTarget, events:RemoveTargets, events:EnableRule, events:DisableRule, events:DeleteRule, events:TagResource, events:ListTagsForResource, events:UntagResource.
- Actions explicitly not supported first: EventBridge console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `events:PutRule` and its paired read/list calls.
- Ledger resource types: `aws_cloudwatch_event_rule`
- Provider errors: map authorization to `AccessDeniedException`, missing rules to `ResourceNotFoundException`, invalid requests to `ValidationException`, invalid pagination to `InvalidToken`, configured limits to `LimitExceededException`, unsupported calls to `UnsupportedOperation`, and authorizer failures to `InternalException`.
- Pagination/tags: list calls expose provider tokens and rule tags round-trip; idempotency tokens and operation IDs are not supported by this local seed.

## Backend

- Backend: n8n.
- Storage and metadata: generated artifacts target `n8n`; the local adapter keeps rules and targets in memory and emits audit decisions.
- Secrets/keys/tokens: compatibility credentials are accepted by the local endpoint; credential issuance and encrypted source inputs are outside this seed.
- Runtime/provisioning: `backend.yaml` records the n8n target, health path, persistence volume, and backup command; provisioning and teardown are outside this local seed.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: events:PutRule, events:DescribeRule, events:ListRules, events:PutEvents, events:PutTargets, events:ListTargetsByRule, events:ListRuleNamesByTarget, events:RemoveTargets, events:EnableRule, events:DisableRule, events:DeleteRule, events:TagResource, events:ListTagsForResource, events:UntagResource.
- Resource: `arn:aws:events:us-east-1:000000000000:rule/{id}` for the default bus, or `arn:aws:events:us-east-1:000000000000:rule/{event-bus}/{id}` for a named bus.
- Context: the adapter forwards provider, service, method, request ID, source IP, current time, user agent, optional credential headers, and header-derived principal attributes/claims to the injected authorizer.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: the injected authorizer defines policy matching; the adapter does not implement region, tag, CIDR, or time conditions itself.

## Adapter

- Endpoints exposed: `/compat/aws/eventbridge` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: EventBridge request bodies map to in-memory rule, target, event, and tag records; no n8n configuration is applied by the adapter.
- Response mapping: return local EventBridge-compatible rule, target, event, tag, and pagination shapes; operation IDs, ETags, audit timestamps, and retry hints are not emitted.
- Error mapping: return the local provider-shaped authorization, not-found, validation, token, configured-limit, unsupported-operation, and authorizer-failure errors; backend timeout and dependency mapping are outside this seed.

## Generated Artifacts

- `artifacts/compat/aws/eventbridge/backend.yaml` records the n8n target, persistence, health check, and backup intent.
- `artifacts/compat/aws/eventbridge/adapter.yaml` records endpoint routes, authz action/resource mappings, errors, pagination, and the configurable quota option.
- `artifacts/compat/aws/eventbridge/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-eventbridge.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises rules, events, targets, tags, pagination, configured quotas, and authorization/audit against `/compat/aws/eventbridge`.
- AWS CLI, Terraform, and boto3 smoke tests are available for the supported endpoint override path when their local binaries are installed.
- Fixture import, credential-expiry enforcement, backend timeout/retry behavior, and cross-service IAM parity remain outside this seed.

## Compatibility Level

- Current level: L3 seed - AWS SDK, AWS CLI, and Terraform endpoint-override checks cover the local EventBridge adapter. SDK contracts also cover rule and target lifecycle, target-to-rule lookup, pagination, rule and target quotas, and centralized authz/audit, but n8n delivery and full acceptance gates still block L4.
- Target level: L4 after `test/conformance/services/aws-eventbridge.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-eventbridge.yaml` must still prove broader provider-error parity, idempotency, and real n8n delivery before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-eventbridge.yaml`, then promote only when that manifest passes in CI.
