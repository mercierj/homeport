# GCP Pub/Sub Compatibility Plan

## Goal

Expose the smallest GCP Pub/Sub-compatible surface needed to migrate the ledger resources to `NATS JetStream` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: pubsub.projects.topics.create -> pubsub.projects.topics.get -> pubsub.projects.topics.list -> pubsub.projects.topics.delete; pubsub.projects.subscriptions.create -> pubsub.projects.subscriptions.get -> pubsub.projects.subscriptions.list -> pubsub.projects.subscriptions.delete.
- Actions explicitly not supported first: Pub/Sub console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `pubsub.projects.topics.create` and its paired read/list calls.
- Ledger resource types: `google_pubsub_topic`, `google_pubsub_subscription`.
- Provider errors: map Pub/Sub authorization failures to GCP access-denied codes, missing `google_pubsub_topic` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/pub-sub` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `google_pubsub_topic` and `google_pubsub_subscription`.

## Backend

- Backend: NATS JetStream.
- Storage and metadata: Pub/Sub state lives in `NATS JetStream`; HomePort stores provider identifiers for `google_pubsub_topic`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `NATS JetStream` with the generated runtime manifest, health endpoint, persistence volume, backup job, endpoint route, and teardown script for `gcp/pub-sub`.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: pubsub.projects.topics.create -> pubsub.projects.topics.get -> pubsub.projects.topics.list -> pubsub.projects.topics.delete; pubsub.projects.subscriptions.create -> pubsub.projects.subscriptions.get -> pubsub.projects.subscriptions.list -> pubsub.projects.subscriptions.delete.
- Resource: projects/{project}/topics/{topic} and projects/{project}/subscriptions/{subscription}.
- Context: evaluate Pub/Sub calls with tenant/project/account, provider region/location, `projects/{project}/topics/{topic} and projects/{project}/subscriptions/{subscription}`, source IP, request id, user agent, tags/labels on `google_pubsub_topic`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Pub/Sub actions, `projects/{project}/topics/{topic} and projects/{project}/subscriptions/{subscription}` prefix checks, tag/label equality on `google_pubsub_topic`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/pub-sub` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Pub/Sub provider names, locations, tags/labels, and request bodies map to HomePort `google_pubsub_topic` records and `NATS JetStream` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Pub/Sub provider ids, `google_pubsub_topic` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/pub-sub` backend auth, missing `google_pubsub_topic`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/pub-sub/backend.yaml` for `NATS JetStream` runtime, network, persistence, health check, and backup policy.
- `artifacts/compat/gcp/pub-sub/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/pub-sub/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-pub-sub.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises pubsub.projects.topics.create -> pubsub.projects.topics.get -> pubsub.projects.topics.list -> pubsub.projects.topics.delete; pubsub.projects.subscriptions.create -> pubsub.projects.subscriptions.get -> pubsub.projects.subscriptions.list -> pubsub.projects.subscriptions.delete against `/compat/gcp/pub-sub` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `google_pubsub_topic`, `google_pubsub_subscription` from `gcp/pub-sub`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L2 - resource mapping exists, but backend selection and provider contract tests are incomplete.
- Target level: L3 after backend artifacts, adapter mappings, and conformance tests are implemented.
- Blocking gaps: no Pub/Sub-compatible local API adapter exists yet; `test/conformance/services/gcp-pub-sub.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-pub-sub.yaml`, then promote only when that manifest passes in CI.
