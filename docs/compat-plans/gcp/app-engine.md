# GCP App Engine Compatibility Plan

## Goal

Expose the smallest GCP App Engine-compatible surface needed to migrate the ledger resources to `Docker` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: appengine.apps.get -> appengine.apps.patch -> appengine.apps.services.list.
- Actions explicitly not supported first: App Engine console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `appengine.apps.get` and its paired read/list calls.
- Ledger resource types: `google_app_engine_application`.
- Provider errors: map App Engine authorization failures to GCP access-denied codes, missing `google_app_engine_application` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/app-engine` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_app_engine_application`.

## Backend

- Backend: Docker.
- Storage and metadata: App Engine state lives in `Docker`; HomePort stores provider identifiers for `google_app_engine_application`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Docker` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: appengine.apps.get -> appengine.apps.patch -> appengine.apps.services.list.
- Resource: projects/{project}/locations/{location}/app-engine/{id}.
- Context: evaluate App Engine calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/app-engine/{id}`, source IP, request id, user agent, tags/labels on `google_app_engine_application`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed App Engine actions, `projects/{project}/locations/{location}/app-engine/{id}` prefix checks, tag/label equality on `google_app_engine_application`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/app-engine` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: App Engine provider names, locations, tags/labels, and request bodies map to HomePort `google_app_engine_application` records and `Docker` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return App Engine provider ids, `google_app_engine_application` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/app-engine` backend auth, missing `google_app_engine_application`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/app-engine/backend.yaml` for `Docker` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/app-engine/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/app-engine/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-app-engine.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises appengine.apps.get -> appengine.apps.patch -> appengine.apps.services.list against `/compat/gcp/app-engine` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_app_engine_application` from `gcp/app-engine`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/gcp-app-engine.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/gcp-app-engine.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-app-engine.yaml`, then promote only when that manifest passes in CI.
