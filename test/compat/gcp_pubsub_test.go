package compat_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	compatgcp "github.com/homeport/homeport/internal/app/compat/gcp"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestGCPPubSubCompatibilityAdapterAuthorizesAndAuditsRESTCalls(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatgcp.NewPubSubAdapter(
		compatgcp.WithPubSubAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"pubsub.*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"pubsub.projects.topics.create"}, Resources: []string{"*"}},
		)),
		compatgcp.WithPubSubAuditSink(auditLog.Record),
	))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer homeport")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Create topic request error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Create topic status = %d, want 403", resp.StatusCode)
	}

	assertDecision(t, auditLog.Decisions(), "pubsub.projects.topics.create", false)
}

func TestGCPPubSubCompatibilityAdapterHandlesSubscriptionLifecycle(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter())
	defer server.Close()

	ctx := context.Background()
	if resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{}`); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Create topic status = %d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events"}`)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Create subscription status = %d, want 200", resp.StatusCode)
	}
	var created struct {
		Name  string `json:"name"`
		Topic string `json:"topic"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("Decode create subscription response error = %v", err)
	}
	resp.Body.Close()
	if created.Name != "projects/homeport/subscriptions/events-sub" || created.Topic != "projects/homeport/topics/events" {
		t.Fatalf("Create subscription = %#v, want name/topic", created)
	}

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/subscriptions/events-sub", ``)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Get subscription status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/subscriptions", ``)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("List subscriptions status = %d, want 200", resp.StatusCode)
	}
	var listed struct {
		Subscriptions []struct {
			Name string `json:"name"`
		} `json:"subscriptions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("Decode list subscriptions response error = %v", err)
	}
	resp.Body.Close()
	if len(listed.Subscriptions) != 1 || listed.Subscriptions[0].Name != "projects/homeport/subscriptions/events-sub" {
		t.Fatalf("List subscriptions = %#v, want events-sub", listed.Subscriptions)
	}

	resp = pubSubRequest(t, ctx, http.MethodDelete, server.URL+"/v1/projects/homeport/subscriptions/events-sub", ``)
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Delete subscription status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/subscriptions/events-sub", ``)
	if resp.StatusCode != http.StatusNotFound {
		resp.Body.Close()
		t.Fatalf("Get deleted subscription status = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestGCPPubSubCompatibilityAdapterAuthorizesSubscriptionActions(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compatgcp.NewPubSubAdapter(
		compatgcp.WithPubSubAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"pubsub.*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"pubsub.projects.subscriptions.create"}, Resources: []string{"*"}},
		)),
		compatgcp.WithPubSubAuditSink(auditLog.Record),
	))
	defer server.Close()

	ctx := context.Background()
	if resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{}`); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Create topic status = %d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Create subscription status = %d, want 403", resp.StatusCode)
	}

	assertDecision(t, auditLog.Decisions(), "pubsub.projects.subscriptions.create", false)
}

func TestGCPPubSubCompatibilityAdapterAuthorizesListResourcePrefix(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter(
		compatgcp.WithPubSubAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"pubsub.projects.topics.create", "pubsub.projects.subscriptions.create"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Allow, Actions: []string{"pubsub.projects.topics.list"}, Resources: []string{"projects/homeport/topics/*"}},
			authz.Rule{Effect: authz.Allow, Actions: []string{"pubsub.projects.subscriptions.list"}, Resources: []string{"projects/homeport/subscriptions/*"}},
		)),
	))
	defer server.Close()

	ctx := context.Background()
	resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create topic status = %d, want 200", resp.StatusCode)
	}
	resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create subscription status = %d, want 200", resp.StatusCode)
	}

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/topics", ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("List topics status = %d, want 200", resp.StatusCode)
	}
	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/subscriptions", ``)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("List subscriptions status = %d, want 200", resp.StatusCode)
	}
}

func TestGCPPubSubCompatibilityAdapterRejectsExpiredCredential(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter(
		compatgcp.WithPubSubAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"pubsub.*"}, Resources: []string{"*"}},
			authz.Rule{
				Effect:    authz.Deny,
				Actions:   []string{"pubsub.projects.topics.create"},
				Resources: []string{"*"},
				Conditions: []authz.Condition{
					{Key: "credential_expired", Values: []string{"true"}},
				},
			},
		)),
	))
	defer server.Close()

	resp := pubSubRequestWithHeaders(t, context.Background(), http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{}`, map[string]string{
		"X-Homeport-Credential-Expired": "true",
	})
	assertGCPError(t, resp, http.StatusForbidden, "PERMISSION_DENIED")

	resp = pubSubRequest(t, context.Background(), http.MethodGet, server.URL+"/v1/projects/homeport/topics/events", ``)
	assertGCPError(t, resp, http.StatusNotFound, "NOT_FOUND")
}

func TestGCPPubSubCompatibilityAdapterAuthorizesCredentialPrincipalAndClaimConditions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		condition authz.Condition
		allowed   map[string]string
		denied    map[string]string
	}{
		{
			name:      "credential-age",
			condition: authz.Condition{Key: "credential_age", Values: []string{"10m"}},
			allowed:   map[string]string{"X-Homeport-Credential-Age": "10m"},
			denied:    map[string]string{"X-Homeport-Credential-Age": "48h"},
		},
		{
			name:      "principal-attribute",
			condition: authz.Condition{Key: "principal:department", Values: []string{"finance"}},
			allowed:   map[string]string{"X-Homeport-Principal-Attribute-Department": "finance"},
			denied:    map[string]string{"X-Homeport-Principal-Attribute-Department": "engineering"},
		},
		{
			name:      "claim",
			condition: authz.Condition{Key: "claim:mfa", Values: []string{"true"}},
			allowed:   map[string]string{"X-Homeport-Claim-Mfa": "true"},
			denied:    map[string]string{"X-Homeport-Claim-Mfa": "false"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(compatgcp.NewPubSubAdapter(
				compatgcp.WithPubSubAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{
					Effect:     authz.Allow,
					Actions:    []string{"pubsub.projects.topics.create"},
					Resources:  []string{"*"},
					Conditions: []authz.Condition{tc.condition},
				})),
			))
			defer server.Close()

			resp := pubSubRequestWithHeaders(t, context.Background(), http.MethodPut, server.URL+"/v1/projects/homeport/topics/"+tc.name+"-allowed", `{}`, tc.allowed)
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Create topic with allowed %s status = %d, want 200", tc.name, resp.StatusCode)
			}

			resp = pubSubRequestWithHeaders(t, context.Background(), http.MethodPut, server.URL+"/v1/projects/homeport/topics/"+tc.name+"-denied", `{}`, tc.denied)
			assertGCPError(t, resp, http.StatusForbidden, "PERMISSION_DENIED")
		})
	}
}

func TestGCPPubSubCompatibilityAdapterRoundTripsLabels(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter())
	defer server.Close()

	ctx := context.Background()
	resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{"labels":{"env":"test"}}`)
	var topic struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&topic); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create topic response error = %v", err)
	}
	resp.Body.Close()
	if topic.Labels["env"] != "test" {
		t.Fatalf("Create topic labels = %#v, want env=test", topic.Labels)
	}

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/topics/events", ``)
	if err := json.NewDecoder(resp.Body).Decode(&topic); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode get topic response error = %v", err)
	}
	resp.Body.Close()
	if topic.Labels["env"] != "test" {
		t.Fatalf("Get topic labels = %#v, want env=test", topic.Labels)
	}

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/topics", ``)
	var topics struct {
		Topics []struct {
			Labels map[string]string `json:"labels"`
		} `json:"topics"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&topics); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode list topics response error = %v", err)
	}
	resp.Body.Close()
	if len(topics.Topics) != 1 || topics.Topics[0].Labels["env"] != "test" {
		t.Fatalf("List topic labels = %#v, want env=test", topics.Topics)
	}

	resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events","labels":{"tier":"gold"}}`)
	var subscription struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&subscription); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create subscription response error = %v", err)
	}
	resp.Body.Close()
	if subscription.Labels["tier"] != "gold" {
		t.Fatalf("Create subscription labels = %#v, want tier=gold", subscription.Labels)
	}

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/subscriptions/events-sub", ``)
	if err := json.NewDecoder(resp.Body).Decode(&subscription); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode get subscription response error = %v", err)
	}
	resp.Body.Close()
	if subscription.Labels["tier"] != "gold" {
		t.Fatalf("Get subscription labels = %#v, want tier=gold", subscription.Labels)
	}

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/subscriptions", ``)
	var subscriptions struct {
		Subscriptions []struct {
			Labels map[string]string `json:"labels"`
		} `json:"subscriptions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&subscriptions); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode list subscriptions response error = %v", err)
	}
	resp.Body.Close()
	if len(subscriptions.Subscriptions) != 1 || subscriptions.Subscriptions[0].Labels["tier"] != "gold" {
		t.Fatalf("List subscription labels = %#v, want tier=gold", subscriptions.Subscriptions)
	}
}

func TestGCPPubSubCompatibilityAdapterRejectsDuplicateCreates(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter())
	defer server.Close()

	ctx := context.Background()
	if resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{}`); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Create topic status = %d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{}`)
	assertGCPError(t, resp, http.StatusConflict, "ALREADY_EXISTS")

	if resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events"}`); resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("Create subscription status = %d, want 200", resp.StatusCode)
	} else {
		resp.Body.Close()
	}

	resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events"}`)
	assertGCPError(t, resp, http.StatusConflict, "ALREADY_EXISTS")
}

func TestGCPPubSubCompatibilityAdapterReturnsNotFoundForMissingDeletes(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter())
	defer server.Close()

	ctx := context.Background()
	for _, url := range []string{
		server.URL + "/v1/projects/homeport/topics/missing",
		server.URL + "/v1/projects/homeport/subscriptions/missing-sub",
	} {
		resp := pubSubRequest(t, ctx, http.MethodDelete, url, ``)
		assertGCPError(t, resp, http.StatusNotFound, "NOT_FOUND")
	}
}

func TestGCPPubSubCompatibilityAdapterPaginatesLists(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter())
	defer server.Close()

	ctx := context.Background()
	for _, name := range []string{"alpha", "bravo", "charlie"} {
		resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/"+name, `{}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create topic %s status = %d, want 200", name, resp.StatusCode)
		}
		resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/"+name, `{"topic":"projects/homeport/topics/`+name+`"}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Create subscription %s status = %d, want 200", name, resp.StatusCode)
		}
	}

	resp := pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/topics?pageSize=2", ``)
	var topics struct {
		Topics        []struct{ Name string } `json:"topics"`
		NextPageToken string                  `json:"nextPageToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&topics); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode list topics response error = %v", err)
	}
	resp.Body.Close()
	if len(topics.Topics) != 2 || topics.Topics[0].Name != "projects/homeport/topics/alpha" || topics.NextPageToken == "" {
		t.Fatalf("List first topic page = %#v, token %q; want alpha/bravo with token", topics.Topics, topics.NextPageToken)
	}

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/topics?pageSize=2&pageToken="+topics.NextPageToken, ``)
	if err := json.NewDecoder(resp.Body).Decode(&topics); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode list topics second page response error = %v", err)
	}
	resp.Body.Close()
	if len(topics.Topics) != 1 || topics.Topics[0].Name != "projects/homeport/topics/charlie" || topics.NextPageToken != "" {
		t.Fatalf("List second topic page = %#v, token %q; want charlie without token", topics.Topics, topics.NextPageToken)
	}

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/subscriptions?pageSize=2", ``)
	var subscriptions struct {
		Subscriptions []struct{ Name string } `json:"subscriptions"`
		NextPageToken string                  `json:"nextPageToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&subscriptions); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode list subscriptions response error = %v", err)
	}
	resp.Body.Close()
	if len(subscriptions.Subscriptions) != 2 || subscriptions.Subscriptions[0].Name != "projects/homeport/subscriptions/alpha" || subscriptions.NextPageToken == "" {
		t.Fatalf("List first subscription page = %#v, token %q; want alpha/bravo with token", subscriptions.Subscriptions, subscriptions.NextPageToken)
	}

	resp = pubSubRequest(t, ctx, http.MethodGet, server.URL+"/v1/projects/homeport/subscriptions?pageSize=2&pageToken="+subscriptions.NextPageToken, ``)
	if err := json.NewDecoder(resp.Body).Decode(&subscriptions); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode list subscriptions second page response error = %v", err)
	}
	resp.Body.Close()
	if len(subscriptions.Subscriptions) != 1 || subscriptions.Subscriptions[0].Name != "projects/homeport/subscriptions/charlie" || subscriptions.NextPageToken != "" {
		t.Fatalf("List second subscription page = %#v, token %q; want charlie without token", subscriptions.Subscriptions, subscriptions.NextPageToken)
	}
}

func TestGCPPubSubCompatibilityAdapterRejectsInvalidListPagination(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter())
	defer server.Close()

	ctx := context.Background()
	resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create topic status = %d, want 200", resp.StatusCode)
	}
	resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create subscription status = %d, want 200", resp.StatusCode)
	}

	for _, path := range []string{
		"/v1/projects/homeport/topics?pageToken=not-a-token",
		"/v1/projects/homeport/topics?pageSize=-1",
		"/v1/projects/homeport/topics?pageToken=99",
		"/v1/projects/homeport/subscriptions?pageToken=not-a-token",
		"/v1/projects/homeport/subscriptions?pageSize=-1",
		"/v1/projects/homeport/subscriptions?pageToken=99",
	} {
		resp := pubSubRequest(t, ctx, http.MethodGet, server.URL+path, ``)
		assertGCPError(t, resp, http.StatusBadRequest, "INVALID_ARGUMENT")
	}
}

func TestGCPPubSubCompatibilityAdapterRejectsInvalidRequests(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter())
	defer server.Close()

	ctx := context.Background()
	resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{`)
	assertGCPError(t, resp, http.StatusBadRequest, "INVALID_ARGUMENT")

	resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{`)
	assertGCPError(t, resp, http.StatusBadRequest, "INVALID_ARGUMENT")

	resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{}`)
	assertGCPError(t, resp, http.StatusBadRequest, "INVALID_ARGUMENT")
}

func TestGCPPubSubCompatibilityAdapterReplaysIdempotentCreates(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter())
	defer server.Close()

	ctx := context.Background()
	resp := pubSubRequestWithIdempotencyKey(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{"labels":{"env":"test"}}`, "topic-create")
	var topic struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&topic); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create topic response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || topic.Name != "projects/homeport/topics/events" || topic.Labels["env"] != "test" {
		t.Fatalf("Create topic = status %d, %#v; want original success", resp.StatusCode, topic)
	}

	resp = pubSubRequestWithIdempotencyKey(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{"labels":{"env":"changed"}}`, "topic-create")
	if err := json.NewDecoder(resp.Body).Decode(&topic); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode replayed topic response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || topic.Labels["env"] != "test" {
		t.Fatalf("Replayed topic = status %d, %#v; want original success", resp.StatusCode, topic)
	}

	resp = pubSubRequestWithIdempotencyKey(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events","labels":{"tier":"gold"}}`, "sub-create")
	var subscription struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&subscription); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create subscription response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || subscription.Labels["tier"] != "gold" {
		t.Fatalf("Create subscription = status %d, %#v; want original success", resp.StatusCode, subscription)
	}

	resp = pubSubRequestWithIdempotencyKey(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events","labels":{"tier":"changed"}}`, "sub-create")
	if err := json.NewDecoder(resp.Body).Decode(&subscription); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode replayed subscription response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || subscription.Labels["tier"] != "gold" {
		t.Fatalf("Replayed subscription = status %d, %#v; want original success", resp.StatusCode, subscription)
	}
}

func TestGCPPubSubCompatibilityAdapterReturnsQuotaErrorWhenLimitsAreExceeded(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter(
		compatgcp.WithPubSubTopicQuota(1),
		compatgcp.WithPubSubSubscriptionQuota(1),
	))
	defer server.Close()

	ctx := context.Background()
	resp := pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create first topic status = %d, want 200", resp.StatusCode)
	}
	resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/overflow", `{}`)
	assertGCPError(t, resp, http.StatusTooManyRequests, "RESOURCE_EXHAUSTED")

	resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Create first subscription status = %d, want 200", resp.StatusCode)
	}
	resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/overflow-sub", `{"topic":"projects/homeport/topics/events"}`)
	assertGCPError(t, resp, http.StatusTooManyRequests, "RESOURCE_EXHAUSTED")
}

func TestGCPPubSubCompatibilityAdapterReturnsOperationIDsForMutations(t *testing.T) {
	server := httptest.NewServer(compatgcp.NewPubSubAdapter())
	defer server.Close()

	ctx := context.Background()
	resp := pubSubRequestWithIdempotencyKey(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{}`, "topic-op")
	var topic struct {
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&topic); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create topic response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || topic.OperationID == "" {
		t.Fatalf("Create topic operation = status %d, %q; want operation id", resp.StatusCode, topic.OperationID)
	}

	resp = pubSubRequestWithIdempotencyKey(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/topics/events", `{}`, "topic-op")
	var replayedTopic struct {
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&replayedTopic); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode replayed topic response error = %v", err)
	}
	resp.Body.Close()
	if replayedTopic.OperationID != topic.OperationID {
		t.Fatalf("Replayed topic operation = %q, want %q", replayedTopic.OperationID, topic.OperationID)
	}

	resp = pubSubRequest(t, ctx, http.MethodPut, server.URL+"/v1/projects/homeport/subscriptions/events-sub", `{"topic":"projects/homeport/topics/events"}`)
	var subscription struct {
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&subscription); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode create subscription response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || subscription.OperationID == "" {
		t.Fatalf("Create subscription operation = status %d, %q; want operation id", resp.StatusCode, subscription.OperationID)
	}

	resp = pubSubRequest(t, ctx, http.MethodDelete, server.URL+"/v1/projects/homeport/subscriptions/events-sub", ``)
	var deletedSubscription struct {
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deletedSubscription); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode delete subscription response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || deletedSubscription.OperationID == "" {
		t.Fatalf("Delete subscription operation = status %d, %q; want operation id", resp.StatusCode, deletedSubscription.OperationID)
	}

	resp = pubSubRequest(t, ctx, http.MethodDelete, server.URL+"/v1/projects/homeport/topics/events", ``)
	var deletedTopic struct {
		OperationID string `json:"operationId"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deletedTopic); err != nil {
		resp.Body.Close()
		t.Fatalf("Decode delete topic response error = %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || deletedTopic.OperationID == "" {
		t.Fatalf("Delete topic operation = status %d, %q; want operation id", resp.StatusCode, deletedTopic.OperationID)
	}
}

func assertGCPError(t *testing.T, resp *http.Response, status int, code string) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("GCP error status = %d, want %d", resp.StatusCode, status)
	}
	var body struct {
		Error struct {
			Status string `json:"status"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode GCP error response error = %v", err)
	}
	if body.Error.Status != code {
		t.Fatalf("GCP error code = %q, want %q", body.Error.Status, code)
	}
}

func pubSubRequest(t *testing.T, ctx context.Context, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer homeport")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s error = %v", method, url, err)
	}
	return resp
}

func pubSubRequestWithIdempotencyKey(t *testing.T, ctx context.Context, method, url, body, key string) *http.Response {
	t.Helper()
	headers := map[string]string{"X-Idempotency-Key": key}
	return pubSubRequestWithHeaders(t, ctx, method, url, body, headers)
}

func pubSubRequestWithHeaders(t *testing.T, ctx context.Context, method, url, body string, headers map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer homeport")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s error = %v", method, url, err)
	}
	return resp
}
