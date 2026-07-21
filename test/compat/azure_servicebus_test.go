package compat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/servicebus/armservicebus"
	compatazure "github.com/homeport/homeport/internal/app/compat/azure"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestAzureServiceBusCompatibilityAdapterAuthorizesAndAuditsQueueWrites(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
		)),
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), strings.NewReader(`{"properties":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer homeport")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Create queue request error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Create queue status = %d, want 403", resp.StatusCode)
	}

	assertDecision(t, auditLog.Decisions(), "Microsoft.ServiceBus/namespaces/queues/write", false)
}

func TestAzureServiceBusCompatibilityAdapterAuditsAllowedAndDeniedQueueWrites(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/drafts"}},
		)),
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Allowed create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "drafts"), `{"properties":{}}`)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")

	decisions := auditLog.Decisions()
	assertDecision(t, decisions, "Microsoft.ServiceBus/namespaces/queues/write", true)
	assertDecision(t, decisions, "Microsoft.ServiceBus/namespaces/queues/write", false)
}

func TestAzureServiceBusCompatibilityAdapterPropagatesRequestID(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`, map[string]string{
		"x-ms-client-request-id": "req-123",
	})
	resp.Body.Close()
	if got := resp.Header.Get("x-ms-request-id"); got != "req-123" {
		t.Fatalf("x-ms-request-id = %q, want req-123", got)
	}

	decisions := auditLog.Decisions()
	if len(decisions) != 1 {
		t.Fatalf("Audit decisions = %d, want 1", len(decisions))
	}
	if got := decisions[0].Request.Context["request_id"]; got != "req-123" {
		t.Fatalf("Audit request_id = %q, want req-123", got)
	}
}

func TestAzureServiceBusCompatibilityAdapterMapsAuthorizerErrorsToInternalError(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
			return authz.Decision{}, errors.New("backend timeout")
		})),
	))
	defer server.Close()

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`, map[string]string{
		"x-ms-client-request-id": "req-500",
	})
	if got := resp.Header.Get("x-ms-request-id"); got != "req-500" {
		resp.Body.Close()
		t.Fatalf("x-ms-request-id = %q, want req-500", got)
	}
	assertAzureError(t, resp, http.StatusInternalServerError, "InternalError")
}

func TestAzureServiceBusCompatibilityAdapterMapsBackendErrorsToInternalError(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusBackendError(errors.New("rabbitmq timeout")),
	))
	defer server.Close()

	ctx := context.Background()
	resp := serviceBusRequestWithHeaders(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`, map[string]string{
		"x-ms-client-request-id": "req-backend-500",
	})
	if got := resp.Header.Get("x-ms-request-id"); got != "req-backend-500" {
		resp.Body.Close()
		t.Fatalf("x-ms-request-id = %q, want req-backend-500", got)
	}
	assertAzureError(t, resp, http.StatusInternalServerError, "InternalError")

	resp = serviceBusRequest(t, ctx, http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``)
	assertAzureError(t, resp, http.StatusNotFound, "ResourceNotFound")
}

func TestAzureServiceBusCompatibilityAdapterMapsDeleteBackendErrorsToInternalError(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusBackendErrorForMethod(http.MethodDelete, errors.New("rabbitmq timeout")),
	))
	defer server.Close()

	ctx := context.Background()
	url := serviceBusQueueURL(server.URL, "orders")
	resp := serviceBusRequest(t, ctx, http.MethodPut, url, `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, ctx, http.MethodDelete, url, ``, map[string]string{
		"x-ms-client-request-id": "req-delete-backend-500",
	})
	if got := resp.Header.Get("x-ms-request-id"); got != "req-delete-backend-500" {
		resp.Body.Close()
		t.Fatalf("x-ms-request-id = %q, want req-delete-backend-500", got)
	}
	assertAzureError(t, resp, http.StatusInternalServerError, "InternalError")

	resp = serviceBusRequest(t, ctx, http.MethodGet, url, ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Get queue after failed delete status = %d, want 200", resp.StatusCode)
	}
}

func TestAzureServiceBusCompatibilityAdapterMapsReadBackendErrorsToInternalError(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusBackendErrorForMethod(http.MethodGet, errors.New("rabbitmq timeout")),
	))
	defer server.Close()

	ctx := context.Background()
	url := serviceBusQueueURL(server.URL, "orders")
	resp := serviceBusRequest(t, ctx, http.MethodPut, url, `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	for name, url := range map[string]string{
		"get":  serviceBusQueueURL(server.URL, "orders"),
		"list": serviceBusListURL(server.URL),
	} {
		resp = serviceBusRequestWithHeaders(t, ctx, http.MethodGet, url, ``, map[string]string{
			"x-ms-client-request-id": "req-" + name + "-backend-500",
		})
		if got := resp.Header.Get("x-ms-request-id"); got != "req-"+name+"-backend-500" {
			resp.Body.Close()
			t.Fatalf("%s x-ms-request-id = %q, want req-%s-backend-500", name, got, name)
		}
		assertAzureError(t, resp, http.StatusInternalServerError, "InternalError")
	}
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueWriteTagCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "tag:env", Values: []string{"prod"}},
			},
		})),
	))
	defer server.Close()

	ctx := context.Background()
	resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"tags":{"env":"prod"}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue with allowed tag status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, "drafts"), `{"tags":{"env":"dev"}}`)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueWriteLocationCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "location", Values: []string{"westeurope"}},
			},
		})),
	))
	defer server.Close()

	ctx := context.Background()
	resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"location":"westeurope","properties":{}}`)
	var created struct {
		Location string `json:"location"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create queue response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue with allowed location status = %d, want 200", resp.StatusCode)
	}
	if created.Location != "westeurope" {
		t.Fatalf("Create queue location = %q, want westeurope", created.Location)
	}

	resp = serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, "drafts"), `{"location":"eastus","properties":{}}`)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueWriteSourceIPCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "source_ip", Values: []string{"127.0.0.0/8", "::1/128"}},
			},
		})),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue with allowed source IP status = %d, want 200", resp.StatusCode)
	}

	denied := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "source_ip", Values: []string{"192.0.2.0/24"}},
			},
		})),
	))
	defer denied.Close()

	resp = serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(denied.URL, "orders"), `{"properties":{}}`)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueWriteUserAgentCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "user_agent", Values: []string{"homeport-test"}},
			},
		})),
	))
	defer server.Close()

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`, map[string]string{
		"User-Agent": "homeport-test",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue with allowed user agent status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "drafts"), `{"properties":{}}`, map[string]string{
		"User-Agent": "other-client",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueWriteCurrentTimeCondition(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "current_time", Values: []string{now.Add(-time.Minute).Format(time.RFC3339) + "/" + now.Add(time.Minute).Format(time.RFC3339)}},
			},
		})),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue with allowed current time status = %d, want 200", resp.StatusCode)
	}

	denied := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "current_time", Values: []string{"2000-01-01T00:00:00Z/2000-01-02T00:00:00Z"}},
			},
		})),
	))
	defer denied.Close()

	resp = serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(denied.URL, "orders"), `{"properties":{}}`)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueWriteCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "credential_age", Values: []string{"10m"}},
			},
		})),
	))
	defer server.Close()

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`, map[string]string{
		"X-Homeport-Credential-Age": "10m",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue with allowed credential age status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "drafts"), `{"properties":{}}`, map[string]string{
		"X-Homeport-Credential-Age": "48h",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterRejectsExpiredCredential(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_expired", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`, map[string]string{
		"X-Homeport-Credential-Expired": "true",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterRejectsExpiredCredentialForReadListDelete(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/read"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/delete"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read", "Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_expired", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()
	for _, queue := range []string{"orders", "drafts"} {
		createServiceBusQueue(t, server.URL, queue)
	}

	headers := map[string]string{"X-Homeport-Credential-Expired": "true"}
	for name, req := range map[string]struct {
		method string
		url    string
	}{
		"read":   {method: http.MethodGet, url: serviceBusQueueURL(server.URL, "orders")},
		"list":   {method: http.MethodGet, url: serviceBusListURL(server.URL)},
		"delete": {method: http.MethodDelete, url: serviceBusQueueURL(server.URL, "drafts")},
	} {
		resp := serviceBusRequestWithHeaders(t, context.Background(), req.method, req.url, ``, headers)
		if resp.StatusCode != http.StatusForbidden {
			resp.Body.Close()
			t.Fatalf("%s with expired credential status = %d, want 403", name, resp.StatusCode)
		}
		assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
	}
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueWriteTenantProjectAccountConditions(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "tenant", Values: []string{"tenant-a"}},
				{Key: "project", Values: []string{"ledger"}},
				{Key: "account", Values: []string{"acct-1"}},
			},
		})),
	))
	defer server.Close()

	headers := map[string]string{
		"X-Homeport-Tenant":  "tenant-a",
		"X-Homeport-Project": "ledger",
		"X-Homeport-Account": "acct-1",
	}
	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`, headers)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue with allowed tenant/project/account status = %d, want 200", resp.StatusCode)
	}

	headers["X-Homeport-Account"] = "acct-2"
	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "drafts"), `{"properties":{}}`, headers)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueWriteResourcePrefix(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/orders*"},
		})),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue with allowed resource prefix status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "drafts"), `{"properties":{}}`)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueReadResourcePrefix(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/orders*"},
			},
		)),
	))
	defer server.Close()
	createServiceBusQueue(t, server.URL, "orders")
	createServiceBusQueue(t, server.URL, "drafts")

	resp := serviceBusRequest(t, context.Background(), http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Get queue with allowed resource prefix status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, context.Background(), http.MethodGet, serviceBusQueueURL(server.URL, "drafts"), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueWritePrincipalAttributeCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "principal:department", Values: []string{"finance"}},
			},
		})),
	))
	defer server.Close()

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`, map[string]string{
		"X-Homeport-Principal-Attribute-Department": "finance",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue with allowed principal attribute status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "drafts"), `{"properties":{}}`, map[string]string{
		"X-Homeport-Principal-Attribute-Department": "engineering",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueWriteClaimCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
			Effect:    authz.Allow,
			Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
			Resources: []string{"*"},
			Conditions: []authz.Condition{
				{Key: "claim:mfa", Values: []string{"true"}},
			},
		})),
	))
	defer server.Close()

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`, map[string]string{
		"X-Homeport-Claim-Mfa": "true",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue with allowed claim status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "drafts"), `{"properties":{}}`, map[string]string{
		"X-Homeport-Claim-Mfa": "false",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueReadPersistedTagCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "tag:env", Values: []string{"prod"}},
				},
			},
		)),
	))
	defer server.Close()

	ctx := context.Background()
	for queue, body := range map[string]string{
		"orders": `{"tags":{"env":"prod"}}`,
		"drafts": `{"tags":{"env":"dev"}}`,
	} {
		resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, queue), body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create queue %s status = %d, want 200", queue, resp.StatusCode)
		}
	}

	resp := serviceBusRequest(t, ctx, http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Get queue with allowed persisted tag status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, ctx, http.MethodGet, serviceBusQueueURL(server.URL, "drafts"), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueReadPersistedLocationCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "location", Values: []string{"westeurope"}},
				},
			},
		)),
	))
	defer server.Close()

	ctx := context.Background()
	for queue, body := range map[string]string{
		"orders": `{"location":"westeurope"}`,
		"drafts": `{"location":"eastus"}`,
	} {
		resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, queue), body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create queue %s status = %d, want 200", queue, resp.StatusCode)
		}
	}

	resp := serviceBusRequest(t, ctx, http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Get queue with allowed persisted location status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, ctx, http.MethodGet, serviceBusQueueURL(server.URL, "drafts"), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueReadCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_age", Values: []string{"10m"}},
				},
			},
		)),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``, map[string]string{
		"X-Homeport-Credential-Age": "10m",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Get queue with allowed credential age status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``, map[string]string{
		"X-Homeport-Credential-Age": "48h",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueReadPrincipalAttributeCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "principal:department", Values: []string{"finance"}},
				},
			},
		)),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``, map[string]string{
		"X-Homeport-Principal-Attribute-Department": "finance",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Get queue with allowed principal attribute status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``, map[string]string{
		"X-Homeport-Principal-Attribute-Department": "engineering",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueReadClaimCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "claim:mfa", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``, map[string]string{
		"X-Homeport-Claim-Mfa": "true",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Get queue with allowed claim status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``, map[string]string{
		"X-Homeport-Claim-Mfa": "false",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueReadTenantProjectAccountConditions(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "tenant", Values: []string{"tenant-a"}},
					{Key: "project", Values: []string{"ledger"}},
					{Key: "account", Values: []string{"acct-1"}},
				},
			},
		)),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	headers := map[string]string{
		"X-Homeport-Tenant":  "tenant-a",
		"X-Homeport-Project": "ledger",
		"X-Homeport-Account": "acct-1",
	}
	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``, headers)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Get queue with allowed tenant/project/account status = %d, want 200", resp.StatusCode)
	}

	headers["X-Homeport-Account"] = "acct-2"
	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``, headers)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueListTenantProjectAccountConditions(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "tenant", Values: []string{"tenant-a"}},
					{Key: "project", Values: []string{"ledger"}},
					{Key: "account", Values: []string{"acct-1"}},
				},
			},
		)),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	headers := map[string]string{
		"X-Homeport-Tenant":  "tenant-a",
		"X-Homeport-Project": "ledger",
		"X-Homeport-Account": "acct-1",
	}
	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``, headers)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("List queues with allowed tenant/project/account status = %d, want 200", resp.StatusCode)
	}

	headers["X-Homeport-Account"] = "acct-2"
	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``, headers)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueListResourcePrefix(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues*"},
			},
		)),
	))
	defer server.Close()
	createServiceBusQueue(t, server.URL, "orders")

	resp := serviceBusRequest(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("List queues with allowed resource prefix status = %d, want 200", resp.StatusCode)
	}

	denied := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/other/queues*"},
			},
		)),
	))
	defer denied.Close()
	createServiceBusQueue(t, denied.URL, "orders")

	resp = serviceBusRequest(t, context.Background(), http.MethodGet, serviceBusListURL(denied.URL), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueListPrincipalAttributeCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "principal:department", Values: []string{"finance"}},
				},
			},
		)),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``, map[string]string{
		"X-Homeport-Principal-Attribute-Department": "finance",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("List queues with allowed principal attribute status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``, map[string]string{
		"X-Homeport-Principal-Attribute-Department": "engineering",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueListClaimCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "claim:mfa", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``, map[string]string{
		"X-Homeport-Claim-Mfa": "true",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("List queues with allowed claim status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``, map[string]string{
		"X-Homeport-Claim-Mfa": "false",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueListCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_age", Values: []string{"10m"}},
				},
			},
		)),
	))
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``, map[string]string{
		"X-Homeport-Credential-Age": "10m",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("List queues with allowed credential age status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``, map[string]string{
		"X-Homeport-Credential-Age": "48h",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueListSourceIPCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "source_ip", Values: []string{"127.0.0.0/8", "::1/128"}},
				},
			},
		)),
	))
	defer server.Close()
	createServiceBusQueue(t, server.URL, "orders")

	resp := serviceBusRequest(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("List queues with allowed source IP status = %d, want 200", resp.StatusCode)
	}

	denied := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "source_ip", Values: []string{"192.0.2.0/24"}},
				},
			},
		)),
	))
	defer denied.Close()
	createServiceBusQueue(t, denied.URL, "orders")

	resp = serviceBusRequest(t, context.Background(), http.MethodGet, serviceBusListURL(denied.URL), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueListUserAgentCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "user_agent", Values: []string{"homeport-test"}},
				},
			},
		)),
	))
	defer server.Close()
	createServiceBusQueue(t, server.URL, "orders")

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``, map[string]string{
		"User-Agent": "homeport-test",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("List queues with allowed user agent status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``, map[string]string{
		"User-Agent": "other-client",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueListCurrentTimeCondition(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "current_time", Values: []string{now.Add(-time.Minute).Format(time.RFC3339) + "/" + now.Add(time.Minute).Format(time.RFC3339)}},
				},
			},
		)),
	))
	defer server.Close()
	createServiceBusQueue(t, server.URL, "orders")

	resp := serviceBusRequest(t, context.Background(), http.MethodGet, serviceBusListURL(server.URL), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("List queues with allowed current time status = %d, want 200", resp.StatusCode)
	}

	denied := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "current_time", Values: []string{"2000-01-01T00:00:00Z/2000-01-02T00:00:00Z"}},
				},
			},
		)),
	))
	defer denied.Close()
	createServiceBusQueue(t, denied.URL, "orders")

	resp = serviceBusRequest(t, context.Background(), http.MethodGet, serviceBusListURL(denied.URL), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueDeleteTenantProjectAccountConditions(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "tenant", Values: []string{"tenant-a"}},
					{Key: "project", Values: []string{"ledger"}},
					{Key: "account", Values: []string{"acct-1"}},
				},
			},
		)),
	))
	defer server.Close()

	for _, queue := range []string{"orders", "drafts"} {
		resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, queue), `{"properties":{}}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create queue %s status = %d, want 200", queue, resp.StatusCode)
		}
	}

	headers := map[string]string{
		"X-Homeport-Tenant":  "tenant-a",
		"X-Homeport-Project": "ledger",
		"X-Homeport-Account": "acct-1",
	}
	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``, headers)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Delete queue with allowed tenant/project/account status = %d, want 200", resp.StatusCode)
	}

	headers["X-Homeport-Account"] = "acct-2"
	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "drafts"), ``, headers)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueDeleteCredentialAgeCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_age", Values: []string{"10m"}},
				},
			},
		)),
	))
	defer server.Close()

	for _, queue := range []string{"orders", "drafts"} {
		resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, queue), `{"properties":{}}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create queue %s status = %d, want 200", queue, resp.StatusCode)
		}
	}

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``, map[string]string{
		"X-Homeport-Credential-Age": "10m",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Delete queue with allowed credential age status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "drafts"), ``, map[string]string{
		"X-Homeport-Credential-Age": "48h",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueDeleteResourcePrefix(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/orders*"},
			},
		)),
	))
	defer server.Close()
	createServiceBusQueue(t, server.URL, "orders")
	createServiceBusQueue(t, server.URL, "drafts")

	resp := serviceBusRequest(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Delete queue with allowed resource prefix status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "drafts"), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueDeleteSourceIPCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "source_ip", Values: []string{"127.0.0.0/8", "::1/128"}},
				},
			},
		)),
	))
	defer server.Close()
	createServiceBusQueue(t, server.URL, "orders")

	resp := serviceBusRequest(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Delete queue with allowed source IP status = %d, want 200", resp.StatusCode)
	}

	denied := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "source_ip", Values: []string{"192.0.2.0/24"}},
				},
			},
		)),
	))
	defer denied.Close()
	createServiceBusQueue(t, denied.URL, "orders")

	resp = serviceBusRequest(t, context.Background(), http.MethodDelete, serviceBusQueueURL(denied.URL, "orders"), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueDeleteUserAgentCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "user_agent", Values: []string{"homeport-test"}},
				},
			},
		)),
	))
	defer server.Close()
	for _, queue := range []string{"orders", "drafts"} {
		createServiceBusQueue(t, server.URL, queue)
	}

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``, map[string]string{
		"User-Agent": "homeport-test",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Delete queue with allowed user agent status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "drafts"), ``, map[string]string{
		"User-Agent": "other-client",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueDeleteCurrentTimeCondition(t *testing.T) {
	now := time.Now().UTC()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "current_time", Values: []string{now.Add(-time.Minute).Format(time.RFC3339) + "/" + now.Add(time.Minute).Format(time.RFC3339)}},
				},
			},
		)),
	))
	defer server.Close()
	createServiceBusQueue(t, server.URL, "orders")

	resp := serviceBusRequest(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Delete queue with allowed current time status = %d, want 200", resp.StatusCode)
	}

	denied := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "current_time", Values: []string{"2000-01-01T00:00:00Z/2000-01-02T00:00:00Z"}},
				},
			},
		)),
	))
	defer denied.Close()
	createServiceBusQueue(t, denied.URL, "orders")

	resp = serviceBusRequest(t, context.Background(), http.MethodDelete, serviceBusQueueURL(denied.URL, "orders"), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueDeletePrincipalAttributeCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "principal:department", Values: []string{"finance"}},
				},
			},
		)),
	))
	defer server.Close()

	for _, queue := range []string{"orders", "drafts"} {
		resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, queue), `{"properties":{}}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create queue %s status = %d, want 200", queue, resp.StatusCode)
		}
	}

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``, map[string]string{
		"X-Homeport-Principal-Attribute-Department": "finance",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Delete queue with allowed principal attribute status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "drafts"), ``, map[string]string{
		"X-Homeport-Principal-Attribute-Department": "engineering",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueDeleteClaimCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "claim:mfa", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()

	for _, queue := range []string{"orders", "drafts"} {
		resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, queue), `{"properties":{}}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create queue %s status = %d, want 200", queue, resp.StatusCode)
		}
	}

	resp := serviceBusRequestWithHeaders(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``, map[string]string{
		"X-Homeport-Claim-Mfa": "true",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Delete queue with allowed claim status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "drafts"), ``, map[string]string{
		"X-Homeport-Claim-Mfa": "false",
	})
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueDeletePersistedTagCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "tag:env", Values: []string{"prod"}},
				},
			},
		)),
	))
	defer server.Close()

	ctx := context.Background()
	for queue, body := range map[string]string{
		"orders": `{"tags":{"env":"prod"}}`,
		"drafts": `{"tags":{"env":"dev"}}`,
	} {
		resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, queue), body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create queue %s status = %d, want 200", queue, resp.StatusCode)
		}
	}

	resp := serviceBusRequest(t, ctx, http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Delete queue with allowed persisted tag status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, ctx, http.MethodDelete, serviceBusQueueURL(server.URL, "drafts"), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterAuthorizesQueueDeletePersistedLocationCondition(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Allow,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "location", Values: []string{"westeurope"}},
				},
			},
		)),
	))
	defer server.Close()

	ctx := context.Background()
	for queue, body := range map[string]string{
		"orders": `{"location":"westeurope"}`,
		"drafts": `{"location":"eastus"}`,
	} {
		resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, queue), body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create queue %s status = %d, want 200", queue, resp.StatusCode)
		}
	}

	resp := serviceBusRequest(t, ctx, http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Delete queue with allowed persisted location status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, ctx, http.MethodDelete, serviceBusQueueURL(server.URL, "drafts"), ``)
	assertAzureError(t, resp, http.StatusForbidden, "AuthorizationFailed")
}

func TestAzureServiceBusCompatibilityAdapterHandlesQueueLifecycle(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{"maxSizeInMegabytes":1024},"tags":{"env":"test"}}`)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}
	var created struct {
		ID   string            `json:"id"`
		Name string            `json:"name"`
		Type string            `json:"type"`
		Tags map[string]string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create queue response error = %v", err)
	}
	resp.Body.Close()
	if created.Name != "orders" || created.Type != "Microsoft.ServiceBus/namespaces/queues" || created.Tags["env"] != "test" {
		t.Fatalf("Create queue response = %#v", created)
	}

	resp = serviceBusRequest(t, ctx, http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Get queue status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = serviceBusRequest(t, ctx, http.MethodGet, serviceBusListURL(server.URL), ``)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("List queues status = %d, want 200", resp.StatusCode)
	}
	var listed struct {
		Value []struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode list queues response error = %v", err)
	}
	resp.Body.Close()
	if len(listed.Value) != 1 || listed.Value[0].Name != "orders" {
		t.Fatalf("List queues = %#v, want orders", listed.Value)
	}

	resp = serviceBusRequest(t, ctx, http.MethodDelete, serviceBusQueueURL(server.URL, "orders"), ``)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Delete queue status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = serviceBusRequest(t, ctx, http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``)
	assertAzureError(t, resp, http.StatusNotFound, "ResourceNotFound")
}

func TestAzureServiceBusCompatibilityAdapterSupportsAzureSDKQueueLifecycle(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)

	maxSize := int32(1024)
	created, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{
		Properties: &armservicebus.SBQueueProperties{
			MaxSizeInMegabytes: &maxSize,
		},
	}, nil)
	if err != nil {
		t.Fatalf("CreateOrUpdate queue error = %v", err)
	}
	if created.Name == nil || *created.Name != "orders" {
		t.Fatalf("Created queue name = %v, want orders", created.Name)
	}

	got, err := client.Get(context.Background(), "rg", "ns", "orders", nil)
	if err != nil {
		t.Fatalf("Get queue error = %v", err)
	}
	if got.Properties == nil || got.Properties.MaxSizeInMegabytes == nil || *got.Properties.MaxSizeInMegabytes != maxSize {
		t.Fatalf("Get queue max size = %#v, want %d", got.Properties, maxSize)
	}

	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "invoices", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create second queue error = %v", err)
	}
	top := int32(1)
	pager := client.NewListByNamespacePager("rg", "ns", &armservicebus.QueuesClientListByNamespaceOptions{Top: &top})
	var names []string
	for pager.More() {
		page, err := pager.NextPage(context.Background())
		if err != nil {
			t.Fatalf("List queues page error = %v", err)
		}
		for _, queue := range page.Value {
			if queue.Name != nil {
				names = append(names, *queue.Name)
			}
		}
	}
	if strings.Join(names, ",") != "invoices,orders" {
		t.Fatalf("List queue names = %v, want invoices,orders", names)
	}

	if _, err := client.Delete(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Delete queue error = %v", err)
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "orders", nil); err == nil {
		t.Fatal("Get deleted queue error = nil, want ResourceNotFound")
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKQueueResourceIdentity(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	created, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil)
	if err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	wantID := "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/orders"
	assertSDKQueueIdentity(t, "created", &created.SBQueue, wantID, "orders")

	got, err := client.Get(context.Background(), "rg", "ns", "orders", nil)
	if err != nil {
		t.Fatalf("Get queue error = %v", err)
	}
	assertSDKQueueIdentity(t, "get", &got.SBQueue, wantID, "orders")

	page, err := client.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background())
	if err != nil {
		t.Fatalf("List queues page error = %v", err)
	}
	if len(page.Value) != 1 {
		t.Fatalf("Listed queues = %d, want 1", len(page.Value))
	}
	assertSDKQueueIdentity(t, "list", page.Value[0], wantID, "orders")
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKQuotaRetryHint(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusQueueQuota(1),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create initial queue error = %v", err)
	}

	_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "invoices", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Quota error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusTooManyRequests || responseErr.ErrorCode != "TooManyRequests" {
		t.Fatalf("Quota error = status %d code %q, want 429 TooManyRequests", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "invoices", nil); err == nil {
		t.Fatal("Get rejected queue error = nil, want ResourceNotFound")
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKQuotaRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusQueueQuota(1),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create initial queue error = %v", err)
	}

	quotaClient := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-quota-429"})
	_, err := quotaClient.CreateOrUpdate(context.Background(), "rg", "ns", "invoices", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Quota error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusTooManyRequests || responseErr.ErrorCode != "TooManyRequests" {
		t.Fatalf("Quota error = status %d code %q, want 429 TooManyRequests", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-quota-429" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-quota-429", got)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKDuplicateConflict(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create initial queue error = %v", err)
	}

	_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Duplicate create error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusConflict || responseErr.ErrorCode != "Conflict" {
		t.Fatalf("Duplicate create error = status %d code %q, want 409 Conflict", responseErr.StatusCode, responseErr.ErrorCode)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKConflictRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create initial queue error = %v", err)
	}

	conflictClient := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-conflict-409"})
	_, err := conflictClient.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Conflict error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusConflict || responseErr.ErrorCode != "Conflict" {
		t.Fatalf("Conflict error = status %d code %q, want 409 Conflict", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-conflict-409" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-conflict-409", got)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKMissingResourceNotFound(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	for name, call := range map[string]func() error{
		"Get": func() error {
			_, err := client.Get(context.Background(), "rg", "ns", "missing", nil)
			return err
		},
		"Delete": func() error {
			_, err := client.Delete(context.Background(), "rg", "ns", "missing", nil)
			return err
		},
	} {
		err := call()
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("%s missing queue error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusNotFound || responseErr.ErrorCode != "ResourceNotFound" {
			t.Fatalf("%s missing queue error = status %d code %q, want 404 ResourceNotFound", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKMissingResourceRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-missing-404"})
	for name, call := range map[string]func() error{
		"Get": func() error {
			_, err := client.Get(context.Background(), "rg", "ns", "missing", nil)
			return err
		},
		"Delete": func() error {
			_, err := client.Delete(context.Background(), "rg", "ns", "missing", nil)
			return err
		},
	} {
		err := call()
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("%s missing queue error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusNotFound || responseErr.ErrorCode != "ResourceNotFound" {
			t.Fatalf("%s missing queue error = status %d code %q, want 404 ResourceNotFound", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
		if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-missing-404" {
			t.Fatalf("%s x-ms-request-id = %q, want sdk-req-missing-404", name, got)
		}
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKInvalidQueueProperties(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	for name, size := range map[string]int32{"negative": -1, "oversize": 999999} {
		_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", name, armservicebus.SBQueue{
			Properties: &armservicebus.SBQueueProperties{
				MaxSizeInMegabytes: &size,
			},
		}, nil)
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("%s invalid max size error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
			t.Fatalf("%s invalid max size error = status %d code %q, want 400 BadRequest", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
		if _, err := client.Get(context.Background(), "rg", "ns", name, nil); err == nil {
			t.Fatalf("Get %s rejected queue error = nil, want ResourceNotFound", name)
		}
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKValidationErrorRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-validation-400"})
	oversize := int32(999999)
	_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{
		Properties: &armservicebus.SBQueueProperties{
			MaxSizeInMegabytes: &oversize,
		},
	}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Validation error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
		t.Fatalf("Validation error = status %d code %q, want 400 BadRequest", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-validation-400" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-validation-400", got)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKMalformedCreateRequest(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClientWithRetryPolicies(t, server.URL, requestBodyPolicy(`{"properties":`))
	_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Malformed create error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
		t.Fatalf("Malformed create error = status %d code %q, want 400 BadRequest", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if _, err := newAzureServiceBusQueuesClient(t, server.URL).Get(context.Background(), "rg", "ns", "orders", nil); err == nil {
		t.Fatal("Get malformed-create queue error = nil, want ResourceNotFound")
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKMalformedCreateRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClientWithRetryPolicies(
		t,
		server.URL,
		requestBodyPolicy(`{"properties":`),
		headerPolicy{"x-ms-client-request-id": "sdk-req-malformed-400"},
	)
	_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Malformed create error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
		t.Fatalf("Malformed create error = status %d code %q, want 400 BadRequest", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-malformed-400" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-malformed-400", got)
	}
	if _, err := newAzureServiceBusQueuesClient(t, server.URL).Get(context.Background(), "rg", "ns", "orders", nil); err == nil {
		t.Fatal("Get malformed-create queue error = nil, want ResourceNotFound")
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKRawTagRoundTrip(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	var createBodies []string
	createClient := newAzureServiceBusQueuesClientWithRetryPolicies(
		t,
		server.URL,
		requestBodyPolicy(`{"tags":{"env":"prod"},"properties":{}}`),
		captureResponseBodyPolicy{bodies: &createBodies},
	)
	if _, err := createClient.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue with tags error = %v", err)
	}

	var readBodies []string
	client := newAzureServiceBusQueuesClient(t, server.URL, captureResponseBodyPolicy{bodies: &readBodies})
	if _, err := client.Get(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Get tagged queue error = %v", err)
	}
	if _, err := client.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background()); err != nil {
		t.Fatalf("List tagged queues error = %v", err)
	}
	if len(createBodies) != 1 || len(readBodies) != 2 {
		t.Fatalf("Captured response bodies = create %d read %d, want 1 and 2", len(createBodies), len(readBodies))
	}
	assertSDKQueueTag(t, "create", createBodies[0], "prod")
	assertSDKQueueTag(t, "get", readBodies[0], "prod")
	assertSDKListedQueueTag(t, readBodies[1], "prod")
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKInvalidAPIVersion(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	for name, version := range map[string]string{"missing": "", "unsupported": "1900-01-01"} {
		client := newAzureServiceBusQueuesClient(t, server.URL, queryPolicy{"api-version": version})
		_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", name, armservicebus.SBQueue{}, nil)
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("%s api-version error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
			t.Fatalf("%s api-version error = status %d code %q, want 400 BadRequest", name, responseErr.StatusCode, responseErr.ErrorCode)
		}

		normalClient := newAzureServiceBusQueuesClient(t, server.URL)
		if _, err := normalClient.Get(context.Background(), "rg", "ns", name, nil); err == nil {
			t.Fatalf("Get %s rejected queue error = nil, want ResourceNotFound", name)
		}
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKInvalidAPIVersionRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	for name, version := range map[string]string{"missing": "", "unsupported": "1900-01-01"} {
		client := newAzureServiceBusQueuesClient(t, server.URL,
			queryPolicy{"api-version": version},
			headerPolicy{"x-ms-client-request-id": "sdk-req-api-version-400"},
		)
		_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", name, armservicebus.SBQueue{}, nil)
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("%s api-version error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
			t.Fatalf("%s api-version error = status %d code %q, want 400 BadRequest", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
		if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-api-version-400" {
			t.Fatalf("%s x-ms-request-id = %q, want sdk-req-api-version-400", name, got)
		}
		if _, err := newAzureServiceBusQueuesClient(t, server.URL).Get(context.Background(), "rg", "ns", name, nil); err == nil {
			t.Fatalf("Get %s rejected queue error = nil, want ResourceNotFound", name)
		}
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKInvalidAPIVersionForReadListDelete(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	for versionName, version := range map[string]string{"missing": "", "unsupported": "1900-01-01"} {
		invalidClient := newAzureServiceBusQueuesClient(t, server.URL, queryPolicy{"api-version": version})
		for name, call := range map[string]func() error{
			"get": func() error {
				_, err := invalidClient.Get(context.Background(), "rg", "ns", "orders", nil)
				return err
			},
			"list": func() error {
				_, err := invalidClient.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background())
				return err
			},
			"delete": func() error {
				_, err := invalidClient.Delete(context.Background(), "rg", "ns", "orders", nil)
				return err
			},
		} {
			err := call()
			var responseErr *azcore.ResponseError
			if !errors.As(err, &responseErr) {
				t.Fatalf("%s %s api-version error = %v, want Azure ResponseError", name, versionName, err)
			}
			if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
				t.Fatalf("%s %s api-version error = status %d code %q, want 400 BadRequest", name, versionName, responseErr.StatusCode, responseErr.ErrorCode)
			}
		}
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Get queue after rejected delete error = %v", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKInvalidAPIVersionRequestIDForReadListDelete(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	invalidClient := newAzureServiceBusQueuesClient(t, server.URL,
		queryPolicy{"api-version": "1900-01-01"},
		headerPolicy{"x-ms-client-request-id": "sdk-req-api-version-read-list-delete-400"},
	)
	for name, call := range map[string]func() error{
		"get": func() error {
			_, err := invalidClient.Get(context.Background(), "rg", "ns", "orders", nil)
			return err
		},
		"list": func() error {
			_, err := invalidClient.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background())
			return err
		},
		"delete": func() error {
			_, err := invalidClient.Delete(context.Background(), "rg", "ns", "orders", nil)
			return err
		},
	} {
		err := call()
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("%s api-version error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
			t.Fatalf("%s api-version error = status %d code %q, want 400 BadRequest", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
		if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-api-version-read-list-delete-400" {
			t.Fatalf("%s x-ms-request-id = %q, want sdk-req-api-version-read-list-delete-400", name, got)
		}
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Get queue after rejected delete error = %v", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKInvalidListPagination(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	for name, top := range map[string]int32{"zero": 0, "negative": -1} {
		pager := client.NewListByNamespacePager("rg", "ns", &armservicebus.QueuesClientListByNamespaceOptions{Top: &top})
		_, err := pager.NextPage(context.Background())
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("%s invalid list pagination error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
			t.Fatalf("%s invalid list pagination error = status %d code %q, want 400 BadRequest", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKInvalidListPaginationRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-list-top-400"})
	top := int32(0)
	pager := client.NewListByNamespacePager("rg", "ns", &armservicebus.QueuesClientListByNamespaceOptions{Top: &top})
	_, err := pager.NextPage(context.Background())
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Invalid list Top error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
		t.Fatalf("Invalid list Top error = status %d code %q, want 400 BadRequest", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-list-top-400" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-list-top-400", got)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKInvalidListSkipPagination(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	for name, skip := range map[string]int32{"negative": -1, "out-of-range": 1} {
		pager := client.NewListByNamespacePager("rg", "ns", &armservicebus.QueuesClientListByNamespaceOptions{Skip: &skip})
		_, err := pager.NextPage(context.Background())
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("%s invalid list skip pagination error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
			t.Fatalf("%s invalid list skip pagination error = status %d code %q, want 400 BadRequest", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKInvalidListSkipPaginationRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-list-skip-400"})
	skip := int32(-1)
	pager := client.NewListByNamespacePager("rg", "ns", &armservicebus.QueuesClientListByNamespaceOptions{Skip: &skip})
	_, err := pager.NextPage(context.Background())
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Invalid list Skip error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
		t.Fatalf("Invalid list Skip error = status %d code %q, want 400 BadRequest", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-list-skip-400" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-list-skip-400", got)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKMalformedListSkipToken(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL, queryPolicy{"$skiptoken": "bad"})
	pager := client.NewListByNamespacePager("rg", "ns", nil)
	_, err := pager.NextPage(context.Background())
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Malformed skiptoken error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
		t.Fatalf("Malformed skiptoken error = status %d code %q, want 400 BadRequest", responseErr.StatusCode, responseErr.ErrorCode)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKMalformedListSkipTokenRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL,
		queryPolicy{"$skiptoken": "bad"},
		headerPolicy{"x-ms-client-request-id": "sdk-req-skiptoken-400"},
	)
	pager := client.NewListByNamespacePager("rg", "ns", nil)
	_, err := pager.NextPage(context.Background())
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Malformed skiptoken error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusBadRequest || responseErr.ErrorCode != "BadRequest" {
		t.Fatalf("Malformed skiptoken error = status %d code %q, want 400 BadRequest", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-skiptoken-400" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-skiptoken-400", got)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKListSkipPagination(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	for _, queue := range []string{"alpha", "bravo", "charlie"} {
		if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", queue, armservicebus.SBQueue{}, nil); err != nil {
			t.Fatalf("Create %s error = %v", queue, err)
		}
	}

	skip := int32(1)
	page, err := client.NewListByNamespacePager("rg", "ns", &armservicebus.QueuesClientListByNamespaceOptions{Skip: &skip}).NextPage(context.Background())
	if err != nil {
		t.Fatalf("List with skip error = %v", err)
	}
	var names []string
	for _, queue := range page.Value {
		if queue.Name != nil {
			names = append(names, *queue.Name)
		}
	}
	if strings.Join(names, ",") != "bravo,charlie" {
		t.Fatalf("List with skip names = %v, want bravo,charlie", names)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKListTopSkipPagination(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	for _, queue := range []string{"alpha", "bravo", "charlie"} {
		if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", queue, armservicebus.SBQueue{}, nil); err != nil {
			t.Fatalf("Create %s error = %v", queue, err)
		}
	}

	top, skip := int32(1), int32(1)
	pager := client.NewListByNamespacePager("rg", "ns", &armservicebus.QueuesClientListByNamespaceOptions{Top: &top, Skip: &skip})
	var names []string
	for pager.More() {
		page, err := pager.NextPage(context.Background())
		if err != nil {
			t.Fatalf("List with top and skip error = %v", err)
		}
		for _, queue := range page.Value {
			if queue.Name != nil {
				names = append(names, *queue.Name)
			}
		}
	}
	if strings.Join(names, ",") != "bravo,charlie" {
		t.Fatalf("List with top and skip names = %v, want bravo,charlie", names)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKStaleETagPreconditionFailed(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	staleClient := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"If-Match": `W/"stale"`})
	_, err := staleClient.Delete(context.Background(), "rg", "ns", "orders", nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Stale If-Match delete error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusPreconditionFailed || responseErr.ErrorCode != "PreconditionFailed" {
		t.Fatalf("Stale If-Match delete error = status %d code %q, want 412 PreconditionFailed", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Get queue after stale If-Match delete error = %v, want queue intact", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKStaleETagRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	staleClient := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{
		"If-Match":               `W/"stale"`,
		"x-ms-client-request-id": "sdk-req-precondition-412",
	})
	_, err := staleClient.Delete(context.Background(), "rg", "ns", "orders", nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Stale If-Match delete error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusPreconditionFailed || responseErr.ErrorCode != "PreconditionFailed" {
		t.Fatalf("Stale If-Match delete error = status %d code %q, want 412 PreconditionFailed", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-precondition-412" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-precondition-412", got)
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Get queue after stale If-Match delete error = %v, want queue intact", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKWildcardIfMatchDelete(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	wildcardClient := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"If-Match": "*"})
	if _, err := wildcardClient.Delete(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Wildcard If-Match delete error = %v", err)
	}

	_, err := client.Get(context.Background(), "rg", "ns", "orders", nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Get queue after wildcard If-Match delete error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusNotFound || responseErr.ErrorCode != "ResourceNotFound" {
		t.Fatalf("Get queue after wildcard If-Match delete error = status %d code %q, want 404 ResourceNotFound", responseErr.StatusCode, responseErr.ErrorCode)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKRepeatabilityReplay(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"Repeatability-Request-ID": "sdk-create-orders"})
	originalSize := int32(1024)
	replayedSize := int32(2048)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{
		Properties: &armservicebus.SBQueueProperties{MaxSizeInMegabytes: &originalSize},
	}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	replayed, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{
		Properties: &armservicebus.SBQueueProperties{MaxSizeInMegabytes: &replayedSize},
	}, nil)
	if err != nil {
		t.Fatalf("Replay create queue error = %v", err)
	}
	if replayed.Properties == nil || replayed.Properties.MaxSizeInMegabytes == nil || *replayed.Properties.MaxSizeInMegabytes != originalSize {
		t.Fatalf("Replayed max size = %#v, want original %d", replayed.Properties, originalSize)
	}

	stored, err := newAzureServiceBusQueuesClient(t, server.URL).Get(context.Background(), "rg", "ns", "orders", nil)
	if err != nil {
		t.Fatalf("Get replayed queue error = %v", err)
	}
	if stored.Properties == nil || stored.Properties.MaxSizeInMegabytes == nil || *stored.Properties.MaxSizeInMegabytes != originalSize {
		t.Fatalf("Stored max size = %#v, want original %d", stored.Properties, originalSize)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKRepeatabilityDeleteReplay(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	if _, err := newAzureServiceBusQueuesClient(t, server.URL).CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	client := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"Repeatability-Request-ID": "sdk-delete-orders"})
	if _, err := client.Delete(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Delete queue error = %v", err)
	}
	if _, err := client.Delete(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Replay delete queue error = %v", err)
	}
	if _, err := newAzureServiceBusQueuesClient(t, server.URL).Get(context.Background(), "rg", "ns", "orders", nil); err == nil {
		t.Fatal("Get deleted queue error = nil, want ResourceNotFound")
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKQueueMetadata(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	location := "westeurope"
	created, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{
		Location: &location,
	}, nil)
	if err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	if created.Location == nil || *created.Location != location {
		t.Fatalf("Created queue location = %v, want %q", created.Location, location)
	}
	if created.Properties == nil || created.Properties.CreatedAt == nil {
		t.Fatalf("Created queue createdAt = %#v, want timestamp", created.Properties)
	}

	got, err := client.Get(context.Background(), "rg", "ns", "orders", nil)
	if err != nil {
		t.Fatalf("Get queue error = %v", err)
	}
	if got.Location == nil || *got.Location != location {
		t.Fatalf("Get queue location = %v, want %q", got.Location, location)
	}
	if got.Properties == nil || got.Properties.CreatedAt == nil || !got.Properties.CreatedAt.Equal(*created.Properties.CreatedAt) {
		t.Fatalf("Get queue createdAt = %#v, want %v", got.Properties, created.Properties.CreatedAt)
	}

	pager := client.NewListByNamespacePager("rg", "ns", nil)
	page, err := pager.NextPage(context.Background())
	if err != nil {
		t.Fatalf("List queues page error = %v", err)
	}
	if len(page.Value) != 1 || page.Value[0].Location == nil || *page.Value[0].Location != location {
		t.Fatalf("Listed queue metadata = %#v, want location %q", page.Value, location)
	}
	if page.Value[0].Properties == nil || page.Value[0].Properties.CreatedAt == nil || !page.Value[0].Properties.CreatedAt.Equal(*created.Properties.CreatedAt) {
		t.Fatalf("Listed queue createdAt = %#v, want %v", page.Value[0].Properties, created.Properties.CreatedAt)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKQueueProperties(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	maxDeliveryCount := int32(7)
	requiresSession := true
	enablePartitioning := true
	created, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{
		Properties: &armservicebus.SBQueueProperties{
			MaxDeliveryCount:   &maxDeliveryCount,
			RequiresSession:    &requiresSession,
			EnablePartitioning: &enablePartitioning,
		},
	}, nil)
	if err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	assertSDKQueueProperties(t, "created", created.Properties, maxDeliveryCount, requiresSession, enablePartitioning)

	got, err := client.Get(context.Background(), "rg", "ns", "orders", nil)
	if err != nil {
		t.Fatalf("Get queue error = %v", err)
	}
	assertSDKQueueProperties(t, "get", got.Properties, maxDeliveryCount, requiresSession, enablePartitioning)

	page, err := client.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background())
	if err != nil {
		t.Fatalf("List queues page error = %v", err)
	}
	if len(page.Value) != 1 {
		t.Fatalf("Listed queues = %d, want 1", len(page.Value))
	}
	assertSDKQueueProperties(t, "list", page.Value[0].Properties, maxDeliveryCount, requiresSession, enablePartitioning)
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKAdditionalQueueProperties(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	lockDuration := "PT45S"
	defaultTTL := "P7D"
	deadLetterOnExpiration := true
	created, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{
		Properties: &armservicebus.SBQueueProperties{
			LockDuration:                     &lockDuration,
			DefaultMessageTimeToLive:         &defaultTTL,
			DeadLetteringOnMessageExpiration: &deadLetterOnExpiration,
		},
	}, nil)
	if err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	assertSDKAdditionalQueueProperties(t, "created", created.Properties, lockDuration, defaultTTL, deadLetterOnExpiration)

	got, err := client.Get(context.Background(), "rg", "ns", "orders", nil)
	if err != nil {
		t.Fatalf("Get queue error = %v", err)
	}
	assertSDKAdditionalQueueProperties(t, "get", got.Properties, lockDuration, defaultTTL, deadLetterOnExpiration)

	page, err := client.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background())
	if err != nil {
		t.Fatalf("List queues page error = %v", err)
	}
	if len(page.Value) != 1 {
		t.Fatalf("Listed queues = %d, want 1", len(page.Value))
	}
	assertSDKAdditionalQueueProperties(t, "list", page.Value[0].Properties, lockDuration, defaultTTL, deadLetterOnExpiration)
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKExtendedQueueProperties(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	autoDeleteOnIdle := "PT10M"
	duplicateWindow := "PT20M"
	enableBatchedOperations := true
	requiresDuplicateDetection := true
	maxMessageSize := int64(2048)
	created, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{
		Properties: &armservicebus.SBQueueProperties{
			AutoDeleteOnIdle:                    &autoDeleteOnIdle,
			DuplicateDetectionHistoryTimeWindow: &duplicateWindow,
			EnableBatchedOperations:             &enableBatchedOperations,
			RequiresDuplicateDetection:          &requiresDuplicateDetection,
			MaxMessageSizeInKilobytes:           &maxMessageSize,
		},
	}, nil)
	if err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	assertSDKExtendedQueueProperties(t, "created", created.Properties, autoDeleteOnIdle, duplicateWindow, enableBatchedOperations, requiresDuplicateDetection, maxMessageSize)

	got, err := client.Get(context.Background(), "rg", "ns", "orders", nil)
	if err != nil {
		t.Fatalf("Get queue error = %v", err)
	}
	assertSDKExtendedQueueProperties(t, "get", got.Properties, autoDeleteOnIdle, duplicateWindow, enableBatchedOperations, requiresDuplicateDetection, maxMessageSize)

	page, err := client.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background())
	if err != nil {
		t.Fatalf("List queues page error = %v", err)
	}
	if len(page.Value) != 1 {
		t.Fatalf("Listed queues = %d, want 1", len(page.Value))
	}
	assertSDKExtendedQueueProperties(t, "list", page.Value[0].Properties, autoDeleteOnIdle, duplicateWindow, enableBatchedOperations, requiresDuplicateDetection, maxMessageSize)
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKExpiredCredential(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_expired", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"X-Homeport-Credential-Expired": "true"})
	_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Expired credential create error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusForbidden || responseErr.ErrorCode != "AuthorizationFailed" {
		t.Fatalf("Expired credential create error = status %d code %q, want 403 AuthorizationFailed", responseErr.StatusCode, responseErr.ErrorCode)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKExpiredCredentialForReadListDelete(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/read"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/delete"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read", "Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_expired", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	for _, queue := range []string{"orders", "drafts"} {
		if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", queue, armservicebus.SBQueue{}, nil); err != nil {
			t.Fatalf("Create %s error = %v", queue, err)
		}
	}

	expiredClient := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"X-Homeport-Credential-Expired": "true"})
	for name, call := range map[string]func() error{
		"get": func() error {
			_, err := expiredClient.Get(context.Background(), "rg", "ns", "orders", nil)
			return err
		},
		"list": func() error {
			_, err := expiredClient.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background())
			return err
		},
		"delete": func() error {
			_, err := expiredClient.Delete(context.Background(), "rg", "ns", "drafts", nil)
			return err
		},
	} {
		err := call()
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("Expired credential %s error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusForbidden || responseErr.ErrorCode != "AuthorizationFailed" {
			t.Fatalf("Expired credential %s error = status %d code %q, want 403 AuthorizationFailed", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "drafts", nil); err != nil {
		t.Fatalf("Get denied-delete queue error = %v", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKExpiredCredentialRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write", "Microsoft.ServiceBus/namespaces/queues/read", "Microsoft.ServiceBus/namespaces/queues/delete"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/write", "Microsoft.ServiceBus/namespaces/queues/read", "Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_expired", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	for _, queue := range []string{"orders", "drafts"} {
		if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", queue, armservicebus.SBQueue{}, nil); err != nil {
			t.Fatalf("Create %s error = %v", queue, err)
		}
	}

	expiredClient := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{
		"X-Homeport-Credential-Expired": "true",
		"x-ms-client-request-id":        "sdk-req-expired-403",
	})
	for name, call := range map[string]func() error{
		"create": func() error {
			_, err := expiredClient.CreateOrUpdate(context.Background(), "rg", "ns", "expired", armservicebus.SBQueue{}, nil)
			return err
		},
		"get": func() error {
			_, err := expiredClient.Get(context.Background(), "rg", "ns", "orders", nil)
			return err
		},
		"list": func() error {
			_, err := expiredClient.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background())
			return err
		},
		"delete": func() error {
			_, err := expiredClient.Delete(context.Background(), "rg", "ns", "drafts", nil)
			return err
		},
	} {
		err := call()
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("Expired credential %s error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusForbidden || responseErr.ErrorCode != "AuthorizationFailed" {
			t.Fatalf("Expired credential %s error = status %d code %q, want 403 AuthorizationFailed", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
		if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-expired-403" {
			t.Fatalf("Expired credential %s x-ms-request-id = %q, want sdk-req-expired-403", name, got)
		}
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "drafts", nil); err != nil {
		t.Fatalf("Get denied-delete queue error = %v", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKAuthorizationFailureAndAudit(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/read"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
		)),
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Denied create error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusForbidden || responseErr.ErrorCode != "AuthorizationFailed" {
		t.Fatalf("Denied create error = status %d code %q, want 403 AuthorizationFailed", responseErr.StatusCode, responseErr.ErrorCode)
	}
	assertDecision(t, auditLog.Decisions(), "Microsoft.ServiceBus/namespaces/queues/write", false)

	_, err = client.Get(context.Background(), "rg", "ns", "orders", nil)
	if !errors.As(err, &responseErr) || responseErr.StatusCode != http.StatusNotFound || responseErr.ErrorCode != "ResourceNotFound" {
		t.Fatalf("Get rejected queue error = %v, want ResourceNotFound", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKWriteAuthorizationFailureRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/read"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
		)),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-write-authz-403"})
	_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Denied create error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusForbidden || responseErr.ErrorCode != "AuthorizationFailed" {
		t.Fatalf("Denied create error = status %d code %q, want 403 AuthorizationFailed", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-write-authz-403" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-write-authz-403", got)
	}

	_, err = newAzureServiceBusQueuesClient(t, server.URL).Get(context.Background(), "rg", "ns", "orders", nil)
	if !errors.As(err, &responseErr) || responseErr.StatusCode != http.StatusNotFound || responseErr.ErrorCode != "ResourceNotFound" {
		t.Fatalf("Get rejected queue error = %v, want ResourceNotFound", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKReadAuthorizationFailureAndAudit(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/read"}, Resources: []string{"*"}},
		)),
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	_, err := client.Get(context.Background(), "rg", "ns", "orders", nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Denied read error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusForbidden || responseErr.ErrorCode != "AuthorizationFailed" {
		t.Fatalf("Denied read error = status %d code %q, want 403 AuthorizationFailed", responseErr.StatusCode, responseErr.ErrorCode)
	}
	assertDecision(t, auditLog.Decisions(), "Microsoft.ServiceBus/namespaces/queues/read", false)
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKListAuthorizationFailureAndAudit(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/read"}, Resources: []string{"*"}},
		)),
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	_, err := client.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background())
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Denied list error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusForbidden || responseErr.ErrorCode != "AuthorizationFailed" {
		t.Fatalf("Denied list error = status %d code %q, want 403 AuthorizationFailed", responseErr.StatusCode, responseErr.ErrorCode)
	}
	assertDecision(t, auditLog.Decisions(), "Microsoft.ServiceBus/namespaces/queues/read", false)
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKDeleteAuthorizationFailureAndAudit(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write", "Microsoft.ServiceBus/namespaces/queues/read"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/delete"}, Resources: []string{"*"}},
		)),
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	_, err := client.Delete(context.Background(), "rg", "ns", "orders", nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Denied delete error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusForbidden || responseErr.ErrorCode != "AuthorizationFailed" {
		t.Fatalf("Denied delete error = status %d code %q, want 403 AuthorizationFailed", responseErr.StatusCode, responseErr.ErrorCode)
	}
	assertDecision(t, auditLog.Decisions(), "Microsoft.ServiceBus/namespaces/queues/delete", false)
	if _, err := client.Get(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Get denied-delete queue error = %v", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKAuthorizationFailureRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write", "Microsoft.ServiceBus/namespaces/queues/read", "Microsoft.ServiceBus/namespaces/queues/delete"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"Microsoft.ServiceBus/namespaces/queues/read", "Microsoft.ServiceBus/namespaces/queues/delete"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "request_id", Values: []string{"sdk-req-authz-403"}},
				},
			},
		)),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	for _, queue := range []string{"orders", "drafts"} {
		if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", queue, armservicebus.SBQueue{}, nil); err != nil {
			t.Fatalf("Create %s error = %v", queue, err)
		}
	}

	deniedClient := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-authz-403"})
	for name, call := range map[string]func() error{
		"get": func() error {
			_, err := deniedClient.Get(context.Background(), "rg", "ns", "orders", nil)
			return err
		},
		"list": func() error {
			_, err := deniedClient.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background())
			return err
		},
		"delete": func() error {
			_, err := deniedClient.Delete(context.Background(), "rg", "ns", "drafts", nil)
			return err
		},
	} {
		err := call()
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("%s authz error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusForbidden || responseErr.ErrorCode != "AuthorizationFailed" {
			t.Fatalf("%s authz error = status %d code %q, want 403 AuthorizationFailed", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
		if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-authz-403" {
			t.Fatalf("%s x-ms-request-id = %q, want sdk-req-authz-403", name, got)
		}
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "drafts", nil); err != nil {
		t.Fatalf("Get denied-delete queue error = %v", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKAllowedDeleteAuditDecision(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write", "Microsoft.ServiceBus/namespaces/queues/delete"}, Resources: []string{"*"}},
		)),
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	if _, err := client.Delete(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Delete queue error = %v", err)
	}
	assertDecision(t, auditLog.Decisions(), "Microsoft.ServiceBus/namespaces/queues/delete", true)
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKAllowedListAuditDecision(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write", "Microsoft.ServiceBus/namespaces/queues/read"}, Resources: []string{"*"}},
		)),
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	if _, err := client.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background()); err != nil {
		t.Fatalf("List queues error = %v", err)
	}
	assertDecision(t, auditLog.Decisions(), "Microsoft.ServiceBus/namespaces/queues/read", true)
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKAllowedReadAuditDecision(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write", "Microsoft.ServiceBus/namespaces/queues/read"}, Resources: []string{"*"}},
		)),
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Get queue error = %v", err)
	}
	assertDecision(t, auditLog.Decisions(), "Microsoft.ServiceBus/namespaces/queues/read", true)
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKAllowedWriteAuditDecision(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"Microsoft.ServiceBus/namespaces/queues/write"}, Resources: []string{"*"}},
		)),
		compatazure.WithServiceBusAuditSink(auditLog.Record),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	assertDecision(t, auditLog.Decisions(), "Microsoft.ServiceBus/namespaces/queues/write", true)
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKMutationOperationIDs(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	var bodies []string
	client := newAzureServiceBusQueuesClient(t, server.URL, captureResponseBodyPolicy{bodies: &bodies})
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	if _, err := client.Delete(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Delete queue error = %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("Captured response bodies = %d, want 2", len(bodies))
	}
	createdOperationID := assertSDKOperationID(t, "create", bodies[0])
	deletedOperationID := assertSDKOperationID(t, "delete", bodies[1])
	if deletedOperationID == createdOperationID {
		t.Fatalf("Delete operationId = create operationId %q, want distinct operation IDs", deletedOperationID)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKDeleteStatus(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	var bodies []string
	client := newAzureServiceBusQueuesClient(t, server.URL, captureResponseBodyPolicy{bodies: &bodies})
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	if _, err := client.Delete(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Delete queue error = %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("Captured response bodies = %d, want 2", len(bodies))
	}
	assertSDKDeleteStatus(t, bodies[1], "Deleted")
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKEtagResponses(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	var bodies []string
	client := newAzureServiceBusQueuesClient(t, server.URL, captureResponseBodyPolicy{bodies: &bodies})
	if _, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Get queue error = %v", err)
	}
	if _, err := client.NewListByNamespacePager("rg", "ns", nil).NextPage(context.Background()); err != nil {
		t.Fatalf("List queues page error = %v", err)
	}
	if len(bodies) != 3 {
		t.Fatalf("Captured response bodies = %d, want 3", len(bodies))
	}

	createdETag := assertSDKQueueETag(t, "create", bodies[0])
	gotETag := assertSDKQueueETag(t, "get", bodies[1])
	listedETag := assertSDKListedQueueETag(t, bodies[2])
	if createdETag != gotETag || createdETag != listedETag {
		t.Fatalf("SDK etags = create %q get %q list %q, want same etag", createdETag, gotETag, listedETag)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKBackendInternalError(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusBackendError(errors.New("rabbitmq timeout")),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-backend-500"})
	_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Backend error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusInternalServerError || responseErr.ErrorCode != "InternalError" {
		t.Fatalf("Backend error = status %d code %q, want 500 InternalError", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-backend-500" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-backend-500", got)
	}
	if _, err := client.Get(context.Background(), "rg", "ns", "orders", nil); err == nil {
		t.Fatal("Get failed-create queue error = nil, want ResourceNotFound")
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKAuthorizerInternalError(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusAuthorizer(authz.AuthorizerFunc(func(context.Context, authz.Request) (authz.Decision, error) {
			return authz.Decision{}, errors.New("policy store timeout")
		})),
	))
	defer server.Close()

	client := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-authz-500"})
	_, err := client.CreateOrUpdate(context.Background(), "rg", "ns", "orders", armservicebus.SBQueue{}, nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Authorizer error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusInternalServerError || responseErr.ErrorCode != "InternalError" {
		t.Fatalf("Authorizer error = status %d code %q, want 500 InternalError", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-authz-500" {
		t.Fatalf("x-ms-request-id = %q, want sdk-req-authz-500", got)
	}
	if _, err := newAzureServiceBusQueuesClient(t, server.URL).Get(context.Background(), "rg", "ns", "orders", nil); err == nil {
		t.Fatal("Get failed-authorizer queue error = nil, want ResourceNotFound")
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKReadListBackendInternalError(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusBackendErrorForMethod(http.MethodGet, errors.New("rabbitmq timeout")),
	))
	defer server.Close()

	ctx := context.Background()
	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(ctx, "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	errorClient := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-read-list-backend-500"})
	for name, call := range map[string]func() error{
		"Get": func() error {
			_, err := errorClient.Get(ctx, "rg", "ns", "orders", nil)
			return err
		},
		"List": func() error {
			pager := errorClient.NewListByNamespacePager("rg", "ns", nil)
			_, err := pager.NextPage(ctx)
			return err
		},
	} {
		err := call()
		var responseErr *azcore.ResponseError
		if !errors.As(err, &responseErr) {
			t.Fatalf("%s backend error = %v, want Azure ResponseError", name, err)
		}
		if responseErr.StatusCode != http.StatusInternalServerError || responseErr.ErrorCode != "InternalError" {
			t.Fatalf("%s backend error = status %d code %q, want 500 InternalError", name, responseErr.StatusCode, responseErr.ErrorCode)
		}
		if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-read-list-backend-500" {
			t.Fatalf("%s x-ms-request-id = %q, want sdk-req-read-list-backend-500", name, got)
		}
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKDeleteBackendInternalError(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(
		compatazure.WithServiceBusBackendErrorForMethod(http.MethodDelete, errors.New("rabbitmq timeout")),
	))
	defer server.Close()

	ctx := context.Background()
	client := newAzureServiceBusQueuesClient(t, server.URL)
	if _, err := client.CreateOrUpdate(ctx, "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}

	errorClient := newAzureServiceBusQueuesClient(t, server.URL, headerPolicy{"x-ms-client-request-id": "sdk-req-delete-backend-500"})
	_, err := errorClient.Delete(ctx, "rg", "ns", "orders", nil)
	var responseErr *azcore.ResponseError
	if !errors.As(err, &responseErr) {
		t.Fatalf("Delete backend error = %v, want Azure ResponseError", err)
	}
	if responseErr.StatusCode != http.StatusInternalServerError || responseErr.ErrorCode != "InternalError" {
		t.Fatalf("Delete backend error = status %d code %q, want 500 InternalError", responseErr.StatusCode, responseErr.ErrorCode)
	}
	if got := responseErr.RawResponse.Header.Get("x-ms-request-id"); got != "sdk-req-delete-backend-500" {
		t.Fatalf("Delete x-ms-request-id = %q, want sdk-req-delete-backend-500", got)
	}
	if _, err := client.Get(ctx, "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Get queue after failed delete error = %v", err)
	}
}

func TestAzureServiceBusCompatibilityAdapterSurfacesSDKSuccessRequestID(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	var responseHeaders http.Header
	client := newAzureServiceBusQueuesClient(
		t,
		server.URL,
		headerPolicy{"x-ms-client-request-id": "sdk-req-success"},
		captureResponseHeadersPolicy{headers: &responseHeaders},
	)
	assertRequestID := func(operation string) {
		t.Helper()
		if got := responseHeaders.Get("x-ms-request-id"); got != "sdk-req-success" {
			t.Fatalf("%s x-ms-request-id = %q, want sdk-req-success", operation, got)
		}
	}

	if _, err := client.CreateOrUpdate(ctx, "rg", "ns", "orders", armservicebus.SBQueue{}, nil); err != nil {
		t.Fatalf("Create queue error = %v", err)
	}
	assertRequestID("Create")

	if _, err := client.Get(ctx, "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Get queue error = %v", err)
	}
	assertRequestID("Get")

	pager := client.NewListByNamespacePager("rg", "ns", nil)
	if _, err := pager.NextPage(ctx); err != nil {
		t.Fatalf("List queues page error = %v", err)
	}
	assertRequestID("List")

	if _, err := client.Delete(ctx, "rg", "ns", "orders", nil); err != nil {
		t.Fatalf("Delete queue error = %v", err)
	}
	assertRequestID("Delete")
}

func TestAzureServiceBusCompatibilityAdapterReturnsQueueETag(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	var created struct {
		ETag string `json:"etag"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create queue response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || created.ETag == "" {
		t.Fatalf("Create queue etag = status %d, %q; want etag", resp.StatusCode, created.ETag)
	}

	resp = serviceBusRequest(t, ctx, http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``)
	var stored struct {
		ETag string `json:"etag"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode get queue response error = %v", err)
	}
	resp.Body.Close()
	if stored.ETag != created.ETag {
		t.Fatalf("Get queue etag = %q, want %q", stored.ETag, created.ETag)
	}
}

func TestAzureServiceBusCompatibilityAdapterReturnsQueueCreatedAt(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	var created struct {
		CreatedAt string `json:"createdAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create queue response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || created.CreatedAt == "" {
		t.Fatalf("Create queue createdAt = status %d, %q; want timestamp", resp.StatusCode, created.CreatedAt)
	}
	if _, err := time.Parse(time.RFC3339, created.CreatedAt); err != nil {
		t.Fatalf("Create queue createdAt parse error = %v", err)
	}

	resp = serviceBusRequest(t, ctx, http.MethodGet, serviceBusQueueURL(server.URL, "orders"), ``)
	var stored struct {
		CreatedAt string `json:"createdAt"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode get queue response error = %v", err)
	}
	resp.Body.Close()
	if stored.CreatedAt != created.CreatedAt {
		t.Fatalf("Get queue createdAt = %q, want %q", stored.CreatedAt, created.CreatedAt)
	}

	resp = serviceBusRequest(t, ctx, http.MethodGet, serviceBusListURL(server.URL), ``)
	var listed struct {
		Value []struct {
			CreatedAt string `json:"createdAt"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode list queue response error = %v", err)
	}
	resp.Body.Close()
	if len(listed.Value) != 1 || listed.Value[0].CreatedAt != created.CreatedAt {
		t.Fatalf("List queue createdAt = %#v, want %q", listed.Value, created.CreatedAt)
	}
}

func TestAzureServiceBusCompatibilityAdapterPaginatesQueueLists(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	for _, queue := range []string{"alpha", "bravo", "charlie"} {
		resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, queue), `{"properties":{}}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create queue %s status = %d, want 200", queue, resp.StatusCode)
		}
	}

	resp := serviceBusRequest(t, ctx, http.MethodGet, serviceBusListURL(server.URL)+"&$top=2", ``)
	var firstPage struct {
		Value    []struct{ Name string } `json:"value"`
		NextLink string                  `json:"nextLink"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&firstPage); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode first page error = %v", err)
	}
	resp.Body.Close()
	if len(firstPage.Value) != 2 || firstPage.Value[0].Name != "alpha" || firstPage.NextLink == "" {
		t.Fatalf("First page = %#v, want two sorted queues and nextLink", firstPage)
	}

	nextURL := firstPage.NextLink
	if strings.HasPrefix(nextURL, "/") {
		nextURL = server.URL + nextURL
	}
	resp = serviceBusRequest(t, ctx, http.MethodGet, nextURL, ``)
	var secondPage struct {
		Value    []struct{ Name string } `json:"value"`
		NextLink string                  `json:"nextLink"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&secondPage); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode second page error = %v", err)
	}
	resp.Body.Close()
	if len(secondPage.Value) != 1 || secondPage.Value[0].Name != "charlie" || secondPage.NextLink != "" {
		t.Fatalf("Second page = %#v, want final queue without nextLink", secondPage)
	}
}

func TestAzureServiceBusCompatibilityAdapterRejectsInvalidListPagination(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	for _, query := range []string{"&$top=bad", "&$top=0", "&$top=-1", "&$skiptoken=bad", "&$skiptoken=-1", "&$skiptoken=99"} {
		resp := serviceBusRequest(t, ctx, http.MethodGet, serviceBusListURL(server.URL)+query, ``)
		assertAzureError(t, resp, http.StatusBadRequest, "BadRequest")
	}
}

func TestAzureServiceBusCompatibilityAdapterRejectsInvalidQueueProperties(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	for queue, body := range map[string]string{
		"orders":   `{"properties":{"maxSizeInMegabytes":"large"}}`,
		"drafts":   `{"properties":{"maxSizeInMegabytes":-1}}`,
		"oversize": `{"properties":{"maxSizeInMegabytes":999999}}`,
	} {
		resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(server.URL, queue), body)
		assertAzureError(t, resp, http.StatusBadRequest, "BadRequest")
	}
}

func TestAzureServiceBusCompatibilityAdapterRejectsMalformedQueueCreate(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	url := serviceBusQueueURL(server.URL, "orders")
	resp := serviceBusRequest(t, ctx, http.MethodPut, url, `{"properties":`)
	assertAzureError(t, resp, http.StatusBadRequest, "BadRequest")

	resp = serviceBusRequest(t, ctx, http.MethodGet, url, ``)
	assertAzureError(t, resp, http.StatusNotFound, "ResourceNotFound")
}

func TestAzureServiceBusCompatibilityAdapterRejectsMissingAPIVersion(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	url := server.URL + "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/orders"
	resp := serviceBusRequest(t, ctx, http.MethodPut, url, `{"properties":{}}`)
	assertAzureError(t, resp, http.StatusBadRequest, "BadRequest")

	resp = serviceBusRequest(t, ctx, http.MethodGet, url, ``)
	assertAzureError(t, resp, http.StatusBadRequest, "BadRequest")
}

func TestAzureServiceBusCompatibilityAdapterRejectsUnsupportedAPIVersion(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	url := server.URL + "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/orders?api-version=1900-01-01"
	resp := serviceBusRequest(t, ctx, http.MethodPut, url, `{"properties":{}}`)
	assertAzureError(t, resp, http.StatusBadRequest, "BadRequest")
}

func TestAzureServiceBusCompatibilityAdapterReturnsQuotaErrorWhenQueueLimitExceeded(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter(compatazure.WithServiceBusQueueQuota(1)))
	defer server.Close()

	ctx := context.Background()
	resp := serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, "orders"), `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create first queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, ctx, http.MethodPut, serviceBusQueueURL(server.URL, "overflow"), `{"properties":{}}`)
	if got := resp.Header.Get("Retry-After"); got != "1" {
		resp.Body.Close()
		t.Fatalf("Retry-After = %q, want 1", got)
	}
	assertAzureError(t, resp, http.StatusTooManyRequests, "TooManyRequests")
}

func TestAzureServiceBusCompatibilityAdapterReturnsConflictForDuplicateQueue(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	url := serviceBusQueueURL(server.URL, "orders")
	resp := serviceBusRequest(t, ctx, http.MethodPut, url, `{"tags":{"env":"first"}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequest(t, ctx, http.MethodPut, url, `{"tags":{"env":"second"}}`)
	assertAzureError(t, resp, http.StatusConflict, "Conflict")

	resp = serviceBusRequest(t, ctx, http.MethodGet, url, ``)
	var stored struct {
		Tags map[string]string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode stored queue response error = %v", err)
	}
	resp.Body.Close()
	if stored.Tags["env"] != "first" {
		t.Fatalf("Stored queue tags = %#v, want original tags", stored.Tags)
	}
}

func TestAzureServiceBusCompatibilityAdapterReturnsNotFoundForMissingQueueDelete(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	resp := serviceBusRequest(t, context.Background(), http.MethodDelete, serviceBusQueueURL(server.URL, "missing"), ``)
	assertAzureError(t, resp, http.StatusNotFound, "ResourceNotFound")
}

func TestAzureServiceBusCompatibilityAdapterReplaysRepeatableMutations(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	url := serviceBusQueueURL(server.URL, "orders")
	resp := serviceBusRequestWithHeaders(t, ctx, http.MethodPut, url, `{"tags":{"env":"first"}}`, map[string]string{
		"Repeatability-Request-ID": "create-orders",
	})
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, ctx, http.MethodPut, url, `{"tags":{"env":"second"}}`, map[string]string{
		"Repeatability-Request-ID": "create-orders",
	})
	var replayed struct {
		Tags map[string]string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&replayed); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode replayed queue response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || replayed.Tags["env"] != "first" {
		t.Fatalf("Replayed queue = status %d body %#v, want original response", resp.StatusCode, replayed)
	}

	resp = serviceBusRequest(t, ctx, http.MethodGet, url, ``)
	var stored struct {
		Tags map[string]string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&stored); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode stored queue response error = %v", err)
	}
	resp.Body.Close()
	if stored.Tags["env"] != "first" {
		t.Fatalf("Stored queue tags = %#v, want original mutation", stored.Tags)
	}
}

func TestAzureServiceBusCompatibilityAdapterReplaysRepeatableDeletes(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	url := serviceBusQueueURL(server.URL, "orders")
	resp := serviceBusRequest(t, ctx, http.MethodPut, url, `{"properties":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue status = %d, want 200", resp.StatusCode)
	}

	resp = serviceBusRequestWithHeaders(t, ctx, http.MethodDelete, url, ``, map[string]string{
		"Repeatability-Request-ID": "delete-orders",
	})
	var deleted struct {
		Status      string `json:"status"`
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deleted); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode delete queue response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || deleted.Status != "Deleted" || deleted.OperationID == "" {
		t.Fatalf("Delete queue = status %d body %#v, want deleted operation", resp.StatusCode, deleted)
	}

	resp = serviceBusRequestWithHeaders(t, ctx, http.MethodDelete, url, ``, map[string]string{
		"Repeatability-Request-ID": "delete-orders",
	})
	var replayed struct {
		Status      string `json:"status"`
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&replayed); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode replayed delete response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || replayed != deleted {
		t.Fatalf("Replayed delete = status %d body %#v, want %#v", resp.StatusCode, replayed, deleted)
	}
}

func TestAzureServiceBusCompatibilityAdapterReturnsOperationIDsForMutations(t *testing.T) {
	server := httptest.NewServer(compatazure.NewServiceBusAdapter())
	defer server.Close()

	ctx := context.Background()
	url := serviceBusQueueURL(server.URL, "orders")
	resp := serviceBusRequestWithHeaders(t, ctx, http.MethodPut, url, `{"properties":{}}`, map[string]string{
		"Repeatability-Request-ID": "create-orders",
	})
	var created struct {
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create queue response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || created.OperationID == "" {
		t.Fatalf("Create queue operation = status %d, %q; want operation id", resp.StatusCode, created.OperationID)
	}

	resp = serviceBusRequestWithHeaders(t, ctx, http.MethodPut, url, `{"properties":{"changed":true}}`, map[string]string{
		"Repeatability-Request-ID": "create-orders",
	})
	var replayed struct {
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&replayed); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode replayed queue response error = %v", err)
	}
	resp.Body.Close()
	if replayed.OperationID != created.OperationID {
		t.Fatalf("Replayed queue operation = %q, want %q", replayed.OperationID, created.OperationID)
	}

	resp = serviceBusRequest(t, ctx, http.MethodDelete, url, ``)
	var deleted struct {
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deleted); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode delete queue response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || deleted.OperationID == "" || deleted.OperationID == created.OperationID {
		t.Fatalf("Delete queue operation = status %d, %q; want distinct operation id", resp.StatusCode, deleted.OperationID)
	}
}

func serviceBusQueueURL(baseURL, queue string) string {
	return baseURL + "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues/" + queue + "?api-version=2021-11-01"
}

func serviceBusListURL(baseURL string) string {
	return baseURL + "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.ServiceBus/namespaces/ns/queues?api-version=2021-11-01"
}

func serviceBusRequest(t *testing.T, ctx context.Context, method, url, body string) *http.Response {
	t.Helper()
	return serviceBusRequestWithHeaders(t, ctx, method, url, body, nil)
}

func serviceBusRequestWithHeaders(t *testing.T, ctx context.Context, method, url, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s error = %v", method, url, err)
	}
	return resp
}

func createServiceBusQueue(t *testing.T, baseURL, queue string) {
	t.Helper()
	resp := serviceBusRequest(t, context.Background(), http.MethodPut, serviceBusQueueURL(baseURL, queue), `{"properties":{}}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create queue %s status = %d, want 200", queue, resp.StatusCode)
	}
}

func assertAzureError(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("status = %d, want %d", resp.StatusCode, status)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode Azure error response error = %v", err)
	}
	if body.Error.Code != code {
		t.Fatalf("Azure error code = %q, want %q", body.Error.Code, code)
	}
}

func assertSDKQueueProperties(t *testing.T, label string, properties *armservicebus.SBQueueProperties, maxDeliveryCount int32, requiresSession, enablePartitioning bool) {
	t.Helper()
	if properties == nil || properties.MaxDeliveryCount == nil || *properties.MaxDeliveryCount != maxDeliveryCount {
		t.Fatalf("%s maxDeliveryCount = %#v, want %d", label, properties, maxDeliveryCount)
	}
	if properties.RequiresSession == nil || *properties.RequiresSession != requiresSession {
		t.Fatalf("%s requiresSession = %#v, want %v", label, properties.RequiresSession, requiresSession)
	}
	if properties.EnablePartitioning == nil || *properties.EnablePartitioning != enablePartitioning {
		t.Fatalf("%s enablePartitioning = %#v, want %v", label, properties.EnablePartitioning, enablePartitioning)
	}
}

func assertSDKAdditionalQueueProperties(t *testing.T, label string, properties *armservicebus.SBQueueProperties, lockDuration, defaultTTL string, deadLetterOnExpiration bool) {
	t.Helper()
	if properties == nil || properties.LockDuration == nil || *properties.LockDuration != lockDuration {
		t.Fatalf("%s lockDuration = %#v, want %q", label, properties, lockDuration)
	}
	if properties.DefaultMessageTimeToLive == nil || *properties.DefaultMessageTimeToLive != defaultTTL {
		t.Fatalf("%s defaultMessageTimeToLive = %#v, want %q", label, properties.DefaultMessageTimeToLive, defaultTTL)
	}
	if properties.DeadLetteringOnMessageExpiration == nil || *properties.DeadLetteringOnMessageExpiration != deadLetterOnExpiration {
		t.Fatalf("%s deadLetteringOnMessageExpiration = %#v, want %v", label, properties.DeadLetteringOnMessageExpiration, deadLetterOnExpiration)
	}
}

func assertSDKExtendedQueueProperties(t *testing.T, label string, properties *armservicebus.SBQueueProperties, autoDeleteOnIdle, duplicateWindow string, enableBatchedOperations, requiresDuplicateDetection bool, maxMessageSize int64) {
	t.Helper()
	if properties == nil || properties.AutoDeleteOnIdle == nil || *properties.AutoDeleteOnIdle != autoDeleteOnIdle {
		t.Fatalf("%s autoDeleteOnIdle = %#v, want %q", label, properties, autoDeleteOnIdle)
	}
	if properties.DuplicateDetectionHistoryTimeWindow == nil || *properties.DuplicateDetectionHistoryTimeWindow != duplicateWindow {
		t.Fatalf("%s duplicateDetectionHistoryTimeWindow = %#v, want %q", label, properties.DuplicateDetectionHistoryTimeWindow, duplicateWindow)
	}
	if properties.EnableBatchedOperations == nil || *properties.EnableBatchedOperations != enableBatchedOperations {
		t.Fatalf("%s enableBatchedOperations = %#v, want %v", label, properties.EnableBatchedOperations, enableBatchedOperations)
	}
	if properties.RequiresDuplicateDetection == nil || *properties.RequiresDuplicateDetection != requiresDuplicateDetection {
		t.Fatalf("%s requiresDuplicateDetection = %#v, want %v", label, properties.RequiresDuplicateDetection, requiresDuplicateDetection)
	}
	if properties.MaxMessageSizeInKilobytes == nil || *properties.MaxMessageSizeInKilobytes != maxMessageSize {
		t.Fatalf("%s maxMessageSizeInKilobytes = %#v, want %d", label, properties.MaxMessageSizeInKilobytes, maxMessageSize)
	}
}

func assertSDKQueueIdentity(t *testing.T, label string, queue *armservicebus.SBQueue, id, name string) {
	t.Helper()
	if queue == nil || queue.ID == nil || *queue.ID != id {
		t.Fatalf("%s id = %#v, want %q", label, queue, id)
	}
	if queue.Name == nil || *queue.Name != name {
		t.Fatalf("%s name = %v, want %q", label, queue.Name, name)
	}
	if queue.Type == nil || *queue.Type != "Microsoft.ServiceBus/namespaces/queues" {
		t.Fatalf("%s type = %v, want Microsoft.ServiceBus/namespaces/queues", label, queue.Type)
	}
}

type azureTestCredential struct{}
type headerPolicy map[string]string
type queryPolicy map[string]string
type requestBodyPolicy string
type captureResponseHeadersPolicy struct{ headers *http.Header }
type captureResponseBodyPolicy struct{ bodies *[]string }

func newAzureServiceBusQueuesClient(t *testing.T, baseURL string, policies ...policy.Policy) *armservicebus.QueuesClient {
	t.Helper()
	return newAzureServiceBusQueuesClientWithPolicies(t, baseURL, policies, nil)
}

func newAzureServiceBusQueuesClientWithRetryPolicies(t *testing.T, baseURL string, policies ...policy.Policy) *armservicebus.QueuesClient {
	t.Helper()
	return newAzureServiceBusQueuesClientWithPolicies(t, baseURL, nil, policies)
}

func newAzureServiceBusQueuesClientWithPolicies(t *testing.T, baseURL string, perCallPolicies, perRetryPolicies []policy.Policy) *armservicebus.QueuesClient {
	t.Helper()
	client, err := armservicebus.NewQueuesClient("sub", azureTestCredential{}, &arm.ClientOptions{
		ClientOptions: azcore.ClientOptions{
			Cloud: cloud.Configuration{
				Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
					cloud.ResourceManager: {
						Audience: baseURL,
						Endpoint: baseURL + "/compat/azure/service-bus",
					},
				},
			},
			InsecureAllowCredentialWithHTTP: true,
			PerCallPolicies:                 perCallPolicies,
			PerRetryPolicies:                perRetryPolicies,
			Retry:                           policy.RetryOptions{MaxRetries: -1},
		},
		DisableRPRegistration: true,
	})
	if err != nil {
		t.Fatalf("NewQueuesClient error = %v", err)
	}
	return client
}

func (azureTestCredential) GetToken(context.Context, policy.TokenRequestOptions) (azcore.AccessToken, error) {
	return azcore.AccessToken{Token: "homeport", ExpiresOn: time.Now().Add(time.Hour)}, nil
}

func (h headerPolicy) Do(req *policy.Request) (*http.Response, error) {
	for key, value := range h {
		req.Raw().Header.Set(key, value)
	}
	return req.Next()
}

func (q queryPolicy) Do(req *policy.Request) (*http.Response, error) {
	values := req.Raw().URL.Query()
	for key, value := range q {
		values.Set(key, value)
	}
	req.Raw().URL.RawQuery = values.Encode()
	return req.Next()
}

func (p requestBodyPolicy) Do(req *policy.Request) (*http.Response, error) {
	body := string(p)
	req.Raw().Body = io.NopCloser(strings.NewReader(body))
	req.Raw().ContentLength = int64(len(body))
	return req.Next()
}

func (p captureResponseHeadersPolicy) Do(req *policy.Request) (*http.Response, error) {
	resp, err := req.Next()
	if err == nil && p.headers != nil {
		*p.headers = resp.Header.Clone()
	}
	return resp, err
}

func (p captureResponseBodyPolicy) Do(req *policy.Request) (*http.Response, error) {
	resp, err := req.Next()
	if err != nil || p.bodies == nil || resp.Body == nil {
		return resp, err
	}
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return resp, readErr
	}
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	*p.bodies = append(*p.bodies, string(body))
	return resp, nil
}

func assertSDKOperationID(t *testing.T, label, body string) string {
	t.Helper()
	var decoded struct {
		OperationID string `json:"operationId"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("Decode %s SDK response body error = %v; body=%s", label, err, body)
	}
	if decoded.OperationID == "" {
		t.Fatalf("%s operationId = empty; body=%s", label, body)
	}
	return decoded.OperationID
}

func assertSDKDeleteStatus(t *testing.T, body, value string) {
	t.Helper()
	var decoded struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("Decode delete SDK response body error = %v; body=%s", err, body)
	}
	if decoded.Status != value {
		t.Fatalf("Delete status = %q, want %q; body=%s", decoded.Status, value, body)
	}
}

func assertSDKQueueETag(t *testing.T, label, body string) string {
	t.Helper()
	var decoded struct {
		ETag string `json:"etag"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("Decode %s SDK response body error = %v; body=%s", label, err, body)
	}
	if decoded.ETag == "" {
		t.Fatalf("%s etag = empty; body=%s", label, body)
	}
	return decoded.ETag
}

func assertSDKListedQueueETag(t *testing.T, body string) string {
	t.Helper()
	var decoded struct {
		Value []struct {
			ETag string `json:"etag"`
		} `json:"value"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("Decode list SDK response body error = %v; body=%s", err, body)
	}
	if len(decoded.Value) != 1 || decoded.Value[0].ETag == "" {
		t.Fatalf("list etag = empty; body=%s", body)
	}
	return decoded.Value[0].ETag
}

func assertSDKQueueTag(t *testing.T, label, body, value string) {
	t.Helper()
	var decoded struct {
		Tags map[string]string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("Decode %s SDK response body error = %v; body=%s", label, err, body)
	}
	if decoded.Tags["env"] != value {
		t.Fatalf("%s tag env = %q, want %q; body=%s", label, decoded.Tags["env"], value, body)
	}
}

func assertSDKListedQueueTag(t *testing.T, body, value string) {
	t.Helper()
	var decoded struct {
		Value []struct {
			Tags map[string]string `json:"tags"`
		} `json:"value"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("Decode list SDK response body error = %v; body=%s", err, body)
	}
	if len(decoded.Value) != 1 || decoded.Value[0].Tags["env"] != value {
		t.Fatalf("list tag env = %#v, want %q; body=%s", decoded.Value, value, body)
	}
}
