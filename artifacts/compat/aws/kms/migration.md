# AWS KMS Migration

This is a migration artifact seed for moving the supported KMS key surface to Vault Transit with the HomePort KMS compatibility adapter. It is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `aws_kms_key`: preserve the AWS account, region, key id, ARN, key policy reference, tags, key usage, origin, and source import id.

## Unsupported actions

- Console-only KMS workflows.
- Account billing and quota purchase flows.
- Managed cross-region key replication and failover controls.
- KMS resources outside `kms:CreateKey`, `kms:DescribeKey`, `kms:ListKeys`, and `kms:ScheduleKeyDeletion`.

## Operator decisions

- Confirm the target Vault Transit deployment and `/compat/aws/kms` adapter route.
- Confirm key import ids before applying changes.
- Confirm unsupported actions are accepted as exclusions for this compatibility slice.
- Confirm backup reference storage before any production cutover.

## Cutover

1. Import `aws_kms_key` records with source import ids.
2. Apply the backend artifact in `artifacts/compat/aws/kms/backend.yaml`.
3. Apply the adapter mapping in `artifacts/compat/aws/kms/adapter.yaml`.
4. Run the KMS compatibility adapter contract check from `test/conformance/services/aws-kms.yaml`.
5. Route KMS key traffic to `/compat/aws/kms` only after the operator accepts the supported surface and unsupported actions.

## Rollback

1. Stop routing new KMS traffic to `/compat/aws/kms`.
2. Restore the previous AWS KMS endpoint and credentials.
3. Keep imported `aws_kms_key` source ids for audit and reconciliation.
4. Restore Vault Transit state from the latest validated backup reference when a retry cutover is needed.
