# AWS SQS Migration

This is a migration artifact seed for moving the supported SQS queue surface to RabbitMQ with the HomePort SQS compatibility adapter. It is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `aws_sqs_queue`: preserve the AWS account, region, queue name, URL, ARN, tags, FIFO attributes, redrive policy, and source import id.

## Unsupported actions

- Console-only SQS workflows.
- Account billing and quota purchase flows.
- Managed cross-region failover controls.
- SQS resources outside `sqs:CreateQueue`, `sqs:GetQueueAttributes`, `sqs:SendMessage`, `sqs:ReceiveMessage`, and `sqs:DeleteQueue`.

## Operator decisions

- Confirm the target RabbitMQ cluster and `/compat/aws/sqs` adapter route.
- Confirm queue import ids before applying changes.
- Confirm unsupported actions are accepted as exclusions for this compatibility slice.
- Confirm backup reference storage before any production cutover.

## Cutover

1. Import `aws_sqs_queue` records with source import ids.
2. Apply the backend artifact in `artifacts/compat/aws/sqs/backend.yaml`.
3. Apply the adapter mapping in `artifacts/compat/aws/sqs/adapter.yaml`.
4. Run the SQS compatibility adapter contract check from `test/conformance/services/aws-sqs.yaml`.
5. Route SQS queue traffic to `/compat/aws/sqs` only after the operator accepts the supported surface and unsupported actions.

## Rollback

1. Stop routing new SQS traffic to `/compat/aws/sqs`.
2. Restore the previous AWS SQS endpoint and credentials.
3. Keep imported `aws_sqs_queue` source ids for audit and reconciliation.
4. Restore RabbitMQ queue state from the latest validated backup reference when a retry cutover is needed.
