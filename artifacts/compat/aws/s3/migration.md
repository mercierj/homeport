# AWS S3 local compatibility seed

The adapter provides in-memory bucket and object operations for endpoint-override checks. MinIO is a migration target only: this artifact does not deploy it or prove persistence, backup, cutover, or rollback.

## Source identifiers

- `aws_s3_bucket`: preserve account, region, bucket name, ARN, tags, source import ID, and object namespace reference when a real migration is authorized.

## Local verification

Run `go test ./test/compat -run S3` for the supported contract.
