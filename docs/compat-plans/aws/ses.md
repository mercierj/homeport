# AWS SES Compatibility Plan

## Goal

Expose the smallest AWS SES-compatible surface needed to migrate the ledger resources to `Postal with HomePort SES compatibility adapter` without claiming managed-service parity outside the contract tests below.

## Provider API Surface

- Initial supported surface: ses:VerifyDomainIdentity, ses:GetIdentityVerificationAttributes, ses:ListIdentities, ses:DeleteIdentity, ses:VerifyDomainDkim, ses:GetIdentityDkimAttributes, ses:PutIdentityPolicy, ses:ListIdentityPolicies, ses:GetIdentityPolicies, ses:DeleteIdentityPolicy, ses:SendEmail, ses:SendRawEmail, ses:CreateTemplate, ses:GetTemplate, ses:ListTemplates, ses:DeleteTemplate, ses:SendTemplatedEmail, SESv2 CreateEmailIdentity/GetEmailIdentity/ListEmailIdentities/DeleteEmailIdentity, and SESv2 TagResource/ListTagsForResource/UntagResource for email identities.
- Actions explicitly not supported first: SES console-only workflows, account billing, quota purchase flows, receipt rules, and managed cross-region failover controls outside the supported SES identity calls.
- Ledger resource types: `aws_ses_domain_identity`
- Provider errors: SES now maps authorization failures to `AccessDenied`, invalid list pagination parameters to `InvalidParameterValue`, and identity quota failures to `LimitExceeded`; missing-resource, duplicate/conflict, backend timeout, and dependency-failure shapes are still needed.
- Pagination/idempotency/tags: `ListIdentities` exposes `MaxItems`/`NextToken` pagination and rejects malformed tokens and invalid limits; `VerifyDomainIdentity` reuses the existing verification token for repeated domain verification. SESv2 create-time and resource tag mutation round-trip for email identities is covered.

## Backend

- Backend: Postal with HomePort SES compatibility adapter.
- Storage and metadata: SES state lives in `Postal with HomePort SES compatibility adapter`; HomePort stores provider identifiers for `aws_ses_domain_identity`, source import ids, authz bindings, generated artifact checksums, backup references, and audit events.
- Secrets/keys/tokens: issue HomePort-scoped credentials from the identity/secrets layer; store provider source credentials only as encrypted migration inputs.
- Runtime/provisioning: provision `Postal with HomePort SES compatibility adapter` with generated Compose/Kubernetes/OpenTofu, health checks, backup hooks, endpoint routing, and teardown scripts.

## Authz Model

- Principal: HomePort subject mapped from AWS user/role/service account/managed identity/session token.
- Actions: ses:VerifyDomainIdentity, ses:GetIdentityVerificationAttributes, ses:ListIdentities, ses:DeleteIdentity, ses:VerifyDomainDkim, ses:GetIdentityDkimAttributes, ses:PutIdentityPolicy, ses:ListIdentityPolicies, ses:GetIdentityPolicies, ses:DeleteIdentityPolicy, ses:SendEmail, ses:SendRawEmail, ses:CreateTemplate, ses:GetTemplate, ses:ListTemplates, ses:DeleteTemplate, ses:SendTemplatedEmail.
- Resource: arn:aws:ses:{region}:{account}:identity/{id}.
- Context: evaluate SES calls with tenant/project/account, provider region/location, `arn:aws:ses:{region}:{account}:identity/{id}`, source IP, request id, user agent, tags/labels on `aws_ses_domain_identity`, credential age, and MFA/managed-identity claims when the source provider supplies them.
- Evaluation: call `Authorize(principal, action, resource, context)` before each mutating operation and each data-plane read/write.
- Conditions: support exact/wildcard matches for the listed SES actions, `arn:aws:ses:{region}:{account}:identity/{id}` prefix checks, tag/label equality on `aws_ses_domain_identity`, requested region/location, source IP CIDR, time window, and principal attributes.

## Adapter

- Endpoints exposed: `/compat/aws/ses` for the actions above.
- SDK used in tests: AWS SDK for Go v2 configured with endpoint override and HomePort credentials.
- Request mapping: SES provider names, locations, tags/labels, and request bodies map to HomePort `aws_ses_domain_identity` records and `Postal with HomePort SES compatibility adapter` configuration; backend-only knobs are omitted from provider responses.
- Response mapping: return SES provider ids, `aws_ses_domain_identity` lifecycle state, operation ids, etags/versions where the source API exposes them, list pagination tokens, and HomePort audit timestamps without exposing backend-only fields.
- Error mapping: translate `aws/ses` backend auth, missing `aws_ses_domain_identity`, duplicate import, malformed request, timeout, quota, and dependency failures to the provider-shaped access-denied/not-found/conflict/validation/throttle/internal-error responses with retry hints.

## Generated Artifacts

- `artifacts/compat/aws/ses/backend.yaml` for `Postal with HomePort SES compatibility adapter` configuration, network, persistence, health checks, and backup policy.
- `artifacts/compat/aws/ses/adapter.yaml` for endpoint routes, authz action/resource mappings, error mappings, pagination/idempotency settings, and quota defaults.
- `artifacts/compat/aws/ses/migration.md` with source import ids, unsupported actions, operator decisions, rollback, and cutover steps.
- `test/conformance/services/aws-ses.yaml` containing the SDK contract cases listed below.

## Contract Tests

- AWS SDK for Go v2 exercises VerifyDomainIdentity -> GetIdentityVerificationAttributes -> ListIdentities -> DeleteIdentity against `/compat/aws/ses`; AWS CLI exercises the same lifecycle through `--endpoint-url`; Terraform applies and destroys `aws_ses_domain_identity` through an AWS provider SES endpoint override.
- AWS SDK for Go v2 covers SES v1 domain DKIM token generation and `GetIdentityDkimAttributes` read-back for domain identities.
- AWS SDK for Go v2 covers SES v1 identity policy put/list/get/delete round-trip for verified domain identities.
- AWS SDK for Go v2 covers SES v1 `SendEmail` acceptance for verified source domains and `MessageRejected` for unverified source domains.
- AWS SDK for Go v2 covers SES v1 `SendRawEmail` acceptance for verified source domains and `MessageRejected` for unverified source domains.
- AWS SDK for Go v2 covers SES v1 `CreateTemplate`, paged `ListTemplates`, `GetTemplate`, `UpdateTemplate`, `TestRenderTemplate`, and `DeleteTemplate` storage/render lifecycle plus `TemplateDoesNotExist` after deletion.
- AWS SDK for Go v2 covers SES v1 `SendTemplatedEmail` acceptance for verified source domains, `TemplateDoesNotExist` for missing templates, and `MessageRejected` for unverified source domains.
- AWS SDK for Go v2 covers SES v1 `SendBulkTemplatedEmail` acceptance for verified source domains, ordered per-destination success statuses, `TemplateDoesNotExist` for missing templates, and `MessageRejected` for unverified source domains.
- AWS SDK for Go v2 covers SESv2 `CreateEmailIdentity`, `GetEmailIdentity`, `ListEmailIdentities`, `DeleteEmailIdentity`, and identity tag mutation/listing through `TagResource`, `ListTagsForResource`, and `UntagResource`.
- AWS SDK for Go v2 covers denied `VerifyDomainIdentity`, `GetIdentityVerificationAttributes`, `ListIdentities`, `DeleteIdentity`, `VerifyDomainDkim`, `GetIdentityDkimAttributes`, `SendEmail`, `SendRawEmail`, `CreateTemplate`, `GetTemplate`, `ListTemplates`, `UpdateTemplate`, `DeleteTemplate`, `TestRenderTemplate`, `SendTemplatedEmail`, and `SendBulkTemplatedEmail` calls through the shared authorizer and audit sink.
- AWS SDK for Go v2 covers `ListIdentities` `MaxItems`/`NextToken` pagination, malformed token and invalid limit rejection, per-identity batch read authorization, and configurable identity quota `LimitExceeded` responses.
- Fixture import for `aws_ses_domain_identity` from `aws/ses` is still needed.
- Negative cases still needed: missing resource, malformed request beyond covered invalid pagination, duplicate/conflict if applicable, expired credential, backend timeout, and dependency failure.
- Cross-service authz/audit cases and real provider credential extraction are still needed.

## Compatibility Level

- Current level: L3 seed - official AWS SDK for Go, AWS CLI, and Terraform endpoint-override lifecycle checks pass for the supported SES v1 identity surface and SESv2 identity/tag surface, with SDK-covered DKIM attributes, identity policy lifecycle, template create/list/get/update/render/delete lifecycle, formatted/raw/templated/bulk-templated email send acceptance, pagination, quota, and per-action authz/audit seeds.
- Target level: L4 after expanded runnable contracts cover provider errors, pagination/idempotency decisions, authz, quota, audit, real Postal persistence, backup, validate, cutover, and rollback.
- Blocking gaps: `test/conformance/services/aws-ses.yaml` still does not prove real Postal persistence/delivery, boto3 or other non-Go SDKs, backup, validate, cutover, or rollback.
- Path to close gaps: add the missing backend, official-tool, artifact, backup, validate, cutover, and rollback checks, then promote only when those contracts pass in CI.
