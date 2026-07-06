# AWS CloudWatch Compatibility Plan

## Goal

Expose the smallest AWS CloudWatch-compatible surface needed to migrate the ledger resources to `Loki, Prometheus, Alertmanager, and Grafana` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: cloudwatch:PutMetricAlarm, cloudwatch:DescribeAlarms, cloudwatch:PutDashboard, cloudwatch:GetDashboard, cloudwatch:DeleteAlarms, cloudwatch:DeleteDashboards.
- Actions explicitly not supported first: CloudWatch console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `cloudwatch:PutMetricAlarm` and its paired read/list calls.
- Ledger resource types: `aws_cloudwatch_metric_alarm`, `aws_cloudwatch_log_group`, `aws_cloudwatch_dashboard`.
- Provider errors: map CloudWatch authorization failures to AWS access-denied codes, missing `aws_cloudwatch_metric_alarm` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/cloudwatch` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_cloudwatch_metric_alarm`.

## Backend

- Backend: Loki, Prometheus, Alertmanager, and Grafana.
- Storage and metadata: CloudWatch state lives in `Loki, Prometheus, Alertmanager, and Grafana`; HomePort stores provider identifiers for `aws_cloudwatch_metric_alarm`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Loki, Prometheus, Alertmanager, and Grafana` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: cloudwatch:PutMetricAlarm, cloudwatch:DescribeAlarms, cloudwatch:PutDashboard, cloudwatch:GetDashboard, cloudwatch:DeleteAlarms, cloudwatch:DeleteDashboards.
- Resource: arn:aws:cloudwatch:{region}:{account}:cloudwatch/{id}.
- Context: evaluate CloudWatch calls with tenant/project/account, provider region/location, `arn:aws:cloudwatch:{region}:{account}:cloudwatch/{id}`, source IP, request id, user agent, tags/labels on `aws_cloudwatch_metric_alarm`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed CloudWatch actions, `arn:aws:cloudwatch:{region}:{account}:cloudwatch/{id}` prefix checks, tag/label equality on `aws_cloudwatch_metric_alarm`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/cloudwatch` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: CloudWatch provider names, locations, tags/labels, and request bodies map to HomePort `aws_cloudwatch_metric_alarm` records and `Loki, Prometheus, Alertmanager, and Grafana` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return CloudWatch provider ids, `aws_cloudwatch_metric_alarm` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/cloudwatch` backend auth, missing `aws_cloudwatch_metric_alarm`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/cloudwatch/backend.yaml` for `Loki, Prometheus, Alertmanager, and Grafana` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/cloudwatch/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/cloudwatch/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-cloudwatch.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises PutMetricAlarm -> DescribeAlarms -> PutDashboard -> GetDashboard -> DeleteAlarms -> DeleteDashboards against `/compat/aws/cloudwatch` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_cloudwatch_metric_alarm`, `aws_cloudwatch_log_group`, `aws_cloudwatch_dashboard` from `aws/cloudwatch`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-cloudwatch.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-cloudwatch.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-cloudwatch.yaml`, then promote only when that manifest passes in CI.
