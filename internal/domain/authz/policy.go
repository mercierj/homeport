package authz

import (
	"context"
	"net/netip"
	"strings"
	"time"
)

type Effect string

const (
	Allow Effect = "allow"
	Deny  Effect = "deny"
)

type Rule struct {
	Effect     Effect
	Principals []string
	Actions    []string
	Resources  []string
	Conditions []Condition
}

type Condition struct {
	Key    string
	Values []string
}

type PolicyAuthorizer struct {
	rules []Rule
}

func NewPolicyAuthorizer(rules ...Rule) *PolicyAuthorizer {
	return &PolicyAuthorizer{rules: append([]Rule(nil), rules...)}
}

func (a *PolicyAuthorizer) Authorize(_ context.Context, req Request) (Decision, error) {
	allowed := false
	for _, rule := range a.rules {
		if !ruleMatches(rule, req) {
			continue
		}
		if rule.Effect == Deny {
			return Decision{Request: req, Allowed: false, Reason: "explicit deny"}, nil
		}
		if rule.Effect == Allow {
			allowed = true
		}
	}
	if allowed {
		return Decision{Request: req, Allowed: true, Reason: "allowed by policy"}, nil
	}
	return Decision{Request: req, Allowed: false, Reason: "default deny"}, nil
}

func ruleMatches(rule Rule, req Request) bool {
	return matchesAny(rule.Principals, req.Principal) &&
		matchesAny(rule.Actions, req.Action) &&
		matchesAny(rule.Resources, req.Resource) &&
		conditionsMatch(rule.Conditions, req)
}

func matchesAny(patterns []string, value string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if pattern == "*" || pattern == value {
			return true
		}
		if strings.HasSuffix(pattern, "*") && strings.HasPrefix(value, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func conditionsMatch(conditions []Condition, req Request) bool {
	for _, condition := range conditions {
		value := conditionValue(condition.Key, req)
		if value == "" || !conditionMatches(condition, value) {
			return false
		}
	}
	return true
}

func conditionValue(key string, req Request) string {
	if attr, ok := strings.CutPrefix(key, "principal:"); ok {
		return req.PrincipalAttributes[attr]
	}
	if claim, ok := strings.CutPrefix(key, "claim:"); ok {
		return req.Claims[claim]
	}
	return req.Context[key]
}

func conditionMatches(condition Condition, value string) bool {
	if condition.Key == "source_ip" {
		return matchesCIDR(condition.Values, value)
	}
	if condition.Key == "current_time" {
		return matchesTimeWindow(condition.Values, value)
	}
	return matchesAny(condition.Values, value)
}

func matchesTimeWindow(patterns []string, value string) bool {
	now, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	for _, pattern := range patterns {
		startValue, endValue, ok := strings.Cut(pattern, "/")
		if !ok {
			continue
		}
		start, startErr := time.Parse(time.RFC3339, startValue)
		end, endErr := time.Parse(time.RFC3339, endValue)
		if startErr == nil && endErr == nil && !now.Before(start) && !now.After(end) {
			return true
		}
	}
	return false
}

func matchesCIDR(patterns []string, value string) bool {
	ip, err := netip.ParseAddr(value)
	if err != nil {
		return false
	}
	for _, pattern := range patterns {
		prefix, err := netip.ParsePrefix(pattern)
		if err == nil && prefix.Contains(ip) {
			return true
		}
		if pattern == value {
			return true
		}
	}
	return false
}
