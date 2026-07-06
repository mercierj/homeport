# <Provider Service> Compatibility Plan

## Goal

Describe the provider-compatible API and the sovereign/open-source backend target.

## Provider API Surface

- Actions supported at the first target level.
- Actions explicitly not supported.
- Provider-like errors.
- Pagination, idempotency, tags or labels, and lifecycle behavior.

## Backend

- Open-source backend, sovereign service, or HomePort implementation.
- Object and metadata storage.
- Secrets, keys, and tokens.
- Runtime and provisioning model.

## Authz Model

- Principal.
- Action.
- Resource.
- Context.
- Policy evaluation.
- Conditions supported.
- Cross-service enforcement.

## Adapter

- Exposed endpoints.
- Official SDK used in tests.
- Request and response mapping.
- Error mapping.

## Generated Artifacts

- Backend configuration.
- Provision, migrate, validate, backup, and cutover scripts.
- Application patch if endpoint/config changes are required.

## Contract Tests

- Official AWS, GCP, or Azure SDK tests.
- Provider fixtures.
- Negative deny/error tests.
- Cross-service tests.

## Compatibility Level

- Target level: L3 or L4.
- Accepted gaps.
- Path to close the gaps.
