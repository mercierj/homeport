# AWS MSK Compatibility Plan

## Goal

Expose the smallest AWS MSK-compatible surface needed to migrate the ledger resources to `Redpanda Kafka-compatible cluster` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: msk:CreateCluster, msk:DescribeCluster, msk:ListClustersV2, msk:UpdateClusterConfiguration, msk:DeleteCluster.
- Actions explicitly not supported first: MSK console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `msk:CreateCluster` and its paired read/list calls.
- Ledger resource types: `aws_msk_cluster`.
- Provider errors: map MSK authorization failures to AWS access-denied codes, missing `aws_msk_cluster` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/msk` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_msk_cluster`.

## Backend

- Backend: Redpanda Kafka-compatible cluster.
- Storage and metadata: MSK state lives in `Redpanda Kafka-compatible cluster`; HomePort stores provider identifiers for `aws_msk_cluster`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Redpanda Kafka-compatible cluster` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: msk:CreateCluster, msk:DescribeCluster, msk:ListClustersV2, msk:UpdateClusterConfiguration, msk:DeleteCluster.
- Resource: arn:aws:msk:{region}:{account}:msk/{id}.
- Context: evaluate MSK calls with tenant/project/account, provider region/location, `arn:aws:msk:{region}:{account}:msk/{id}`, source IP, request id, user agent, tags/labels on `aws_msk_cluster`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed MSK actions, `arn:aws:msk:{region}:{account}:msk/{id}` prefix checks, tag/label equality on `aws_msk_cluster`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/msk` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: MSK provider names, locations, tags/labels, and request bodies map to HomePort `aws_msk_cluster` records and `Redpanda Kafka-compatible cluster` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return MSK provider ids, `aws_msk_cluster` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/msk` backend auth, missing `aws_msk_cluster`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/msk/backend.yaml` for `Redpanda Kafka-compatible cluster` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/msk/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/msk/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-msk.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateCluster -> DescribeCluster -> ListClustersV2 -> UpdateClusterConfiguration -> DeleteCluster against `/compat/aws/msk` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_msk_cluster` from `aws/msk`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-msk.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-msk.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-msk.yaml`, then promote only when that manifest passes in CI.
