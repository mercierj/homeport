# GCP Document AI Compatibility Plan

## Goal

Expose the smallest GCP Document AI-compatible surface needed to migrate the ledger resources to `Tesseract OCR` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: documentai.projects.locations.processors.create -> documentai.projects.locations.processors.get -> documentai.projects.locations.processors.list -> documentai.projects.locations.processors.process -> documentai.projects.locations.processors.delete.
- Actions explicitly not supported first: Document AI console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `documentai.projects.locations.processors.create` and its paired read/list calls.
- Ledger resource types: source Document AI resource model
- First concrete resource model to add: source Document AI resource with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Document AI authorization failures to GCP access-denied codes, missing `source Document AI resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `gcp/document-ai` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on the source Document AI resource model.

## Backend

- Backend: Tesseract OCR.
- Storage and metadata: Document AI state lives in `Tesseract OCR`; HomePort stores provider identifiers for `source Document AI resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Tesseract OCR` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from GCP user/role/service account/managed identity/session token.
- Actions: documentai.projects.locations.processors.create -> documentai.projects.locations.processors.get -> documentai.projects.locations.processors.list -> documentai.projects.locations.processors.process -> documentai.projects.locations.processors.delete.
- Resource: projects/{project}/locations/{location}/document-ai/{id}.
- Context: evaluate Document AI calls with tenant/project/account, provider region/location, `projects/{project}/locations/{location}/document-ai/{id}`, source IP, request id, user agent, tags/labels on `source Document AI resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Document AI actions, `projects/{project}/locations/{location}/document-ai/{id}` prefix checks, tag/label equality on `source Document AI resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/gcp/document-ai` for the actions above.
- SDK used in tests: Google Cloud REST client configured with endpoint override and HomePort credentials.
- Request mapping: Document AI provider names, locations, tags/labels, and request bodies map to HomePort `source Document AI resource model` records and `Tesseract OCR` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Document AI provider ids, `source Document AI resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `gcp/document-ai` backend auth, missing `source Document AI resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/gcp/document-ai/backend.yaml` for `Tesseract OCR` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/gcp/document-ai/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/gcp/document-ai/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/gcp-document-ai.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Google Cloud REST client exercises documentai.projects.locations.processors.create -> documentai.projects.locations.processors.get -> documentai.projects.locations.processors.list -> documentai.projects.locations.processors.process -> documentai.projects.locations.processors.delete against `/compat/gcp/document-ai` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the source Document AI resource model from `gcp/document-ai`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/gcp-document-ai.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/gcp-document-ai.yaml`, then promote only when that manifest passes in CI.
