# AWS Step Functions Compatibility Plan

## Goal

Expose the smallest AWS Step Functions-compatible surface needed to migrate the ledger resources to `Temporal` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: states:CreateStateMachine, states:DescribeStateMachine, states:ListStateMachines, states:UpdateStateMachine, states:DeleteStateMachine.
- Actions explicitly not supported first: Step Functions console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `states:CreateStateMachine` and its paired read/list calls.
- Ledger resource types: `aws_sfn_state_machine`.
- Provider errors: map Step Functions authorization failures to AWS access-denied codes, missing `aws_sfn_state_machine` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/step-functions` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_sfn_state_machine`.

## Backend

- Backend: Temporal.
- Storage and metadata: Step Functions state lives in `Temporal`; HomePort stores provider identifiers for `aws_sfn_state_machine`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Temporal` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: states:CreateStateMachine, states:DescribeStateMachine, states:ListStateMachines, states:UpdateStateMachine, states:DeleteStateMachine.
- Resource: arn:aws:states:{region}:{account}:step-functions/{id}.
- Context: evaluate Step Functions calls with tenant/project/account, provider region/location, `arn:aws:states:{region}:{account}:step-functions/{id}`, source IP, request id, user agent, tags/labels on `aws_sfn_state_machine`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Step Functions actions, `arn:aws:states:{region}:{account}:step-functions/{id}` prefix checks, tag/label equality on `aws_sfn_state_machine`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/step-functions` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Step Functions provider names, locations, tags/labels, and request bodies map to HomePort `aws_sfn_state_machine` records and `Temporal` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Step Functions provider ids, `aws_sfn_state_machine` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/step-functions` backend auth, missing `aws_sfn_state_machine`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/step-functions/backend.yaml` for `Temporal` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/step-functions/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/step-functions/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-step-functions.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateStateMachine -> DescribeStateMachine -> ListStateMachines -> UpdateStateMachine -> DeleteStateMachine against `/compat/aws/step-functions` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_sfn_state_machine` from `aws/step-functions`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-step-functions.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-step-functions.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-step-functions.yaml`, then promote only when that manifest passes in CI.
