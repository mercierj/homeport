package security

import (
	"fmt"
	"strings"
	"sync"
)

// Permission represents a specific access permission
type Permission int

const (
	PermissionNone Permission = 0
	PermissionRead Permission = 1 << iota
	PermissionWrite
	PermissionDelete
	PermissionAll = PermissionRead | PermissionWrite | PermissionDelete
)

// String returns a string representation of Permission
func (p Permission) String() string {
	if p == PermissionNone {
		return "NONE"
	}
	var perms []string
	if p&PermissionRead != 0 {
		perms = append(perms, "READ")
	}
	if p&PermissionWrite != 0 {
		perms = append(perms, "WRITE")
	}
	if p&PermissionDelete != 0 {
		perms = append(perms, "DELETE")
	}
	return strings.Join(perms, ",")
}

// Has checks if this permission includes another permission
func (p Permission) Has(other Permission) bool {
	return p&other == other
}

// TableACL represents access control rules for a specific table
type TableACL struct {
	Schema      string     // Schema name (empty = match all schemas)
	Table       string     // Table name (empty = match all tables, supports wildcards)
	Permission  Permission // Allowed permissions
	DenyColumns []string   // Columns that should be masked or hidden
}

// ACLRule represents a single ACL rule with optional conditions
type ACLRule struct {
	ID          string
	Description string
	ACL         TableACL
	Priority    int  // Higher priority rules are evaluated first
	Enabled     bool // Whether this rule is active
}

// ACLConfig holds the complete ACL configuration
type ACLConfig struct {
	// DefaultPermission is applied when no rules match
	DefaultPermission Permission
	// DefaultDenySchemas are schemas denied by default
	DefaultDenySchemas []string
	// Rules are the ACL rules in order of evaluation
	Rules []ACLRule
}

// DefaultACLConfig returns a secure default ACL configuration
func DefaultACLConfig() *ACLConfig {
	return &ACLConfig{
		DefaultPermission: PermissionRead,
		DefaultDenySchemas: []string{
			"pg_catalog",
			"information_schema",
			"pg_toast",
			"pg_temp_1",
		},
		Rules: []ACLRule{
			{
				ID:          "deny-system-schemas",
				Description: "Deny access to PostgreSQL system schemas",
				ACL: TableACL{
					Schema:     "pg_catalog",
					Permission: PermissionNone,
				},
				Priority: 1000,
				Enabled:  true,
			},
			{
				ID:          "deny-information-schema",
				Description: "Deny access to information_schema",
				ACL: TableACL{
					Schema:     "information_schema",
					Permission: PermissionNone,
				},
				Priority: 1000,
				Enabled:  true,
			},
			{
				ID:          "allow-public-read",
				Description: "Allow read access to public schema",
				ACL: TableACL{
					Schema:     "public",
					Permission: PermissionRead,
				},
				Priority: 100,
				Enabled:  true,
			},
		},
	}
}

// ACLManager manages table-level access control
type ACLManager struct {
	config *ACLConfig
	mu     sync.RWMutex
}

// NewACLManager creates a new ACL manager with the given configuration
func NewACLManager(config *ACLConfig) *ACLManager {
	if config == nil {
		config = DefaultACLConfig()
	}
	return &ACLManager{config: config}
}

// CheckAccess checks if access to a table is allowed
func (m *ACLManager) CheckAccess(schema, table string, requiredPermission Permission) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if schema == "" {
		schema = "public"
	}

	// Check default deny schemas
	for _, denied := range m.config.DefaultDenySchemas {
		if strings.EqualFold(schema, denied) {
			return &ACLError{
				Code:    "ACL_SCHEMA_DENIED",
				Message: fmt.Sprintf("access to schema '%s' is denied", schema),
				Schema:  schema,
				Table:   table,
			}
		}
	}

	// Find matching rules (sorted by priority)
	permission := m.config.DefaultPermission
	for _, rule := range m.getSortedRules() {
		if !rule.Enabled {
			continue
		}
		if m.ruleMatches(rule.ACL, schema, table) {
			permission = rule.ACL.Permission
			break
		}
	}

	// Check if required permission is granted
	if !permission.Has(requiredPermission) {
		return &ACLError{
			Code:    "ACL_PERMISSION_DENIED",
			Message: fmt.Sprintf("permission '%s' denied for table '%s.%s'", requiredPermission, schema, table),
			Schema:  schema,
			Table:   table,
		}
	}

	return nil
}

// CheckTableReferences checks access for multiple table references
func (m *ACLManager) CheckTableReferences(tables []TableReference, requiredPermission Permission) error {
	for _, table := range tables {
		if err := m.CheckAccess(table.Schema, table.Table, requiredPermission); err != nil {
			return err
		}
	}
	return nil
}

// GetDeniedColumns returns columns that should be masked for a given table
func (m *ACLManager) GetDeniedColumns(schema, table string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if schema == "" {
		schema = "public"
	}

	for _, rule := range m.getSortedRules() {
		if !rule.Enabled {
			continue
		}
		if m.ruleMatches(rule.ACL, schema, table) && len(rule.ACL.DenyColumns) > 0 {
			return rule.ACL.DenyColumns
		}
	}
	return nil
}

// AddRule adds a new ACL rule
func (m *ACLManager) AddRule(rule ACLRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Rules = append(m.config.Rules, rule)
}

// RemoveRule removes an ACL rule by ID
func (m *ACLManager) RemoveRule(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, rule := range m.config.Rules {
		if rule.ID == id {
			m.config.Rules = append(m.config.Rules[:i], m.config.Rules[i+1:]...)
			return true
		}
	}
	return false
}

// SetDefaultPermission sets the default permission
func (m *ACLManager) SetDefaultPermission(perm Permission) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.DefaultPermission = perm
}

// getSortedRules returns rules sorted by priority (highest first)
func (m *ACLManager) getSortedRules() []ACLRule {
	// Simple insertion sort (rules list is typically small)
	sorted := make([]ACLRule, len(m.config.Rules))
	copy(sorted, m.config.Rules)
	for i := 1; i < len(sorted); i++ {
		j := i
		for j > 0 && sorted[j].Priority > sorted[j-1].Priority {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			j--
		}
	}
	return sorted
}

// ruleMatches checks if an ACL rule matches a schema/table
func (m *ACLManager) ruleMatches(acl TableACL, schema, table string) bool {
	// Match schema
	if acl.Schema != "" && !strings.EqualFold(acl.Schema, schema) {
		// Check for wildcard
		if !matchWildcard(acl.Schema, schema) {
			return false
		}
	}

	// Match table
	if acl.Table != "" && !strings.EqualFold(acl.Table, table) {
		// Check for wildcard
		if !matchWildcard(acl.Table, table) {
			return false
		}
	}

	return true
}

// matchWildcard performs simple wildcard matching (supports * and ?)
func matchWildcard(pattern, s string) bool {
	pattern = strings.ToLower(pattern)
	s = strings.ToLower(s)

	// Simple implementation - could be optimized
	for len(pattern) > 0 && len(s) > 0 {
		switch pattern[0] {
		case '*':
			// Match any sequence
			if len(pattern) == 1 {
				return true
			}
			// Try matching the rest of the pattern at each position
			for i := 0; i <= len(s); i++ {
				if matchWildcard(pattern[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			// Match any single character
			pattern = pattern[1:]
			s = s[1:]
		default:
			if pattern[0] != s[0] {
				return false
			}
			pattern = pattern[1:]
			s = s[1:]
		}
	}

	// Handle trailing wildcards
	for len(pattern) > 0 && pattern[0] == '*' {
		pattern = pattern[1:]
	}

	return len(pattern) == 0 && len(s) == 0
}

// ACLError represents an ACL-related error
type ACLError struct {
	Code    string
	Message string
	Schema  string
	Table   string
}

func (e *ACLError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
