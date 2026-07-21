# AWS SNS local compatibility seed

The adapter provides process-local topics, subscriptions, and publish operations for endpoint-override checks. NATS is a migration target only: this artifact does not deploy it or prove persistence, backup, cutover, or rollback.

## Source identifiers

- `aws_sns_topic`: preserve account, region, topic name, ARN, tags, delivery policy, FIFO attributes, and source import ID when a real migration is authorized.

## Local verification

Run `go test ./test/compat -run SNS` for the supported contract.
