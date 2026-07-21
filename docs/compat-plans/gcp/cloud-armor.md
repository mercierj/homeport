# GCP Cloud Armor Compatibility Plan

## Goal

Expose the smallest GCP Cloud Armor-compatible surface needed to migrate the ledger resources to `ModSecurity CRS with nginx` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: compute.securityPolicies.insert -> compute.securityPolicies.get -> compute.securityPolicies.list -> compute.securityPolicies.patch -> compute.securityPolicies.delete.
- Actions explicitly not supported first: Cloud Armor console-only workflows, account billing, quota purchase flows, and managed cross-region failover controls outside `compute.securityPolicies.insert` and its paired read/list calls.
- Ledger resource types: `google_compute_security_policy`
- Provider errors: map Cloud Armor authorization failures to GCP access-denied codes, missing `google_compute_security_policy` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/cloud-armor` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_compute_security_policy`.

## Backend

- Backend: ModSecurity CRS with nginx.
- Storage and metadata: Cloud Armor state lives in `ModSecurity CRS with nginx`; HomePort stores provider identifiers for `google_compute_security_policy`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `ModSecurity CRS with nginx` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: compute.securityPolicies.insert -> compute.securityPolicies.get -> compute.securityPolicies.list -> compute.securityPolicies.patch -> compute.securityPolicies.delete.
- Resource: projects/{project}/global/securityPolicies/{policy}.
- Context: evaluate Cloud Armor calls with tenant/project/account, provider region/location, `projects/{project}/global/securityPolicies/{policy}`, source IP, request id, user agent, tags/labels on `google_compute_security_policy`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cloud Armor actions, `projects/{project}/global/securityPolicies/{policy}` prefix checks, tag/label equality on `google_compute_security_policy`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/cloud-armor` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Cloud Armor provider names, locations, tags/labels, and request bodies map to HomePort `google_compute_security_policy` records and `ModSecurity CRS with nginx` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cloud Armor provider ids, `google_compute_security_policy` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/cloud-armor` backend auth, missing `google_compute_security_policy`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/cloud-armor/backend.yaml` for `ModSecurity CRS with nginx` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/cloud-armor/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/cloud-armor/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-cloud-armor.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises compute.securityPolicies.insert -> compute.securityPolicies.get -> compute.securityPolicies.list -> compute.securityPolicies.patch -> compute.securityPolicies.delete against `/compat/gcp/cloud-armor` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_compute_security_policy` from `gcp/cloud-armor`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/gcp-cloud-armor.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/gcp-cloud-armor.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-cloud-armor.yaml`, then promote only when that manifest passes in CI.
