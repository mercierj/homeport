# AWS Translate Compatibility Plan

## Goal

Expose the smallest AWS Translate-compatible surface needed to migrate the ledger resources to `LibreTranslate` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: translate:TranslateText, translate:StartTextTranslationJob, translate:DescribeTextTranslationJob, translate:ListTextTranslationJobs.
- Actions explicitly not supported first: Translate console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `translate:TranslateText` and its paired read/list calls.
- Ledger resource types: `aws_translate_text`.
- Provider errors: map Translate authorization failures to AWS access-denied codes, missing `aws_translate_text` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/translate` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_translate_text`.

## Backend

- Backend: LibreTranslate.
- Storage and metadata: Translate state lives in `LibreTranslate`; HomePort stores provider identifiers for `aws_translate_text`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `LibreTranslate` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: translate:TranslateText, translate:StartTextTranslationJob, translate:DescribeTextTranslationJob, translate:ListTextTranslationJobs.
- Resource: arn:aws:translate:{region}:{account}:translate/{id}.
- Context: evaluate Translate calls with tenant/project/account, provider region/location, `arn:aws:translate:{region}:{account}:translate/{id}`, source IP, request id, user agent, tags/labels on `aws_translate_text`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed Translate actions, `arn:aws:translate:{region}:{account}:translate/{id}` prefix checks, tag/label equality on `aws_translate_text`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/translate` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: Translate provider names, locations, tags/labels, and request bodies map to HomePort `aws_translate_text` records and `LibreTranslate` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return Translate provider ids, `aws_translate_text` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/translate` backend auth, missing `aws_translate_text`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/translate/backend.yaml` for `LibreTranslate` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/translate/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/translate/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-translate.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises TranslateText -> StartTextTranslationJob -> DescribeTextTranslationJob -> ListTextTranslationJobs against `/compat/aws/translate` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_translate_text` from `aws/translate`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-translate.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-translate.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-translate.yaml`, then promote only when that manifest passes in CI.
