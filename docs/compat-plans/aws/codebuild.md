# AWS CodeBuild Local Compatibility Plan

## Goal

Expose an in-memory CodeBuild project surface for local endpoint-override checks without claiming build execution or a deployed GitLab backend.

## Provider API Surface

- Supported actions are `CreateProject`, `BatchGetProjects`, `ListProjects`, `UpdateProject`, and `DeleteProject`.
- Builds, webhooks, reports, and idempotency are unsupported; create-time project tags are retained and returned by `BatchGetProjects`, and supported management actions emit authorization decisions to the adapter audit sink.
- Ledger resource types: `aws_codebuild_project`

## Backend

- Backend: GitLab Runner and GitLab CI.
- Local status: proposed migration seed only; no GitLab process is started and project state is not persisted.

## Authz Model

- Named-project actions authorize `codebuild:<action>` on `arn:aws:codebuild:us-east-1:000000000000:project/{id}`.
- `ListProjects` authorizes the project wildcard and denied requests do not mutate state.

## Adapter

- Endpoint: `/compat/aws/codebuild`.
- State is process-local; project fields required by the SDK are retained but no build is executed.
- Local errors cover invalid input, duplicates, missing projects, quota exhaustion, authorization failures, and unsupported actions.

## Generated Artifacts

- `artifacts/compat/aws/codebuild/backend.yaml` records the proposed GitLab target.
- `artifacts/compat/aws/codebuild/adapter.yaml` records local actions and errors.
- `artifacts/compat/aws/codebuild/migration.md` preserves source identifiers without asserting migration execution.
- `test/conformance/services/aws-codebuild.yaml` records the runnable local contract.

## Contract Tests

- AWS SDK for Go v2 exercises CreateProject -> BatchGetProjects -> ListProjects -> UpdateProject -> DeleteProject against the local endpoint.

## Compatibility Level

- Current level: L3 local SDK contract seed.
- Target level: L4 only after durable GitLab integration and build execution are proved.
- Blocking gaps: GitLab execution, persistent state, external validation, cutover, and rollback.
