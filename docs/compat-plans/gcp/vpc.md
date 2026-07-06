# GCP VPC Compatibility Plan

## Goal

Expose the smallest GCP VPC-compatible surface needed to migrate the ledger resources to `Cilium and Linux bridge networking` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: compute.networks.insert -> compute.networks.get -> compute.networks.list -> compute.networks.patch -> compute.networks.delete.
- Actions explicitly not supported first: VPC console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `compute.networks.insert` and its paired read/list calls.
- Ledger resource types: `google_compute_network`.
- Provider errors: map VPC authorization failures to GCP access-denied codes, missing `google_compute_network` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/vpc` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_compute_network`.

## Backend

- Backend: Cilium and Linux bridge networking.
- Storage and metadata: VPC state lives in `Cilium and Linux bridge networking`; HomePort stores provider identifiers for `google_compute_network`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Cilium and Linux bridge networking` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/vpc`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: compute.networks.insert -> compute.networks.get -> compute.networks.list -> compute.networks.patch -> compute.networks.delete.
- Resource: projects/{project}/global/networks/{network}.
- Context: evaluate VPC calls with tenant/project/account, provider region/location, `projects/{project}/global/networks/{network}`, source IP, request id, user agent, tags/labels on `google_compute_network`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed VPC actions, `projects/{project}/global/networks/{network}` prefix checks, tag/label equality on `google_compute_network`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/vpc` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: VPC provider names, locations, tags/labels, and request bodies map to HomePort `google_compute_network` records and `Cilium and Linux bridge networking` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return VPC provider ids, `google_compute_network` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/vpc` backend auth, missing `google_compute_network`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/vpc/backend.yaml` for `Cilium and Linux bridge networking` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/vpc/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/vpc/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-vpc.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises compute.networks.insert -> compute.networks.get -> compute.networks.list -> compute.networks.patch -> compute.networks.delete against `/compat/gcp/vpc` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_compute_network` from `gcp/vpc`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: `test/conformance/services/gcp-vpc.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-vpc.yaml`, then promote only when that manifest passes in CI.
