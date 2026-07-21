# GCP IAM Compatibility Plan

## Goal

Expose the smallest GCP IAM-compatible surface needed to migrate the ledger resources to `Keycloak with OpenFGA` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: cloudresourcemanager.projects.getIamPolicy -> cloudresourcemanager.projects.setIamPolicy -> cloudresourcemanager.projects.testIamPermissions.
- Actions explicitly not supported first: IAM console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `cloudresourcemanager.projects.getIamPolicy` and its paired read/list calls.
- Ledger resource types: `google_project_iam_member`
- Provider errors: map IAM authorization failures to GCP access-denied codes, missing `google_project_iam_member` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/iam` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_project_iam_member`.

## Backend

- Backend: Keycloak.
- Storage and metadata: IAM state lives in `Keycloak with OpenFGA`; HomePort stores provider identifiers for `google_project_iam_member`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Keycloak with OpenFGA` with generated `artifacts/compat/gcp/iam/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/iam`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: cloudresourcemanager.projects.getIamPolicy -> cloudresourcemanager.projects.setIamPolicy -> cloudresourcemanager.projects.testIamPermissions.
- Resource: projects/{project}/locations/{location}/iam/{id}.
- Context: evaluate IAM calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/iam/{id}`, source IP, request id, user agent, tags/labels on `google_project_iam_member`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed IAM actions, `projects/{project}/locations/{location}/iam/{id}` prefix checks, tag/label equality on `google_project_iam_member`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/iam` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: IAM provider names, locations, tags/labels, and request bodies map to HomePort `google_project_iam_member` records and `Keycloak with OpenFGA` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return IAM provider ids, `google_project_iam_member` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/iam` backend auth, missing `google_project_iam_member`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/iam/backend.yaml` for `Keycloak with OpenFGA` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/iam/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/iam/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-iam.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises cloudresourcemanager.projects.getIamPolicy -> cloudresourcemanager.projects.setIamPolicy -> cloudresourcemanager.projects.testIamPermissions against `/compat/gcp/iam` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_project_iam_member` from `gcp/iam`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-iam.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-iam.yaml`, then promote only when that manifest passes in CI.
