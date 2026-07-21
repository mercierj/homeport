# AWS DynamoDB Migration

This is a migration artifact seed for the supported DynamoDB surface to ScyllaDB Alternator. It is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `aws_dynamodb_table`: preserve account, region, table name, ARN, key schema, tags, and source import id.

## Cutover

1. Import `aws_dynamodb_table` records with source import ids.
2. Apply `artifacts/compat/aws/dynamodb/backend.yaml` and `adapter.yaml`.
3. Run `test/conformance/services/aws-dynamodb.yaml` before routing traffic to `/compat/aws/dynamodb`.

## Rollback

1. Stop routing traffic to `/compat/aws/dynamodb`.
2. Restore the prior DynamoDB endpoint and credentials.
3. Retain source import ids for reconciliation.
