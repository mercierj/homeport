# AWS MQ Compatibility Plan

## Goal

Expose the smallest AWS MQ-compatible surface needed to migrate the ledger resources to `RabbitMQ` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: mq:CreateBroker, mq:DescribeBroker, mq:ListBrokers, mq:UpdateBroker, mq:DeleteBroker.
- Actions explicitly not supported first: MQ console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `mq:CreateBroker` and its paired read/list calls.
- Ledger resource types: `aws_mq_broker`.
- Provider errors: map MQ authorization failures to AWS access-denied codes, missing `aws_mq_broker` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/mq` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_mq_broker`.

## Backend

- Backend: RabbitMQ.
- Storage and metadata: MQ state lives in `RabbitMQ`; HomePort stores provider identifiers for `aws_mq_broker`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `RabbitMQ` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: mq:CreateBroker, mq:DescribeBroker, mq:ListBrokers, mq:UpdateBroker, mq:DeleteBroker.
- Resource: arn:aws:mq:{region}:{account}:mq/{id}.
- Context: evaluate MQ calls with tenant/project/account, provider region/location, `arn:aws:mq:{region}:{account}:mq/{id}`, source IP, request id, user agent, tags/labels on `aws_mq_broker`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed MQ actions, `arn:aws:mq:{region}:{account}:mq/{id}` prefix checks, tag/label equality on `aws_mq_broker`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/mq` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: MQ provider names, locations, tags/labels, and request bodies map to HomePort `aws_mq_broker` records and `RabbitMQ` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return MQ provider ids, `aws_mq_broker` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/mq` backend auth, missing `aws_mq_broker`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/mq/backend.yaml` for `RabbitMQ` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/mq/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/mq/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-mq.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateBroker -> DescribeBroker -> ListBrokers -> UpdateBroker -> DeleteBroker against `/compat/aws/mq` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_mq_broker` from `aws/mq`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-mq.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-mq.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-mq.yaml`, then promote only when that manifest passes in CI.
