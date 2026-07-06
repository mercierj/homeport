# HomePort Runbook Orchestrator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace markdown/manual migration notes with executable, resumable, validated multi-step migration runbooks.

**Architecture:** Add a runbook domain model used by CLI, API, and web UI. Existing mappers/executors emit steps; the orchestrator executes steps, stores state, validates outputs, and blocks completion until every required step passes.

**Tech Stack:** Go domain/app/API handlers, existing datamigration service, React wizard.

---

## Files

- Create: `internal/domain/runbook/runbook.go`
- Create: `internal/domain/runbook/runbook_test.go`
- Create: `internal/app/runbook/service.go`
- Create: `internal/app/runbook/service_test.go`
- Create: `internal/api/handlers/runbook.go`
- Modify: `internal/api/server.go`
- Modify: `web/src/components/MigrationWizard/steps/*`
- Create: `web/src/lib/runbook-api.ts`

## Task 1: Define executable steps

- [ ] Create `StepType`: `input`, `command`, `api_call`, `dns_check`, `health_check`, `data_verify`, `approval`, `rollback`.
- [ ] Create `StepStatus`: `pending`, `running`, `passed`, `failed`, `skipped`, `blocked`.
- [ ] Create `Runbook`, `Step`, `StepResult`.
- [ ] Add validation: every non-optional step needs an executor type and a success condition.
- [ ] Test invalid steps fail validation.
- [ ] Run `go test ./internal/domain/runbook`.

## Task 2: Convert manual steps into guided steps

- [ ] In `internal/app/runbook/service.go`, add `FromMappingResult(result *mapper.MappingResult)`.
- [ ] Convert legacy manual strings into `approval` or `input` steps with `blocked` status.
- [ ] Add `HasUnresolvedManualText()` so strict mode can fail.
- [ ] Test that `"Update DNS"` becomes a `dns_check` step when the text mentions DNS.
- [ ] Test that `"Update application code"` becomes blocked unless an API-compat adapter exists.

## Task 3: Add execution state

- [ ] Store runbook state under generated output directory as `.homeport/runbook.json`.
- [ ] Implement resume by loading the JSON and continuing at first non-passed step.
- [ ] Add `RunNext(ctx, id)` and `RunAll(ctx, id)`.
- [ ] Test resume after one failed step.

## Task 4: Add API endpoints

- [ ] Add `GET /api/v1/runbooks/{id}`.
- [ ] Add `POST /api/v1/runbooks/{id}/steps/{stepID}/run`.
- [ ] Add `POST /api/v1/runbooks/{id}/run`.
- [ ] Add `POST /api/v1/runbooks/{id}/rollback`.
- [ ] Test handlers with `httptest`.

## Task 5: Wire the wizard

- [ ] Add `web/src/lib/runbook-api.ts`.
- [ ] Show steps grouped as Credentials, Provision, Sync, Validate, Cutover, Rollback.
- [ ] Disable "finish" until required steps are passed.
- [ ] Replace static manual notes display with executable step cards.
- [ ] Run `cd web && npm run test` if tests exist, otherwise `npm run build`.

## Task 6: Strict-mode enforcement

- [ ] Add CLI flag `--strict-click-bam`.
- [ ] Fail if any required step is blocked.
- [ ] Fail if any mapper emits raw manual text with no runbook mapping.
- [ ] Commit with `git commit -m "feat: add executable migration runbooks"`.
