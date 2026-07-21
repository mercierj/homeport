# AWS CloudWatch Logs local compatibility seed

The adapter provides process-local CloudWatch Logs group and stream operations for endpoint-override checks. Loki is a migration target only: this artifact does not deploy it or prove persistence, backup, cutover, or rollback.

## Source identifiers

- `aws_cloudwatch_log_group`: preserve account, region, group name, streams, retention, tags, and source import ID when a real migration is authorized.

## Local verification

Run `go test ./test/compat -run CloudWatchLogs` for the supported contract.
