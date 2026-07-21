# AWS IAM Migration

This is a migration artifact seed for the supported IAM role and policy surface to Keycloak. It is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `aws_iam_role`: preserve account, role name, ARN, trust policy, attached policies, tags, and source import id.

## Cutover

1. Import `aws_iam_role` records with source import ids.
2. Apply `artifacts/compat/aws/iam/backend.yaml` and `adapter.yaml`.
3. Run `test/conformance/services/aws-iam.yaml` before routing traffic to `/compat/aws/iam`.

## Rollback

1. Stop routing traffic to `/compat/aws/iam`.
2. Restore the prior IAM endpoint and credentials.
3. Retain source import ids for reconciliation.
