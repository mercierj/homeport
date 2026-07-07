# GCP Apigee Compatibility Plan

## Goal

Expose the smallest GCP Apigee-compatible surface needed to migrate the ledger resources to `Kong` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: apigee.organizations.get -> apigee.organizations.environments.list -> apigee.organizations.apis.list.
- Actions explicitly not supported first: Apigee console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `apigee.organizations.get` and its paired read/list calls.
- Ledger resource types: `google_apigee_organization`.
- Provider errors: map Apigee authorization failures to GCP access-denied codes, missing `google_apigee_organization` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/apigee` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_apigee_organization`.

## Backend

- Backend: Kong.
- Storage and metadata: Apigee state lives in `Kong`; HomePort stores provider identifiers for `google_apigee_organization`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Kong` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: apigee.organizations.get -> apigee.organizations.environments.list -> apigee.organizations.apis.list.
- Resource: projects/{project}/locations/{location}/apigee/{id}.
- Context: evaluate Apigee calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/apigee/{id}`, source IP, request id, user agent, tags/labels on `google_apigee_organization`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Apigee actions, `projects/{project}/locations/{location}/apigee/{id}` prefix checks, tag/label equality on `google_apigee_organization`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/apigee` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Apigee provider names, locations, tags/labels, and request bodies map to HomePort `google_apigee_organization` records and `Kong` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Apigee provider ids, `google_apigee_organization` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/apigee` backend auth, missing `google_apigee_organization`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/apigee/backend.yaml` for `Kong` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/apigee/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/apigee/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-apigee.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises apigee.organizations.get -> apigee.organizations.environments.list -> apigee.organizations.apis.list against `/compat/gcp/apigee` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_apigee_organization` from `gcp/apigee`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/gcp-apigee.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/gcp-apigee.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-apigee.yaml`, then promote only when that manifest passes in CI.
