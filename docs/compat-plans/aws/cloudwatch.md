# AWS CloudWatch Logs Local Compatibility Plan

## Goal

Provide a process-local CloudWatch Logs surface for endpoint-override checks without claiming Loki deployment or CloudWatch metrics/dashboard parity.

## Provider API Surface

- Supported operations cover log groups, streams, retention, log events, legacy name-based and current ARN-based tags, pagination, quotas, authorization, and audit callbacks.
- Metrics and dashboards are unsupported by this adapter.
- Ledger resource types: `aws_cloudwatch_metric_alarm`, `aws_cloudwatch_log_group`, `aws_cloudwatch_dashboard`

## Backend

- Backend: Loki, Prometheus, Alertmanager, and Grafana.
- Local status: proposed migration targets only; none is deployed and persistence, HA, backup, validation, cutover, and rollback are not proved.

## Authz Model

- Every supported operation calls the shared authorizer with the log resource ARN when available.
- Denied requests do not mutate process-local state.

## Adapter

- Endpoint: `/compat/aws/cloudwatchlogs`.
- State is held in memory and disappears when the adapter stops.
- Requests and responses use the AWS JSON shapes exercised by official clients.

## Generated Artifacts

- `artifacts/compat/aws/cloudwatch/backend.yaml` records the proposed Loki target.
- `artifacts/compat/aws/cloudwatch/adapter.yaml` records the local action and error mappings.
- `artifacts/compat/aws/cloudwatch/migration.md` preserves source identifiers without asserting migration execution.
- `test/conformance/services/aws-cloudwatch.yaml` records the runnable local contract.

## Contract Tests

- AWS SDK for Go v2 exercises log group, stream, retention, legacy and current ARN-based tag APIs, event, pagination, quota, authorization, and audit behavior against the local endpoint.
- AWS CLI, Terraform, and Boto3 endpoint-override checks cover their documented local slices.

## Compatibility Level

- Current level: L3 local seed.
- Target level: L4 only after durable backend integration and external migration gates are proved.
- Blocking gaps: durable Loki storage, metrics and dashboard compatibility, production validation, cutover, and rollback.
