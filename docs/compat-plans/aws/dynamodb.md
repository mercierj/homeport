# AWS DynamoDB Compatibility Plan

## Goal

Expose the smallest AWS DynamoDB-compatible surface needed to migrate the ledger resources to `ScyllaDB Alternator` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: dynamodb:CreateTable, dynamodb:DescribeTable, dynamodb:ListTables, dynamodb:PutItem, dynamodb:GetItem, dynamodb:Query, dynamodb:Scan, dynamodb:DescribeTimeToLive, dynamodb:ListTagsOfResource, dynamodb:TagResource, dynamodb:UntagResource, dynamodb:DeleteTable.
- Actions explicitly not supported first: DynamoDB console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `dynamodb:CreateTable` and its paired read/list calls.
- Ledger resource types: `aws_dynamodb_table`
- Provider errors: map DynamoDB authorization failures to AWS access-denied codes, missing `aws_dynamodb_table` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/dynamodb` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_dynamodb_table`.

## Backend

- Backend: ScyllaDB Alternator.
- Storage and metadata: DynamoDB state lives in `ScyllaDB Alternator`; HomePort stores provider identifiers for `aws_dynamodb_table`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `ScyllaDB Alternator` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: dynamodb:CreateTable, dynamodb:DescribeTable, dynamodb:ListTables, dynamodb:PutItem, dynamodb:GetItem, dynamodb:Query, dynamodb:Scan, dynamodb:DescribeTimeToLive, dynamodb:ListTagsOfResource, dynamodb:TagResource, dynamodb:UntagResource, dynamodb:DeleteTable.
- Resource: arn:aws:dynamodb:{region}:{account}:dynamodb/{id}.
- Context: evaluate DynamoDB calls with tenant/project/account, provider region/location, `arn:aws:dynamodb:{region}:{account}:dynamodb/{id}`, source IP, request id, user agent, tags/labels on `aws_dynamodb_table`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed DynamoDB actions, `arn:aws:dynamodb:{region}:{account}:dynamodb/{id}` prefix checks, tag/label equality on `aws_dynamodb_table`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/dynamodb` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: DynamoDB provider names, locations, tags/labels, and request bodies map to HomePort `aws_dynamodb_table` records and `ScyllaDB Alternator` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return DynamoDB provider ids, `aws_dynamodb_table` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/dynamodb` backend auth, missing `aws_dynamodb_table`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/dynamodb/backend.yaml` for `ScyllaDB Alternator` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/dynamodb/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/dynamodb/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-dynamodb.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateTable -> DescribeTable -> PutItem -> GetItem -> Query -> DeleteTable against `/compat/aws/dynamodb` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_dynamodb_table` from `aws/dynamodb`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; the local adapter covers table/item lifecycle, tags, quotas, table and item pagination, stream metadata, and authorization/audit, but provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-dynamodb.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-dynamodb.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-dynamodb.yaml`, then promote only when that manifest passes in CI.
