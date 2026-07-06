# AWS Transcribe Compatibility Plan

## Goal

Expose the smallest AWS Transcribe-compatible surface needed to migrate the ledger resources to `Whisper` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: transcribe:StartTranscriptionJob, transcribe:GetTranscriptionJob, transcribe:ListTranscriptionJobs, transcribe:DeleteTranscriptionJob.
- Actions explicitly not supported first: Transcribe console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `transcribe:StartTranscriptionJob` and its paired read/list calls.
- Ledger resource types: `aws_transcribe_vocabulary`.
- Provider errors: map Transcribe authorization failures to AWS access-denied codes, missing `aws_transcribe_vocabulary` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/transcribe` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_transcribe_vocabulary`.

## Backend

- Backend: Whisper.
- Storage and metadata: Transcribe state lives in `Whisper`; HomePort stores provider identifiers for `aws_transcribe_vocabulary`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Whisper` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: transcribe:StartTranscriptionJob, transcribe:GetTranscriptionJob, transcribe:ListTranscriptionJobs, transcribe:DeleteTranscriptionJob.
- Resource: arn:aws:transcribe:{region}:{account}:transcribe/{id}.
- Context: evaluate Transcribe calls with tenant/project/account, provider region/location, `arn:aws:transcribe:{region}:{account}:transcribe/{id}`, source IP, request id, user agent, tags/labels on `aws_transcribe_vocabulary`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Transcribe actions, `arn:aws:transcribe:{region}:{account}:transcribe/{id}` prefix checks, tag/label equality on `aws_transcribe_vocabulary`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/transcribe` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Transcribe provider names, locations, tags/labels, and request bodies map to HomePort `aws_transcribe_vocabulary` records and `Whisper` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Transcribe provider ids, `aws_transcribe_vocabulary` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/transcribe` backend auth, missing `aws_transcribe_vocabulary`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/transcribe/backend.yaml` for `Whisper` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/transcribe/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/transcribe/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-transcribe.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises StartTranscriptionJob -> GetTranscriptionJob -> ListTranscriptionJobs -> DeleteTranscriptionJob against `/compat/aws/transcribe` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_transcribe_vocabulary` from `aws/transcribe`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-transcribe.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-transcribe.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-transcribe.yaml`, then promote only when that manifest passes in CI.
