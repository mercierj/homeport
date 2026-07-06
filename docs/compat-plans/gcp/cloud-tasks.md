# GCP Cloud Tasks Compatibility Plan

## Goal

Expose the smallest GCP Cloud Tasks-compatible surface needed to migrate the ledger resources to `RabbitMQ delayed jobs` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: cloudtasks.projects.locations.queues.create -> cloudtasks.projects.locations.queues.get -> cloudtasks.projects.locations.queues.list -> cloudtasks.projects.locations.queues.patch -> cloudtasks.projects.locations.queues.delete.
- Actions explicitly not supported first: Cloud Tasks console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `cloudtasks.projects.locations.queues.create` and its paired read/list calls.
- Ledger resource types: `google_cloud_tasks_queue`.
- Provider errors: map Cloud Tasks authorization failures to GCP access-denied codes, missing `google_cloud_tasks_queue` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/cloud-tasks` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_cloud_tasks_queue`.

## Backend

- Backend: RabbitMQ delayed jobs.
- Storage and metadata: Cloud Tasks state lives in `RabbitMQ delayed jobs`; HomePort stores provider identifiers for `google_cloud_tasks_queue`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `RabbitMQ delayed jobs` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/cloud-tasks`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: cloudtasks.projects.locations.queues.create -> cloudtasks.projects.locations.queues.get -> cloudtasks.projects.locations.queues.list -> cloudtasks.projects.locations.queues.patch -> cloudtasks.projects.locations.queues.delete.
- Resource: projects/{project}/locations/{location}/cloud-tasks/{id}.
- Context: evaluate Cloud Tasks calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/cloud-tasks/{id}`, source IP, request id, user agent, tags/labels on `google_cloud_tasks_queue`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Cloud Tasks actions, `projects/{project}/locations/{location}/cloud-tasks/{id}` prefix checks, tag/label equality on `google_cloud_tasks_queue`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/cloud-tasks` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Cloud Tasks provider names, locations, tags/labels, and request bodies map to HomePort `google_cloud_tasks_queue` records and `RabbitMQ delayed jobs` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Cloud Tasks provider ids, `google_cloud_tasks_queue` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/cloud-tasks` backend auth, missing `google_cloud_tasks_queue`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/cloud-tasks/backend.yaml` for `RabbitMQ delayed jobs` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/cloud-tasks/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/cloud-tasks/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-cloud-tasks.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises cloudtasks.projects.locations.queues.create -> cloudtasks.projects.locations.queues.get -> cloudtasks.projects.locations.queues.list -> cloudtasks.projects.locations.queues.patch -> cloudtasks.projects.locations.queues.delete against `/compat/gcp/cloud-tasks` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_cloud_tasks_queue` from `gcp/cloud-tasks`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: no Cloud Tasks-compatible local API adapter exists yet; `test/conformance/services/gcp-cloud-tasks.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-cloud-tasks.yaml`, then promote only when that manifest passes in CI.
