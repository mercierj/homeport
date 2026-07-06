# AWS 100 Percent Managed Services Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Promote every AWS coverage row to fully managed A-to-Z status with discovery, open-source target, migration, compatibility strategy, validation, cutover, rollback, and no unresolved blockers.

**Architecture:** Reuse existing parser, mapper, datamigration, compatibility, runbook, and coverage-promotion patterns. Add the smallest mapper/executor/adapter per AWS service, then promote only when `homeport coverage promote --status full` accepts the row.

**Tech Stack:** Go AWS parsers, mapper registry, datamigration executors, compatibility adapters, coverage CLI, integration tests.

---

## Files

- Modify: `docs/coverage/services.yaml`
- Modify: `docs/coverage/services.md`
- Modify: `internal/app/coverage/services.yaml`
- Modify: `internal/infrastructure/mapper/aws/registry.go`
- Create or modify AWS mapper files under `internal/infrastructure/mapper/aws/`
- Create or modify AWS parser files under `internal/infrastructure/parser/aws/`
- Create or modify AWS datamigration executors under `internal/app/datamigration/`
- Create or modify AWS compatibility adapters under `internal/app/compat/aws/`
- Create or modify tests under `test/integration/aws/`, `test/compat/`, and mapper package tests

## Required AWS service closure list

Every service below must finish as `status: full`, `blocker` empty, `manual_steps_resolved: true`, and all checklist booleans true in both `docs/coverage/services.yaml` and embedded `internal/app/coverage/services.yaml`.

Mapped rows to prove and promote:

- ALB: `aws_lb`
- ACM: `aws_acm_certificate`
- API Gateway: `aws_api_gateway_rest_api`
- CloudFront: `aws_cloudfront_distribution`
- CloudWatch: `aws_cloudwatch_metric_alarm`, `aws_cloudwatch_log_group`, `aws_cloudwatch_dashboard`
- DynamoDB: `aws_dynamodb_table`
- EBS: `aws_ebs_volume`
- EC2: `aws_instance`
- ECS: `aws_ecs_service`, `aws_ecs_task_definition`
- EFS: `aws_efs_file_system`
- EKS: `aws_eks_cluster`
- ElastiCache: `aws_elasticache_cluster`
- IAM: `aws_iam_role`
- Lambda: `aws_lambda_function`
- RDS: `aws_db_instance`, `aws_rds_cluster`
- Route 53: `aws_route53_zone`
- S3: `aws_s3_bucket`
- SNS: `aws_sns_topic`
- SQS: `aws_sqs_queue`
- Secrets Manager: `aws_secretsmanager_secret`
- VPC: `aws_vpc`

Guided rows to automate or adapter-shield, then promote:

- Cognito: `aws_cognito_user_pool`
- EventBridge: `aws_cloudwatch_event_rule`
- KMS: `aws_kms_key`
- Kinesis: `aws_kinesis_stream`
- SES: `aws_ses_domain_identity`

Missing rows to implement, then promote:

- AppSync
- Athena
- Bedrock
- CodeBuild
- CodePipeline
- ECR
- EMR
- Glue
- GuardDuty
- MSK
- OpenSearch
- Redshift
- SageMaker
- Step Functions
- WAF
- X-Ray
- Lake Formation: `aws_lakeformation_data_lake_settings`, `aws_lakeformation_permissions`
- QuickSight: `aws_quicksight_data_source`, `aws_quicksight_dashboard`
- MQ: `aws_mq_broker`
- IoT Core: `aws_iot_thing`, `aws_iot_topic_rule`
- App Mesh: `aws_appmesh_mesh`, `aws_appmesh_virtual_node`
- CodeDeploy: `aws_codedeploy_app`, `aws_codedeploy_deployment_group`
- CloudFormation full import: `aws_cloudformation_stack`
- Shield: `aws_shield_protection`
- Security Hub: `aws_securityhub_account`, `aws_securityhub_standards_subscription`
- Config: `aws_config_configuration_recorder`, `aws_config_config_rule`
- Organizations: `aws_organizations_organization`, `aws_organizations_account`
- Control Tower: `aws_controltower_control`
- Textract: `aws_textract_adapter`
- Transcribe: `aws_transcribe_vocabulary`, `aws_transcribe_language_model`
- Translate
- Rekognition: `aws_rekognition_collection`, `aws_rekognition_project`
- Comprehend: `aws_comprehend_document_classifier`, `aws_comprehend_entity_recognizer`

## Task 1: Add one AWS service closure harness

- [ ] Create `test/integration/aws/full_service_closure_test.go` with this table-driven guard:

```go
package aws_test

import (
	"testing"

	appcoverage "github.com/homeport/homeport/internal/app/coverage"
	domaincoverage "github.com/homeport/homeport/internal/domain/coverage"
)

func TestAllAWSCoverageRowsAreFull(t *testing.T) {
	catalog, err := appcoverage.LoadDefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range catalog.Services {
		if row.Provider != "aws" {
			continue
		}
		if row.Status != domaincoverage.StatusFull || domaincoverage.ComputeStatus(row) != domaincoverage.StatusFull {
			t.Fatalf("AWS %s is not full: status=%s blocker=%q", row.Service, row.Status, row.Blocker)
		}
		if !row.ManualStepsResolved {
			t.Fatalf("AWS %s manual steps are not resolved", row.Service)
		}
	}
}
```

- [ ] Run:

```bash
go test ./test/integration/aws -run TestAllAWSCoverageRowsAreFull
```

Expected before this plan is complete: fail on the first non-full AWS row.

## Task 2: Close mapped AWS rows

For each mapped row in the Required AWS service closure list:

- [ ] Add or update parser coverage for Terraform, tfstate, CloudFormation, and AWS API import where the service supports those source shapes.
- [ ] Add or update mapper test proving the service emits an open-source target and runbook action.
- [ ] Add or update datamigration executor when the service contains data or runtime state.
- [ ] Add or update compatibility adapter when the source SDK/API can be shielded without app code changes.
- [ ] Add or update application-change detector when an adapter cannot hide all differences.
- [ ] Run the narrow tests for the touched package.
- [ ] Promote the service:

```bash
go run ./cmd/homeport coverage promote --provider aws --service "S3" --status full --manual-steps-resolved --markdown docs/coverage/services.md
```

Expected: promotion succeeds only after every checklist field is true and blocker is empty. Repeat the same command shape for each AWS service in the closure list, using its exact service name such as `EC2`, `Lambda`, `Cognito`, `Athena`, or `Comprehend`.

- [ ] Copy the changed `docs/coverage/services.yaml` into the embedded catalog path:

```bash
cp docs/coverage/services.yaml internal/app/coverage/services.yaml
go test ./internal/app/coverage -run TestDefaultCatalogMatchesDocsLedger
```

Expected: pass.

## Task 3: Close guided AWS rows

For Cognito, EventBridge, KMS, Kinesis, and SES:

- [ ] Convert the existing guided path into one of these fully managed outcomes:

```text
adapter: existing application behavior continues through a HomePort-compatible endpoint
generated_patch: HomePort emits exact file/env/config changes and validates them
replacement_runbook: HomePort creates the replacement service, migration tasks, verification checks, and rollback instructions
```

- [ ] Add regression tests proving the previous blocker text no longer applies.
- [ ] Remove the blocker from both coverage catalogs.
- [ ] Promote each row to `full` with `--manual-steps-resolved`.

## Task 4: Implement missing AWS rows by category

- [ ] Analytics/data: Athena, Glue, EMR, Lake Formation, OpenSearch, Redshift, QuickSight.
- [ ] Messaging/integration: AppSync, Step Functions, MQ, IoT Core, App Mesh.
- [ ] DevOps/artifacts: ECR, CodeBuild, CodePipeline, CodeDeploy, CloudFormation full import.
- [ ] Security/governance: GuardDuty, WAF, Shield, Security Hub, Config, Organizations, Control Tower.
- [ ] AI/ML: Bedrock, SageMaker, Textract, Transcribe, Translate, Rekognition, Comprehend.

For every service in each category:

- [ ] Add resource type constants and parser recognition.
- [ ] Register mapper support.
- [ ] Pick the open-source target in the coverage row.
- [ ] Generate deployment artifacts.
- [ ] Generate migration or replacement runbook.
- [ ] Add compatibility adapter or generated app-change report.
- [ ] Add validation, cutover, rollback, and backup behavior.
- [ ] Promote to `full`.

## Task 5: Verify and commit AWS closure

- [ ] Run:

```bash
go test ./internal/domain/coverage ./internal/app/coverage ./internal/cli
go test ./internal/infrastructure/parser/aws/... ./internal/infrastructure/mapper/aws/...
go test ./internal/app/datamigration ./test/compat/... ./test/integration/aws/...
go run ./cmd/homeport coverage --provider aws --format markdown > /tmp/aws-coverage.md
go run ./cmd/homeport coverage assert-full --catalog docs/coverage/services.yaml
```

Expected after GCP and Azure plans are also complete: `assert-full` passes. During this AWS-only plan, provider-specific AWS full-service test must pass even if global assert-full still fails on other providers.

- [ ] Commit:

```bash
git add docs/coverage/services.yaml docs/coverage/services.md internal/app/coverage/services.yaml internal/infrastructure/parser/aws internal/infrastructure/mapper/aws internal/app/datamigration internal/app/compat/aws test/integration/aws test/compat
git commit -m "feat: fully manage AWS service coverage"
```
