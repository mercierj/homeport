# HomePort Networking And Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automate traffic, DNS, edge, firewall, logs, metrics, dashboards, and alerts through validated runbook steps.

**Architecture:** Networking creates routes and certs before cutover; observability validates that migrated apps are visible and alertable. DNS registrar changes are guided only when no API exists, with automatic polling.

**Tech Stack:** Traefik/Caddy, CoreDNS/PowerDNS, ModSecurity/OPNsense where needed, Prometheus, Grafana, Loki, Alertmanager.

---

## Scope

- AWS: ALB, API Gateway, CloudFront, Route53, VPC, CloudWatch Logs/Metrics/Alarms/Dashboards
- Google Cloud: Cloud LB, Cloud DNS, Cloud CDN, VPC, Cloud Armor, Monitoring/Logging/Trace
- Azure: LB, App Gateway, DNS, CDN, Front Door, VNet, Firewall, Monitor, Log Analytics, App Insights

## Task 1: Routing

- [ ] Convert ALB/API Gateway/Cloud LB/App Gateway/LB routes into Traefik dynamic config.
- [ ] Preserve host/path methods, headers, redirects, auth hooks, timeouts.
- [ ] Validate route table with generated HTTP requests.
- [ ] Block unsupported auth/transforms until adapter exists.

## Task 2: DNS

- [ ] Automate Route53/Cloud DNS/Azure DNS export.
- [ ] Provision CoreDNS/PowerDNS zones.
- [ ] For external registrar changes, generate exact NS/A/CNAME/TXT records and poll public DNS.
- [ ] Validate internal and external resolution.

## Task 3: CDN and edge

- [ ] Convert CloudFront/Cloud CDN/Azure CDN/Front Door cache policies to Caddy/Varnish/Traefik config.
- [ ] Preserve origins, custom domains, TLS, compression, cache TTLs.
- [ ] Detect Lambda@Edge/edge functions and mark `Guided` unless converted.
- [ ] Validate cache hit/miss behavior.

## Task 4: Network and firewall

- [ ] Convert VPC/VNet/network/subnet intent into target network config.
- [ ] Generate host firewall/security group rules.
- [ ] Convert Cloud Armor/Azure Firewall/WAF rules where possible.
- [ ] Validate allowed and denied flows.

## Task 5: Logs and metrics

- [ ] Replace "update app logging" notes with generated agents/config/env.
- [ ] Add CloudWatch Logs compatibility adapter from Plan 03.
- [ ] Generate Prometheus scrape config from app units.
- [ ] Validate log ingestion and metric targets.

## Task 6: Dashboards and alerts

- [ ] Convert CloudWatch dashboards and alarms into Grafana/Prometheus/Alertmanager as far as metrics map.
- [ ] Mark unsupported metrics as `Guided` with exact missing signal.
- [ ] Validate alert rule syntax and test alert route.
- [ ] Commit with `git commit -m "feat: automate networking and observability migration"`.
