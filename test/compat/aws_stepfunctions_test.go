package compat_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/aws/smithy-go"
	compataws "github.com/homeport/homeport/internal/app/compat/aws"
	"github.com/homeport/homeport/internal/domain/authz"
)

func TestStepFunctionsCompatibilityAdapterWithAWSSDK(t *testing.T) {
	server := httptest.NewServer(compataws.NewStepFunctionsAdapter())
	defer server.Close()
	client := sfn.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sfn.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateStateMachine(ctx, &sfn.CreateStateMachineInput{Name: aws.String("orders"), Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions")})
	if err != nil || aws.ToString(created.StateMachineArn) == "" {
		t.Fatalf("CreateStateMachine() = %#v, %v", created, err)
	}
	described, err := client.DescribeStateMachine(ctx, &sfn.DescribeStateMachineInput{StateMachineArn: created.StateMachineArn})
	if err != nil || aws.ToString(described.Name) != "orders" {
		t.Fatalf("DescribeStateMachine() = %#v, %v", described, err)
	}
	listed, err := client.ListStateMachines(ctx, &sfn.ListStateMachinesInput{})
	if err != nil || len(listed.StateMachines) != 1 || aws.ToString(listed.StateMachines[0].Name) != "orders" {
		t.Fatalf("ListStateMachines() = %#v, %v", listed, err)
	}
	if _, err := client.UpdateStateMachine(ctx, &sfn.UpdateStateMachineInput{StateMachineArn: created.StateMachineArn, Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`)}); err != nil {
		t.Fatalf("UpdateStateMachine() error = %v", err)
	}
	if _, err := client.DeleteStateMachine(ctx, &sfn.DeleteStateMachineInput{StateMachineArn: created.StateMachineArn}); err != nil {
		t.Fatalf("DeleteStateMachine() error = %v", err)
	}
}

func TestStepFunctionsCompatibilityAdapterRejectsInvalidDefinitions(t *testing.T) {
	server := httptest.NewServer(compataws.NewStepFunctionsAdapter())
	defer server.Close()
	client := sfn.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sfn.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	_, err := client.CreateStateMachine(ctx, &sfn.CreateStateMachineInput{Name: aws.String("orders"), Definition: aws.String(`{`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidDefinition" {
		t.Fatalf("CreateStateMachine(invalid) error = %v, want InvalidDefinition", err)
	}
	created, err := client.CreateStateMachine(ctx, &sfn.CreateStateMachineInput{Name: aws.String("valid"), Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions")})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.UpdateStateMachine(ctx, &sfn.UpdateStateMachineInput{StateMachineArn: created.StateMachineArn, Definition: aws.String(`{`)})
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "InvalidDefinition" {
		t.Fatalf("UpdateStateMachine(invalid) error = %v, want InvalidDefinition", err)
	}
}

func TestStepFunctionsCompatibilityAdapterPaginatesStateMachines(t *testing.T) {
	server := httptest.NewServer(compataws.NewStepFunctionsAdapter())
	defer server.Close()
	client := sfn.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sfn.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	for _, name := range []string{"alpha", "bravo"} {
		if _, err := client.CreateStateMachine(ctx, &sfn.CreateStateMachineInput{Name: aws.String(name), Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions")}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := client.ListStateMachines(ctx, &sfn.ListStateMachinesInput{MaxResults: 1})
	if err != nil || len(first.StateMachines) != 1 || aws.ToString(first.NextToken) == "" || first.StateMachines[0].Type == "" {
		t.Fatalf("ListStateMachines(first) = %#v, %v", first, err)
	}
	second, err := client.ListStateMachines(ctx, &sfn.ListStateMachinesInput{MaxResults: 1, NextToken: first.NextToken})
	if err != nil || len(second.StateMachines) != 1 || second.NextToken != nil {
		t.Fatalf("ListStateMachines(second) = %#v, %v", second, err)
	}
}

func TestStepFunctionsCompatibilityAdapterManagesStateMachineTags(t *testing.T) {
	server := httptest.NewServer(compataws.NewStepFunctionsAdapter())
	defer server.Close()
	client := sfn.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sfn.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	created, err := client.CreateStateMachine(ctx, &sfn.CreateStateMachineInput{Name: aws.String("orders"), Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions"), Tags: []types.Tag{{Key: aws.String("team"), Value: aws.String("platform")}}})
	if err != nil {
		t.Fatal(err)
	}
	tags, err := client.ListTagsForResource(ctx, &sfn.ListTagsForResourceInput{ResourceArn: created.StateMachineArn})
	if err != nil || len(tags.Tags) != 1 || aws.ToString(tags.Tags[0].Key) != "team" || aws.ToString(tags.Tags[0].Value) != "platform" {
		t.Fatalf("ListTagsForResource() = %#v, %v; want create-time tag", tags, err)
	}
	if _, err := client.TagResource(ctx, &sfn.TagResourceInput{ResourceArn: created.StateMachineArn, Tags: []types.Tag{{Key: aws.String("environment"), Value: aws.String("test")}}}); err != nil {
		t.Fatalf("TagResource() error = %v", err)
	}
	if _, err := client.UntagResource(ctx, &sfn.UntagResourceInput{ResourceArn: created.StateMachineArn, TagKeys: []string{"team"}}); err != nil {
		t.Fatalf("UntagResource() error = %v", err)
	}
	tags, err = client.ListTagsForResource(ctx, &sfn.ListTagsForResourceInput{ResourceArn: created.StateMachineArn})
	if err != nil || len(tags.Tags) != 1 || aws.ToString(tags.Tags[0].Key) != "environment" || aws.ToString(tags.Tags[0].Value) != "test" {
		t.Fatalf("ListTagsForResource(after update) = %#v, %v; want remaining tag", tags, err)
	}
}

func TestStepFunctionsCompatibilityAdapterManagesExecutionLifecycle(t *testing.T) {
	server := httptest.NewServer(compataws.NewStepFunctionsAdapter())
	defer server.Close()
	client := sfn.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sfn.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	machine, err := client.CreateStateMachine(ctx, &sfn.CreateStateMachineInput{Name: aws.String("orders"), Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions")})
	if err != nil {
		t.Fatal(err)
	}
	started, err := client.StartExecution(ctx, &sfn.StartExecutionInput{StateMachineArn: machine.StateMachineArn, Name: aws.String("initial"), Input: aws.String(`{"order":"42"}`)})
	if err != nil || aws.ToString(started.ExecutionArn) == "" {
		t.Fatalf("StartExecution() = %#v, %v", started, err)
	}
	described, err := client.DescribeExecution(ctx, &sfn.DescribeExecutionInput{ExecutionArn: started.ExecutionArn})
	if err != nil || described.Status != types.ExecutionStatusRunning || aws.ToString(described.Input) != `{"order":"42"}` {
		t.Fatalf("DescribeExecution(running) = %#v, %v", described, err)
	}
	if _, err := client.StopExecution(ctx, &sfn.StopExecutionInput{ExecutionArn: started.ExecutionArn, Error: aws.String("Cancelled")}); err != nil {
		t.Fatalf("StopExecution() error = %v", err)
	}
	described, err = client.DescribeExecution(ctx, &sfn.DescribeExecutionInput{ExecutionArn: started.ExecutionArn})
	if err != nil || described.Status != types.ExecutionStatusAborted || aws.ToString(described.Error) != "Cancelled" || described.StopDate == nil {
		t.Fatalf("DescribeExecution(stopped) = %#v, %v", described, err)
	}
}

func TestStepFunctionsCompatibilityAdapterListsExecutions(t *testing.T) {
	server := httptest.NewServer(compataws.NewStepFunctionsAdapter())
	defer server.Close()
	client := sfn.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sfn.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	machine, err := client.CreateStateMachine(ctx, &sfn.CreateStateMachineInput{Name: aws.String("orders"), Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions")})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "bravo"} {
		if _, err := client.StartExecution(ctx, &sfn.StartExecutionInput{StateMachineArn: machine.StateMachineArn, Name: aws.String(name)}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := client.ListExecutions(ctx, &sfn.ListExecutionsInput{StateMachineArn: machine.StateMachineArn, MaxResults: 1})
	if err != nil || len(first.Executions) != 1 || aws.ToString(first.Executions[0].Name) != "alpha" || aws.ToString(first.NextToken) == "" || first.Executions[0].Status != types.ExecutionStatusRunning {
		t.Fatalf("ListExecutions(first page) = %#v, %v", first, err)
	}
	second, err := client.ListExecutions(ctx, &sfn.ListExecutionsInput{StateMachineArn: machine.StateMachineArn, MaxResults: 1, NextToken: first.NextToken})
	if err != nil || len(second.Executions) != 1 || aws.ToString(second.Executions[0].Name) != "bravo" || second.NextToken != nil {
		t.Fatalf("ListExecutions(second page) = %#v, %v", second, err)
	}
}

func TestStepFunctionsCompatibilityAdapterReturnsExecutionHistory(t *testing.T) {
	server := httptest.NewServer(compataws.NewStepFunctionsAdapter())
	defer server.Close()
	client := sfn.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sfn.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	machine, err := client.CreateStateMachine(ctx, &sfn.CreateStateMachineInput{Name: aws.String("orders"), Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions")})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := client.StartExecution(ctx, &sfn.StartExecutionInput{StateMachineArn: machine.StateMachineArn, Name: aws.String("initial")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.StopExecution(ctx, &sfn.StopExecutionInput{ExecutionArn: execution.ExecutionArn}); err != nil {
		t.Fatal(err)
	}
	history, err := client.GetExecutionHistory(ctx, &sfn.GetExecutionHistoryInput{ExecutionArn: execution.ExecutionArn})
	if err != nil || len(history.Events) != 2 || history.Events[0].Type != types.HistoryEventTypeExecutionStarted || history.Events[1].Type != types.HistoryEventTypeExecutionAborted {
		t.Fatalf("GetExecutionHistory() = %#v, %v", history, err)
	}
}

func TestStepFunctionsCompatibilityAdapterReplaysNamedExecution(t *testing.T) {
	server := httptest.NewServer(compataws.NewStepFunctionsAdapter())
	defer server.Close()
	client := sfn.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sfn.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	machine, err := client.CreateStateMachine(ctx, &sfn.CreateStateMachineInput{Name: aws.String("orders"), Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions")})
	if err != nil {
		t.Fatal(err)
	}
	input := &sfn.StartExecutionInput{StateMachineArn: machine.StateMachineArn, Name: aws.String("initial"), Input: aws.String(`{"order":"42"}`)}
	first, err := client.StartExecution(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.StartExecution(ctx, input)
	if err != nil || aws.ToString(second.ExecutionArn) != aws.ToString(first.ExecutionArn) {
		t.Fatalf("StartExecution(replay) = %#v, %v; want %q", second, err, aws.ToString(first.ExecutionArn))
	}
}

func TestStepFunctionsCompatibilityAdapterEnforcesStateMachineQuota(t *testing.T) {
	server := httptest.NewServer(compataws.NewStepFunctionsAdapter(compataws.WithStepFunctionsStateMachineQuota(1)))
	defer server.Close()
	client := sfn.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sfn.Options) { o.BaseEndpoint = aws.String(server.URL) })
	ctx := context.Background()
	for _, name := range []string{"first", "second"} {
		_, err := client.CreateStateMachine(ctx, &sfn.CreateStateMachineInput{Name: aws.String(name), Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions")})
		if name == "first" && err != nil {
			t.Fatal(err)
		}
		if name == "second" {
			var apiErr smithy.APIError
			if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "StateMachineLimitExceeded" {
				t.Fatalf("CreateStateMachine(over quota) error = %v", err)
			}
		}
	}
}

func TestStepFunctionsCompatibilityAdapterAuthorizesCreation(t *testing.T) {
	auditLog := authz.NewAuditLog()
	server := httptest.NewServer(compataws.NewStepFunctionsAdapter(
		compataws.WithStepFunctionsAuthorizer(authz.NewPolicyAuthorizer(authz.Rule{Effect: authz.Deny, Actions: []string{"states:CreateStateMachine"}, Resources: []string{"*"}})),
		compataws.WithStepFunctionsAuditSink(auditLog.Record),
	))
	defer server.Close()
	client := sfn.NewFromConfig(aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("homeport", "homeport", "")}, func(o *sfn.Options) { o.BaseEndpoint = aws.String(server.URL) })
	_, err := client.CreateStateMachine(context.Background(), &sfn.CreateStateMachineInput{Name: aws.String("orders"), Definition: aws.String(`{"StartAt":"Done","States":{"Done":{"Type":"Succeed"}}}`), RoleArn: aws.String("arn:aws:iam::000000000000:role/step-functions")})
	var apiErr smithy.APIError
	if err == nil || !errors.As(err, &apiErr) || apiErr.ErrorCode() != "AccessDeniedException" {
		t.Fatalf("CreateStateMachine(denied) error = %v", err)
	}
	assertDecision(t, auditLog.Decisions(), "states:CreateStateMachine", false)
}
