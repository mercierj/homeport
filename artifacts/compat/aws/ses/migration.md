# AWS SES Migration

This is a migration artifact seed for the supported SES identity and email surface to Postal. It is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `aws_ses_domain_identity`: preserve account, region, domain, verification token, DKIM data, policies, tags, and source import id.

## Cutover

1. Import `aws_ses_domain_identity` records with source import ids.
2. Apply `artifacts/compat/aws/ses/backend.yaml` and `adapter.yaml`.
3. Run `test/conformance/services/aws-ses.yaml` before routing traffic to `/compat/aws/ses`.

## Rollback

1. Stop routing traffic to `/compat/aws/ses`.
2. Restore the prior SES endpoint and credentials.
3. Retain source import ids for reconciliation.
