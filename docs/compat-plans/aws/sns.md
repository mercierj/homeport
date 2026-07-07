# AWS SNS Compatibility Plan

## Goal

Expose the smallest AWS SNS-compatible surface needed to migrate the ledger resources to `NATS` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: sns:CreateTopic, sns:GetTopicAttributes, sns:ListTopics, sns:SetTopicAttributes, sns:DeleteTopic.
- Actions explicitly not supported first: SNS console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `sns:CreateTopic` and its paired read/list calls.
- Ledger resource types: `aws_sns_topic`.
- Provider errors: map SNS authorization failures to AWS access-denied codes, missing `aws_sns_topic` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/sns` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_sns_topic`.

## Backend

- Backend: NATS.
- Storage and metadata: SNS state lives in `NATS`; HomePort stores provider identifiers for `aws_sns_topic`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `NATS` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: sns:CreateTopic, sns:GetTopicAttributes, sns:ListTopics, sns:SetTopicAttributes, sns:DeleteTopic.
- Resource: arn:aws:sns:{region}:{account}:sns/{id}.
- Context: evaluate SNS calls with tenant/project/account, provider region/location, `arn:aws:sns:{region}:{account}:sns/{id}`, source IP, request id, user agent, tags/labels on `aws_sns_topic`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed SNS actions, `arn:aws:sns:{region}:{account}:sns/{id}` prefix checks, tag/label equality on `aws_sns_topic`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/sns` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: SNS provider names, locations, tags/labels, and request bodies map to HomePort `aws_sns_topic` records and `NATS` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return SNS provider ids, `aws_sns_topic` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/sns` backend auth, missing `aws_sns_topic`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/sns/backend.yaml` for `NATS` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/sns/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/sns/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-sns.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateTopic -> GetTopicAttributes -> ListTopics -> SetTopicAttributes -> DeleteTopic against `/compat/aws/sns` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_sns_topic` from `aws/sns`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-sns.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-sns.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-sns.yaml`, then promote only when that manifest passes in CI.
