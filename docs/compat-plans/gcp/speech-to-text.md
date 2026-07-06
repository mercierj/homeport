# GCP Speech-to-Text Compatibility Plan

## Goal

Expose the smallest GCP Speech-to-Text-compatible surface needed to migrate the ledger resources to `Whisper` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: speech.recognizers.create -> speech.recognizers.get -> speech.recognizers.list -> speech.recognizers.recognize -> speech.recognizers.batchRecognize -> speech.recognizers.delete.
- Actions explicitly not supported first: Speech-to-Text console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `speech.recognizers.create` and its paired read/list calls.
- Ledger resource types: source Speech-to-Text resource model
- First concrete resource model to add: source Speech-to-Text resource with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Speech-to-Text authorization failures to GCP access-denied codes, missing `source Speech-to-Text resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/speech-to-text` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the source Speech-to-Text resource model.

## Backend

- Backend: Whisper.
- Storage and metadata: Speech-to-Text state lives in `Whisper`; HomePort stores provider identifiers for `source Speech-to-Text resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Whisper` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: speech.recognizers.create -> speech.recognizers.get -> speech.recognizers.list -> speech.recognizers.recognize -> speech.recognizers.batchRecognize -> speech.recognizers.delete.
- Resource: projects/{project}/locations/{location}/speech-to-text/{id}.
- Context: evaluate Speech-to-Text calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/speech-to-text/{id}`, source IP, request id, user agent, tags/labels on `source Speech-to-Text resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Speech-to-Text actions, `projects/{project}/locations/{location}/speech-to-text/{id}` prefix checks, tag/label equality on `source Speech-to-Text resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/speech-to-text` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Speech-to-Text provider names, locations, tags/labels, and request bodies map to HomePort `source Speech-to-Text resource model` records and `Whisper` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Speech-to-Text provider ids, `source Speech-to-Text resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/speech-to-text` backend auth, missing `source Speech-to-Text resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/speech-to-text/backend.yaml` for `Whisper` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/speech-to-text/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/speech-to-text/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-speech-to-text.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises speech.recognizers.create -> speech.recognizers.get -> speech.recognizers.list -> speech.recognizers.recognize -> speech.recognizers.batchRecognize -> speech.recognizers.delete against `/compat/gcp/speech-to-text` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the source Speech-to-Text resource model from `gcp/speech-to-text`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-speech-to-text.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-speech-to-text.yaml`, then promote only when that manifest passes in CI.
