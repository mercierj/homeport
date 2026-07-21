# AWS Step Functions Compatibility Plan

## Goal

Expose a local, in-memory AWS Step Functions-compatible surface. Temporal is a migration target only; this plan does not claim a deployed backend, persistence, HA, cutover, or rollback.

## Provider API Surface

- Initial supported surface: states:CreateStateMachine, states:DescribeStateMachine, states:ListStateMachines, states:UpdateStateMachine, states:DeleteStateMachine.
- Actions explicitly not supported: Step Functions console-only workflows, account billing, quota purchase flows, managed cross-region failover controls, workflow-state execution, aliases, and versions.
- Local resource state: state machines are held in process for the adapter lifetime.
- Provider errors: invalid definitions, missing or duplicate state machines, configured quota exhaustion, authorization denial, unsupported actions, invalid list tokens, and authorizer failures use AWS-shaped codes; supported actions emit authorization decisions to the adapter audit sink.
- Pagination: `ListStateMachines` and `ListExecutions` support `maxResults` and `nextToken`. Create-time tags plus `TagResource`, `ListTagsForResource`, and `UntagResource` are retained in the local adapter. The local execution lifecycle supports `StartExecution`, `DescribeExecution`, `StopExecution`, `ListExecutions`, and a metadata-only `GetExecutionHistory` stream containing start and stop events. Repeated named starts with identical input replay the original execution. Workflow-state history, aliases, and versions are unsupported.
- Ledger resource types: `aws_sfn_state_machine`

## Backend

- Backend: Temporal.
- Current implementation: no Temporal process is started and no state is persisted. `backend.yaml` is a proposed configuration seed, not deployment evidence.

## Authz Model

- Principal: request principal and claims supplied to the compatibility endpoint.
- Actions: states:CreateStateMachine, states:DescribeStateMachine, states:ListStateMachines, states:UpdateStateMachine, states:DeleteStateMachine.
- Resource: `arn:aws:states:us-east-1:000000000000:stateMachine:{id}`.
- Context: provider, service, method, source IP, current time, user agent, claims, and principal attributes.
- Evaluation: call `Authorize` before every supported action.

## Adapter

- Endpoint exposed: `/compat/aws/stepfunctions` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: names, ASL JSON definitions, role ARNs, state-machine ARNs, and list pagination values map to in-memory state.
- Response mapping: return state-machine ARNs, names, definitions, role ARNs, creation dates, type `STANDARD`, and list tokens.
- Error mapping: return the local errors listed above; no backend timeout, retries, or request IDs are claimed.

## Generated Artifacts

- `artifacts/compat/aws/stepfunctions/backend.yaml` is a proposed Temporal configuration seed.
- `artifacts/compat/aws/stepfunctions/adapter.yaml` records endpoint, action/resource mappings, errors, pagination, and the local quota option.
- `artifacts/compat/aws/stepfunctions/migration.md` identifies source data and operator decisions without asserting migration execution.
- `test/conformance/services/aws-step-functions.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateStateMachine -> DescribeStateMachine -> ListStateMachines -> UpdateStateMachine -> DeleteStateMachine against `/compat/aws/stepfunctions`.
- Negative cases cover denied authorization, invalid definitions, quota exhaustion, and pagination.

## Compatibility Level

- Current level: local SDK contract seed; no provider-grade compatibility claim.
- Target level: L4 only after durable Temporal integration and execution behavior are proved.
- Blocking gaps: Temporal execution, persistent state, external validation, cutover, and rollback require infrastructure outside this local scope.
