# HomePort Messaging And Events Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove application rewrites for queues, topics, event buses, streams, and scheduled tasks where API compatibility is practical.

**Architecture:** Keep RabbitMQ/NATS/Redpanda as internal engines, but expose cloud-compatible APIs at the edge. Existing SDKs should talk to HomePort endpoints where possible.

**Tech Stack:** RabbitMQ, NATS, Redpanda/Kafka, n8n only for workflow replacement, Go compatibility adapters, provider SDK conformance tests.

---

## Scope

- AWS: SQS, SNS, EventBridge, Kinesis, SES
- Google Cloud: Pub/Sub, Pub/Sub subscriptions, Cloud Tasks, Cloud Scheduler
- Azure: Service Bus, Event Hubs, Event Grid, Logic Apps

## Task 1: Queue compatibility

- [ ] Build SQS-compatible adapter before changing SQS docs to `Full`.
- [ ] Map queue URL, visibility timeout, DLQ, retention, delay, FIFO, dedupe.
- [ ] Back adapter with RabbitMQ.
- [ ] Validate with AWS SDK send/receive/delete/change-visibility tests.

## Task 2: Topic/pubsub compatibility

- [ ] Build SNS-compatible publish/subscribe adapter.
- [ ] Add Pub/Sub-compatible adapter or mark Pub/Sub as `Guided`.
- [ ] Add Azure Service Bus-compatible adapter or mark as `Guided`.
- [ ] Validate topic publish, subscription delivery, retry, DLQ.

## Task 3: Streams

- [ ] For Kinesis/Event Hubs, expose either Kinesis-compatible API or Kafka-compatible path with explicit `Guided`.
- [ ] Preserve partitions/shards, retention, consumer groups, offsets.
- [ ] Validate producer/consumer flow and replay.

## Task 4: Event routing and schedules

- [ ] Convert EventBridge/Event Grid rules to executable routing config.
- [ ] Convert Cloud Scheduler to scheduler service with generated healthchecks.
- [ ] Convert Logic Apps only when workflow can be represented; otherwise mark `Guided` with generated review checklist.
- [ ] Validate trigger delivery end-to-end.

## Task 5: Email

- [ ] Replace SES Mailhog dev target with production-capable Postal/Maddy target.
- [ ] Automate DKIM/SPF/DMARC generation.
- [ ] Add DNS verification step.
- [ ] Validate SMTP send and bounce/webhook behavior.

## Task 6: Tests

- [ ] Add SDK conformance tests for SQS, SNS, Kinesis basic operations.
- [ ] Add one GCP Pub/Sub fixture and one Azure Service Bus fixture.
- [ ] Commit with `git commit -m "feat: add messaging compatibility migration path"`.
