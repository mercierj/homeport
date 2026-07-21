# AWS Kinesis Migration

This is a migration artifact seed for moving the supported Kinesis stream surface to Redpanda with the HomePort Kinesis compatibility adapter. It is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `aws_kinesis_stream`: preserve the AWS account, region, stream name, ARN, shard count, retention period, tags, and source import id.

## Unsupported actions

- Console-only Kinesis workflows.
- Account billing and quota purchase flows.
- Managed cross-region failover controls.
- Kinesis resources outside `kinesis:CreateStream`, `kinesis:DescribeStream`, `kinesis:ListStreams`, `kinesis:UpdateShardCount`, and `kinesis:DeleteStream`.

## Operator decisions

- Confirm the target Redpanda deployment and `/compat/aws/kinesis` adapter route.
- Confirm stream import ids before applying changes.
- Confirm unsupported actions are accepted as exclusions for this compatibility slice.
- Confirm backup reference storage before any production cutover.

## Cutover

1. Import `aws_kinesis_stream` records with source import ids.
2. Apply the backend artifact in `artifacts/compat/aws/kinesis/backend.yaml`.
3. Apply the adapter mapping in `artifacts/compat/aws/kinesis/adapter.yaml`.
4. Run the Kinesis compatibility adapter contract check from `test/conformance/services/aws-kinesis.yaml`.
5. Route Kinesis stream traffic to `/compat/aws/kinesis` only after the operator accepts the supported surface and unsupported actions.

## Rollback

1. Stop routing new Kinesis traffic to `/compat/aws/kinesis`.
2. Restore the previous AWS Kinesis endpoint and credentials.
3. Keep imported `aws_kinesis_stream` source ids for audit and reconciliation.
4. Restore Redpanda topic state from the latest validated backup reference when a retry cutover is needed.
