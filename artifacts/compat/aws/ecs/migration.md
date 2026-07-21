# AWS ECS Migration

This is a migration artifact seed for the supported ECS service surface to Docker Compose. It is not live backend, backup, cutover, or rollback proof.

## Source import IDs

- `aws_ecs_service`: preserve account, region, cluster, service name, ARN, task definition, tags, and source import id.

## Unsupported actions

- ECS workflows outside the adapter's supported service and task-definition surface.

## Cutover

1. Import `aws_ecs_service` records with source import ids.
2. Apply `artifacts/compat/aws/ecs/backend.yaml` and `adapter.yaml`.
3. Run `test/conformance/services/aws-ecs.yaml` before routing traffic to `/compat/aws/ecs`.

## Rollback

1. Stop routing traffic to `/compat/aws/ecs`.
2. Restore the prior ECS endpoint and credentials.
3. Retain source import ids for reconciliation.
