# AWS API compatibility boundary audit — 2026-07-21

## Decision

`docs/coverage/services.yaml` is the authoritative contract for the intended
compatibility mechanism. A service marked `generated_patch` or `no_change`
does **not** expose an AWS endpoint, does not accept an AWS SDK endpoint
override, and does not require `artifacts/compat/aws/<service>/`.

Several older per-service plans were generated from an endpoint-adapter
template. Their `Adapter`, `Generated Artifacts`, `Contract Tests`, and L4
sections must therefore be read as superseded for the services below. They
describe an optional future AWS-emulation product, not an unimplemented part
of this repository's migration contract.

## Service-by-service result

| Services | Catalog strategy | Local proof | Local endpoint required |
| --- | --- | --- | --- |
| Athena, EMR, ElastiCache, Glue, OpenSearch, RDS, Redshift, QuickSight | `generated_patch` | database mapper conformance tests | No |
| EBS | `generated_patch` | storage mapper conformance test | No |
| Bedrock, EC2, SageMaker, Textract, Transcribe, Translate, Rekognition | `generated_patch` | compute mapper conformance tests | No |
| CodePipeline, CodeDeploy, CloudFormation full import | `generated_patch` | devops mapper conformance tests | No |
| MSK, MQ, IoT Core | `generated_patch` / `no_change` | messaging mapper conformance tests | No |
| CloudFront, Route 53, VPC, App Mesh | `generated_patch` / `no_change` | networking mapper conformance tests | No |
| GuardDuty, WAF, Lake Formation, Shield, Security Hub, Config, Organizations, Control Tower | `generated_patch` | security mapper conformance tests | No |
| X-Ray | `generated_patch` | monitoring mapper conformance test | No |

The corresponding manifests in `test/conformance/services/` intentionally
run these mapper contracts. Their `api_compat` key means *application
compatibility under the selected strategy*, not AWS SDK/REST wire
compatibility.

## Endpoint adapters

Services whose catalog strategy is `adapter` or `local adapter seed` are
separate: their endpoint overrides are implemented and registered in
`internal/app/compat/registry.go`, with SDK tests under `test/compat/`.

Bedrock formerly used the `adapter` label even though its mapper only emits
configuration for the external Ollama Bedrock adapter. It is now correctly
classified as `generated_patch`; no HomePort AWS endpoint is part of that
local contract.

The audit found two genuine local omissions in that group:

- ALB had no endpoint adapter. The local Query/XML adapter, route,
  authorization, audit, validation, pagination and quota tests are now in
  place.
- Comprehend had only lifecycle/pagination/authorization coverage. The
  adapter now distinguishes a missing classifier name from a duplicate,
  authorizes creation against the classifier parent ARN, and enforces a
  configurable classifier quota; these behaviours have SDK tests.

## Verification caveat

Some legacy endpoint-override tests also invoke `terraform init`. That step
downloads the HashiCorp AWS provider and is not a local, deployment-free
conformance check; without a populated provider cache it can block waiting
for the network. SDK and mapper checks remain the local verification source
for this audit.
