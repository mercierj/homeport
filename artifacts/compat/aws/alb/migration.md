# AWS ALB Migration

This is a migration artifact seed for the supported ALB management surface to Traefik. The local adapter is in-memory; it does not prove a deployed Traefik backend, backup restoration, cutover, or rollback.

## Source import IDs

- `aws_lb`: preserve account, region, load-balancer name, ARN, source import ID, and generated artifact checksum.

## Supported local API surface

- `CreateLoadBalancer`, `DescribeLoadBalancers`, `ModifyLoadBalancerAttributes`, and `DeleteLoadBalancer` use the AWS Query protocol through `/compat/aws/alb`.
- The adapter checks authorization, emits audit decisions, paginates describe results, and applies an optional local quota.

## Cutover

1. Import `aws_lb` records with their source import IDs.
2. Apply `backend.yaml` and `adapter.yaml` to a real Traefik environment.
3. Run `test/conformance/services/aws-alb.yaml` before routing compatible calls to `/compat/aws/alb`.

## Rollback

1. Stop routing traffic to `/compat/aws/alb`.
2. Restore the prior ALB endpoint and credentials.
3. Retain source import IDs for reconciliation.
