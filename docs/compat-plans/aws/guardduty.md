# AWS GuardDuty Compatibility Plan

## Goal

Expose the smallest AWS GuardDuty-compatible surface needed to migrate the ledger resources to `Wazuh` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: guardduty:CreateDetector, guardduty:GetDetector, guardduty:ListDetectors, guardduty:UpdateDetector, guardduty:DeleteDetector.
- Actions explicitly not supported first: GuardDuty console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `guardduty:CreateDetector` and its paired read/list calls.
- Ledger resource types: `aws_guardduty_detector`.
- Provider errors: map GuardDuty authorization failures to AWS access-denied codes, missing `aws_guardduty_detector` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/guardduty` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_guardduty_detector`.

## Backend

- Backend: Wazuh.
- Storage and metadata: GuardDuty state lives in `Wazuh`; HomePort stores provider identifiers for `aws_guardduty_detector`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Wazuh` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: guardduty:CreateDetector, guardduty:GetDetector, guardduty:ListDetectors, guardduty:UpdateDetector, guardduty:DeleteDetector.
- Resource: arn:aws:guardduty:{region}:{account}:guardduty/{id}.
- Context: evaluate GuardDuty calls with tenant/project/account, provider region/location, `arn:aws:guardduty:{region}:{account}:guardduty/{id}`, source IP, request id, user agent, tags/labels on `aws_guardduty_detector`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed GuardDuty actions, `arn:aws:guardduty:{region}:{account}:guardduty/{id}` prefix checks, tag/label equality on `aws_guardduty_detector`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/guardduty` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: GuardDuty provider names, locations, tags/labels, and request bodies map to HomePort `aws_guardduty_detector` records and `Wazuh` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return GuardDuty provider ids, `aws_guardduty_detector` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/guardduty` backend auth, missing `aws_guardduty_detector`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/guardduty/backend.yaml` for `Wazuh` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/guardduty/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/guardduty/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-guardduty.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateDetector -> GetDetector -> ListDetectors -> UpdateDetector -> DeleteDetector against `/compat/aws/guardduty` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_guardduty_detector` from `aws/guardduty`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-guardduty.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-guardduty.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-guardduty.yaml`, then promote only when that manifest passes in CI.
