# AWS ALB Compatibility Plan

## Goal

Expose the smallest AWS ALB-compatible surface needed to migrate the ledger resources to `Traefik` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: elasticloadbalancing:CreateLoadBalancer, elasticloadbalancing:DescribeLoadBalancers, elasticloadbalancing:ModifyLoadBalancerAttributes, elasticloadbalancing:DeleteLoadBalancer.
- Actions explicitly not supported: listeners, rules, target groups, tags, account billing, quota purchase flows, managed cross-region failover, and ALB console-only workflows.
- Ledger resource types: `aws_lb`
- Provider errors: the local adapter returns provider-shaped access-denied, not-found, duplicate-name, validation, quota, and internal-failure codes for the supported surface.
- Pagination/idempotency/tags: `DescribeLoadBalancers` exposes `Marker`/`NextMarker`; this local slice does not persist idempotency keys or tags.

## Backend

- Backend: Traefik.
- Storage and metadata: the local adapter keeps ALB metadata in memory. A deployed Traefik integration must persist provider identifiers, source import ids, authorization bindings, artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Traefik` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: elasticloadbalancing:CreateLoadBalancer, elasticloadbalancing:DescribeLoadBalancers, elasticloadbalancing:ModifyLoadBalancerAttributes, elasticloadbalancing:DeleteLoadBalancer.
- Resource: `arn:aws:elasticloadbalancing:{region}:{account}:loadbalancer/app/{id}`.
- Context: evaluate ALB calls with tenant/project/account, provider region/location, `arn:aws:elasticloadbalancing:{region}:{account}:alb/{id}`, source IP, request id, user agent, tags/labels on `aws_lb`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: the local slice evaluates the listed actions against supplied ARNs (including each ARN requested by `DescribeLoadBalancers`), with source IP, current time, user agent, and principal-attribute context.

## Adapter

- Endpoints exposed: `/compat/aws/alb` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: ALB names, subnets, scheme, type, requested ARNs, and attribute mutations map to local `aws_lb` metadata. Backend-only knobs are omitted from provider responses.
- Response mapping: return ALB ARNs, names, DNS names, scheme, type, active state, modified attributes, and `Marker`/`NextMarker` pagination without exposing backend-only fields.
- Error mapping: translate authorization, missing resource, duplicate name, malformed request, quota, and authorizer failures to provider-shaped error codes.

## Generated Artifacts

- `artifacts/compat/aws/alb/backend.yaml` for `Traefik` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/alb/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/alb/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-alb.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateLoadBalancer -> DescribeLoadBalancers -> ModifyLoadBalancerAttributes -> DeleteLoadBalancer against `/compat/aws/alb`.
- SDK tests prove `PageSize`/`Marker` pagination, denied create audit emission, ARN-scoped describe authorization, optional local quota, POST-only handling, and invalid create-input rejection.
- Live Traefik persistence/delivery, source import, retry semantics, backup restoration, cutover, rollback, and cross-service behavior are not local-adapter evidence.

## Compatibility Level

- Current level: L3 local seed. The AWS SDK v2 endpoint-override contract covers create/describe/modify-attributes/delete, Query-protocol pagination, centralized authorization/audit, and an optional adapter quota.
- Target level: L4 only after a durable Traefik integration and external migration gates are proved.
- Blocking gaps: durable Traefik state, production validation, backup restoration, cutover, and rollback are not proven.
