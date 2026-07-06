# HomePort Identity Secrets Keys And Certificates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automate identity, secret, key, and certificate migration while clearly handling provider-imposed impossibilities.

**Architecture:** Keycloak handles OIDC/SAML identity, Vault handles secrets and transit crypto, Let's Encrypt or imported certs handle TLS. Impossible exports become guided flows with validation, not loose manual notes.

**Tech Stack:** Keycloak, Vault, Traefik/Caddy cert automation, provider CLIs/APIs, existing secret resolver.

---

## Scope

- AWS: Cognito, IAM, Secrets Manager, KMS, ACM
- Google Cloud: Identity Platform, IAM, Secret Manager
- Azure: AD B2C, Key Vault

## Task 1: Secrets

- [ ] Replace secret manual export notes with provider API import steps where permissions allow reading values.
- [ ] For unreadable secrets, request value entry through encrypted UI input.
- [ ] Initialize Vault automatically in dev/self-hosted mode.
- [ ] Generate Vault policies from source permissions.
- [ ] Validate `GetSecretValue` compatibility where adapter exists.

## Task 2: KMS and key vault crypto

- [ ] Mark original key-material export as `Impossible` for managed non-exportable keys.
- [ ] Create Vault Transit keys with matching metadata.
- [ ] Build KMS-compatible decrypt/encrypt/sign/HMAC adapter for new operations.
- [ ] Add re-encryption runbook for existing ciphertext.
- [ ] Validate encrypt/decrypt roundtrip and re-encryption sample.

## Task 3: Identity

- [ ] Automate Keycloak realm/client/flow import from Cognito, Identity Platform, and AD B2C metadata.
- [ ] Migrate users where provider APIs allow it.
- [ ] For non-exportable passwords, add progressive migration or forced reset runbook.
- [ ] Configure email, MFA, social providers, and callback URLs.
- [ ] Validate login, token issuance, refresh, logout, and protected route access.

## Task 4: IAM policy translation

- [ ] Convert IAM roles/policies to application roles and Vault/Keycloak policies.
- [ ] Detect conditions unsupported by Keycloak/Vault and mark `Guided`.
- [ ] Add a policy diff screen showing source permissions and target permissions.
- [ ] Validate least-privilege checks with generated test principals.

## Task 5: Certificates and DNS challenges

- [ ] Automate ACM/Key Vault certificate import when exportable.
- [ ] Automate Let's Encrypt issuance with DNS provider credentials.
- [ ] For registrar/DNS providers without API, generate exact records and poll DNS until valid.
- [ ] Validate TLS chain and expiry monitoring.

## Task 6: Tests

- [ ] Add unit tests for impossible key export classification.
- [ ] Add Keycloak realm generation snapshot tests.
- [ ] Add Vault secret import tests.
- [ ] Commit with `git commit -m "feat: automate identity secrets keys and certs"`.
