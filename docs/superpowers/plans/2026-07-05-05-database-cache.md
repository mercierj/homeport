# HomePort Database And Cache Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make SQL, NoSQL, and cache migrations run without application rewrites where protocol/API compatibility is possible.

**Architecture:** Keep protocol-compatible databases where possible. Use API-compatible backends for DynamoDB and Redis. Treat semantic database changes as guided or impossible unless HomePort provides an adapter.

**Tech Stack:** Postgres, MySQL/MariaDB, Scylla Alternator, Redis, CockroachDB, MongoDB where compatible, existing datamigration executors.

---

## Scope

- AWS: RDS, RDS Cluster, DynamoDB, ElastiCache
- Google Cloud: Cloud SQL, Firestore, Bigtable, Memorystore, Spanner
- Azure: Azure SQL, Azure PostgreSQL, Azure MySQL, Cosmos DB, Azure Cache

## Task 1: SQL runbook

- [ ] Replace RDS/Cloud SQL/Azure SQL/Postgres/MySQL manual steps with runbook steps.
- [ ] Automate credential collection and validation.
- [ ] Automate dump/restore for small databases.
- [ ] Add live replication path for Postgres/MySQL when source supports it.
- [ ] Validate schema count, table count, row counts, sequences, extensions, and sampled checksums.

## Task 2: SQL endpoint preservation

- [ ] Generate connection-string aliases per app service.
- [ ] Inject env vars into generated compose/K3s/provider targets.
- [ ] Add DNS aliases for source hostnames when cutover mode allows it.
- [ ] Validate app container can connect before cutover.

## Task 3: Redis/cache full path

- [ ] Replace ElastiCache/Memorystore/Azure Cache manual steps with runbook steps.
- [ ] Automate auth token/password generation.
- [ ] Support TLS mode or mark as blocked until TLS proxy is generated.
- [ ] Support Redis single-node, Sentinel, and cluster mode.
- [ ] Sync with RDB or key-level DUMP/RESTORE.
- [ ] Validate key count, types, TTLs, streams, sampled values, and failover if HA is selected.

## Task 4: DynamoDB no-rewrite path

- [ ] Provision Scylla Alternator.
- [ ] Generate AWS SDK endpoint env vars and dummy credentials.
- [ ] Automate table/index/TTL migration.
- [ ] Add DynamoDB Streams coverage: adapter or `Guided` with reason.
- [ ] Validate with AWS SDK CRUD/query/scan tests.

## Task 5: Non-compatible databases

- [ ] Firestore: keep `Guided` unless HomePort ships Firestore API compatibility.
- [ ] Bigtable: keep `Guided` unless HomePort ships Bigtable-compatible API.
- [ ] Spanner: keep `Guided`; Postgres/Cockroach target requires query/driver changes.
- [ ] Cosmos DB: split by API mode: Mongo-compatible can be closer to no-rewrite; SQL/Gremlin/Table need adapters or guided migration.

## Task 6: Tests

- [ ] Add a fixture with Postgres, Redis, DynamoDB.
- [ ] Add conformance tests using original clients: `lib/pq` or app DSN, Redis client, AWS DynamoDB SDK.
- [ ] Commit with `git commit -m "feat: automate database and cache migrations"`.
