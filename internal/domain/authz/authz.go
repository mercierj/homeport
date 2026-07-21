package authz

import (
	"context"
	"sync"
)

type Request struct {
	Principal           string
	PrincipalAttributes map[string]string
	Claims              map[string]string
	Action              string
	Resource            string
	Context             map[string]string
}

type Decision struct {
	Request Request
	Allowed bool
	Reason  string
}

type Authorizer interface {
	Authorize(context.Context, Request) (Decision, error)
}

type AuthorizerFunc func(context.Context, Request) (Decision, error)

func (f AuthorizerFunc) Authorize(ctx context.Context, req Request) (Decision, error) {
	return f(ctx, req)
}

var AllowAll Authorizer = AuthorizerFunc(func(_ context.Context, req Request) (Decision, error) {
	return Decision{Request: req, Allowed: true, Reason: "allow all"}, nil
})

type AuditLog struct {
	mu        sync.Mutex
	decisions []Decision
}

func NewAuditLog() *AuditLog {
	return &AuditLog{}
}

func (l *AuditLog) Record(decision Decision) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.decisions = append(l.decisions, decision)
}

func (l *AuditLog) Decisions() []Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Decision(nil), l.decisions...)
}
