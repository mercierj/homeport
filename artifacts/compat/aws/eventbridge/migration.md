# AWS EventBridge Migration

This is a migration artifact seed for the supported EventBridge surface to n8n. It is not live n8n delivery, cutover, or rollback proof.

## Source import IDs

- `aws_cloudwatch_event_rule`: preserve account, region, event bus, rule name, ARN, targets, tags, and source import ID.

## Unsupported actions

- Console workflows, billing, managed cross-region failover, and n8n workflow delivery parity are not mapped by this adapter.

## Operator decisions

1. Select the n8n endpoint and persistent `eventbridge-n8n-data` volume.
2. Map rule tags and targets to the generated n8n workflow configuration.
3. Review generated target endpoints before applying configuration.

## Cutover

1. Export `aws_cloudwatch_event_rule` records with their source import IDs.
2. Review `backend.yaml` and `adapter.yaml` as generated migration inputs.
3. Run `test/conformance/services/aws-eventbridge.yaml` before routing producers to `/compat/aws/eventbridge`.

## Rollback

1. Stop routing traffic to `/compat/aws/eventbridge`.
2. Restore the AWS endpoint and credentials.
3. Retain source import IDs for reconciliation.
