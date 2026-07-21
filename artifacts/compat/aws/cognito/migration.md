# AWS Cognito Migration

This is a migration artifact seed for the supported Cognito surface to Keycloak. It is not live Keycloak persistence, cutover, or rollback proof.

## Source import IDs

- `aws_cognito_user_pool`: preserve account, region, user-pool ID, ARN, tags, clients, domains, users, groups, and source import ID.

## Unsupported actions

- Cognito console workflows, billing, managed cross-region failover, hosted UI configuration, and full password/MFA delivery parity are not mapped by this adapter.

## Operator decisions

1. Select the Keycloak endpoint and persistent `cognito-keycloak-data` volume.
2. Map user-pool tags and client settings to Keycloak realm configuration.
3. Review generated users and groups before applying configuration.

## Cutover

1. Export each `aws_cognito_user_pool` with its source import ID.
2. Apply `backend.yaml`, then import the generated Keycloak realm and users.
3. Run `test/conformance/services/aws-cognito.yaml` before routing clients to `/compat/aws/cognito`.

## Rollback

1. Stop routing traffic to `/compat/aws/cognito`.
2. Restore the AWS endpoint and credentials.
3. Retain source import IDs for reconciliation.
