# HomePort Coverage Ledger Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the source of truth for every AWS, Google Cloud, and Microsoft Azure service HomePort supports, partially supports, or does not support.

**Architecture:** Use a small checked-in catalog plus generated reports from existing mappers/executors. The ledger drives docs, UI badges, strict-mode failures, and future planning.

**Tech Stack:** Go structs, JSON/Markdown generation, existing `internal/domain/resource`, mapper registry, datamigration service, docs.

---

## Files

- Create: `internal/domain/coverage/coverage.go`
- Create: `internal/domain/coverage/coverage_test.go`
- Create: `internal/app/coverage/service.go`
- Create: `internal/app/coverage/service_test.go`
- Create: `internal/cli/coverage.go`
- Modify: `internal/cli/root.go`
- Create: `docs/coverage/services.yaml`
- Generate: `docs/coverage/services.md`

## Task 1: Define coverage statuses

- [ ] Create `internal/domain/coverage/coverage.go` with `StatusFull`, `StatusMapped`, `StatusGuided`, `StatusMissing`, `StatusImpossible`.
- [ ] Add fields: `Provider`, `Service`, `ResourceTypes`, `Discover`, `Cost`, `Provision`, `Migrate`, `APICompat`, `EnvDNS`, `HA`, `Backup`, `Validate`, `Cutover`, `Rollback`, `Status`, `Blocker`.
- [ ] Add `ComputeStatus(row ServiceCoverage) Status` that returns the lowest status implied by checklist booleans and blockers.
- [ ] Test that a row with every capability true is `Full`.
- [ ] Test that a row with `APICompat=false` is not `Full`.
- [ ] Run `go test ./internal/domain/coverage`.

## Task 2: Seed the initial catalog

- [ ] Create `docs/coverage/services.yaml`.
- [ ] Add rows for every current resource in `internal/domain/resource/types.go`.
- [ ] Add missing high-priority services:
  - AWS: Redshift, Athena, Glue, EMR, OpenSearch, MSK, Step Functions, AppSync, ECR, CodeBuild, CodePipeline, X-Ray, WAF, GuardDuty, Bedrock, SageMaker.
  - GCP: BigQuery, Dataflow, Dataproc, Composer, Vertex AI, Apigee, Workflows, Eventarc, Artifact Registry, Cloud Build, Monitoring, Logging, Trace.
  - Azure: API Management, Container Apps, Container Registry, VM Scale Sets, Monitor, Log Analytics, App Insights, Data Factory, Synapse, Databricks, AI Search, Foundry/OpenAI, IoT Hub, SignalR.
- [ ] Mark all seeded current resources as `Mapped` unless the checklist proves `Full`.
- [ ] Mark missing rows as `Missing`.

## Task 3: Compare catalog with code

- [ ] Implement `internal/app/coverage/service.go` to load the YAML catalog.
- [ ] Read registered mapper types from `mapper.NewRegistry().SupportedTypes()`.
- [ ] Read registered executor types from `datamigration.NewService().ListExecutors()`.
- [ ] Add `FindDrift()` that reports mapper-without-ledger and ledger-without-mapper.
- [ ] Test with a fake catalog containing one supported and one missing resource.
- [ ] Run `go test ./internal/app/coverage`.

## Task 4: Add CLI reporting

- [ ] Add `homeport coverage --format table|json|markdown`.
- [ ] Add `homeport coverage --provider aws|gcp|azure`.
- [ ] Add `homeport coverage --strict` that exits non-zero on drift.
- [ ] Wire command in `internal/cli/root.go`.
- [ ] Run `go test ./internal/cli/...`.

## Task 5: Generate docs

- [ ] Generate `docs/coverage/services.md` from the catalog.
- [ ] Include a summary table per provider.
- [ ] Link from `README.md`, `docs/aws-services.md`, `docs/gcp-services.md`, `docs/azure-services.md`.
- [ ] Run `go test ./...` if time permits; otherwise run coverage package tests and CLI tests.
- [ ] Commit with `git commit -m "feat: add service coverage ledger"`.
