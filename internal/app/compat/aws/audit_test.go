package aws

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/homeport/homeport/internal/domain/authz"
)

func TestAppSyncAdapterRecordsAuthorizationDecision(t *testing.T) {
	adapter := NewAppSyncAdapter()
	auditLog := authz.NewAuditLog()
	adapter.auditSink = auditLog.Record
	adapter.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/apis", bytes.NewBufferString(`{"name":"orders","authenticationType":"API_KEY"}`)))

	assertAuditDecision(t, auditLog.Decisions(), "appsync:CreateGraphqlApi")
}

func TestCodeBuildAdapterRecordsAuthorizationDecision(t *testing.T) {
	adapter := NewCodeBuildAdapter()
	auditLog := authz.NewAuditLog()
	adapter.auditSink = auditLog.Record
	request := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"name":"orders","artifacts":{},"environment":{},"serviceRole":"role","source":{}}`))
	request.Header.Set("X-Amz-Target", "CodeBuild_20161006.CreateProject")
	adapter.ServeHTTP(httptest.NewRecorder(), request)

	assertAuditDecision(t, auditLog.Decisions(), "codebuild:CreateProject")
}

func TestStepFunctionsAdapterRecordsAuthorizationDecision(t *testing.T) {
	adapter := NewStepFunctionsAdapter()
	auditLog := authz.NewAuditLog()
	adapter.auditSink = auditLog.Record
	request := httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"name":"orders","definition":"{\"StartAt\":\"Done\",\"States\":{\"Done\":{\"Type\":\"Succeed\"}}}","roleArn":"role"}`))
	request.Header.Set("X-Amz-Target", "AWSStepFunctions.CreateStateMachine")
	adapter.ServeHTTP(httptest.NewRecorder(), request)

	assertAuditDecision(t, auditLog.Decisions(), "states:CreateStateMachine")
}

func assertAuditDecision(t *testing.T, decisions []authz.Decision, action string) {
	t.Helper()
	if len(decisions) != 1 || decisions[0].Request.Action != action || !decisions[0].Allowed {
		t.Fatalf("audit decisions = %#v, want one allowed %s decision", decisions, action)
	}
}
