# AWS SES Compatibility Plan

## Goal

Expose the smallest AWS SES-compatible surface needed to migrate the ledger resources to `Postal` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: ses:CreateEmailIdentity, ses:GetEmailIdentity, ses:ListEmailIdentities, ses:DeleteEmailIdentity.
- Actions explicitly not supported first: SES console-only workflows, commercial billing/quota administration, provider-managed fleet automation, and cross-region control-plane features outside `ses:CreateEmailIdentity` and its paired read/list calls.
- Ledger resource types: `aws_ses_domain_identity`.
- Provider errors: map SES authorization failures to AWS access-denied codes, missing `aws_ses_domain_identity` records to not-found codes, duplicate imports to conflict/already-exists, invalid mapped fields to validation errors, backend saturation to throttle/quota responses, and unexpected `aws/ses` failures to provider internal-error shapes with request ids.
- Pagination/idempotency/tags: list/read calls expose provider tokens where the API has them; mutating calls persist idempotency keys or operation ids; tags/labels round-trip on `aws_ses_domain_identity`.

## Backend

- Backend: Postal.
- Storage and metadata: SES state lives in `Postal`; HomePort stores provider identifiers for `aws_ses_domain_identity`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Postal` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: ses:CreateEmailIdentity, ses:GetEmailIdentity, ses:ListEmailIdentities, ses:DeleteEmailIdentity.
- Resource: arn:aws:ses:{region}:{account}:ses/{id}.
- Context: evaluate SES calls with tenant/project/account, provider region/location, `arn:aws:ses:{region}:{account}:ses/{id}`, source IP, request id, user agent, tags/labels on `aws_ses_domain_identity`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed SES actions, `arn:aws:ses:{region}:{account}:ses/{id}` prefix checks, tag/label equality on `aws_ses_domain_identity`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/ses` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: SES provider names, locations, tags/labels, and request bodies map to HomePort `aws_ses_domain_identity` records and `Postal` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return SES provider ids, `aws_ses_domain_identity` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/ses` backend auth, missing `aws_ses_domain_identity`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider error families above with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/ses/backend.yaml` for `Postal` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/ses/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/ses/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-ses.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises CreateEmailIdentity -> GetEmailIdentity -> ListEmailIdentities -> DeleteEmailIdentity against `/compat/aws/ses` and asserts provider-shaped request, response, error, authz, retry, and pagination behavior.
- Fixture import covers `aws_ses_domain_identity` from `aws/ses`.
- Negative cases: denied principal, missing resource, malformed request, duplicate/conflict, expired credential, backend timeout, and quota/throttle.
- Cross-service case: one allowed and one denied call pass through the central authorization engine and emit audit events.

## Compatibility Level

- Current level: L3 - ledger migration path is complete; provider SDK/REST conformance still blocks L4.
- Target level: L4 after `test/conformance/services/aws-ses.yaml` passes in CI.
- Blocking gaps: `test/conformance/services/aws-ses.yaml` must prove provider error, pagination, idempotency, authz, quota, and audit behavior before promotion.
- Path to close gaps: generate backend artifacts, implement the endpoint mapping above, add `test/conformance/services/aws-ses.yaml`, then promote only when that manifest passes in CI.
