# AWS SNS Local Compatibility Plan

## Goal

Provide a process-local SNS surface for endpoint-override checks without claiming NATS deployment or durable delivery.

## Provider API Surface

- Supported operations cover topic lifecycle and tags, subscription lifecycle, publishing, pagination, idempotency, configurable quotas, authorization, and audit callbacks.
- State is held in memory and disappears when the adapter stops.
- Ledger resource types: `aws_sns_topic`

## Backend

- Backend: NATS.
- Local status: proposed migration seed only; NATS is not deployed and persistence, HA, backup, validation, cutover, and rollback are not proved.

## Authz Model

- Every supported operation calls the shared authorizer with the topic or subscription resource when available.
- Denied requests do not mutate process-local state.

## Adapter

- Endpoint: `/compat/aws/sns`.
- Requests and responses use AWS Query shapes exercised by official clients.
- Unsupported operations return provider-shaped errors.

## Generated Artifacts

- `artifacts/compat/aws/sns/backend.yaml` records the proposed NATS target.
- `artifacts/compat/aws/sns/adapter.yaml` records local actions, errors, pagination, idempotency, and quota behavior.
- `artifacts/compat/aws/sns/migration.md` preserves source identifiers without asserting migration execution.
- `test/conformance/services/aws-sns.yaml` records the runnable local contract.

## Contract Tests

- AWS SDK for Go v2 exercises topics including idempotent deletion, topic tag lifecycle including denied tag operations with audit and no mutation, subscriptions, publishing, pagination, idempotency, quota, authorization, and audit behavior against `/compat/aws/sns`.
- AWS CLI, Terraform, and Boto3 endpoint-override checks cover their documented local slices.

## Compatibility Level

- Current level: L3 local seed.
- Target level: L4 only after durable NATS integration and external migration gates are proved.
- Blocking gaps: durable NATS delivery, production validation, cutover, and rollback.
