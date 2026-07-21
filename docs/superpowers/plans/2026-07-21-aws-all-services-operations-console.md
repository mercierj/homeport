# AWS All-Services Operations Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver a Homeport operations interface for every AWS service in the repository catalogue after successful migration, backed solely by local targets.

**Architecture:** Generate service visibility from the AWS coverage catalogue, persist server-attested post-cutover bindings, and dispatch resource operations through family/service drivers. The React console uses a common service shell with metadata-driven service panels; capabilities and target health come exclusively from the API.

**Tech Stack:** Go, chi, existing compatibility adapters/local services, React, TypeScript, TanStack Query, Tailwind, Vitest and Playwright.

---

## Non-negotiable invariants

- Every one of the 59 AWS catalogue services is represented after it is migrated; no hard-coded Lambda/SQS allow-list.
- No AWS SDK, credentials, endpoint or control-plane call is allowed.
- A browser never supplies a local resource identity, binding or capability.
- Activation is part of successful cutover/deployment persistence, never dependent on an SSE subscription.
- Mutations are capability-gated, authorized against persisted bindings and fail closed when audit persistence is unavailable.
- A target lacking a local operation remains visible and truthful rather than pretending to be AWS-compatible.

## Service inventory and delivery matrix

| Family | Services | Console panel contract |
|---|---|---|
| Edge, networking and delivery | ACM, ALB, API Gateway, App Mesh, CloudFront, Route 53, VPC, WAF, Shield | routes, listeners, domains, certificates, rules, network policies and target health |
| Compute and orchestration | Lambda, ECS, EKS, EC2, ECR, CodeBuild, CodeDeploy, CodePipeline, Step Functions, CloudFormation full import | workloads, images, deployments, builds, executions, logs and lifecycle actions |
| Storage, database and analytics | S3, EBS, EFS, DynamoDB, RDS, ElastiCache, Redshift, OpenSearch, Athena, EMR, Glue, Lake Formation, QuickSight | data resources, capacity/health, access policies, jobs, query/execution history and safe maintenance actions |
| Messaging and events | SQS, SNS, Kinesis, EventBridge, MQ, MSK, IoT Core | queues/topics/streams/brokers, messages/events, consumers, retries, purge/replay and delivery health |
| Identity, security and governance | IAM, Cognito, KMS, Secrets Manager, Config, Control Tower, GuardDuty, Organizations, Security Hub | principals, policies, keys, secrets metadata, findings, accounts, controls and audit state |
| AI and application services | AppSync, Bedrock, Comprehend, Rekognition, SageMaker, Textract, Transcribe, Translate, SES | APIs/models/endpoints/jobs, inference or processing executions, mail identities/delivery and logs |
| Observability | CloudWatch, X-Ray | dashboards, metrics, alarms, traces, logs and retention controls |

The explicit catalogue includes `App Mesh` and `CloudFormation full import`; normalise their keys in the metadata registry rather than inventing browser-only services.

### Task 1: Replace the Lambda/SQS projection with an exhaustive AWS service registry

**Files:** `internal/app/awsoperations/types.go`, `catalog.go`, `catalog_test.go`, `service.go`, `internal/app/coverage/services.yaml` (read-only source), `internal/api/handlers/cutover.go`.

- [ ] Define canonical service metadata from every `provider: aws` coverage entry: service key, display name, resource types, target, family, panel kind and declared driver.
- [ ] Write a failing catalogue parity test that parses the coverage catalogue and fails for a missing/duplicate/unregistered AWS service.
- [ ] Derive workspace services and bindings from metadata rather than a Lambda/SQS switch; preserve unsupported-but-migrated service entries as unavailable with a reason.
- [ ] Replace in-memory trusted binding hand-off with a durable cutover/deployment binding record. Commit it with cutover completion so a disconnected SSE client cannot affect activation.
- [ ] Add a test for successful cutover without opening `/stream`, restart/reload, and all-service discovery activation.

### Task 2: Establish common all-service operations API and security boundary

**Files:** `internal/api/handlers/awsoperations.go`, `internal/api/server.go`, `api/openapi.yaml`, `internal/domain/authz/*`, `internal/app/awsoperations/audit.go` and tests.

- [ ] Add metadata-first routes: workspace catalogue, `/services/{service}`, resource list/detail, health and capabilities for every registered service.
- [ ] Define common response envelopes with non-null arrays, binding metadata, target health and stable operation errors.
- [ ] Move cutover activation to the executor completion transaction; remove SSE activation side effects.
- [ ] Wire a real request-derived authorizer; reject unauthenticated/unauthorized mutations before driver dispatch.
- [ ] Make audit setup and write failure fail closed for mutations, and persist workspace/service/bound-resource/action/decision.
- [ ] Add OpenAPI schemas and responses for the common routes and generated per-service metadata; ensure every service key appears in the public contract.

### Task 3: Implement family drivers and all service declarations

**Files:** `internal/app/awsoperations/driver.go`, `internal/app/awsoperations/drivers/*`, adapter bridges, `driver_test.go`.

- [ ] Add a registry that requires a driver declaration for all 59 services, even when it is a truthful read-only/unavailable driver.
- [ ] Implement reusable family bridges for networking, compute, storage/database, messaging, identity/security, analytics, AI/application and observability.
- [ ] Reuse existing local compatibility adapters and target services; add narrow local backend interfaces where needed.
- [ ] For every service, map resources to AWS-facing records and expose only backend-proven capabilities.
- [ ] Test every driver for bound-resource isolation, unavailable/degraded state, capability gating, authorization-before-call and audit decision emission.
- [ ] Add service-specific operation tests for all mutating capabilities; keep generic existing endpoints compatible.

### Task 4: Build the metadata-driven all-service web console

**Files:** `web/src/lib/aws-operations-*`, `web/src/pages/AWSOperations.tsx`, `AWSService.tsx`, `web/src/components/aws-operations/*`, `App.tsx`, `Sidebar.tsx` and tests.

- [ ] Render `/aws` as a searchable catalogue of all migrated AWS services, grouped by family and with availability/target-health/action summaries.
- [ ] Add one parameterised `/aws/:service` route and a service metadata registry; no service may require an ad-hoc route merely to appear.
- [ ] Build a shared resource list/detail/action shell, with family/service panels for the matrix above.
- [ ] Render only server capabilities; show unavailable/degraded services and reasons, never hidden services.
- [ ] Require confirmation for destructive actions; preserve resource context on operation failure; use accessible dialogs/toasts and Homeport styles only.
- [ ] Add Vitest coverage that iterates every service metadata entry, validates navigation/availability/capability gating, and verifies no copied AWS assets or copy.

### Task 5: End-to-end conformance, documentation and migration of the current prototype

**Files:** existing Lambda/SQS code/tests, new all-service fixtures, `web/tests/aws-all-services.spec.ts`, `README.md`, `docs/web-dashboard.md`.

- [ ] Migrate Lambda and SQS from bespoke pages to the shared contract without losing their editor/message operations.
- [ ] Add fixtures for every catalogue service with a representative migrated resource and target health; run API conformance for 59/59 services.
- [ ] Add Playwright coverage that traverses every service tile, asserts no AWS request, and exercises a representative safe action per capability family.
- [ ] Document the activation source, binding requirements, target mapping, all-service coverage policy and service-specific capability limits.
- [ ] Run `go test -race ./...`, `cd web && npm test && npm run lint && npm run build && npx playwright test`, and `git diff --check`; record any pre-existing failure separately.

## Completion audit

Completion requires catalogue parity for all 59 AWS entries, a visible console surface for each migrated service, an attested local backend binding, truthful capabilities, authorization/audit for every mutation, and the complete backend/frontend/E2E verification suite.
