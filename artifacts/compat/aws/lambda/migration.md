# AWS Lambda to OpenFaaS

1. Export each `aws_lambda_function` ARN, code revision, tags, and source import id.
2. Apply `backend.yaml`, then deploy the compatible function containers to OpenFaaS.
3. Run `test/conformance/services/aws-lambda.yaml` before routing Lambda clients to `/compat/aws/lambda`.
4. Roll back by restoring the AWS endpoint; retain source import ids and the latest validated OpenFaaS backup for reconciliation.
