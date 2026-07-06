# Azure Speech Compatibility Plan

## Goal

Expose the smallest Azure Speech-compatible surface needed to migrate the ledger resources to `Whisper` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: Microsoft.CognitiveServices/accounts/read, Microsoft.CognitiveServices/accounts/write, Microsoft.CognitiveServices/accounts/delete.
- Actions explicitly not supported first: Speech console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `Microsoft.CognitiveServices/accounts/read` and its paired read/list calls.
- Ledger resource types: source Speech resource model
- First concrete resource model to add: `homeport_azure_speech_resource` with import id, region/location, labels/tags, backend target id, lifecycle state, and owner principal.
- Provider errors: map Speech authorization failures to Azure access-denied codes, missing `source Speech resource model` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `azure/speech` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `homeport_azure_speech_resource`.

## Backend

- Backend: Whisper.
- Storage and metadata: Speech state lives in `Whisper`; HomePort stores provider identifiers for `source Speech resource model`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Whisper` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from Azure user/role/service account/managed identity/session token.
- Actions: Microsoft.CognitiveServices/accounts/read, Microsoft.CognitiveServices/accounts/write, Microsoft.CognitiveServices/accounts/delete.
- Resource: /subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.CognitiveServices/accounts/{name}.
- Context: evaluate Speech calls with tenant/project/account, provider region/location, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.CognitiveServices/accounts/{name}`, source IP, request id, user agent, tags/labels on `source Speech resource model`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Speech actions, `/subscriptions/{subscription}/resourceGroups/{group}/providers/Microsoft.CognitiveServices/accounts/{name}` prefix checks, tag/label equality on `source Speech resource model`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/azure/speech` for the actions above.
- SDK used in tests: Azure SDK for Go or Python configured with endpoint override and HomePort credentials.
- Request mapping: Speech provider names, locations, tags/labels, and request bodies map to HomePort `source Speech resource model` records and `Whisper` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Speech provider ids, `source Speech resource model` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `azure/speech` backend auth, missing `source Speech resource model`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/azure/speech/backend.yaml` for `Whisper` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/azure/speech/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/azure/speech/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/azure-speech.yaml` containing the SDK contract cases listed below.

## Contract Tests

- Azure SDK for Go or Python exercises AccountsGet -> AccountsCreateOrUpdate -> AccountsList -> AccountsDelete against `/compat/azure/speech` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers the new `homeport_azure_speech_resource` model from `azure/speech`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L0 - service is not fully modeled in the ledger yet.
- Target level: L2 after a concrete resource model and backend decision are added.
- Blocking gaps: not modeled yet; `test/conformance/services/azure-speech.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/azure-speech.yaml`, then promote only when that manifest passes in CI.
