# AWS EFS Migration

This is a migration artifact seed for the supported EFS surface to an NFS server. It is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `aws_efs_file_system`: preserve account, region, file-system ID, ARN, creation token, tags, lifecycle policy, and source import id.

## Cutover

1. Import `aws_efs_file_system` records with source import ids.
2. Apply `artifacts/compat/aws/efs/backend.yaml` and `adapter.yaml`.
3. Run `test/conformance/services/aws-efs.yaml` before routing traffic to `/compat/aws/efs`.

## Rollback

1. Stop routing traffic to `/compat/aws/efs`.
2. Restore the prior EFS endpoint and credentials.
3. Retain source import ids for reconciliation.
