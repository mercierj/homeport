# HomePort Compute Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make VM, container, Kubernetes, and function migrations deploy and validate without hand-editing generated files.

**Architecture:** Normalize source compute into app units with image, env, ports, volumes, healthchecks, secrets, identity, scaling, and ingress. Generate target-specific deployments for Compose, Swarm, K3s/K8s, OVH, Scaleway, Hetzner, and on-prem.

**Tech Stack:** Existing compute mappers/executors, Docker build, Compose/K3s generators, provider target generators.

---

## Scope

- AWS: EC2, ECS service/task definition, EKS, Lambda
- Google Cloud: Compute Engine, Cloud Run, Cloud Functions, GKE, App Engine
- Azure: Linux/Windows VM, App Service, Functions, AKS, Container Instances

## Task 1: App unit model

- [ ] Create a normalized app unit model if existing mapper result is insufficient.
- [ ] Capture image/source path, runtime, command, env, secrets, ports, healthcheck, scaling, volumes, service account, ingress.
- [ ] Convert current compute mapper outputs into app units.
- [ ] Test ECS, Cloud Run, and App Service fixtures produce app units.

## Task 2: Build automation

- [ ] Detect whether source has a container image or source bundle.
- [ ] Pull existing images where possible.
- [ ] Build functions/apps automatically when source is available.
- [ ] If source is unavailable, block with an input step that asks for repo/image, then validate.
- [ ] Remove manual "build image" notes from compute consolidation.

## Task 3: Runtime targets

- [ ] Generate Compose target.
- [ ] Generate K3s/Kubernetes target.
- [ ] Generate provider target for Hetzner, Scaleway, OVH using existing target code.
- [ ] Inject env, DNS aliases, secrets, volumes, and healthchecks.
- [ ] Validate containers reach healthy state.

## Task 4: Serverless compatibility

- [ ] For Lambda/Cloud Functions/Azure Functions, preserve HTTP/event invocation where possible.
- [ ] Generate adapter endpoints for cloud-style invocation APIs.
- [ ] Convert triggers from messaging/storage plans.
- [ ] Validate function invocation with original event samples.

## Task 5: Kubernetes migration

- [ ] Export workloads from EKS/GKE/AKS.
- [ ] Preserve namespaces, deployments, services, ingress, configmaps, secrets, PVCs.
- [ ] Detect cloud-specific controllers and route to networking/storage/identity plans.
- [ ] Validate `kubectl get deploy`, service reachability, and ingress.

## Task 6: Tests

- [ ] Add fixtures for one app from each provider.
- [ ] Run compute mapper tests and generated compose validation.
- [ ] Commit with `git commit -m "feat: automate compute runtime migration"`.
