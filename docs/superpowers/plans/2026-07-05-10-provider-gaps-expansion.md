# HomePort Provider Gaps Expansion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Track and close unsupported AWS, Google Cloud, and Azure services without pretending HomePort supports everything today.

**Architecture:** The coverage ledger owns the truth. New services enter as `Missing`, then become `Mapped`, `Guided`, or `Full` only after parser, mapper, migration, compatibility, validation, and docs are implemented.

**Tech Stack:** Coverage ledger, parser registry, mapper registry, datamigration executors, runbook orchestrator.

---

## Task 1: Add missing-service intake

- [ ] Add `homeport coverage add-missing --provider --service --category`.
- [ ] Add fields for source API, Terraform resource types, likely open-source target, API compatibility strategy, impossibility notes.
- [ ] Test that new missing rows appear in `docs/coverage/services.md`.

## Task 2: AWS expansion backlog

- [ ] Add missing rows for analytics/data: Redshift, Athena, Glue, EMR, Lake Formation, OpenSearch, MSK, QuickSight.
- [ ] Add missing rows for app/integration: Step Functions, AppSync, MQ, IoT Core, App Mesh.
- [ ] Add missing rows for DevOps: ECR, CodeBuild, CodePipeline, CodeDeploy, CloudFormation full import.
- [ ] Add missing rows for security/governance: WAF, Shield, GuardDuty, Security Hub, Config, Organizations, Control Tower.
- [ ] Add missing rows for AI/ML: Bedrock, SageMaker, Textract, Transcribe, Translate, Rekognition, Comprehend.

## Task 3: Google Cloud expansion backlog

- [ ] Add missing rows for analytics/data: BigQuery, Dataflow, Dataproc, Composer, Dataplex, Looker.
- [ ] Add missing rows for app/integration: Apigee, Workflows, Eventarc.
- [ ] Add missing rows for DevOps: Artifact Registry, Cloud Build, Cloud Deploy.
- [ ] Add missing rows for observability: Monitoring, Logging, Trace, Error Reporting, Profiler.
- [ ] Add missing rows for AI/ML: Vertex AI, TPU, Document AI, Vision AI, Speech-to-Text, Translation.

## Task 4: Azure expansion backlog

- [ ] Add missing rows for analytics/data: Data Factory, Synapse, Data Lake, Databricks, Fabric, Power BI Embedded.
- [ ] Add missing rows for app/integration: API Management, Container Apps, Logic Apps advanced, SignalR, Notification Hubs.
- [ ] Add missing rows for DevOps: Container Registry, DevOps Pipelines.
- [ ] Add missing rows for observability/governance: Monitor, Log Analytics, Application Insights, Automation, Purview.
- [ ] Add missing rows for AI/ML: AI Search, Azure OpenAI/Foundry, Machine Learning, Document Intelligence, Speech, Translator.

## Task 5: Service promotion checklist

- [ ] Add CLI command `homeport coverage promote`.
- [ ] Refuse promotion to `Full` unless all checklist columns are true.
- [ ] Refuse promotion when a mapper emits unresolved manual steps.
- [ ] Generate a conformance-test checklist file for each newly promoted service.
- [ ] Commit with `git commit -m "feat: track unsupported provider service backlog"`.
