# AWS Rekognition Compatibility Plan

## Goal

Expose the smallest AWS Rekognition-compatible surface needed to migrate the ledger resources to `OpenCV` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: rekognition:CreateCollection, rekognition:DescribeCollection, rekognition:ListCollections, rekognition:DeleteCollection.
- Actions explicitly not supported first: Rekognition console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `rekognition:CreateCollection` and its paired read/list calls.
- Ledger resource types: `aws_rekognition_collection`
- Provider errors: map Rekognition authorization failures to AWS access-denied codes, missing `aws_rekognition_collection` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/rekognition` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_rekognition_collection`.

## Backend

- Backend: OpenCV.
- Storage and metadata: Rekognition state lives in `OpenCV`; HomePort stores provider identifiers for `aws_rekognition_collection`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `OpenCV` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: rekognition:CreateCollection, rekognition:DescribeCollection, rekognition:ListCollections, rekognition:DeleteCollection.
- Resource: arn:aws:rekognition:{region}:{account}:rekognition/{id}.
- Context: evaluate Rekognition calls with tenant/project/account, provider region/location, `arn:aws:rekognition:{region}:{account}:rekognition/{id}`, source IP, request id, user agent, tags/labels on `aws_rekognition_collection`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Rekognition actions, `arn:aws:rekognition:{region}:{account}:rekognition/{id}` prefix checks, tag/label equality on `aws_rekognition_collection`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/rekognition` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Rekognition provider names, locations, tags/labels, and request bodies map to HomePort `aws_rekognition_collection` records and `OpenCV` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Rekognition provider ids, `aws_rekognition_collection` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/rekognition` backend auth, missing `aws_rekognition_collection`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/rekognition/backend.yaml` for `OpenCV` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/rekognition/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/rekognition/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-rekognition.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateCollection -> DescribeCollection -> ListCollections -> DeleteCollection against `/compat/aws/rekognition` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_rekognition_collection` from `aws/rekognition`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-rekognition.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-rekognition.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-rekognition.yaml`, then promote only when that manifest passes in CI.
