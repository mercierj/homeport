# AWS API Gateway Local Compatibility Plan

## Goal

Provide an in-memory API Gateway REST API management surface for local endpoint-override checks without claiming that Kong or any external migration phase is operational.

## Provider API Surface

- Supported operations cover REST API lifecycle plus tested resources, methods, integrations, deployments, stages, custom domains, tags, pagination, quotas, authorization, and audit callbacks.
- State is process-local and disappears when the adapter stops.
- Ledger resource types: `aws_api_gateway_rest_api`

## Backend

- Backend: Kong.
- Local status: proposed migration seed only; Kong is not deployed and persistence, HA, backup, validation, cutover, and rollback are not proved.

## Authz Model

- Every supported operation calls the shared authorizer.
- Creation uses the assigned REST API ARN; the remaining operations currently authorize with `*`.

## Adapter

- Endpoint: `/compat/aws/apigateway`.
- Requests and responses use the AWS API Gateway REST-JSON shapes exercised by official clients.
- Unsupported operations return provider-shaped errors and do not mutate local state.

## Generated Artifacts

- `artifacts/compat/aws/api-gateway/backend.yaml` records the proposed Kong target.
- `artifacts/compat/aws/api-gateway/adapter.yaml` records routes, mappings, and unsupported external work.
- `artifacts/compat/aws/api-gateway/migration.md` preserves source identifiers without asserting migration execution.
- `test/conformance/services/aws-api-gateway.yaml` records the runnable local contract.

## Contract Tests

- AWS SDK for Go v2 exercises REST API and subordinate resource management against `/compat/aws/apigateway`.
- AWS CLI, Terraform, and Boto3 endpoint-override checks cover their documented local slices.

## Compatibility Level

- Current level: L3 local seed.
- Target level: L4 only after durable Kong integration and the external migration gates are proved.
- Blocking gaps: persistent Kong storage, resource-scoped authorization beyond creation, production validation, cutover, and rollback.
