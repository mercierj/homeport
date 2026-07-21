# AWS API Gateway local compatibility seed

The adapter provides an in-memory REST API management contract for local SDK checks. Kong is a migration target only: this artifact does not deploy it or prove persistence, backup, cutover, or rollback.

## Source identifiers

- `aws_api_gateway_rest_api`: preserve account, region, API ID, name, description, tags, and source import ID when a real migration is authorized.

## Local verification

Run `go test ./test/compat -run APIGateway` for the supported REST API lifecycle and management actions.
