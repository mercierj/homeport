# AWS All-Services Operations Console Design

## Goal

Provide a Homeport operations console for every AWS service represented in a completed AWS migration. The UI preserves the AWS service mental model while operating only local target backends; it never contacts AWS.

## Coverage contract

The authoritative catalogue is `docs/coverage/services.yaml`. Its 59 AWS entries are all first-class console services: ACM, ALB, API Gateway, App Mesh, AppSync, Athena, Bedrock, CloudFormation, CloudFront, CloudWatch, CodeBuild, CodeDeploy, CodePipeline, Cognito, Comprehend, Config, Control Tower, DynamoDB, EBS, EC2, ECR, ECS, EFS, EKS, EMR, ElastiCache, EventBridge, Glue, GuardDuty, IAM, IoT Core, KMS, Kinesis, Lake Formation, Lambda, MQ, MSK, OpenSearch, Organizations, QuickSight, RDS, Redshift, Rekognition, Route 53, S3, SES, SNS, SQS, SageMaker, Secrets Manager, Security Hub, Shield, Step Functions, Textract, Transcribe, Translate, VPC, WAF, and X-Ray.

No catalogue service can be absent after it has migrated. A service without a usable local operation is still visible, with its migrated resources, target health, an explicit unavailable/degraded reason, and no falsely advertised action.

## Architecture

`AWSOperationsWorkspace` is a durable post-cutover projection. A cutover/deployment result writes a server-attested local binding for every imported resource: AWS service key, imported resource identity, target stack/backend, local identity, health and capabilities. The projection is activated atomically when cutover completes, independent of an SSE client.

The backend exposes a common service catalogue and resource contract, then dispatches to one driver per service family. Family drivers map resource records and local operations to target adapters; specialised drivers handle service-specific state and actions. Drivers never accept a browser-supplied local ID, evaluate capability and authorization before a backend call, and append every allow/deny decision to durable audit storage.

The web console renders a common service shell at `/aws/:service`, using service metadata from the API. It supplies a consistent resource list/detail/action layout and plugs in service-specific panels. A service tile is present for every migrated service, regardless of whether mutating capabilities are currently available.

## Capability model

Capabilities are resource-scoped, backend-proven values rather than frontend assumptions: `list`, `read`, `create`, `update`, `delete`, `invoke`, `logs`, `purge`, `retry`, plus family extensions. The service catalogue also exposes target health and an unavailable/degraded reason. The API omits capabilities whose target implementation cannot honestly provide the action.

## Security and reliability

Bindings originate only from trusted cutover/deployment output and are persisted before activation. Activation happens from the cutover completion transaction, not the SSE stream. Every mutation is authorized against the persisted binding, audited fail-closed, and uses a stable action name (`aws-operations:<service>:<action>`). The registry uses cross-process safe persistence.

## Testing

The catalogue has an exhaustive coverage test: each AWS entry has service metadata, a driver declaration, API visibility, a web route/panel registration, and conformance fixtures. Family contract tests prove bindings, capability gating, authorization and audit. Playwright iterates every service tile and confirms no AWS endpoint is requested.
