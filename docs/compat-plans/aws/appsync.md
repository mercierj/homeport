# AWS AppSync Local Compatibility Plan

## Goal

Expose an in-memory AppSync API-management surface for local endpoint-override checks without claiming Hasura deployment or GraphQL execution.

## Provider API Surface

- Supported SDK actions are `CreateGraphqlApi`, `GetGraphqlApi`, `ListGraphqlApis`, `UpdateGraphqlApi`, `DeleteGraphqlApi`, `CreateApiKey`, `ListApiKeys`, `UpdateApiKey`, and `DeleteApiKey`.
- Schema deployment, resolvers, distributed tracing, and GraphQL requests are unsupported.
- Ledger resource types: `aws_appsync_graphql_api`

## Backend

- Backend: Hasura GraphQL Engine.
- Local status: proposed migration seed only; Hasura is not deployed and persistence, HA, backup, validation, cutover, and rollback are not proved.

## Authz Model

- Every supported management action calls the shared authorizer before accessing state and records its allow or deny decision through the adapter audit sink.
- Denied requests return `AccessDeniedException` without mutation.

## Adapter

- Endpoint: `/compat/aws/appsync/v1/apis`; `ListGraphqlApis`, `ListApiKeys`, and `ListDataSources` accept `maxResults` (1–25) and `nextToken` query parameters and reject invalid values with `BadRequestException`. Create-time tags plus `TagResource`, `ListTagsForResource`, and `UntagResource` are retained in the local adapter, as are API-key, data-source, `xrayEnabled`, and introspection-configuration lifecycles. Data sources are metadata only; no backend connections or distributed trace data are emitted.
- State is process-local and returns API id, name, authentication type, and ARN without advertising a runnable GraphQL URI.

## Generated Artifacts

- `artifacts/compat/aws/appsync/backend.yaml` records the proposed Hasura target.
- `artifacts/compat/aws/appsync/adapter.yaml` records the local management contract.
- `artifacts/compat/aws/appsync/migration.md` preserves source identifiers without asserting migration execution.
- `test/conformance/services/aws-appsync.yaml` records the runnable local contract.

## Contract Tests

- AWS SDK for Go v2 exercises GraphQL API, API-key, tag, and paginated-list lifecycles against the local endpoint.

## Compatibility Level

- Current level: L3 local SDK lifecycle seed.
- Target level: L4 only after durable Hasura integration and runnable GraphQL behavior are proved.
- Blocking gaps: persistent backend, GraphQL execution, external validation, cutover, and rollback.
