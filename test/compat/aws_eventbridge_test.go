package compat_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func eventBridgeClient(endpoint string) *eventbridge.Client {
	return eventbridge.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eventbridge.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
}

func TestEventBridgeCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()

	client := eventbridge.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", ""),
	}, func(o *eventbridge.Options) {
		o.BaseEndpoint = aws.String(server.URL)
	})

	rule, err := client.PutRule(context.Background(), &eventbridge.PutRuleInput{
		Name:         aws.String("orders-created"),
		EventPattern: aws.String(`{"source":["orders"]}`),
		State:        types.RuleStateEnabled,
	})
	if err != nil {
		t.Fatalf("PutRule() error = %v", err)
	}
	if aws.ToString(rule.RuleArn) == "" {
		t.Fatal("PutRule() returned empty RuleArn")
	}

	listed, err := client.ListRules(context.Background(), &eventbridge.ListRulesInput{})
	if err != nil {
		t.Fatalf("ListRules() error = %v", err)
	}
	if len(listed.Rules) != 1 || aws.ToString(listed.Rules[0].Name) != "orders-created" {
		t.Fatalf("ListRules() = %#v, want orders-created", listed.Rules)
	}

	put, err := client.PutEvents(context.Background(), &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{{
			Source:     aws.String("orders"),
			DetailType: aws.String("created"),
			Detail:     aws.String(`{"id":"1"}`),
		}},
	})
	if err != nil {
		t.Fatalf("PutEvents() error = %v", err)
	}
	if put.FailedEntryCount != 0 || len(put.Entries) != 1 || aws.ToString(put.Entries[0].EventId) == "" {
		t.Fatalf("PutEvents() = %#v, want one accepted event", put)
	}

	if _, err := client.DeleteRule(context.Background(), &eventbridge.DeleteRuleInput{Name: aws.String("orders-created")}); err != nil {
		t.Fatalf("DeleteRule() error = %v", err)
	}
	listed, err = client.ListRules(context.Background(), &eventbridge.ListRulesInput{})
	if err != nil {
		t.Fatalf("ListRules(after delete) error = %v", err)
	}
	if len(listed.Rules) != 0 {
		t.Fatalf("ListRules(after delete) = %#v, want no rules", listed.Rules)
	}
}

func TestEventBridgeCompatibilityAdapterManagesRuleTargetsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("orders-created"), ScheduleExpression: aws.String("rate(1 hour)")}); err != nil {
		t.Fatalf("PutRule() error = %v", err)
	}
	if _, err := client.PutTargets(ctx, &eventbridge.PutTargetsInput{
		Rule: aws.String("orders-created"),
		Targets: []types.Target{{
			Id:  aws.String("orders-webhook"),
			Arn: aws.String("arn:aws:events:us-east-1:000000000000:event-bus/default"),
		}},
	}); err != nil {
		t.Fatalf("PutTargets() error = %v", err)
	}
	listed, err := client.ListTargetsByRule(ctx, &eventbridge.ListTargetsByRuleInput{Rule: aws.String("orders-created")})
	if err != nil {
		t.Fatalf("ListTargetsByRule() error = %v", err)
	}
	if len(listed.Targets) != 1 || aws.ToString(listed.Targets[0].Id) != "orders-webhook" {
		t.Fatalf("ListTargetsByRule() = %#v, want orders-webhook", listed.Targets)
	}
	if _, err := client.RemoveTargets(ctx, &eventbridge.RemoveTargetsInput{Rule: aws.String("orders-created"), Ids: []string{"orders-webhook"}}); err != nil {
		t.Fatalf("RemoveTargets() error = %v", err)
	}
	listed, err = client.ListTargetsByRule(ctx, &eventbridge.ListTargetsByRuleInput{Rule: aws.String("orders-created")})
	if err != nil {
		t.Fatalf("ListTargetsByRule(after remove) error = %v", err)
	}
	if len(listed.Targets) != 0 {
		t.Fatalf("ListTargetsByRule(after remove) = %#v, want empty", listed.Targets)
	}
}

func TestEventBridgeCompatibilityAdapterDescribesRuleFieldsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{
		Name:               aws.String("orders"),
		Description:        aws.String("order schedule"),
		ScheduleExpression: aws.String("rate(1 hour)"),
		RoleArn:            aws.String("arn:aws:iam::000000000000:role/events"),
	}); err != nil {
		t.Fatalf("PutRule() error = %v", err)
	}
	rule, err := client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{Name: aws.String("orders")})
	if err != nil || aws.ToString(rule.Description) != "order schedule" || aws.ToString(rule.ScheduleExpression) != "rate(1 hour)" || aws.ToString(rule.RoleArn) != "arn:aws:iam::000000000000:role/events" {
		t.Fatalf("DescribeRule() = %#v, %v; want configured fields", rule, err)
	}
}

func TestEventBridgeCompatibilityAdapterSeparatesRulesByEventBus(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	for _, bus := range []string{"orders", "billing"} {
		created, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("process"), EventBusName: aws.String(bus), ScheduleExpression: aws.String("rate(1 hour)")})
		if err != nil {
			t.Fatalf("PutRule(%s) error = %v", bus, err)
		}
		if got, want := aws.ToString(created.RuleArn), "arn:aws:events:us-east-1:000000000000:rule/"+bus+"/process"; got != want {
			t.Fatalf("PutRule(%s) RuleArn = %q, want %q", bus, got, want)
		}
	}
	for _, bus := range []string{"orders", "billing"} {
		described, err := client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{Name: aws.String("process"), EventBusName: aws.String(bus)})
		if err != nil || aws.ToString(described.EventBusName) != bus {
			t.Fatalf("DescribeRule(%s) = %#v, %v; want isolated rule", bus, described, err)
		}
		listed, err := client.ListRules(ctx, &eventbridge.ListRulesInput{EventBusName: aws.String(bus)})
		if err != nil || len(listed.Rules) != 1 || aws.ToString(listed.Rules[0].Arn) != "arn:aws:events:us-east-1:000000000000:rule/"+bus+"/process" {
			t.Fatalf("ListRules(%s) = %#v, %v; want isolated rule", bus, listed, err)
		}
	}
}

func TestEventBridgeCompatibilityAdapterNormalizesEventBusARN(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	busARN := "arn:aws:events:us-east-1:000000000000:event-bus/orders"
	if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("process"), EventBusName: aws.String(busARN), ScheduleExpression: aws.String("rate(1 hour)")}); err != nil {
		t.Fatalf("PutRule() error = %v", err)
	}
	described, err := client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{Name: aws.String("process"), EventBusName: aws.String("orders")})
	if err != nil || aws.ToString(described.EventBusName) != "orders" {
		t.Fatalf("DescribeRule() = %#v, %v; want rule created through bus ARN", described, err)
	}
}

func TestEventBridgeCompatibilityAdapterPaginatesRuleTargetsWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("orders-created"), ScheduleExpression: aws.String("rate(1 hour)")}); err != nil {
		t.Fatalf("PutRule() error = %v", err)
	}
	if _, err := client.PutTargets(ctx, &eventbridge.PutTargetsInput{
		Rule: aws.String("orders-created"),
		Targets: []types.Target{
			{Id: aws.String("first"), Arn: aws.String("arn:aws:lambda:us-east-1:000000000000:function:first")},
			{Id: aws.String("second"), Arn: aws.String("arn:aws:lambda:us-east-1:000000000000:function:second")},
		},
	}); err != nil {
		t.Fatalf("PutTargets() error = %v", err)
	}
	first, err := client.ListTargetsByRule(ctx, &eventbridge.ListTargetsByRuleInput{Rule: aws.String("orders-created"), Limit: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListTargetsByRule(first page) error = %v", err)
	}
	if len(first.Targets) != 1 || aws.ToString(first.Targets[0].Id) != "first" || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListTargetsByRule(first page) = %#v, want first target and next token", first)
	}
	second, err := client.ListTargetsByRule(ctx, &eventbridge.ListTargetsByRuleInput{Rule: aws.String("orders-created"), NextToken: first.NextToken, Limit: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListTargetsByRule(second page) error = %v", err)
	}
	if len(second.Targets) != 1 || aws.ToString(second.Targets[0].Id) != "second" || second.NextToken != nil {
		t.Fatalf("ListTargetsByRule(second page) = %#v, want second target and no next token", second)
	}
}

func TestEventBridgeCompatibilityAdapterAuthorizesAndAuditsPutRule(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewEventBridgeAdapter(
		compataws.WithEventBridgeAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"events:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"events:PutRule"}, Resources: []string{"*"}},
		)),
		compataws.WithEventBridgeAuditSink(auditLog.Record),
	))
	defer server.Close()
	client := eventBridgeClient(server.URL)
	_, err := client.PutRule(context.Background(), &eventbridge.PutRuleInput{Name: aws.String("orders-created")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDeniedException" {
		t.Fatalf("PutRule(denied) error = %v, want AccessDeniedException", err)
	}
	assertDecision(t, auditLog.Decisions(), "events:PutRule", false)
	listed, err := client.ListRules(context.Background(), &eventbridge.ListRulesInput{})
	if err != nil {
		t.Fatalf("ListRules(after denied PutRule) error = %v", err)
	}
	if len(listed.Rules) != 0 {
		t.Fatalf("ListRules(after denied PutRule) = %#v, want no rules", listed.Rules)
	}
}

func TestEventBridgeCompatibilityAdapterAuthorizesPutEventsByEventBusARN(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewEventBridgeAdapter(
		compataws.WithEventBridgeAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"events:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"events:PutEvents"}, Resources: []string{"arn:aws:events:us-east-1:000000000000:event-bus/orders"}},
		)),
		compataws.WithEventBridgeAuditSink(auditLog.Record),
	))
	defer server.Close()
	_, err := eventBridgeClient(server.URL).PutEvents(context.Background(), &eventbridge.PutEventsInput{Entries: []types.PutEventsRequestEntry{{
		EventBusName: aws.String("orders"), Source: aws.String("orders"), DetailType: aws.String("created"), Detail: aws.String(`{"id":"1"}`),
	}}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDeniedException" {
		t.Fatalf("PutEvents(denied event bus) error = %v, want AccessDeniedException", err)
	}
	assertDecision(t, auditLog.Decisions(), "events:PutEvents", false)
}

func TestEventBridgeCompatibilityAdapterAuthorizesRuleOperationsByRuleARN(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter(
		compataws.WithEventBridgeAuthorizer(authz.NewPolicyAuthorizer(
			authz.Rule{Effect: authz.Allow, Actions: []string{"events:*"}, Resources: []string{"*"}},
			authz.Rule{Effect: authz.Deny, Actions: []string{"events:PutTargets"}, Resources: []string{"arn:aws:events:us-east-1:000000000000:rule/orders/process"}},
		)),
	))
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("process"), EventBusName: aws.String("orders"), ScheduleExpression: aws.String("rate(1 hour)")}); err != nil {
		t.Fatalf("PutRule() error = %v", err)
	}
	_, err := client.PutTargets(ctx, &eventbridge.PutTargetsInput{Rule: aws.String("process"), EventBusName: aws.String("orders"), Targets: []types.Target{{Id: aws.String("target"), Arn: aws.String("arn:aws:lambda:us-east-1:000000000000:function:process")}}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDeniedException" {
		t.Fatalf("PutTargets(denied rule) error = %v, want AccessDeniedException", err)
	}
}

func TestEventBridgeCompatibilityAdapterEnforcesRuleQuota(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter(compataws.WithEventBridgeRuleQuota(1)))
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("first"), ScheduleExpression: aws.String("rate(1 hour)")}); err != nil {
		t.Fatalf("PutRule(first) error = %v", err)
	}
	_, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("second"), ScheduleExpression: aws.String("rate(1 hour)")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("PutRule(over quota) error = %v, want LimitExceededException", err)
	}
}

func TestEventBridgeCompatibilityAdapterEnablesAndDisablesRuleWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("orders-created"), ScheduleExpression: aws.String("rate(1 hour)"), State: types.RuleStateDisabled}); err != nil {
		t.Fatalf("PutRule() error = %v", err)
	}
	if _, err := client.EnableRule(ctx, &eventbridge.EnableRuleInput{Name: aws.String("orders-created")}); err != nil {
		t.Fatalf("EnableRule() error = %v", err)
	}
	described, err := client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{Name: aws.String("orders-created")})
	if err != nil || described.State != types.RuleStateEnabled {
		t.Fatalf("DescribeRule(after enable) = %#v, %v; want enabled rule", described, err)
	}
	if _, err := client.DisableRule(ctx, &eventbridge.DisableRuleInput{Name: aws.String("orders-created")}); err != nil {
		t.Fatalf("DisableRule() error = %v", err)
	}
	described, err = client.DescribeRule(ctx, &eventbridge.DescribeRuleInput{Name: aws.String("orders-created")})
	if err != nil || described.State != types.RuleStateDisabled {
		t.Fatalf("DescribeRule(after disable) = %#v, %v; want disabled rule", described, err)
	}
}

func TestEventBridgeCompatibilityAdapterPaginatesRulesWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	for _, name := range []string{"first", "second"} {
		if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String(name), ScheduleExpression: aws.String("rate(1 hour)")}); err != nil {
			t.Fatalf("PutRule(%s) error = %v", name, err)
		}
	}
	first, err := client.ListRules(ctx, &eventbridge.ListRulesInput{Limit: aws.Int32(1)})
	if err != nil {
		t.Fatalf("ListRules(first page) error = %v", err)
	}
	if len(first.Rules) != 1 || aws.ToString(first.Rules[0].Name) != "first" || aws.ToString(first.NextToken) == "" {
		t.Fatalf("ListRules(first page) = %#v, want first rule and next token", first)
	}
	second, err := client.ListRules(ctx, &eventbridge.ListRulesInput{Limit: aws.Int32(1), NextToken: first.NextToken})
	if err != nil {
		t.Fatalf("ListRules(second page) error = %v", err)
	}
	if len(second.Rules) != 1 || aws.ToString(second.Rules[0].Name) != "second" || second.NextToken != nil {
		t.Fatalf("ListRules(second page) = %#v, want second rule and no next token", second)
	}
}

func TestEventBridgeCompatibilityAdapterRejectsOversizedPutEventsBatch(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventbridge.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *eventbridge.Options) { o.BaseEndpoint = aws.String(server.URL) })
	entries := make([]types.PutEventsRequestEntry, 11)
	for i := range entries {
		entries[i] = types.PutEventsRequestEntry{Source: aws.String("homeport"), DetailType: aws.String("event"), Detail: aws.String(`{}`)}
	}
	_, err := client.PutEvents(context.Background(), &eventbridge.PutEventsInput{Entries: entries})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
		t.Fatalf("PutEvents(11 entries) error = %v, want ValidationException", err)
	}
}

func TestEventBridgeCompatibilityAdapterAcceptsMaximumPutEventsBatch(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventbridge.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *eventbridge.Options) { o.BaseEndpoint = aws.String(server.URL) })
	entries := make([]types.PutEventsRequestEntry, 10)
	for i := range entries {
		entries[i] = types.PutEventsRequestEntry{Source: aws.String("homeport"), DetailType: aws.String("event"), Detail: aws.String(`{}`)}
	}
	result, err := client.PutEvents(context.Background(), &eventbridge.PutEventsInput{Entries: entries})
	if err != nil || result.FailedEntryCount != 0 || len(result.Entries) != 10 {
		t.Fatalf("PutEvents(10 entries) = %#v, %v; want ten successful entries", result, err)
	}
}

func TestEventBridgeCompatibilityAdapterRejectsEmptyPutEventsBatch(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{}`))
	request.Header.Set("X-Amz-Target", "AWSEvents.PutEvents")
	compataws.NewEventBridgeAdapter().ServeHTTP(recorder, request)
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "ValidationException") {
		t.Fatalf("PutEvents({}) = %d %s, want ValidationException", recorder.Code, recorder.Body.String())
	}
}

func TestEventBridgeCompatibilityAdapterShapesMalformedJSONAsValidationException(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"Entries":`))
	request.Header.Set("X-Amz-Target", "AWSEvents.PutEvents")
	compataws.NewEventBridgeAdapter().ServeHTTP(recorder, request)
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "ValidationException") {
		t.Fatalf("malformed request = %d %s, want ValidationException", recorder.Code, recorder.Body.String())
	}
}

func TestEventBridgeCompatibilityAdapterReportsInvalidPutEventsEntries(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	result, err := eventBridgeClient(server.URL).PutEvents(context.Background(), &eventbridge.PutEventsInput{Entries: []types.PutEventsRequestEntry{
		{Source: aws.String("orders"), DetailType: aws.String("created"), Detail: aws.String(`{"id":"1"}`)},
		{Source: aws.String("orders")},
		{Source: aws.String("orders"), DetailType: aws.String("created"), Detail: aws.String("not-json")},
	}})
	if err != nil {
		t.Fatalf("PutEvents() error = %v", err)
	}
	if result.FailedEntryCount != 2 || len(result.Entries) != 3 || result.Entries[0].EventId == nil || aws.ToString(result.Entries[1].ErrorCode) != "InvalidArgument" || aws.ToString(result.Entries[2].ErrorCode) != "MalformedDetail" {
		t.Fatalf("PutEvents() = %#v, want one valid event and errors for incomplete and malformed entries", result)
	}
}

func TestEventBridgeCompatibilityAdapterListsRuleNamesByTargetWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	targetARN := "arn:aws:lambda:us-east-1:000000000000:function:orders"
	for _, name := range []string{"first", "second"} {
		if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String(name), ScheduleExpression: aws.String("rate(1 hour)")}); err != nil {
			t.Fatalf("PutRule(%s) error = %v", name, err)
		}
		if _, err := client.PutTargets(ctx, &eventbridge.PutTargetsInput{Rule: aws.String(name), Targets: []types.Target{{Id: aws.String(name), Arn: aws.String(targetARN)}}}); err != nil {
			t.Fatalf("PutTargets(%s) error = %v", name, err)
		}
	}
	listed, err := client.ListRuleNamesByTarget(ctx, &eventbridge.ListRuleNamesByTargetInput{TargetArn: aws.String(targetARN)})
	if err != nil {
		t.Fatalf("ListRuleNamesByTarget() error = %v", err)
	}
	if got := strings.Join(listed.RuleNames, ","); got != "first,second" {
		t.Fatalf("ListRuleNamesByTarget() = %q, want first,second", got)
	}
}

func TestEventBridgeCompatibilityAdapterEnforcesRuleTargetQuota(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("orders"), ScheduleExpression: aws.String("rate(1 hour)")}); err != nil {
		t.Fatalf("PutRule() error = %v", err)
	}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("target-%d", i)
		if _, err := client.PutTargets(ctx, &eventbridge.PutTargetsInput{Rule: aws.String("orders"), Targets: []types.Target{{Id: aws.String(id), Arn: aws.String("arn:aws:lambda:us-east-1:000000000000:function:" + id)}}}); err != nil {
			t.Fatalf("PutTargets(%s) error = %v", id, err)
		}
	}
	_, err := client.PutTargets(ctx, &eventbridge.PutTargetsInput{Rule: aws.String("orders"), Targets: []types.Target{{Id: aws.String("target-5"), Arn: aws.String("arn:aws:lambda:us-east-1:000000000000:function:target-5")}}})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "LimitExceededException" {
		t.Fatalf("PutTargets(over quota) error = %v, want LimitExceededException", err)
	}
}

func TestEventBridgeCompatibilityAdapterRejectsOversizedPutTargetsBatch(t *testing.T) {
	targets := make([]map[string]string, 11)
	for i := range targets {
		targets[i] = map[string]string{"Id": fmt.Sprintf("target-%d", i), "Arn": "arn:aws:lambda:us-east-1:000000000000:function:orders"}
	}
	body, err := json.Marshal(map[string]any{"Rule": "orders", "Targets": targets})
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	request.Header.Set("X-Amz-Target", "AWSEvents.PutTargets")
	compataws.NewEventBridgeAdapter().ServeHTTP(recorder, request)
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "ValidationException") {
		t.Fatalf("PutTargets(11 targets) = %d %s, want ValidationException", recorder.Code, recorder.Body.String())
	}
}

func TestEventBridgeCompatibilityAdapterRejectsInvalidRuleNameWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	_, err := eventBridgeClient(server.URL).PutRule(context.Background(), &eventbridge.PutRuleInput{Name: aws.String("invalid rule")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
		t.Fatalf("PutRule(invalid name) error = %v, want ValidationException", err)
	}
}

func TestEventBridgeCompatibilityAdapterRejectsInvalidEventPatternWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	_, err := eventBridgeClient(server.URL).PutRule(context.Background(), &eventbridge.PutRuleInput{Name: aws.String("orders"), EventPattern: aws.String("not-json")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidEventPatternException" {
		t.Fatalf("PutRule(invalid pattern) error = %v, want InvalidEventPatternException", err)
	}
}

func TestEventBridgeCompatibilityAdapterRejectsNonObjectEventPatternWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	_, err := eventBridgeClient(server.URL).PutRule(context.Background(), &eventbridge.PutRuleInput{Name: aws.String("orders"), EventPattern: aws.String(`"orders"`)})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidEventPatternException" {
		t.Fatalf("PutRule(non-object pattern) error = %v, want InvalidEventPatternException", err)
	}
}

func TestEventBridgeCompatibilityAdapterRequiresPatternOrScheduleWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	_, err := eventBridgeClient(server.URL).PutRule(context.Background(), &eventbridge.PutRuleInput{Name: aws.String("orders")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
		t.Fatalf("PutRule(no pattern or schedule) error = %v, want ValidationException", err)
	}
}

func TestEventBridgeCompatibilityAdapterRejectsInvalidRuleStateWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	_, err := eventBridgeClient(server.URL).PutRule(context.Background(), &eventbridge.PutRuleInput{Name: aws.String("orders"), ScheduleExpression: aws.String("rate(1 hour)"), State: types.RuleState("INVALID")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "ValidationException" {
		t.Fatalf("PutRule(invalid state) error = %v, want ValidationException", err)
	}
}

func TestEventBridgeCompatibilityAdapterPreservesTagsWhenUpdatingRuleWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()
	client := eventBridgeClient(server.URL)
	ctx := context.Background()
	created, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("orders"), EventPattern: aws.String(`{"source":["orders"]}`), Tags: []types.Tag{{Key: aws.String("env"), Value: aws.String("prod")}}})
	if err != nil {
		t.Fatalf("PutRule(create) error = %v", err)
	}
	if _, err := client.PutRule(ctx, &eventbridge.PutRuleInput{Name: aws.String("orders"), ScheduleExpression: aws.String("rate(1 hour)"), Tags: []types.Tag{{Key: aws.String("env"), Value: aws.String("dev")}}}); err != nil {
		t.Fatalf("PutRule(update) error = %v", err)
	}
	tags, err := client.ListTagsForResource(ctx, &eventbridge.ListTagsForResourceInput{ResourceARN: created.RuleArn})
	if err != nil || len(tags.Tags) != 1 || aws.ToString(tags.Tags[0].Value) != "prod" {
		t.Fatalf("ListTagsForResource(after update) = %#v, %v; want original prod tag", tags, err)
	}
}

func TestEventBridgeCompatibilityAdapterWithAWSCLIEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("aws"); err != nil {
		t.Skip("aws CLI not installed")
	}
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()

	runAWS := func(args ...string) []byte {
		t.Helper()
		base := []string{"--endpoint-url", server.URL, "--region", "us-east-1", "--output", "json", "--no-cli-pager"}
		cmd := exec.Command("aws", append(base, args...)...)
		cmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID=homeport",
			"AWS_SECRET_ACCESS_KEY=homeport",
			"AWS_EC2_METADATA_DISABLED=true",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("aws %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}

	var rule struct {
		RuleArn string `json:"RuleArn"`
	}
	if err := json.Unmarshal(runAWS("events", "put-rule", "--name", "orders-created", "--event-pattern", `{"source":["orders"]}`), &rule); err != nil {
		t.Fatalf("decode put-rule output: %v", err)
	}
	if rule.RuleArn == "" {
		t.Fatal("put-rule returned empty RuleArn")
	}

	var listed struct {
		Rules []struct {
			Name string `json:"Name"`
		} `json:"Rules"`
	}
	if err := json.Unmarshal(runAWS("events", "list-rules"), &listed); err != nil {
		t.Fatalf("decode list-rules output: %v", err)
	}
	if len(listed.Rules) != 1 || listed.Rules[0].Name != "orders-created" {
		t.Fatalf("list-rules = %#v, want orders-created", listed.Rules)
	}

	var put struct {
		FailedEntryCount int `json:"FailedEntryCount"`
		Entries          []struct {
			EventID string `json:"EventId"`
		} `json:"Entries"`
	}
	if err := json.Unmarshal(runAWS("events", "put-events", "--entries", `[{"Source":"orders","DetailType":"created","Detail":"{\"id\":\"1\"}"}]`), &put); err != nil {
		t.Fatalf("decode put-events output: %v", err)
	}
	if put.FailedEntryCount != 0 || len(put.Entries) != 1 || put.Entries[0].EventID == "" {
		t.Fatalf("put-events = %#v, want one accepted event", put)
	}

	runAWS("events", "delete-rule", "--name", "orders-created")
}

func TestEventBridgeCompatibilityAdapterWithTerraformEndpointOverride(t *testing.T) {
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform not installed")
	}
	server := httptest.NewServer(compataws.NewEventBridgeAdapter())
	defer server.Close()

	dir := t.TempDir()
	config := fmt.Sprintf(`terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "5.47.0"
    }
  }
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "homeport"
  secret_key                  = "homeport"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  skip_region_validation      = true

  endpoints {
    events = %q
  }
}

resource "aws_cloudwatch_event_rule" "deploy" {
  name          = "terraform-orders-created"
  event_pattern = jsonencode({ source = ["orders"] })
  tags = {
    env = "test"
  }
}

output "rule_arn" {
  value = aws_cloudwatch_event_rule.deploy.arn
}
`, server.URL)
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte(config), 0o600); err != nil {
		t.Fatalf("write Terraform config: %v", err)
	}

	runTerraform := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command("terraform", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"AWS_ACCESS_KEY_ID=homeport",
			"AWS_SECRET_ACCESS_KEY=homeport",
			"AWS_EC2_METADATA_DISABLED=true",
			"CHECKPOINT_DISABLE=1",
			"TF_IN_AUTOMATION=1",
			"TF_CLI_ARGS=-no-color",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("terraform %s failed: %v\n%s", strings.Join(args, " "), err, out)
		}
		return out
	}

	runTerraform("init", "-input=false")
	runTerraform("apply", "-input=false", "-auto-approve")
	defer runTerraform("destroy", "-input=false", "-auto-approve")

	out := runTerraform("output", "-raw", "rule_arn")
	if strings.TrimSpace(string(out)) == "" {
		t.Fatal("terraform output rule_arn is empty")
	}
}
