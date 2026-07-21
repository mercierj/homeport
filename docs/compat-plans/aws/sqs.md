# AWS SQS Compatibility Plan

## Goal

Expose the smallest AWS SQS-compatible surface needed to migrate the ledger resources to `RabbitMQ` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: sqs:CreateQueue, sqs:GetQueueAttributes, sqs:SendMessage, sqs:ReceiveMessage, sqs:DeleteQueue.
- Actions explicitly not supported first: SQS console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `sqs:CreateQueue` and its paired read/list calls.
- Ledger resource types: `aws_sqs_queue`
- Provider errors: map SQS authorization failures to AWS access-denied codes, missing `aws_sqs_queue` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/sqs` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_sqs_queue`.

## Backend

- Backend: RabbitMQ.
- Storage and metadata: SQS state lives in `RabbitMQ`; HomePort stores provider identifiers for `aws_sqs_queue`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `RabbitMQ` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: sqs:CreateQueue, sqs:GetQueueAttributes, sqs:SendMessage, sqs:ReceiveMessage, sqs:DeleteQueue.
- Resource: arn:aws:sqs:{region}:{account}:sqs/{id}.
- Context: evaluate SQS calls with tenant/project/account, provider region/location, `arn:aws:sqs:{region}:{account}:sqs/{id}`, source IP, request id, user agent, tags/labels on `aws_sqs_queue`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed SQS actions, `arn:aws:sqs:{region}:{account}:sqs/{id}` prefix checks, tag/label equality on `aws_sqs_queue`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/sqs` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: SQS provider names, locations, tags/labels, and request bodies map to HomePort `aws_sqs_queue` records and `RabbitMQ` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return SQS provider ids, `aws_sqs_queue` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/sqs` backend auth, missing `aws_sqs_queue`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/sqs/backend.yaml` for `RabbitMQ` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/sqs/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/sqs/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-sqs.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateQueue -> GetQueueAttributes -> SendMessage -> ReceiveMessage -> DeleteQueue against `/compat/aws/sqs` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior; Terraform applies and destroys `aws_sqs_queue` through an AWS provider SQS endpoint override.
- Fixture import covers `aws_sqs_queue` from `aws/sqs`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 seed - AWS SDK, AWS CLI, and Terraform endpoint-override checks cover the local SQS adapter, but real RabbitMQ-backed durability and full acceptance gates still block L4.
- Target level: L4 after `test/conformance/services/aws-sqs.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-sqs.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-sqs.yaml`, then promote only when that manifest passes in CI.
