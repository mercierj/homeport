# GCP 100 Percent Managed Services Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote every Google Cloud coverage row to fully managed A-to-Z status with discovery, open-source target, migration, compatibility strategy, validation, cutover, rollback, and no unresolved blockers.

**Architecture:** Reuse existing GCP parser, mapper, datamigration, and compatibility patterns. Services that cannot be API-compatible by configuration must get a HomePort adapter or a generated app-change report before promotion.

**Tech Stack:** Go GCP parsers, mapper registry, datamigration executors, compatibility adapters, coverage CLI, integration tests.

---

## Files

- Modify: `docs/coverage/services.yaml`
- Modify: `docs/coverage/services.md`
- Modify: `internal/app/coverage/services.yaml`
- Modify: `internal/infrastructure/mapper/gcp/registry.go`
- Create or modify GCP mapper files under `internal/infrastructure/mapper/gcp/`
- Create or modify GCP parser files under `internal/infrastructure/parser/gcp/`
- Create or modify GCP datamigration executors under `internal/app/datamigration/`
- Create or modify GCP compatibility adapters under `internal/app/compat/gcp/`
- Create or modify tests under `test/integration/gcp/`, `test/compat/`, and mapper package tests

## Required GCP service closure list

Mapped rows to prove and promote:

- App Engine: `google_app_engine_application`
- Cloud Armor: `google_compute_security_policy`
- Cloud CDN: `google_compute_backend_bucket`
- Cloud DNS: `google_dns_managed_zone`
- Cloud Functions: `google_cloudfunctions_function`
- Cloud Load Balancing: `google_compute_backend_service`
- Cloud Run: `google_cloud_run_service`
- Cloud SQL: `google_sql_database_instance`
- Compute Engine: `google_compute_instance`
- Filestore: `google_filestore_instance`
- GKE: `google_container_cluster`
- IAM: `google_project_iam_member`
- Identity Platform: `google_identity_platform_config`
- Memorystore: `google_redis_instance`
- Persistent Disk: `google_compute_disk`
- Secret Manager: `google_secret_manager_secret`
- VPC: `google_compute_network`

Guided rows to automate or adapter-shield, then promote:

- Bigtable: `google_bigtable_instance`
- Cloud Scheduler: `google_cloud_scheduler_job`
- Cloud Storage: `google_storage_bucket`
- Cloud Tasks: `google_cloud_tasks_queue`
- Firestore: `google_firestore_database`
- Pub/Sub: `google_pubsub_topic`, `google_pubsub_subscription`
- Spanner: `google_spanner_instance`

Missing rows to implement, then promote:

- Apigee
- Artifact Registry
- BigQuery
- Cloud Build
- Composer
- Dataflow
- Dataproc
- Eventarc
- Logging
- Monitoring
- Trace
- Vertex AI
- Workflows
- Dataplex: `google_dataplex_lake`, `google_dataplex_zone`
- Looker: `google_looker_instance`
- Cloud Deploy: `google_clouddeploy_delivery_pipeline`, `google_clouddeploy_target`
- Error Reporting
- Profiler
- TPU: `google_tpu_node`, `google_tpu_v2_vm`
- Document AI: `google_document_ai_processor`
- Vision AI
- Speech-to-Text: `google_speech_custom_class`, `google_speech_phrase_set`
- Translation

## Task 1: Add one GCP service closure harness

- [ ] Create `test/integration/gcp/full_service_closure_test.go`:

```go
package gcp_test

import (
	"testing"

	appcoverage "github.com/homeport/homeport/internal/app/coverage"
	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
)

func TestAllGCPCoverageRowsAreFull(t *testing.T) {
	catalog, err := appcoverage.LoadDefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range catalog.Services {
		if row.Provider != "gcp" {
			continue
		}
		if row.Status != domaincoverage.StatusFull || domaincoverage.ComputeStatus(row) != domaincoverage.StatusFull {
			t.Fatalf("GCP %s is not full: status=%s blocker=%q", row.Service, row.Status, row.Blocker)
		}
		if !row.ManualStepsResolved {
			t.Fatalf("GCP %s manual steps are not resolved", row.Service)
		}
	}
}
```

- [ ] Run:

```bash
go test ./test/integration/gcp -run TestAllGCPCoverageRowsAreFull
```

Expected before this plan is complete: fail on the first non-full GCP row.

## Task 2: Close mapped GCP rows

For each mapped row in the Required GCP service closure list:

- [ ] Add or update parser coverage for Terraform, tfstate, Deployment Manager, and GCP API import where applicable.
- [ ] Add or update mapper tests proving generated open-source target artifacts.
- [ ] Add or update datamigration executor when data, state, jobs, or runtime config must move.
- [ ] Add or update compatibility adapter when native GCP SDK behavior can be shielded.
- [ ] Add or update generated application-change report when code/config changes remain.
- [ ] Promote the service:

```bash
go run ./cmd/homeport coverage promote --provider gcp --service "Cloud Storage" --status full --manual-steps-resolved --markdown docs/coverage/services.md
```

Expected: promotion succeeds only after every checklist field is true and blocker is empty. Repeat the same command shape for each GCP service in the closure list, using its exact service name such as `Cloud Run`, `Pub/Sub`, `BigQuery`, `Vertex AI`, or `Translation`.

## Task 3: Close guided GCP rows

For Bigtable, Cloud Scheduler, Cloud Storage, Cloud Tasks, Firestore, Pub/Sub, and Spanner:

- [ ] Implement the adapter or generated app-change report named in the current blocker.
- [ ] Add an integration or compatibility test proving the adapter/report is generated during the wizard analyze/export path.
- [ ] Remove the blocker.
- [ ] Promote to `full`.

## Task 4: Implement missing GCP rows by category

- [ ] Analytics/data: BigQuery, Dataflow, Dataproc, Composer, Dataplex, Looker.
- [ ] API/app/integration: Apigee, Workflows, Eventarc.
- [ ] DevOps/artifacts: Artifact Registry, Cloud Build, Cloud Deploy.
- [ ] Observability: Monitoring, Logging, Trace, Error Reporting, Profiler.
- [ ] AI/ML: Vertex AI, TPU, Document AI, Vision AI, Speech-to-Text, Translation.

For every service:

- [ ] Add resource type constants and parser recognition.
- [ ] Register mapper support.
- [ ] Pick the open-source target in the coverage row.
- [ ] Generate deployment artifacts.
- [ ] Generate migration or replacement runbook.
- [ ] Add compatibility adapter or generated app-change report.
- [ ] Add validation, cutover, rollback, and backup behavior.
- [ ] Promote to `full`.

## Task 5: Verify and commit GCP closure

- [ ] Run:

```bash
cp docs/coverage/services.yaml internal/app/coverage/services.yaml
go test ./internal/domain/coverage ./internal/app/coverage ./internal/cli
go test ./internal/infrastructure/parser/gcp/... ./internal/infrastructure/mapper/gcp/...
go test ./internal/app/datamigration ./test/compat/... ./test/integration/gcp/...
```

Expected: GCP full-service closure test passes.

- [ ] Commit:

```bash
git add docs/coverage/services.yaml docs/coverage/services.md internal/app/coverage/services.yaml internal/infrastructure/parser/gcp internal/infrastructure/mapper/gcp internal/app/datamigration internal/app/compat/gcp test/integration/gcp test/compat
git commit -m "feat: fully manage GCP service coverage"
```
