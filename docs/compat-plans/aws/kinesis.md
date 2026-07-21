# AWS Kinesis Compatibility Plan

## Goal

Expose the smallest AWS Kinesis-compatible surface needed to migrate the ledger resources to `Redpanda with the HomePort Kinesis adapter` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: kinesis stream lifecycle/describe/list, tags and retention, split/merge shard lifecycle, and record iterator reads/writes.
- Actions explicitly not supported first: Kinesis console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `kinesis:CreateStream` and its paired read/list calls.
- Ledger resource types: `aws_kinesis_stream`
- Provider errors: map Kinesis authorization failures to AWS access-denied codes, missing `aws_kinesis_stream` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/kinesis` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_kinesis_stream`.

## Backend

- Backend: Redpanda with HomePort Kinesis adapter.
- Storage and metadata: Kinesis state lives in `Redpanda with the HomePort Kinesis adapter`; HomePort stores provider identifiers for `aws_kinesis_stream`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision Redpanda and the HomePort Kinesis adapter with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: kinesis:CreateStream, kinesis:DeleteStream, kinesis:ListStreams, kinesis:ListShards, kinesis:DescribeStream, kinesis:DescribeStreamSummary, kinesis:ListTagsForStream, kinesis:AddTagsToStream, kinesis:RemoveTagsFromStream, kinesis:IncreaseStreamRetentionPeriod, kinesis:DecreaseStreamRetentionPeriod, kinesis:SplitShard, kinesis:MergeShards, kinesis:PutRecord, kinesis:PutRecords, kinesis:GetShardIterator, kinesis:GetRecords.
- Resource: arn:aws:kinesis:{region}:{account}:stream/{id}.
- Context: evaluate Kinesis calls with tenant/project/account, provider region/location, `arn:aws:kinesis:{region}:{account}:stream/{id}`, source IP, request id, user agent, tags/labels on `aws_kinesis_stream`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Kinesis actions, `arn:aws:kinesis:{region}:{account}:stream/{id}` prefix checks, tag/label equality on `aws_kinesis_stream`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/kinesis` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Kinesis provider names, locations, tags/labels, and request bodies map to HomePort `aws_kinesis_stream` records and `Redpanda with the HomePort Kinesis adapter` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Kinesis provider ids, `aws_kinesis_stream` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/kinesis` backend auth, missing `aws_kinesis_stream`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/kinesis/backend.yaml` for `Redpanda with the HomePort Kinesis adapter` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/kinesis/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/kinesis/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-kinesis.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateStream -> DescribeStream -> ListStreams -> UpdateShardCount -> DeleteStream against `/compat/aws/kinesis` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Terraform applies and destroys `aws_kinesis_stream` with tags through a provider Kinesis endpoint override, including `DescribeStreamSummary` waiter read-back.
- Fixture import covers `aws_kinesis_stream` from `aws/kinesis`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 seed - AWS SDK, AWS CLI, and Terraform endpoint-override checks cover the local Kinesis adapter, but Redpanda durability and full acceptance gates still block L4.
- Target level: L4 after `test/conformance/services/aws-kinesis.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-kinesis.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-kinesis.yaml`, then promote only when that manifest passes in CI.
