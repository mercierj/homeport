# GCP Translation Compatibility Plan

## Goal

Expose the smallest GCP Translation-compatible surface needed to migrate the ledger resources to `LibreTranslate` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: translate.projects.locations.translateText -> translate.projects.locations.detectLanguage -> translate.projects.locations.batchTranslateText.
- Actions explicitly not supported first: Translation console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `translate.projects.locations.translateText` and its paired read/list calls.
- Ledger resource types: source Translation resource model
- First concrete resource model to add: source Translation resource with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Translation authorization failures to GCP access-denied codes, missing `source Translation resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/translation` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the source Translation resource model.

## Backend

- Backend: LibreTranslate.
- Storage and metadata: Translation state lives in `LibreTranslate`; HomePort stores provider identifiers for `source Translation resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `LibreTranslate` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: translate.projects.locations.translateText -> translate.projects.locations.detectLanguage -> translate.projects.locations.batchTranslateText.
- Resource: projects/{project}/locations/{location}/translation/{id}.
- Context: evaluate Translation calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/translation/{id}`, source IP, request id, user agent, tags/labels on `source Translation resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Translation actions, `projects/{project}/locations/{location}/translation/{id}` prefix checks, tag/label equality on `source Translation resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/translation` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Translation provider names, locations, tags/labels, and request bodies map to HomePort `source Translation resource model` records and `LibreTranslate` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Translation provider ids, `source Translation resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/translation` backend auth, missing `source Translation resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/translation/backend.yaml` for `LibreTranslate` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/translation/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/translation/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-translation.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises translate.projects.locations.translateText -> translate.projects.locations.detectLanguage -> translate.projects.locations.batchTranslateText against `/compat/gcp/translation` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the source Translation resource model from `gcp/translation`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-translation.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-translation.yaml`, then promote only when that manifest passes in CI.
