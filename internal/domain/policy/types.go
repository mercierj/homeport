// Package policy defines types for cloud IAM and resource policies.
package policy

import (
	"encoding/json"
	"time"
)

// PolicyType identifies the kind of policy.
type PolicyType string

const (
	PolicyTypeIAM      PolicyType = "iam"      // Identity policies (IAM roles, users)
	PolicyTypeResource PolicyType = "resource" // Resource-based policies (S3 bucket, KMS key)
	PolicyTypeNetwork  PolicyType = "network"  // Network policies (security groups, firewalls)
)

// Provider identifies the cloud provider.
type Provider string

const (
	ProviderAWS   Provider = "aws"
	ProviderGCP   Provider = "gcp"
	ProviderAzure Provider = "azure"
)

// Effect represents policy statement effect.
type Effect string

const (
	EffectAllow Effect = "Allow"
	EffectDeny  Effect = "Deny"
)

// Policy represents a cloud policy in normalized format.
type Policy struct {
	// ID is a unique identifier for this policy
	ID string `json:"id"`

	// Name is the human-readable name
	Name string `json:"name"`

	// Type identifies whether this is IAM, resource, or network policy
	Type PolicyType `json:"type"`

	// Provider is the cloud provider (aws, gcp, azure)
	Provider Provider `json:"provider"`

	// ResourceID is the ID of the resource this policy is attached to
	ResourceID string `json:"resource_id"`

	// ResourceType is the type of resource (e.g., "aws_s3_bucket", "aws_iam_role")
	ResourceType string `json:"resource_type"`

	// ResourceName is the friendly name of the resource
	ResourceName string `json:"resource_name"`

	// OriginalDocument contains the exact original policy document (preserved)
	OriginalDocument json.RawMessage `json:"original_document"`

	// OriginalFormat describes the format of the original document
	OriginalFormat string `json:"original_format,omitempty"` // "json", "hcl", "yaml"

	// NormalizedPolicy is the parsed, normalized representation
	NormalizedPolicy *NormalizedPolicy `json:"normalized_policy,omitempty"`

	// KeycloakMapping is the suggested Keycloak mapping
	KeycloakMapping *KeycloakMapping `json:"keycloak_mapping,omitempty"`

	// Warnings contains issues found during normalization
	Warnings []string `json:"warnings,omitempty"`

	// CreatedAt is when this policy was extracted
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when this policy was last modified
	UpdatedAt time.Time `json:"updated_at"`
}

// NormalizedPolicy represents a cloud-agnostic policy format.
type NormalizedPolicy struct {
	// Version is the policy format version
	Version string `json:"version,omitempty"`

	// Statements are the policy statements
	Statements []Statement `json:"statements"`

	// NetworkRules are network-specific rules (for network policies)
	NetworkRules []NetworkRule `json:"network_rules,omitempty"`
}

// Statement represents a single policy statement.
type Statement struct {
	// SID is an optional statement identifier
	SID string `json:"sid,omitempty"`

	// Effect is Allow or Deny
	Effect Effect `json:"effect"`

	// Principals are the entities the statement applies to
	Principals []Principal `json:"principals,omitempty"`

	// Actions are the actions allowed/denied
	Actions []string `json:"actions"`

	// NotActions are actions explicitly excluded
	NotActions []string `json:"not_actions,omitempty"`

	// Resources are the resources the statement applies to
	Resources []string `json:"resources"`

	// NotResources are resources explicitly excluded
	NotResources []string `json:"not_resources,omitempty"`

	// Conditions are optional conditions for the statement
	Conditions []Condition `json:"conditions,omitempty"`
}

// Principal represents an entity in a policy.
type Principal struct {
	// Type is the principal type (AWS, Service, Federated, etc.)
	Type string `json:"type"`

	// ID is the principal identifier (ARN, account ID, service name, etc.)
	ID string `json:"id"`
}

// Condition represents a policy condition.
type Condition struct {
	// Operator is the condition operator (StringEquals, IpAddress, etc.)
	Operator string `json:"operator"`

	// Key is the condition key
	Key string `json:"key"`

	// Values are the condition values
	Values []string `json:"values"`
}

// NetworkRule represents a network-level access rule.
type NetworkRule struct {
	// Direction is inbound or outbound
	Direction string `json:"direction"` // "ingress" or "egress"

	// Protocol is the network protocol
	Protocol string `json:"protocol"` // "tcp", "udp", "icmp", "-1" (all)

	// FromPort is the start of the port range
	FromPort int `json:"from_port"`

	// ToPort is the end of the port range
	ToPort int `json:"to_port"`

	// CIDRBlocks are the CIDR ranges allowed/denied
	CIDRBlocks []string `json:"cidr_blocks,omitempty"`

	// SecurityGroups are referenced security groups
	SecurityGroups []string `json:"security_groups,omitempty"`

	// Description describes this rule
	Description string `json:"description,omitempty"`

	// Priority is the rule priority (for Azure, GCP)
	Priority int `json:"priority,omitempty"`

	// Action is the action (allow/deny) for stateful firewall rules
	Action string `json:"action,omitempty"`
}

// ActionCategory groups actions by category for UI display.
type ActionCategory struct {
	Name    string   `json:"name"`    // "Storage", "Compute", etc.
	Actions []string `json:"actions"` // List of available actions
}

// PredefinedActions contains common action categories for policy editing.
var PredefinedActions = []ActionCategory{
	{
		Name:    "Storage",
		Actions: []string{"read", "write", "delete", "list"},
	},
	{
		Name:    "Compute",
		Actions: []string{"invoke", "manage", "deploy", "scale"},
	},
	{
		Name:    "Database",
		Actions: []string{"read", "write", "admin", "backup"},
	},
	{
		Name:    "Messaging",
		Actions: []string{"send", "receive", "manage", "subscribe"},
	},
	{
		Name:    "Security",
		Actions: []string{"encrypt", "decrypt", "manage-keys", "audit"},
	},
}

// NewPolicy creates a new Policy with defaults.
func NewPolicy(id, name string, policyType PolicyType, provider Provider) *Policy {
	now := time.Now()
	return &Policy{
		ID:        id,
		Name:      name,
		Type:      policyType,
		Provider:  provider,
		Warnings:  make([]string, 0),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// AddWarning adds a warning to the policy.
func (p *Policy) AddWarning(warning string) {
	p.Warnings = append(p.Warnings, warning)
}

// HasWarnings returns true if the policy has warnings.
func (p *Policy) HasWarnings() bool {
	return len(p.Warnings) > 0
}

// IsEditable returns true if the policy can be edited.
// Network policies are not directly editable (mapped to Docker/iptables instead).
func (p *Policy) IsEditable() bool {
	return p.Type != PolicyTypeNetwork
}
