# AWS Post-Migration Operations Console Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a Homeport-branded AWS operations console that becomes available service-by-service after successful cutover and manages the local targets for migrated Lambda and SQS resources.

**Architecture:** Persist an AWS operations workspace as a post-cutover projection of an imported discovery. The projection exposes only activated services and resource bindings; provider drivers translate Lambda and SQS actions to the existing Homeport functions and queues services. A new API serves service discovery, resource lists, capabilities, and operations. The React console consumes that API under a dedicated `/aws` route and never participates in import or migration screens.

**Tech Stack:** Go, chi, existing migration state store, Homeport functions/queues application services, React, TypeScript, TanStack Query, Tailwind, Vitest/Playwright.

---

## Product boundary

- The console is available only after an explicit `cutover_completed` activation for an imported AWS discovery.
- Activation is per service: a project may expose Lambda while SQS remains unavailable.
- The UI uses Homeport’s own design system and copy. AWS service names are descriptive labels only; do not reuse AWS visual assets, page copy, layouts, screenshots, icons, or branding.
- The console shows only migrated resources and local runtime state. It must not call AWS credentials or AWS APIs.
- A capability response controls each action. The client must not infer that create, update, delete, invoke, purge, retry, or message inspection is supported.

## Data contracts

Create `internal/app/awsoperations/types.go` with the following contracts. Keep these provider-neutral enough for a future non-AWS read model, but expose only AWS routes in this plan.

```go
type ServiceKey string

const (
    ServiceLambda ServiceKey = "lambda"
    ServiceSQS    ServiceKey = "sqs"
)

type ServiceStatus string

const (
    ServiceStatusAvailable   ServiceStatus = "available"
    ServiceStatusUnavailable ServiceStatus = "unavailable"
    ServiceStatusDegraded    ServiceStatus = "degraded"
)

type Capability string

const (
    CapabilityList   Capability = "list"
    CapabilityRead   Capability = "read"
    CapabilityCreate Capability = "create"
    CapabilityUpdate Capability = "update"
    CapabilityDelete Capability = "delete"
    CapabilityInvoke Capability = "invoke"
    CapabilityLogs   Capability = "logs"
    CapabilityPurge  Capability = "purge"
    CapabilityRetry  Capability = "retry"
)

type ResourceBinding struct {
    ImportedResourceID string            `json:"imported_resource_id"`
    Service            ServiceKey        `json:"service"`
    LocalResourceID    string            `json:"local_resource_id"`
    Name               string            `json:"name"`
    Region             string            `json:"region,omitempty"`
    Tags               map[string]string `json:"tags,omitempty"`
}

type Workspace struct {
    ID                string                      `json:"id"`
    DiscoveryID       string                      `json:"discovery_id"`
    Name              string                      `json:"name"`
    Provider          string                      `json:"provider"`
    CutoverCompletedAt time.Time                  `json:"cutover_completed_at"`
    Services          map[ServiceKey]ServiceState `json:"services"`
    Bindings          []ResourceBinding           `json:"bindings"`
}

type ServiceState struct {
    Status       ServiceStatus `json:"status"`
    Capabilities []Capability  `json:"capabilities"`
    Reason       string        `json:"reason,omitempty"`
}
```

`Workspace` is the authoritative post-cutover read model. `DiscoveryState` remains the immutable import snapshot; do not overload it with runtime mutation state.

## Task 1: Persist the post-cutover AWS operations workspace

**Files:**
- Create: `internal/app/awsoperations/types.go`
- Create: `internal/app/awsoperations/store.go`
- Create: `internal/app/awsoperations/service.go`
- Create: `internal/app/awsoperations/service_test.go`
- Modify: `internal/app/migrate/state.go`
- Modify: `internal/api/handlers/cutover.go`
- Test: `internal/api/handlers/cutover_test.go`

- [ ] **Step 1: Write failing service tests for service-level visibility**

Add tests that construct a workspace from an AWS discovery containing `aws_lambda_function`, `aws_sqs_queue`, and an unsupported resource. Assert that only Lambda and SQS exist, both begin unavailable, and no workspace is returned before cutover completion.

```go
func TestServiceActivatesOnlyMigratedAWSServiceAfterCutover(t *testing.T) {
    workspace := service.Activate(ActivationInput{
        Discovery: discoveryWith("aws_lambda_function", "aws_sqs_queue", "aws_s3_bucket"),
        Activated: []awsoperations.ServiceKey{awsoperations.ServiceLambda},
    })

    require.Equal(t, awsoperations.ServiceStatusAvailable, workspace.Services[awsoperations.ServiceLambda].Status)
    require.Equal(t, awsoperations.ServiceStatusUnavailable, workspace.Services[awsoperations.ServiceSQS].Status)
    require.NotContains(t, workspace.Services, awsoperations.ServiceKey("s3"))
}
```

- [ ] **Step 2: Run the service tests and confirm the red state**

Run: `GOCACHE=/private/tmp/exit-gafam-go-build go test ./internal/app/awsoperations -run TestServiceActivatesOnlyMigratedAWSServiceAfterCutover -count=1`

Expected: failure because the package and activation service do not exist.

- [ ] **Step 3: Implement immutable workspace persistence**

Implement a file-backed store beside the existing migration state store. Use a mutex, atomic write through a temporary file and rename, and stable UUID workspace IDs. The service must:

1. Load the discovery identified by `DiscoveryID`.
2. Reject non-AWS discoveries.
3. Derive eligible services from resource types (`aws_lambda_function` and `aws_sqs_queue` initially).
4. Mark only explicitly activated eligible services as `available`.
5. Create one `ResourceBinding` per imported resource, preserving imported ID, name, region and tags.
6. Persist `CutoverCompletedAt` and return the workspace.

Do not let the handler accept arbitrary bindings or capabilities from the browser.

- [ ] **Step 4: Wire successful cutover to activation**

Extend the cutover completion path to call `awsoperations.Service.Activate` only after the cutover executor reports completed status. Add `discovery_id` and the activated service keys to the cutover request, validate both, and leave existing cutover behaviour unchanged when the fields are absent.

- [ ] **Step 5: Verify the green service and handler tests**

Run:

```bash
GOCACHE=/private/tmp/exit-gafam-go-build go test ./internal/app/awsoperations ./internal/api/handlers -run 'Test(ServiceActivatesOnlyMigratedAWSServiceAfterCutover|Cutover.*Activation)' -count=1
```

Expected: PASS. Tests must prove no workspace exists before completion, only selected migrated services are available, and a failed/cancelled cutover does not activate a workspace.

## Task 2: Expose post-migration AWS operations APIs

**Files:**
- Create: `internal/api/handlers/awsoperations.go`
- Create: `internal/api/handlers/awsoperations_test.go`
- Modify: `internal/api/server.go`
- Modify: `api/openapi.yaml`
- Test: `internal/api/server_test.go`

- [ ] **Step 1: Write failing HTTP contract tests**

Cover these routes with a real `httptest` server and persisted workspace fixture:

```text
GET /api/v1/aws/operations/workspaces
GET /api/v1/aws/operations/workspaces/{workspaceID}
GET /api/v1/aws/operations/workspaces/{workspaceID}/services
GET /api/v1/aws/operations/workspaces/{workspaceID}/services/lambda/resources
GET /api/v1/aws/operations/workspaces/{workspaceID}/services/sqs/resources
```

Assert `404` for unknown workspace, `404` for an unavailable service, and that the service response exposes the capability list rather than a boolean guessed by the UI.

- [ ] **Step 2: Run the handler tests and confirm the red state**

Run: `GOCACHE=/private/tmp/exit-gafam-go-build go test ./internal/api/handlers -run TestAWSOperations -count=1`

Expected: failure because the handler and routes do not exist.

- [ ] **Step 3: Implement read-only workspace and service discovery endpoints**

`AWSOperationsHandler` loads workspaces through `awsoperations.Service`. It must return empty arrays rather than `null`, preserve the operation workspace ID in responses, and reject GCP/Azure data by construction. Register it under `/api/v1/aws/operations` in `internal/api/server.go`.

- [ ] **Step 4: Document the public contract**

Add OpenAPI schemas for `AWSOperationsWorkspace`, `AWSOperationsService`, `AWSResourceBinding`, and the five GET endpoints. Document that `available` means a post-cutover local backend is eligible for management; it does not imply AWS is reachable.

- [ ] **Step 5: Verify API contracts**

Run:

```bash
GOCACHE=/private/tmp/exit-gafam-go-build go test ./internal/api/handlers ./internal/api -run 'TestAWSOperations|TestServer' -count=1
```

Expected: PASS, including unavailable-service and unknown-workspace paths.

## Task 3: Add Lambda and SQS provider drivers

**Files:**
- Create: `internal/app/awsoperations/driver.go`
- Create: `internal/app/awsoperations/lambda_driver.go`
- Create: `internal/app/awsoperations/sqs_driver.go`
- Create: `internal/app/awsoperations/driver_test.go`
- Modify: `internal/api/handlers/awsoperations.go`
- Modify: `internal/api/handlers/functions.go`
- Modify: `internal/api/handlers/queues.go`
- Test: `internal/api/handlers/awsoperations_test.go`

- [ ] **Step 1: Write failing driver tests for capability-gated operations**

Use fakes for the existing functions and queues application services. Test that:

```go
func TestLambdaDriverRejectsUpdateOutsideWorkspaceBinding(t *testing.T)
func TestLambdaDriverInvokesOnlyBoundFunction(t *testing.T)
func TestSQSDriverListsOnlyBoundQueues(t *testing.T)
func TestSQSDriverRejectsPurgeWhenCapabilityIsAbsent(t *testing.T)
```

The tests must prove a caller cannot pass an arbitrary local function ID or queue name belonging to another workspace.

- [ ] **Step 2: Run the driver tests and confirm the red state**

Run: `GOCACHE=/private/tmp/exit-gafam-go-build go test ./internal/app/awsoperations -run 'Test(LambdaDriver|SQSDriver)' -count=1`

Expected: failure because drivers do not exist.

- [ ] **Step 3: Implement the driver interface and Lambda driver**

Define:

```go
type Driver interface {
    Service() ServiceKey
    List(ctx context.Context, workspace Workspace) ([]any, error)
    Capabilities(workspace Workspace) []Capability
}
```

The Lambda driver delegates list/get/create/update/delete/invoke/log reads to the existing functions service. It maps the bound local function ID to a Lambda-facing resource response with name, runtime, handler, memory, timeout, environment, status, invocation count, timestamps and imported metadata. It must reject mutations for unavailable services and unbound resource IDs before calling the downstream service.

- [ ] **Step 4: Implement the SQS driver**

The SQS driver delegates queue and message reads, retry and purge to the queues service. It exposes queue metadata, message counts, messages and message details only for queue bindings in the workspace. Do not expose generic queue creation or queue deletion until the queues backend provides those operations; omit those capabilities from the response.

- [ ] **Step 5: Add driver-backed REST operations**

Register only supported endpoints:

```text
GET    /workspaces/{id}/services/lambda/resources
GET    /workspaces/{id}/services/lambda/resources/{resourceID}
POST   /workspaces/{id}/services/lambda/resources
PUT    /workspaces/{id}/services/lambda/resources/{resourceID}
DELETE /workspaces/{id}/services/lambda/resources/{resourceID}
POST   /workspaces/{id}/services/lambda/resources/{resourceID}/invoke
GET    /workspaces/{id}/services/lambda/resources/{resourceID}/logs
GET    /workspaces/{id}/services/sqs/resources
GET    /workspaces/{id}/services/sqs/resources/{resourceID}/messages
POST   /workspaces/{id}/services/sqs/resources/{resourceID}/messages/{messageID}/retry
DELETE /workspaces/{id}/services/sqs/resources/{resourceID}/messages/{messageID}
DELETE /workspaces/{id}/services/sqs/resources/{resourceID}/messages
```

Return `409` for an unavailable/degraded service and `403` for a resource that is not bound to the workspace. Keep request/response models in the operations handler; do not expose internal service structs directly.

- [ ] **Step 6: Verify driver isolation and existing endpoints**

Run:

```bash
GOCACHE=/private/tmp/exit-gafam-go-build go test ./internal/app/awsoperations ./internal/api/handlers -run 'Test(LambdaDriver|SQSDriver|AWSOperations|Functions|Queues)' -count=1
```

Expected: PASS. Existing generic `/functions` and `/stacks/{stackID}/queues` routes remain compatible.

## Task 4: Build the post-migration AWS console shell

**Files:**
- Create: `web/src/lib/aws-operations-api.ts`
- Create: `web/src/lib/aws-operations-types.ts`
- Create: `web/src/pages/AWSOperations.tsx`
- Create: `web/src/components/aws-operations/AWSServiceGrid.tsx`
- Create: `web/src/components/aws-operations/AWSOperationsEmptyState.tsx`
- Create: `web/src/components/aws-operations/AWSServiceUnavailable.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/navigation/Sidebar.tsx`
- Test: `web/src/pages/AWSOperations.test.tsx`

- [ ] **Step 1: Write failing component tests for visibility and navigation**

Mock the operations API with three cases:

1. no workspace: the sidebar entry and route content are absent from normal navigation;
2. a workspace with Lambda available and SQS unavailable: the page shows Lambda as actionable and SQS as unavailable with its reason;
3. a workspace with Lambda and SQS available: both cards navigate to their service pages.

- [ ] **Step 2: Run the frontend test and confirm the red state**

Run: `cd web && npm test -- AWSOperations`

Expected: failure because the route, API client and components do not exist.

- [ ] **Step 3: Implement the typed operations client**

Create API functions for workspace listing, workspace detail, services and service resources. Use `fetchAPI`, keep the workspace ID in TanStack Query keys, and model service state/capabilities as string unions. Do not use the existing migration API client here.

- [ ] **Step 4: Implement the Homeport operations landing page**

Add `/aws` to `App.tsx`. `AWSOperations` selects the active workspace, renders original Homeport cards with service name, resource count, availability, target health and available actions, and handles loading/error/empty states. Add a sidebar section labelled `AWS operations` only when the workspace-list query finds an available workspace. Do not use AWS logos, screenshots, copied labels or copied layout.

- [ ] **Step 5: Verify the page and typecheck**

Run:

```bash
cd web && npm test -- AWSOperations && npm run lint && npm run build
```

Expected: PASS.

## Task 5: Build Lambda and SQS operational pages

**Files:**
- Create: `web/src/pages/AWSLambda.tsx`
- Create: `web/src/pages/AWSSQS.tsx`
- Create: `web/src/components/aws-operations/LambdaResourceList.tsx`
- Create: `web/src/components/aws-operations/LambdaResourceDetail.tsx`
- Create: `web/src/components/aws-operations/SQSResourceList.tsx`
- Create: `web/src/components/aws-operations/SQSResourceDetail.tsx`
- Modify: `web/src/App.tsx`
- Test: `web/src/pages/AWSLambda.test.tsx`
- Test: `web/src/pages/AWSSQS.test.tsx`
- Test: `web/tests/aws-operations.spec.ts`

- [ ] **Step 1: Write failing Lambda page tests**

Test that the Lambda page lists bound functions, opens a detail panel, renders configuration and environment safely, and renders only actions in the backend capability response. Test invoke success and a `409` degraded-backend response without clearing the current resource view.

- [ ] **Step 2: Implement Lambda page and actions**

Implement an original Homeport resource list/detail view. Include filters by name/region, runtime and status; detail tabs for configuration, code metadata, environment, invocation and logs; create/edit/delete/invoke controls conditional on capabilities. Require confirmation before delete. Show operation feedback through existing `sonner` toasts.

- [ ] **Step 3: Write failing SQS page tests**

Test bound-queue listing, message status filtering, message detail, retry and purge confirmation. Assert there is no Create/Delete Queue control when the API omits those capabilities.

- [ ] **Step 4: Implement SQS page and actions**

Implement queue cards/list, count badges, message-state filters and a message detail drawer. Render retry/delete/purge only from capabilities. Require a confirmation dialog for purge and delete. Include the imported region and tags in read-only resource metadata.

- [ ] **Step 5: Add browser E2E coverage**

Add a Playwright spec that seeds a workspace with Lambda and SQS active, visits `/aws`, traverses both services, invokes a Lambda test payload, retries one failed SQS message and asserts no migration route is used. Add a second case with only Lambda active and assert SQS is visibly unavailable but inaccessible for mutations.

- [ ] **Step 6: Verify the frontend acceptance suite**

Run:

```bash
cd web && npm test -- AWSLambda AWSSQS && npm run lint && npm run build && npx playwright test tests/aws-operations.spec.ts
```

Expected: PASS.

## Task 6: Integrate authorization, audit and documentation

**Files:**
- Modify: `internal/api/handlers/awsoperations.go`
- Modify: `internal/domain/authz/*` (only the existing action registration file used by current handlers)
- Modify: `docs/web-dashboard.md`
- Modify: `README.md`
- Test: `internal/api/handlers/awsoperations_test.go`
- Test: `web/tests/aws-operations.spec.ts`

- [ ] **Step 1: Write failing authorization/audit tests**

Add one allowed and one denied operation for each service: Lambda update and SQS purge. Assert denial occurs before the local backend call and that both decisions produce audit events with workspace ID, service, bound resource ID and action.

- [ ] **Step 2: Implement authorization and audit boundaries**

Map actions to Homeport operations names such as `aws-operations:lambda:update` and `aws-operations:sqs:purge`. Evaluate authorization before driver dispatch. Use the workspace resource binding as the authorization resource; never trust a client supplied local ID. Persist audit decisions through the repository’s existing audit sink.

- [ ] **Step 3: Document activation and operation limits**

Document that operators activate a workspace after successful cutover; only services recorded as available can be managed. List Lambda and SQS capabilities and explicitly state that the console is a Homeport UI, not the AWS console and not an AWS control-plane proxy.

- [ ] **Step 4: Run final focused verification**

Run:

```bash
GOCACHE=/private/tmp/exit-gafam-go-build go test ./internal/app/awsoperations ./internal/api/handlers ./internal/api -count=1
cd web && npm test && npm run lint && npm run build && npx playwright test tests/aws-operations.spec.ts
git diff --check
```

Expected: all commands exit `0`. If unrelated pre-existing tests fail, record their package, test name and output separately; do not attribute them to this feature.

## Plan self-review

- Scope: the plan creates a post-cutover AWS console and deliberately excludes import UI, GCP and Azure.
- Safety: every mutating operation is capability-gated, workspace-bound, authorized and audited.
- Fidelity: Lambda and SQS are implemented with the existing local services, while the service driver abstraction permits later AWS service additions without copying AWS UI assets.
- Deferred capability: SQS queue creation/deletion is absent because the existing queues backend has no corresponding operation; the API and UI must truthfully omit it until a backend capability is added.
