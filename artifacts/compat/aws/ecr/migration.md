# AWS ECR Migration

This is a migration artifact seed for the supported ECR surface to an OCI Distribution registry. The local adapter is in-memory; this is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `aws_ecr_repository`: preserve account, region, repository name, ARN, source import ID, and generated artifact checksum.

## Unsupported actions

- ECR console workflows, account billing, quota purchases, image replication, and managed cross-region failover are not mapped by this adapter.

## Operator decisions

1. Select the OCI Distribution registry endpoint and persistent `ecr-data` volume.
2. Map repository access policy to HomePort authorization bindings.
3. Review repository names and tags before applying generated configuration.

## Cutover

1. Import `aws_ecr_repository` records with their source import IDs.
2. Apply `artifacts/compat/aws/ecr/backend.yaml` and `adapter.yaml`.
3. Run `test/conformance/services/aws-ecr.yaml` before routing compatible calls to `/compat/aws/ecr`.

## Rollback

1. Stop routing traffic to `/compat/aws/ecr`.
2. Restore the prior ECR endpoint and credentials.
3. Retain source import IDs for reconciliation.
