# AWS App Mesh Compatibility Plan

## Goal

Expose the smallest AWS App Mesh-compatible surface needed to migrate the ledger resources to `Istio` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: appmesh:CreateMesh, appmesh:DescribeMesh, appmesh:ListMeshes, appmesh:UpdateMesh, appmesh:DeleteMesh.
- Actions explicitly not supported first: App Mesh console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `appmesh:CreateMesh` and its paired read/list calls.
- Ledger resource types: `aws_appmesh_mesh`.
- Provider errors: map App Mesh authorization failures to AWS access-denied codes, missing `aws_appmesh_mesh` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/app-mesh` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_appmesh_mesh`.

## Backend

- Backend: Istio.
- Storage and metadata: App Mesh state lives in `Istio`; HomePort stores provider identifiers for `aws_appmesh_mesh`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Istio` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: appmesh:CreateMesh, appmesh:DescribeMesh, appmesh:ListMeshes, appmesh:UpdateMesh, appmesh:DeleteMesh.
- Resource: arn:aws:appmesh:{region}:{account}:app-mesh/{id}.
- Context: evaluate App Mesh calls with tenant/project/account, provider region/location, `arn:aws:appmesh:{region}:{account}:app-mesh/{id}`, source IP, request id, user agent, tags/labels on `aws_appmesh_mesh`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed App Mesh actions, `arn:aws:appmesh:{region}:{account}:app-mesh/{id}` prefix checks, tag/label equality on `aws_appmesh_mesh`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/app-mesh` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: App Mesh provider names, locations, tags/labels, and request bodies map to HomePort `aws_appmesh_mesh` records and `Istio` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return App Mesh provider ids, `aws_appmesh_mesh` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/app-mesh` backend auth, missing `aws_appmesh_mesh`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/app-mesh/backend.yaml` for `Istio` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/app-mesh/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/app-mesh/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-app-mesh.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateMesh -> DescribeMesh -> ListMeshes -> UpdateMesh -> DeleteMesh against `/compat/aws/app-mesh` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_appmesh_mesh` from `aws/app-mesh`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-app-mesh.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-app-mesh.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-app-mesh.yaml`, then promote only when that manifest passes in CI.
