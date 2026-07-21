# AWS-First Endpoint Override Roadmap

## Goal

Make HomePort an AWS-compatible HTTP endpoint layer implemented in Go, so official AWS SDKs, AWS CLI, Terraform, and similar tools can point at HomePort through standard endpoint override settings.

This roadmap does not target SDK forks, monkey-patching, or language wrappers that reimplement AWS behavior. Optional helpers can exist later, but only to configure official clients.

## Official Integration Model

- HomePort exposes service-shaped AWS HTTP endpoints such as `/compat/aws/s3`, `/compat/aws/dynamodb`, `/compat/aws/sqs`, `/compat/aws/sns`, `/compat/aws/ses`, `/compat/aws/kms`, `/compat/aws/secretsmanager`, `/compat/aws/cloudwatchlogs`, `/compat/aws/kinesis`, `/compat/aws/lambda`, `/compat/aws/eventbridge`, `/compat/aws/acm`, `/compat/aws/cognito`, `/compat/aws/ecs`, `/compat/aws/apigateway`, `/compat/aws/efs`, `/compat/aws/eks`, and `/compat/aws/iam`.
- AWS clients use native endpoint override mechanisms: service-specific `AWS_ENDPOINT_URL_*`, CLI `--endpoint-url`, Terraform provider endpoint configuration, or SDK endpoint options.
- HomePort credentials are accepted only as compatibility credentials for the HomePort endpoint. They are not a claim of AWS IAM parity.
- The compatibility implementation lives in Go under `internal/app/compat/aws/`; language examples should stay configuration-only.

## Current AWS Core State

| Service | Endpoint | Existing evidence | Current level |
| --- | --- | --- | --- |
| S3 | `AWS_ENDPOINT_URL_S3`, `/compat/aws/s3` | Go AWS SDK tests cover bucket/object lifecycle, ETag, tags, pagination, idempotency seeds, authz context, selected errors, quota seed, and audit seed. AWS CLI smoke covers create bucket, put/get/delete object through `--endpoint-url`. | L3 seed |
| DynamoDB | `AWS_ENDPOINT_URL_DYNAMODB`, `/compat/aws/dynamodb` | Go AWS SDK test covers CreateTable -> DescribeTable -> PutItem -> GetItem -> Query -> DeleteTable against the Go adapter. AWS CLI smoke covers create/describe/put/get/query/delete through `--endpoint-url`. | L3 seed |
| SQS | `AWS_ENDPOINT_URL_SQS`, `/compat/aws/sqs` | Go AWS SDK tests cover queue lifecycle, send/receive/delete, batch operations, FIFO seeds, tags, pagination, validation, quotas, idempotency, authz, and audit. AWS CLI smoke covers create/send/receive/delete through `--endpoint-url`. | L3 seed |
| SNS | `AWS_ENDPOINT_URL_SNS`, `/compat/aws/sns` | Go AWS SDK tests cover topic/subscription lifecycle, publish, list pagination, selected validation/errors, deduplication seed, authz, audit, and minimal HTTP delivery. AWS CLI smoke covers create-topic/publish/list/delete through `--endpoint-url`. | L3 seed |
| SES | `AWS_ENDPOINT_URL_SES`, `/compat/aws/ses` | Go AWS SDK test covers VerifyDomainIdentity -> GetIdentityVerificationAttributes -> ListIdentities -> DeleteIdentity against the Go adapter. AWS CLI smoke covers verify/get/list/delete through `--endpoint-url`. | L3 seed |
| KMS | `AWS_ENDPOINT_URL_KMS`, `/compat/aws/kms` | Go AWS SDK tests cover key lifecycle, encrypt/decrypt/MAC happy paths, pagination, selected errors, quota seed, authz, and audit. AWS CLI smoke covers create/describe/list/schedule-delete through `--endpoint-url`. | L3 seed |
| Secrets Manager | `AWS_ENDPOINT_URL_SECRETSMANAGER`, `/compat/aws/secretsmanager` | Go AWS SDK tests cover create/update/delete/get/describe/list, version-stage reads, pagination validation, quota seed, authz, and audit. AWS CLI smoke covers create/get/put/delete through `--endpoint-url`. | L3 seed |
| CloudWatch Logs | `AWS_ENDPOINT_URL_CLOUDWATCHLOGS`, `/compat/aws/cloudwatchlogs` | Go AWS SDK tests cover log group/stream lifecycle, put/get/describe, pagination validation, retention, selected errors, quota seed, authz, audit, and manifest metadata. AWS CLI smoke covers create group/stream, put/get events, and delete group through `--endpoint-url`. | L3 seed |
| Kinesis | `AWS_ENDPOINT_URL_KINESIS`, `/compat/aws/kinesis` | Go AWS SDK tests cover stream lifecycle, put/get records, pagination, shard iterator advancement, retention, split/merge seeds, selected errors, quota seed, authz, and audit. AWS CLI smoke covers create/describe/put/get records through `--endpoint-url`. | L3 seed |
| Lambda | `AWS_ENDPOINT_URL_LAMBDA`, `/compat/aws/lambda` | Go AWS SDK test covers CreateFunction -> GetFunction -> Invoke -> DeleteFunction against the Go adapter. AWS CLI smoke covers create/get/invoke/delete through `--endpoint-url`. | L3 seed |
| EventBridge | `AWS_ENDPOINT_URL_EVENTBRIDGE`, `/compat/aws/eventbridge` | Go AWS SDK test covers PutRule -> ListRules -> PutEvents -> DeleteRule against the Go adapter. AWS CLI smoke covers put/list rule, put events, and delete rule through `--endpoint-url`. | L3 seed |
| ACM | `AWS_ENDPOINT_URL_ACM`, `/compat/aws/acm` | Go AWS SDK test covers RequestCertificate -> DescribeCertificate -> ListCertificates -> DeleteCertificate against the Go adapter. AWS CLI smoke covers request/describe/list/delete through `--endpoint-url`. | L3 seed |
| Cognito | `AWS_ENDPOINT_URL_COGNITO_IDP`, `/compat/aws/cognito` | Go AWS SDK test covers CreateUserPool -> DescribeUserPool -> ListUserPools -> UpdateUserPool -> DeleteUserPool against the Go adapter. AWS CLI smoke covers create/describe/list/update/delete through `--endpoint-url`. | L3 seed |
| ECS | `AWS_ENDPOINT_URL_ECS`, `/compat/aws/ecs` | Go AWS SDK test covers CreateService -> DescribeServices -> ListServices -> UpdateService -> DeleteService against the Go adapter. AWS CLI smoke covers create/describe/list/update/delete through `--endpoint-url`. | L3 seed |
| API Gateway | `AWS_ENDPOINT_URL_APIGATEWAY`, `/compat/aws/apigateway` | Go AWS SDK test covers CreateRestApi -> GetRestApi -> GetRestApis -> UpdateRestApi -> DeleteRestApi against the Go adapter. AWS CLI smoke covers create/get/list/update/delete through `--endpoint-url`. | L3 seed |
| EFS | `AWS_ENDPOINT_URL_EFS`, `/compat/aws/efs` | Go AWS SDK test covers CreateFileSystem -> DescribeFileSystems -> UpdateFileSystem -> DeleteFileSystem against the Go adapter. AWS CLI smoke covers create/describe/update/delete through `--endpoint-url`. | L3 seed |
| EKS | `AWS_ENDPOINT_URL_EKS`, `/compat/aws/eks` | Go AWS SDK test covers CreateCluster -> DescribeCluster -> ListClusters -> UpdateClusterConfig -> DeleteCluster against the Go adapter. AWS CLI smoke covers create/describe/list/update/delete through `--endpoint-url`. | L3 seed |
| IAM | `AWS_ENDPOINT_URL_IAM`, `/compat/aws/iam` | Go AWS SDK test covers CreateRole -> GetRole -> ListRoles -> UpdateRole -> DeleteRole against the Go adapter. AWS CLI smoke covers create/get/list/update/delete through `--endpoint-url`. | L3 seed |

All eighteen are useful because official AWS SDK for Go v2 clients already reach Go HTTP adapters through endpoint override. None is provider-grade L4 yet.

## Full AWS Catalog Direction

The AWS catalog currently has 59 service plans under `docs/compat-plans/aws/` with matching `test/conformance/services/aws-*.yaml` manifests. Treat those plans as the full AWS backlog, not as proven support.

- Registered Go HTTP adapters: S3, DynamoDB, SQS, SNS, SES, Kinesis, KMS, Secrets Manager, CloudWatch Logs, Lambda, EventBridge, ACM, Cognito, ECS, API Gateway, EFS, EKS, and IAM.
- Native endpoint-compatible backends without HomePort API adapters: SSM/Vault seed and Redis native protocol.
- Planned next adapter families: compute/control plane (`lambda`, `ecs`, `eks`, `ec2`, `ecr`), data (`dynamodb`, `rds`, `opensearch`, `redshift`, `athena`), events/messaging (`eventbridge`, `mq`, `msk`, `ses`), security/identity (`iam`, `cognito`, `acm`, `waf`, `security-hub`, `guardduty`), and observability/AI/edge services.
- Promotion rule: a service moves only as far as runnable endpoint-override contracts prove. Mapper-only manifests remain migration scaffolding evidence.

## What Blocks Provider-Grade Support

- Official-tool breadth is thin: Go SDK and AWS CLI are seeded for the eighteen registered adapters, and boto3, JavaScript, Java, Terraform, and most AWS services are not broadly proven.
- Backends are mostly in-memory adapter seeds, not durable MinIO, RabbitMQ, NATS, Vault, Loki, or Redpanda integrations with backup/validate/cutover/rollback evidence.
- Error parity is partial: common provider-shaped errors exist, but complete action-by-action AWS error shapes are not proven.
- Authz is meaningful but incomplete: `Authorize(principal, action, resource, context)` is seeded across AWS Core, but full per-action matrices and cross-service IAM-style enforcement remain open.
- Quotas, idempotency, pagination, FIFO, delivery retries, retention, and audit are covered by useful seeds, not complete provider-grade contracts.
- Conformance manifests still mix mapper/runtime checks with API compatibility checks; L4 needs runnable contracts for each required behavior.

## First Service Recommendation

Start with SQS for real usability.

SQS has the broadest useful adapter surface today, avoids S3 path-style/virtual-host addressing pitfalls for early official-tool smoke tests, and maps cleanly to a European/self-hosted RabbitMQ target. S3 stays the next candidate because object storage is high-value and MinIO is a strong backend, but it should not be the first CLI/Terraform proof unless path-style endpoint behavior is part of the slice.

## Minimal Next Slices

1. Add the first SDK lifecycle test for one planned-but-unregistered service at a time; EKS is the current template for REST compute/control-plane services.
2. Add Terraform endpoint-override fixtures only after the service has a stable Go adapter, SDK lifecycle test, and AWS CLI smoke.
3. Add one non-Go SDK smoke, probably boto3, after the selected service has CLI coverage.
4. Wire one service at a time to its real self-hosted backend, starting with SQS/RabbitMQ or S3/MinIO.
5. Add backup/validate/cutover/rollback checks only after the real backend path exists.

## Non-Goals For This Phase

- No Azure or GCP micro-slices.
- No claims of full AWS, 100 percent compatibility, L4, or production readiness.
- No generated language SDK forks, monkey patches, or wrappers with AWS business logic.
- No broad cross-language matrix until one AWS Core service has a proven official-tool path.
