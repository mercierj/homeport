# Provider Compatibility Levels

## L0: Generated Migration

The service has generated infrastructure, runbooks, or application-change instructions only. Existing provider SDK calls are not expected to keep working.

Promotion evidence:

- Generated backend configuration or migration instructions exist.
- Gaps are explicit in the coverage ledger.

## L1: Real Backend Provisioning

HomePort provisions a real open-source, sovereign, or HomePort-owned backend for the service.

Promotion evidence:

- Backend runtime is generated and deployable.
- Storage, networking, credentials, backup, and validation scripts exist.
- No compatibility API is claimed.

## L2: Partial API Adapter

HomePort exposes a provider-shaped API for common calls, but does not claim contractual coverage.

Promotion evidence:

- Adapter exposes documented endpoints.
- Common happy paths work.
- Unsupported actions return explicit errors.
- Coverage plan lists missing provider semantics.

## L3: Contractual SDK Compatibility

The official provider SDK can run against the HomePort endpoint for the supported API surface.

Promotion evidence:

- Contract tests use the official AWS, GCP, or Azure SDK.
- Request and response mappings are tested.
- Provider-like error shapes are tested.
- Pagination, idempotency, tags or labels, and lifecycle operations are either implemented or explicitly unsupported.

## L4: Provider-Grade Compatibility

The adapter behaves like a production provider surface for the supported API area.

Promotion evidence:

- All L3 evidence exists.
- Every adapter action calls `Authorize(principal, action, resource, context)` before execution.
- Fine-grained policy semantics are tested with allow and deny cases.
- Provider-like errors, pagination, idempotency, lifecycle state, quota limits, and audit events are covered.
- Cross-service enforcement is tested, for example IAM policy denies S3 but allows SQS.

## Promotion Rule

Do not promote a service above the weakest proven layer. If the backend is strong but SDK compatibility is untested, the service is L1 or L2, not L3.
