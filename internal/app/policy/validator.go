package policy

import (
	"encoding/json"
	"fmt"

	"github.com/homeport/homeport/internal/domain/policy"
)

// ValidationError represents a validation issue.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Severe  bool   `json:"severe"`
}

// ValidationResult contains validation results.
type ValidationResult struct {
	Valid  bool              `json:"valid"`
	Errors []ValidationError `json:"errors,omitempty"`
}

// Validator validates policies.
type Validator struct{}

// NewValidator creates a new validator.
func NewValidator() *Validator {
	return &Validator{}
}

// Validate checks a policy and returns errors.
func (v *Validator) Validate(p *policy.Policy) []ValidationError {
	return v.ValidateWithDetails(p).Errors
}

// ValidateWithDetails performs full validation and returns details.
func (v *Validator) ValidateWithDetails(p *policy.Policy) *ValidationResult {
	result := &ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}

	// Validate required fields
	if p.Name == "" {
		result.addError("name", "Policy name is required", true)
	}

	if p.Type == "" {
		result.addError("type", "Policy type is required", true)
	}

	// Validate normalized policy if present
	if p.NormalizedPolicy != nil {
		v.validateNormalizedPolicy(p.NormalizedPolicy, p.Type, result)
	}

	// Validate original document is valid JSON if present
	if len(p.OriginalDocument) > 0 {
		var js interface{}
		if err := json.Unmarshal(p.OriginalDocument, &js); err != nil {
			result.addError("original_document", "Invalid JSON document", true)
		}
	}

	return result
}

// validateNormalizedPolicy validates the normalized policy structure.
func (v *Validator) validateNormalizedPolicy(np *policy.NormalizedPolicy, pType policy.PolicyType, result *ValidationResult) {
	// IAM and resource policies need statements
	if pType != policy.PolicyTypeNetwork && len(np.Statements) == 0 {
		result.addError("statements", "At least one statement is required", true)
	}

	// Network policies need rules
	if pType == policy.PolicyTypeNetwork && len(np.NetworkRules) == 0 {
		result.addError("network_rules", "At least one network rule is required", true)
	}

	// Validate each statement
	for i, stmt := range np.Statements {
		v.validateStatement(&stmt, i, result)
	}

	// Validate each network rule
	for i, rule := range np.NetworkRules {
		v.validateNetworkRule(&rule, i, result)
	}
}

// validateStatement validates a single statement.
func (v *Validator) validateStatement(stmt *policy.Statement, idx int, result *ValidationResult) {
	prefix := fmt.Sprintf("statements[%d]", idx)

	// Effect is required
	if stmt.Effect == "" {
		result.addError(prefix+".effect", "Effect (Allow/Deny) is required", true)
	} else if stmt.Effect != policy.EffectAllow && stmt.Effect != policy.EffectDeny {
		result.addError(prefix+".effect", "Effect must be Allow or Deny", true)
	}

	// Actions or NotActions required
	if len(stmt.Actions) == 0 && len(stmt.NotActions) == 0 {
		result.addError(prefix+".actions", "At least one action is required", true)
	}

	// Resources or NotResources required
	if len(stmt.Resources) == 0 && len(stmt.NotResources) == 0 {
		result.addError(prefix+".resources", "At least one resource is required", true)
	}

	// Validate wildcards
	for _, action := range stmt.Actions {
		if action == "*" {
			result.addError(prefix+".actions", "Wildcard '*' action is overly permissive", false)
		}
	}

	for _, resource := range stmt.Resources {
		if resource == "*" {
			result.addError(prefix+".resources", "Wildcard '*' resource is overly permissive", false)
		}
	}

	// Validate principals
	for j, principal := range stmt.Principals {
		if principal.Type == "" {
			result.addError(fmt.Sprintf("%s.principals[%d].type", prefix, j), "Principal type is required", true)
		}
		if principal.ID == "" && principal.Type != "*" {
			result.addError(fmt.Sprintf("%s.principals[%d].id", prefix, j), "Principal ID is required", true)
		}
	}

	// Validate conditions
	for j, cond := range stmt.Conditions {
		if cond.Operator == "" {
			result.addError(fmt.Sprintf("%s.conditions[%d].operator", prefix, j), "Condition operator is required", true)
		}
		if cond.Key == "" {
			result.addError(fmt.Sprintf("%s.conditions[%d].key", prefix, j), "Condition key is required", true)
		}
		if len(cond.Values) == 0 {
			result.addError(fmt.Sprintf("%s.conditions[%d].values", prefix, j), "At least one condition value is required", true)
		}
	}
}

// validateNetworkRule validates a network rule.
func (v *Validator) validateNetworkRule(rule *policy.NetworkRule, idx int, result *ValidationResult) {
	prefix := fmt.Sprintf("network_rules[%d]", idx)

	// Direction is required
	if rule.Direction == "" {
		result.addError(prefix+".direction", "Direction (ingress/egress) is required", true)
	} else if rule.Direction != "ingress" && rule.Direction != "egress" {
		result.addError(prefix+".direction", "Direction must be ingress or egress", true)
	}

	// Protocol is required
	if rule.Protocol == "" {
		result.addError(prefix+".protocol", "Protocol is required", true)
	}

	// Port range validation
	if rule.Protocol != "icmp" && rule.Protocol != "-1" {
		if rule.FromPort < 0 || rule.FromPort > 65535 {
			result.addError(prefix+".from_port", "Port must be between 0 and 65535", true)
		}
		if rule.ToPort < 0 || rule.ToPort > 65535 {
			result.addError(prefix+".to_port", "Port must be between 0 and 65535", true)
		}
		if rule.FromPort > rule.ToPort {
			result.addError(prefix+".from_port", "From port must be less than or equal to to port", true)
		}
	}

	// Source/destination validation
	if len(rule.CIDRBlocks) == 0 && len(rule.SecurityGroups) == 0 {
		result.addError(prefix, "Either CIDR blocks or security groups must be specified", true)
	}

	// CIDR validation
	for j, cidr := range rule.CIDRBlocks {
		if cidr == "0.0.0.0/0" {
			result.addError(fmt.Sprintf("%s.cidr_blocks[%d]", prefix, j), "0.0.0.0/0 allows access from anywhere", false)
		}
		if cidr == "::/0" {
			result.addError(fmt.Sprintf("%s.cidr_blocks[%d]", prefix, j), "::/0 allows IPv6 access from anywhere", false)
		}
	}
}

// addError adds a validation error.
func (r *ValidationResult) addError(field, message string, severe bool) {
	r.Errors = append(r.Errors, ValidationError{
		Field:   field,
		Message: message,
		Severe:  severe,
	})
	if severe {
		r.Valid = false
	}
}

// ValidateJSON validates a raw JSON policy document.
func (v *Validator) ValidateJSON(doc json.RawMessage) *ValidationResult {
	result := &ValidationResult{
		Valid:  true,
		Errors: make([]ValidationError, 0),
	}

	if len(doc) == 0 {
		result.addError("document", "Empty document", true)
		return result
	}

	var parsed interface{}
	if err := json.Unmarshal(doc, &parsed); err != nil {
		result.addError("document", fmt.Sprintf("Invalid JSON: %v", err), true)
		return result
	}

	// Check for basic AWS policy structure
	if obj, ok := parsed.(map[string]interface{}); ok {
		if _, hasVersion := obj["Version"]; !hasVersion {
			result.addError("Version", "Policy version is recommended", false)
		}
		if _, hasStatement := obj["Statement"]; !hasStatement {
			result.addError("Statement", "Statement is required", true)
		}
	}

	return result
}
