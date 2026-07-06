# GCP Secret Manager Compatibility Plan

## Goal

Expose the smallest GCP Secret Manager-compatible surface needed to migrate the ledger resources to `HashiCorp Vault` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: secretmanager.projects.secrets.create -> secretmanager.projects.secrets.get -> secretmanager.projects.secrets.list -> secretmanager.projects.secrets.patch -> secretmanager.projects.secrets.delete; versions.access.
- Actions explicitly not supported first: Secret Manager console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `secretmanager.projects.secrets.create` and its paired read/list calls.
- Ledger resource types: `google_secret_manager_secret`.
- Provider errors: map Secret Manager authorization failures to GCP access-denied codes, missing `google_secret_manager_secret` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/secret-manager` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_secret_manager_secret`.

## Backend

- Backend: HashiCorp Vault.
- Storage and metadata: Secret Manager state lives in `HashiCorp Vault`; HomePort stores provider identifiers for `google_secret_manager_secret`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `HashiCorp Vault` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/secret-manager`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: secretmanager.projects.secrets.create -> secretmanager.projects.secrets.get -> secretmanager.projects.secrets.list -> secretmanager.projects.secrets.patch -> secretmanager.projects.secrets.delete; versions.access.
- Resource: projects/{project}/locations/{location}/secret-manager/{id}.
- Context: evaluate Secret Manager calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/secret-manager/{id}`, source IP, request id, user agent, tags/labels on `google_secret_manager_secret`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Secret Manager actions, `projects/{project}/locations/{location}/secret-manager/{id}` prefix checks, tag/label equality on `google_secret_manager_secret`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/secret-manager` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Secret Manager provider names, locations, tags/labels, and request bodies map to HomePort `google_secret_manager_secret` records and `HashiCorp Vault` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Secret Manager provider ids, `google_secret_manager_secret` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/secret-manager` backend auth, missing `google_secret_manager_secret`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/secret-manager/backend.yaml` for `HashiCorp Vault` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/secret-manager/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/secret-manager/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-secret-manager.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises secretmanager.projects.secrets.create -> secretmanager.projects.secrets.get -> secretmanager.projects.secrets.list -> secretmanager.projects.secrets.patch -> secretmanager.projects.secrets.delete; versions.access against `/compat/gcp/secret-manager` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_secret_manager_secret` from `gcp/secret-manager`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-secret-manager.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-secret-manager.yaml`, then promote only when that manifest passes in CI.
