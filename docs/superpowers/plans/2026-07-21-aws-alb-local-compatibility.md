# AWS ALB Local Compatibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a local AWS SDK v2 endpoint-override surface for the ALB management lifecycle without claiming a deployed Traefik backend.

**Architecture:** An in-memory `ALBAdapter` follows the existing AWS JSON compatibility-adapter pattern. It stores only ALB management metadata, enforces centralized authorization and an optional quota, and exposes Create/Describe/ModifyAttributes/Delete through `/compat/aws/alb`. Static artifacts and the conformance manifest document the exact local contract; the coverage level stays L3.

**Tech Stack:** Go, AWS SDK for Go v2 `elasticloadbalancingv2`, `httptest`, HomePort compatibility registry, central authz/audit domain.

---

### Task 1: Specify the provider lifecycle through failing SDK tests

**Files:**
- Create: `test/compat/aws_alb_test.go`

- [ ] **Step 1: Write the failing lifecycle test**

```go
server := httptest.NewServer(compataws.NewALBAdapter())
client := elbv2.NewFromConfig(testAWSConfig(server.URL))
created, err := client.CreateLoadBalancer(ctx, &elbv2.CreateLoadBalancerInput{
    Name: aws.String("edge"), Subnets: []string{"subnet-a", "subnet-b"},
    Type: types.LoadBalancerTypeEnumApplication,
})
// Assert the returned ARN, DNS name, and application type; then describe,
// modify `deletion_protection.enabled`, and delete the returned ARN.
```

- [ ] **Step 2: Run the lifecycle test and verify it fails because `NewALBAdapter` is undefined**

Run: `GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run TestALBCompatibilityAdapterLifecycleWithAWSSDK -count=1`

Expected: FAIL at compilation until the adapter exists.

### Task 2: Add the minimal local ALB adapter and registry entry

**Files:**
- Create: `internal/app/compat/aws/alb.go`
- Modify: `internal/app/compat/registry.go`

- [ ] **Step 1: Implement only the lifecycle contract**

```go
func (ALBAdapter) Provider() string { return "aws" }
func (ALBAdapter) Service() string  { return "alb" }
func (ALBAdapter) Routes() []string { return []string{"POST /compat/aws/alb"} }
```

Use `decodeAWSAction`, a mutex-protected ARN-keyed store, and provider-shaped JSON errors. Support `CreateLoadBalancer`, `DescribeLoadBalancers`, `ModifyLoadBalancerAttributes`, and `DeleteLoadBalancer`; reject malformed names, missing resources, and duplicates without mutation.

- [ ] **Step 2: Register the adapter**

```go
compataws.NewALBAdapter(),
```

Add it to `NewDefaultRegistry` alongside the other AWS adapters.

- [ ] **Step 3: Re-run the lifecycle test**

Run: `GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run TestALBCompatibilityAdapterLifecycleWithAWSSDK -count=1`

Expected: PASS.

### Task 3: Cover pagination, authz/audit, quota, and errors

**Files:**
- Modify: `test/compat/aws_alb_test.go`
- Modify: `internal/app/compat/aws/alb.go`

- [ ] **Step 1: Write failing tests for one-item `PageSize`/`Marker` pagination, denied create, quota exhaustion, duplicate creation, invalid names, and missing delete**

```go
server := httptest.NewServer(compataws.NewALBAdapter(
    compataws.WithALBQuota(1),
    compataws.WithALBAuthorizer(denyCreate),
    compataws.WithALBAuditSink(auditSink),
))
```

Assert AWS SDK `smithy.APIError` codes: `AccessDenied`, `TooManyLoadBalancers`, `DuplicateLoadBalancerName`, `ValidationError`, and `LoadBalancerNotFound`.

- [ ] **Step 2: Run the focused tests and verify each new behavior fails**

Run: `GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run 'TestALBCompatibilityAdapter(PaginatesLoadBalancers|AuthorizesAndAuditsCreate|RejectsQuota|ReturnsProviderErrors)' -count=1`

Expected: FAIL until the corresponding behavior is implemented.

- [ ] **Step 3: Implement the minimal supporting behavior**

Persist only name, ARN, scheme, type, DNS name, tags, and attributes; sort load balancers by name before paginating. Call the central authorizer before every supported operation and send each decision to the optional audit sink.

- [ ] **Step 4: Re-run the focused tests**

Run: `GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run 'TestALBCompatibilityAdapter' -count=1`

Expected: PASS.

### Task 4: Add local evidence and preserve the L3 boundary

**Files:**
- Create: `artifacts/compat/aws/alb/backend.yaml`
- Create: `artifacts/compat/aws/alb/adapter.yaml`
- Create: `artifacts/compat/aws/alb/migration.md`
- Create: `test/conformance/services/aws-alb.yaml`
- Modify: `docs/compat-plans/aws/alb.md`

- [ ] **Step 1: Add seed-only Traefik backend and adapter artifacts**

Document `/compat/aws/alb`, the four supported actions, authz/error/pagination/quota behavior, and that the state is in-memory rather than deployed Traefik proof.

- [ ] **Step 2: Add the conformance manifest**

```yaml
provider: aws
service: ALB
checks:
  api_compat: GOCACHE=/private/tmp/exit-gafam-go-build go test ./test/compat -run TestALBCompatibilityAdapter -count=1
```

- [ ] **Step 3: Update the ALB plan’s current-level evidence**

State that the local AWS SDK lifecycle, provider errors, pagination, authorization, audit, and quota checks are covered, while durable Traefik state, production validation, cutover, and rollback remain the L4 blockers.

### Task 5: Verify the integration boundary

**Files:**
- Test: `internal/app/compat/registry_test.go`
- Test: `internal/app/conformance/service_test.go`
- Test: `test/compat/aws_alb_test.go`

- [ ] **Step 1: Run the focused adapter and registry tests**

Run: `GOCACHE=/private/tmp/exit-gafam-go-build go test ./internal/app/compat ./test/compat -run 'Test(ALBCompatibilityAdapter|DefaultRegistry)' -count=1`

Expected: PASS.

- [ ] **Step 2: Run conformance and all AWS integration checks**

Run: `GOCACHE=/private/tmp/exit-gafam-go-build go test ./internal/app/conformance ./test/integration/aws -count=1`

Expected: PASS; ALB remains `mapped`/L3 because no live Traefik backend was deployed.

- [ ] **Step 3: Commit**

```bash
git add internal/app/compat/aws/alb.go internal/app/compat/registry.go test/compat/aws_alb_test.go artifacts/compat/aws/alb test/conformance/services/aws-alb.yaml docs/compat-plans/aws/alb.md docs/superpowers/plans/2026-07-21-aws-alb-local-compatibility.md
git commit -m "feat: add local AWS ALB compatibility adapter"
```
