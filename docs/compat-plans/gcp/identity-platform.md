# GCP Identity Platform Compatibility Plan

## Goal

Expose the smallest GCP Identity Platform-compatible surface needed to migrate the ledger resources to `Keycloak` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: identitytoolkit.projects.getConfig -> identitytoolkit.projects.updateConfig.
- Actions explicitly not supported first: Identity Platform console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `identitytoolkit.projects.getConfig` and its paired read/list calls.
- Ledger resource types: `google_identity_platform_config`
- Provider errors: map Identity Platform authorization failures to GCP access-denied codes, missing `google_identity_platform_config` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/identity-platform` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_identity_platform_config`.

## Backend

- Backend: Keycloak.
- Storage and metadata: Identity Platform state lives in `Keycloak`; HomePort stores provider identifiers for `google_identity_platform_config`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Keycloak` with generated `artifacts/compat/gcp/identity-platform/backend.yaml`, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/identity-platform`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: identitytoolkit.projects.getConfig -> identitytoolkit.projects.updateConfig.
- Resource: projects/{project}/locations/{location}/identity-platform/{id}.
- Context: evaluate Identity Platform calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/identity-platform/{id}`, source IP, request id, user agent, tags/labels on `google_identity_platform_config`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Identity Platform actions, `projects/{project}/locations/{location}/identity-platform/{id}` prefix checks, tag/label equality on `google_identity_platform_config`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/identity-platform` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Identity Platform provider names, locations, tags/labels, and request bodies map to HomePort `google_identity_platform_config` records and `Keycloak` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Identity Platform provider ids, `google_identity_platform_config` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/identity-platform` backend auth, missing `google_identity_platform_config`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/identity-platform/backend.yaml` for `Keycloak` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/identity-platform/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/identity-platform/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-identity-platform.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises identitytoolkit.projects.getConfig -> identitytoolkit.projects.updateConfig against `/compat/gcp/identity-platform` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_identity_platform_config` from `gcp/identity-platform`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-identity-platform.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-identity-platform.yaml`, then promote only when that manifest passes in CI.
