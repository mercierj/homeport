# AWS AppSync Migration

This is a local adapter seed for Hasura. It does not prove Hasura deployment, persistence, cutover, or rollback.

## Source import IDs

- `aws_appsync_graphql_api`: preserve API name, authentication type, schema, and tags as migration input.

## Unsupported actions

- Schema deployment, resolvers, data sources, keys, and GraphQL execution are outside this adapter.

## Operator decisions

1. Review the existing Hasura mapper output.
2. Keep AWS authoritative until external GraphQL validation is performed.
