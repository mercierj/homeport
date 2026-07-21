package authz

import (
	"path/filepath"
	"testing"
)

func TestAuditLogRecordsDecisionsInOrder(t *testing.T) {
	log := NewAuditLog()

	log.Record(Decision{Request: Request{Action: "sqs:CreateQueue"}, Allowed: true})
	log.Record(Decision{Request: Request{Action: "sqs:SendMessage"}, Allowed: false})

	decisions := log.Decisions()
	if len(decisions) != 2 {
		t.Fatalf("decisions length = %d, want 2", len(decisions))
	}
	if decisions[0].Request.Action != "sqs:CreateQueue" || !decisions[0].Allowed {
		t.Fatalf("first decision = %#v", decisions[0])
	}
	if decisions[1].Request.Action != "sqs:SendMessage" || decisions[1].Allowed {
		t.Fatalf("second decision = %#v", decisions[1])
	}

	decisions[0].Allowed = false
	if !log.Decisions()[0].Allowed {
		t.Fatal("Decisions returned mutable backing slice")
	}
}

func TestFileAuditLogPersistsDecisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authz-audit.jsonl")
	log := NewFileAuditLog(path)

	if err := log.Record(Decision{Request: Request{
		Principal: "homeport",
		Action:    "sqs:SendMessage",
		Resource:  "arn:aws:sqs:us-east-1:homeport:jobs",
	}, Allowed: false, Reason: "test policy"}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	reopened := NewFileAuditLog(path)
	decisions, err := reopened.Decisions()
	if err != nil {
		t.Fatalf("Decisions() error = %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions length = %d, want 1", len(decisions))
	}
	if decisions[0].Request.Action != "sqs:SendMessage" || decisions[0].Allowed {
		t.Fatalf("decision = %#v", decisions[0])
	}
}

func TestPolicyAuthorizerAllowsWildcardMatches(t *testing.T) {
	authorizer := NewPolicyAuthorizer(Rule{
		Effect:     Allow,
		Principals: []string{"user:*"},
		Actions:    []string{"sqs:*"},
		Resources:  []string{"arn:aws:sqs:us-east-1:homeport:*"},
	})

	decision, err := authorizer.Authorize(t.Context(), Request{
		Principal: "user:alice",
		Action:    "sqs:SendMessage",
		Resource:  "arn:aws:sqs:us-east-1:homeport:jobs",
	})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("decision = %#v, want allowed", decision)
	}
}

func TestPolicyAuthorizerDenyOverridesAllow(t *testing.T) {
	authorizer := NewPolicyAuthorizer(
		Rule{Effect: Allow, Actions: []string{"sqs:*"}, Resources: []string{"*"}},
		Rule{Effect: Deny, Actions: []string{"sqs:DeleteQueue"}, Resources: []string{"*"}},
	)

	decision, err := authorizer.Authorize(t.Context(), Request{
		Action:   "sqs:DeleteQueue",
		Resource: "arn:aws:sqs:us-east-1:homeport:jobs",
	})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}
	if decision.Allowed {
		t.Fatalf("decision = %#v, want denied", decision)
	}
}

func TestPolicyAuthorizerMatchesSourceIPCidrCondition(t *testing.T) {
	authorizer := NewPolicyAuthorizer(Rule{
		Effect:    Allow,
		Actions:   []string{"sqs:*"},
		Resources: []string{"*"},
		Conditions: []Condition{
			{Key: "source_ip", Values: []string{"192.0.2.0/24"}},
		},
	})

	allowed, err := authorizer.Authorize(t.Context(), Request{
		Action:   "sqs:ReceiveMessage",
		Resource: "arn:aws:sqs:us-east-1:homeport:jobs",
		Context:  map[string]string{"source_ip": "192.0.2.42"},
	})
	if err != nil {
		t.Fatalf("Authorize(allowed) error = %v", err)
	}
	if !allowed.Allowed {
		t.Fatalf("allowed decision = %#v, want allowed", allowed)
	}

	denied, err := authorizer.Authorize(t.Context(), Request{
		Action:   "sqs:ReceiveMessage",
		Resource: "arn:aws:sqs:us-east-1:homeport:jobs",
		Context:  map[string]string{"source_ip": "198.51.100.42"},
	})
	if err != nil {
		t.Fatalf("Authorize(denied) error = %v", err)
	}
	if denied.Allowed {
		t.Fatalf("denied decision = %#v, want denied", denied)
	}
}

func TestPolicyAuthorizerMatchesTimeWindowCondition(t *testing.T) {
	authorizer := NewPolicyAuthorizer(Rule{
		Effect:    Allow,
		Actions:   []string{"s3:GetObject"},
		Resources: []string{"*"},
		Conditions: []Condition{
			{Key: "current_time", Values: []string{"2026-07-07T09:00:00Z/2026-07-07T17:00:00Z"}},
		},
	})

	allowed, err := authorizer.Authorize(t.Context(), Request{
		Action:   "s3:GetObject",
		Resource: "arn:aws:s3:us-east-1:homeport:s3/bucket/key",
		Context:  map[string]string{"current_time": "2026-07-07T12:00:00Z"},
	})
	if err != nil {
		t.Fatalf("Authorize(allowed) error = %v", err)
	}
	if !allowed.Allowed {
		t.Fatalf("allowed decision = %#v, want allowed", allowed)
	}

	denied, err := authorizer.Authorize(t.Context(), Request{
		Action:   "s3:GetObject",
		Resource: "arn:aws:s3:us-east-1:homeport:s3/bucket/key",
		Context:  map[string]string{"current_time": "2026-07-07T18:00:00Z"},
	})
	if err != nil {
		t.Fatalf("Authorize(denied) error = %v", err)
	}
	if denied.Allowed {
		t.Fatalf("denied decision = %#v, want denied", denied)
	}
}

func TestPolicyAuthorizerMatchesPrincipalAttributeCondition(t *testing.T) {
	authorizer := NewPolicyAuthorizer(Rule{
		Effect:    Allow,
		Actions:   []string{"s3:GetObject"},
		Resources: []string{"*"},
		Conditions: []Condition{
			{Key: "principal:department", Values: []string{"finance"}},
		},
	})

	allowed, err := authorizer.Authorize(t.Context(), Request{
		Action:   "s3:GetObject",
		Resource: "arn:aws:s3:us-east-1:homeport:s3/bucket/key",
		PrincipalAttributes: map[string]string{
			"department": "finance",
		},
	})
	if err != nil {
		t.Fatalf("Authorize(allowed) error = %v", err)
	}
	if !allowed.Allowed {
		t.Fatalf("allowed decision = %#v, want allowed", allowed)
	}

	denied, err := authorizer.Authorize(t.Context(), Request{
		Action:   "s3:GetObject",
		Resource: "arn:aws:s3:us-east-1:homeport:s3/bucket/key",
		PrincipalAttributes: map[string]string{
			"department": "engineering",
		},
	})
	if err != nil {
		t.Fatalf("Authorize(denied) error = %v", err)
	}
	if denied.Allowed {
		t.Fatalf("denied decision = %#v, want denied", denied)
	}
}

func TestPolicyAuthorizerMatchesClaimCondition(t *testing.T) {
	authorizer := NewPolicyAuthorizer(Rule{
		Effect:    Allow,
		Actions:   []string{"s3:GetObject"},
		Resources: []string{"*"},
		Conditions: []Condition{
			{Key: "claim:mfa", Values: []string{"true"}},
		},
	})

	allowed, err := authorizer.Authorize(t.Context(), Request{
		Action:   "s3:GetObject",
		Resource: "arn:aws:s3:us-east-1:homeport:s3/bucket/key",
		Claims: map[string]string{
			"mfa": "true",
		},
	})
	if err != nil {
		t.Fatalf("Authorize(allowed) error = %v", err)
	}
	if !allowed.Allowed {
		t.Fatalf("allowed decision = %#v, want allowed", allowed)
	}

	denied, err := authorizer.Authorize(t.Context(), Request{
		Action:   "s3:GetObject",
		Resource: "arn:aws:s3:us-east-1:homeport:s3/bucket/key",
		Claims: map[string]string{
			"mfa": "false",
		},
	})
	if err != nil {
		t.Fatalf("Authorize(denied) error = %v", err)
	}
	if denied.Allowed {
		t.Fatalf("denied decision = %#v, want denied", denied)
	}
}
