# GCP Logging Compatibility Plan

## Goal

Expose the smallest GCP Logging-compatible surface needed to migrate the ledger resources to `Loki with OpenTelemetry Collector` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: logging.sinks.create -> logging.sinks.get -> logging.sinks.list -> logging.sinks.update -> logging.sinks.delete; logging.entries.write/list.
- Actions explicitly not supported first: Logging console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `logging.sinks.create` and its paired read/list calls.
- Ledger resource types: source Logging resource model
- First concrete resource model to add: source Logging resource with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Logging authorization failures to GCP access-denied codes, missing `source Logging resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/logging` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the source Logging resource model.

## Backend

- Backend: Loki with OpenTelemetry Collector.
- Storage and metadata: Logging state lives in `Loki with OpenTelemetry Collector`; HomePort stores provider identifiers for `source Logging resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Loki with OpenTelemetry Collector` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/logging`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: logging.sinks.create -> logging.sinks.get -> logging.sinks.list -> logging.sinks.update -> logging.sinks.delete; logging.entries.write/list.
- Resource: projects/{project}/locations/{location}/logging/{id}.
- Context: evaluate Logging calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/logging/{id}`, source IP, request id, user agent, tags/labels on `source Logging resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Logging actions, `projects/{project}/locations/{location}/logging/{id}` prefix checks, tag/label equality on `source Logging resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/logging` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Logging provider names, locations, tags/labels, and request bodies map to HomePort `source Logging resource model` records and `Loki with OpenTelemetry Collector` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Logging provider ids, `source Logging resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/logging` backend auth, missing `source Logging resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/logging/backend.yaml` for `Loki with OpenTelemetry Collector` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/logging/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/logging/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-logging.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises logging.sinks.create -> logging.sinks.get -> logging.sinks.list -> logging.sinks.update -> logging.sinks.delete; logging.entries.write/list against `/compat/gcp/logging` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the source Logging resource model from `gcp/logging`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-logging.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-logging.yaml`, then promote only when that manifest passes in CI.
