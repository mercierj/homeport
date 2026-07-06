# Azure 100 Percent Managed Services Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote every Azure coverage row to fully managed A-to-Z status with discovery, open-source target, migration, compatibility strategy, validation, cutover, rollback, and no unresolved blockers.

**Architecture:** Reuse existing Azure parser, mapper, datamigration, and compatibility patterns. Services that cannot be API-compatible by endpoint configuration must get a HomePort adapter or generated app-change report before promotion.

**Tech Stack:** Go Azure parsers, mapper registry, datamigration executors, compatibility adapters, coverage CLI, integration tests.

---

## Files

- Modify: `docs/coverage/services.yaml`
- Modify: `docs/coverage/services.md`
- Modify: `internal/app/coverage/services.yaml`
- Modify: `internal/infrastructure/mapper/azure/registry.go`
- Create or modify Azure mapper files under `internal/infrastructure/mapper/azure/`
- Create or modify Azure parser files under `internal/infrastructure/parser/azure/`
- Create or modify Azure datamigration executors under `internal/app/datamigration/`
- Create or modify Azure compatibility adapters under `internal/app/compat/azure/`
- Create or modify tests under `test/integration/azure/`, `test/compat/`, and mapper package tests

## Required Azure service closure list

Mapped rows to prove and promote:

- AKS: `azurerm_kubernetes_cluster`
- App Gateway: `azurerm_application_gateway`
- App Service: `azurerm_app_service`
- Azure AD B2C: `azurerm_aadb2c_directory`
- Azure Cache: `azurerm_redis_cache`
- Azure CDN: `azurerm_cdn_profile`
- Azure DNS: `azurerm_dns_zone`
- Azure Firewall: `azurerm_firewall`
- Azure Functions: `azurerm_function_app`
- Azure Load Balancer: `azurerm_lb`
- Azure SQL: `azurerm_mssql_database`
- Azure VNet: `azurerm_virtual_network`
- Azure VM: `azurerm_linux_virtual_machine`, `azurerm_windows_virtual_machine`
- Container Instances: `azurerm_container_group`
- Front Door: `azurerm_frontdoor`
- Key Vault: `azurerm_key_vault`
- Managed Disk: `azurerm_managed_disk`
- MySQL: `azurerm_mysql_flexible_server`
- PostgreSQL: `azurerm_postgresql_flexible_server`

Guided rows to automate or adapter-shield, then promote:

- Azure Storage: `azurerm_storage_container`, `azurerm_storage_account`, `azurerm_storage_share`
- Cosmos DB: `azurerm_cosmosdb_account`
- Event Grid: `azurerm_eventgrid_topic`
- Event Hubs: `azurerm_eventhub`
- Logic Apps: `azurerm_logic_app_workflow`
- Service Bus: `azurerm_servicebus_namespace`, `azurerm_servicebus_queue`

Missing rows to implement, then promote:

- AI Search
- API Management
- App Insights
- Container Apps
- Container Registry
- Data Factory
- Databricks
- Foundry/OpenAI
- IoT Hub
- Log Analytics
- Monitor
- SignalR
- Synapse
- VM Scale Sets
- Data Lake: `azurerm_storage_data_lake_gen2_filesystem`
- Fabric
- Power BI Embedded: `azurerm_powerbi_embedded_capacity`
- Logic Apps advanced: `azurerm_logic_app_workflow`, `azurerm_logic_app_trigger_http_request`
- Notification Hubs: `azurerm_notification_hub_namespace`, `azurerm_notification_hub`
- DevOps Pipelines: `azuredevops_build_definition`, `azuredevops_release_definition`
- Application Insights: `azurerm_application_insights`
- Automation: `azurerm_automation_account`, `azurerm_automation_runbook`
- Purview: `azurerm_purview_account`
- Machine Learning: `azurerm_machine_learning_workspace`
- Document Intelligence: `azurerm_cognitive_account`
- Speech: `azurerm_cognitive_account`
- Translator: `azurerm_cognitive_account`

## Task 1: Add one Azure service closure harness

- [ ] Create `test/integration/azure/full_service_closure_test.go`:

```go
package azure_test

import (
	"testing"

	appcoverage "github.com/homeport/homeport/internal/app/coverage"
	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
)

func TestAllAzureCoverageRowsAreFull(t *testing.T) {
	catalog, err := appcoverage.LoadDefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range catalog.Services {
		if row.Provider != "azure" {
			continue
		}
		if row.Status != domaincoverage.StatusFull || domaincoverage.ComputeStatus(row) != domaincoverage.StatusFull {
			t.Fatalf("Azure %s is not full: status=%s blocker=%q", row.Service, row.Status, row.Blocker)
		}
		if !row.ManualStepsResolved {
			t.Fatalf("Azure %s manual steps are not resolved", row.Service)
		}
	}
}
```

- [ ] Run:

```bash
go test ./test/integration/azure -run TestAllAzureCoverageRowsAreFull
```

Expected before this plan is complete: fail on the first non-full Azure row.

## Task 2: Close mapped Azure rows

For each mapped row in the Required Azure service closure list:

- [ ] Add or update parser coverage for Terraform, tfstate, ARM, Bicep, and Azure API import where applicable.
- [ ] Add or update mapper tests proving generated open-source target artifacts.
- [ ] Add or update datamigration executor when data, state, jobs, or runtime config must move.
- [ ] Add or update compatibility adapter when native Azure SDK behavior can be shielded.
- [ ] Add or update generated application-change report when code/config changes remain.
- [ ] Promote the service:

```bash
go run ./cmd/homeport coverage promote --provider azure --service "Azure Storage" --status full --manual-steps-resolved --markdown docs/coverage/services.md
```

Expected: promotion succeeds only after every checklist field is true and blocker is empty. Repeat the same command shape for each Azure service in the closure list, using its exact service name such as `Azure VM`, `Service Bus`, `Data Factory`, `Foundry/OpenAI`, or `Translator`.

## Task 3: Close guided Azure rows

For Azure Storage, Cosmos DB, Event Grid, Event Hubs, Logic Apps, and Service Bus:

- [ ] Implement the adapter or generated app-change report named in the current blocker.
- [ ] Add an integration or compatibility test proving the adapter/report is generated during the wizard analyze/export path.
- [ ] Remove the blocker.
- [ ] Promote to `full`.

## Task 4: Implement missing Azure rows by category

- [ ] Analytics/data: Data Factory, Synapse, Data Lake, Databricks, Fabric, Power BI Embedded.
- [ ] API/app/integration: API Management, Container Apps, Logic Apps advanced, SignalR, Notification Hubs, IoT Hub.
- [ ] DevOps/artifacts: Container Registry, DevOps Pipelines.
- [ ] Observability/governance: Monitor, Log Analytics, App Insights, Application Insights, Automation, Purview.
- [ ] AI/ML: AI Search, Foundry/OpenAI, Machine Learning, Document Intelligence, Speech, Translator.
- [ ] Compute gap: VM Scale Sets.

For every service:

- [ ] Add resource type constants and parser recognition.
- [ ] Register mapper support.
- [ ] Pick the open-source target in the coverage row.
- [ ] Generate deployment artifacts.
- [ ] Generate migration or replacement runbook.
- [ ] Add compatibility adapter or generated app-change report.
- [ ] Add validation, cutover, rollback, and backup behavior.
- [ ] Promote to `full`.

## Task 5: Verify and commit Azure closure

- [ ] Run:

```bash
cp docs/coverage/services.yaml internal/app/coverage/services.yaml
go test ./internal/domain/coverage ./internal/app/coverage ./internal/cli
go test ./internal/infrastructure/parser/azure/... ./internal/infrastructure/mapper/azure/...
go test ./internal/app/datamigration ./test/compat/... ./test/integration/azure/...
```

Expected: Azure full-service closure test passes.

- [ ] Commit:

```bash
git add docs/coverage/services.yaml docs/coverage/services.md internal/app/coverage/services.yaml internal/infrastructure/parser/azure internal/infrastructure/mapper/azure internal/app/datamigration internal/app/compat/azure test/integration/azure test/compat
git commit -m "feat: fully manage Azure service coverage"
```
