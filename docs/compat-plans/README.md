# Provider Compatibility Plans

HomePort compatibility plans define how every provider service in the coverage ledger moves from generated migration artifacts to a provider-grade compatibility layer.

For AWS plans generated before 2026-07-21, the compatibility mechanism in
`docs/coverage/services.yaml` takes precedence over any endpoint/SDK language
in an individual plan. See
[`aws-api-compatibility-audit-2026-07-21.md`](../coverage/aws-api-compatibility-audit-2026-07-21.md)
for the service-by-service boundary: `generated_patch` and `no_change` do not
imply an AWS endpoint adapter.

The coverage ledger remains the operational source of truth for current status. These plans define the target contract for adapters, backends, security semantics, and contract tests before a service can claim strong API compatibility.

## Compatibility Model

All services use the same levels:

- L0: generated migration only, no compatibility API.
- L1: real provisioning of the open-source or sovereign backend.
- L2: partial API adapter for common calls.
- L3: contractual compatibility evidenced by the selected strategy: official
  provider SDK tests for an endpoint adapter, or mapper/application
  conformance where no provider API is exposed.
- L4: advanced compatibility with fine-grained authz, provider-like errors, pagination, idempotency, lifecycle support, and reasonable quotas.

Service targets:

- Core services should target L4: identity/authz, object storage, messaging, secrets/config/KMS, and observability ingestion.
- Non-core services should target L3 before being described as API-compatible.
- Services with provider-specific semantics that cannot be honestly reproduced must stay at L0-L2 until their gaps are closed by tests.

See [provider-compat-levels.md](provider-compat-levels.md) for promotion gates.

## Mandatory Service Plan Template

Every service plan must use these sections:

- Goal
- Provider API Surface
- Backend
- Authz Model
- Adapter
- Generated Artifacts
- Contract Tests
- Compatibility Level

The template is in [service-plan-template.md](service-plan-template.md).

## Execution Order

Build the compatibility layer by families, not by writing weak adapters for every service:

1. Identity/Authz core: AWS IAM/STS, Azure identity, GCP IAM, and the central `Authorize(principal, action, resource, context)` engine.
2. Storage: S3, GCS, Azure Blob. Prioritize official SDK calls, policies, and signed URLs.
3. Messaging/Eventing: SQS, SNS, Pub/Sub, Service Bus first; EventBridge, Cloud Tasks, Eventarc, Event Grid, and Event Hubs after the core queue/topic model is stable.
4. Secrets/Config/KMS: AWS Secrets Manager, Parameter Store, KMS, GCP Secret Manager, Azure Key Vault.
5. Observability: logs, metrics, traces over Loki, Prometheus, Grafana, and OpenTelemetry.
6. Databases: straightforward relational targets first; DynamoDB, Firestore, and Cosmos DB need real API adapters; BigQuery and Spanner are major projects.
7. AI/ML: Transcribe, Translate, Rekognition, Comprehend and equivalents as compatible-enough APIs, not identical model quality.
8. Governance/Security: Config, Security Hub, Control Tower, Organizations and equivalents with policy semantics and audit trails.

## Exhaustive Plans From The Coverage Ledger

### AWS

- [ALB](aws/alb.md)
- [ACM](aws/acm.md)
- [API Gateway](aws/api-gateway.md)
- [AppSync](aws/appsync.md)
- [Athena](aws/athena.md)
- [Bedrock](aws/bedrock.md)
- [CloudFront](aws/cloudfront.md)
- [CloudWatch](aws/cloudwatch.md)
- [CodeBuild](aws/codebuild.md)
- [CodePipeline](aws/codepipeline.md)
- [Cognito](aws/cognito.md)
- [DynamoDB](aws/dynamodb.md)
- [EBS](aws/ebs.md)
- [EC2](aws/ec2.md)
- [ECR](aws/ecr.md)
- [ECS](aws/ecs.md)
- [EFS](aws/efs.md)
- [EKS](aws/eks.md)
- [EMR](aws/emr.md)
- [ElastiCache](aws/elasticache.md)
- [EventBridge](aws/eventbridge.md)
- [Glue](aws/glue.md)
- [GuardDuty](aws/guardduty.md)
- [IAM](aws/iam.md)
- [KMS](aws/kms.md)
- [Kinesis](aws/kinesis.md)
- [Lambda](aws/lambda.md)
- [MSK](aws/msk.md)
- [OpenSearch](aws/opensearch.md)
- [RDS](aws/rds.md)
- [Redshift](aws/redshift.md)
- [Route 53](aws/route-53.md)
- [S3](aws/s3.md)
- [SES](aws/ses.md)
- [SNS](aws/sns.md)
- [SQS](aws/sqs.md)
- [SageMaker](aws/sagemaker.md)
- [Secrets Manager](aws/secrets-manager.md)
- [Step Functions](aws/step-functions.md)
- [VPC](aws/vpc.md)
- [WAF](aws/waf.md)
- [X-Ray](aws/x-ray.md)
- [Lake Formation](aws/lake-formation.md)
- [QuickSight](aws/quicksight.md)
- [MQ](aws/mq.md)
- [IoT Core](aws/iot-core.md)
- [App Mesh](aws/app-mesh.md)
- [CodeDeploy](aws/codedeploy.md)
- [CloudFormation full import](aws/cloudformation-full-import.md)
- [Shield](aws/shield.md)
- [Security Hub](aws/security-hub.md)
- [Config](aws/config.md)
- [Organizations](aws/organizations.md)
- [Control Tower](aws/control-tower.md)
- [Textract](aws/textract.md)
- [Transcribe](aws/transcribe.md)
- [Translate](aws/translate.md)
- [Rekognition](aws/rekognition.md)
- [Comprehend](aws/comprehend.md)

### GCP

- [Apigee](gcp/apigee.md)
- [App Engine](gcp/app-engine.md)
- [Artifact Registry](gcp/artifact-registry.md)
- [BigQuery](gcp/bigquery.md)
- [Bigtable](gcp/bigtable.md)
- [Cloud Armor](gcp/cloud-armor.md)
- [Cloud Build](gcp/cloud-build.md)
- [Cloud CDN](gcp/cloud-cdn.md)
- [Cloud DNS](gcp/cloud-dns.md)
- [Cloud Functions](gcp/cloud-functions.md)
- [Cloud Load Balancing](gcp/cloud-load-balancing.md)
- [Cloud Run](gcp/cloud-run.md)
- [Cloud Scheduler](gcp/cloud-scheduler.md)
- [Cloud SQL](gcp/cloud-sql.md)
- [Cloud Storage](gcp/cloud-storage.md)
- [Cloud Tasks](gcp/cloud-tasks.md)
- [Compute Engine](gcp/compute-engine.md)
- [Composer](gcp/composer.md)
- [Dataflow](gcp/dataflow.md)
- [Dataproc](gcp/dataproc.md)
- [Eventarc](gcp/eventarc.md)
- [Filestore](gcp/filestore.md)
- [Firestore](gcp/firestore.md)
- [GKE](gcp/gke.md)
- [IAM](gcp/iam.md)
- [Identity Platform](gcp/identity-platform.md)
- [Logging](gcp/logging.md)
- [Memorystore](gcp/memorystore.md)
- [Monitoring](gcp/monitoring.md)
- [Persistent Disk](gcp/persistent-disk.md)
- [Pub/Sub](gcp/pub-sub.md)
- [Secret Manager](gcp/secret-manager.md)
- [Spanner](gcp/spanner.md)
- [Trace](gcp/trace.md)
- [VPC](gcp/vpc.md)
- [Vertex AI](gcp/vertex-ai.md)
- [Workflows](gcp/workflows.md)
- [Dataplex](gcp/dataplex.md)
- [Looker](gcp/looker.md)
- [Cloud Deploy](gcp/cloud-deploy.md)
- [Error Reporting](gcp/error-reporting.md)
- [Profiler](gcp/profiler.md)
- [TPU](gcp/tpu.md)
- [Document AI](gcp/document-ai.md)
- [Vision AI](gcp/vision-ai.md)
- [Speech-to-Text](gcp/speech-to-text.md)
- [Translation](gcp/translation.md)

### Azure

- [AI Search](azure/ai-search.md)
- [AKS](azure/aks.md)
- [API Management](azure/api-management.md)
- [App Gateway](azure/app-gateway.md)
- [App Insights](azure/app-insights.md)
- [App Service](azure/app-service.md)
- [Azure AD B2C](azure/azure-ad-b2c.md)
- [Azure Cache](azure/azure-cache.md)
- [Azure CDN](azure/azure-cdn.md)
- [Azure DNS](azure/azure-dns.md)
- [Azure Firewall](azure/azure-firewall.md)
- [Azure Functions](azure/azure-functions.md)
- [Azure Load Balancer](azure/azure-load-balancer.md)
- [Azure SQL](azure/azure-sql.md)
- [Azure Storage](azure/azure-storage.md)
- [Azure VNet](azure/azure-vnet.md)
- [Azure VM](azure/azure-vm.md)
- [Container Apps](azure/container-apps.md)
- [Container Instances](azure/container-instances.md)
- [Container Registry](azure/container-registry.md)
- [Cosmos DB](azure/cosmos-db.md)
- [Data Factory](azure/data-factory.md)
- [Databricks](azure/databricks.md)
- [Event Grid](azure/event-grid.md)
- [Event Hubs](azure/event-hubs.md)
- [Foundry/OpenAI](azure/foundry-openai.md)
- [Front Door](azure/front-door.md)
- [IoT Hub](azure/iot-hub.md)
- [Key Vault](azure/key-vault.md)
- [Log Analytics](azure/log-analytics.md)
- [Logic Apps](azure/logic-apps.md)
- [Managed Disk](azure/managed-disk.md)
- [Monitor](azure/monitor.md)
- [MySQL](azure/mysql.md)
- [PostgreSQL](azure/postgresql.md)
- [Service Bus](azure/service-bus.md)
- [SignalR](azure/signalr.md)
- [Synapse](azure/synapse.md)
- [VM Scale Sets](azure/vm-scale-sets.md)
- [Data Lake](azure/data-lake.md)
- [Fabric](azure/fabric.md)
- [Power BI Embedded](azure/power-bi-embedded.md)
- [Logic Apps advanced](azure/logic-apps-advanced.md)
- [Notification Hubs](azure/notification-hubs.md)
- [DevOps Pipelines](azure/devops-pipelines.md)
- [Application Insights](azure/application-insights.md)
- [Automation](azure/automation.md)
- [Purview](azure/purview.md)
- [Machine Learning](azure/machine-learning.md)
- [Document Intelligence](azure/document-intelligence.md)
- [Speech](azure/speech.md)
- [Translator](azure/translator.md)

## Extra Core Plans

- [AWS STS](aws/sts.md)
- [Azure Blob Storage](azure/blob-storage.md)
