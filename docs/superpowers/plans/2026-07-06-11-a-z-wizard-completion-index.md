# HomePort A To Z Wizard Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the remaining work so the web multistep migration wizard can run a full migration from source analysis to validated cutover without hidden dead ends.

**Architecture:** Keep the existing React wizard and Go handlers. Close the gaps with the smallest durable contracts: a persisted wizard session, encrypted bundle secrets, cloud deployment jobs, bundle-derived cutover data, and final browser/build acceptance checks.

**Tech Stack:** Go API/app/domain packages, React + Zustand + TanStack Query, existing runbook/cutover/deploy/migrate services, Terraform CLI when present, Playwright for final browser verification.

---

## `/goal` Handoff

Use this as the next agent objective:

```text
/goal Execute the remaining HomePort A-to-Z migration wizard plans in order:
docs/superpowers/plans/2026-07-06-12-wizard-session-contract.md,
docs/superpowers/plans/2026-07-06-13-secret-persistence.md,
docs/superpowers/plans/2026-07-06-14-cloud-deploy-and-export.md,
docs/superpowers/plans/2026-07-06-15-cutover-from-bundle.md,
docs/superpowers/plans/2026-07-06-16-final-acceptance-build-e2e.md.
After each plan, run its verification commands, commit, then continue. Do not claim A-to-Z readiness until Plan 16 passes.
```

## Execution Order

1. `2026-07-06-12-wizard-session-contract.md`
2. `2026-07-06-13-secret-persistence.md`
3. `2026-07-06-14-cloud-deploy-and-export.md`
4. `2026-07-06-15-cutover-from-bundle.md`
5. `2026-07-06-16-final-acceptance-build-e2e.md`

## Done Means

- Analyze or upload creates one persisted wizard session.
- The session records selected resources, bundle ID, secrets state, deployment target, runbook state, sync state, and cutover state.
- Required secrets are persisted encrypted or explicitly absent; process restart does not mark them resolved by accident.
- Local/SSH deploy, cloud Terraform ZIP export, and cloud Terraform apply paths are all explicit in the UI.
- Cutover suggestions come from bundle/deployment outputs instead of an empty `buildFromManifest()`.
- The wizard cannot finish while required runbook/cutover validation is incomplete.
- `npm run build`, Go tests, and at least one browser A-to-Z smoke test pass in a documented environment.

## Existing Plans Still Required For True Service Coverage

These new plans finish the product flow. They do not magically make every cloud service `Full`. For full per-service migration coverage, execute the existing service plans too:

- `2026-07-05-01-coverage-ledger.md`
- `2026-07-05-03-api-compat-gateway.md`
- `2026-07-05-04-storage.md`
- `2026-07-05-05-database-cache.md`
- `2026-07-05-06-messaging-events.md`
- `2026-07-05-07-identity-secrets-keys-certs.md`
- `2026-07-05-08-compute-runtime.md`
- `2026-07-05-09-networking-observability.md`
- `2026-07-05-10-provider-gaps-expansion.md`

