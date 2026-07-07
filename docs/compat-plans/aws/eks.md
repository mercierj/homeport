# AWS EKS Compatibility Plan

## Goal

Expose the smallest AWS EKS-compatible surface needed to migrate the ledger resources to `K3s` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: eks:CreateCluster, eks:DescribeCluster, eks:ListClusters, eks:UpdateClusterConfig, eks:DeleteCluster.
- Actions explicitly not supported first: EKS console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `eks:CreateCluster` and its paired read/list calls.
- Ledger resource types: `aws_eks_cluster`.
- Provider errors: map EKS authorization failures to AWS access-denied codes, missing `aws_eks_cluster` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/eks` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_eks_cluster`.

## Backend

- Backend: K3s.
- Storage and metadata: EKS state lives in `K3s`; HomePort stores provider identifiers for `aws_eks_cluster`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `K3s` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: eks:CreateCluster, eks:DescribeCluster, eks:ListClusters, eks:UpdateClusterConfig, eks:DeleteCluster.
- Resource: arn:aws:eks:{region}:{account}:eks/{id}.
- Context: evaluate EKS calls with tenant/project/account, provider region/location, `arn:aws:eks:{region}:{account}:eks/{id}`, source IP, request id, user agent, tags/labels on `aws_eks_cluster`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed EKS actions, `arn:aws:eks:{region}:{account}:eks/{id}` prefix checks, tag/label equality on `aws_eks_cluster`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/eks` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: EKS provider names, locations, tags/labels, and request bodies map to HomePort `aws_eks_cluster` records and `K3s` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return EKS provider ids, `aws_eks_cluster` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/eks` backend auth, missing `aws_eks_cluster`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/eks/backend.yaml` for `K3s` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/eks/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/eks/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-eks.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateCluster -> DescribeCluster -> ListClusters -> UpdateClusterConfig -> DeleteCluster against `/compat/aws/eks` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_eks_cluster` from `aws/eks`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-eks.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-eks.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-eks.yaml`, then promote only when that manifest passes in CI.
