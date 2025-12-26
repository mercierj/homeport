package security

import (
	"context"
	"log/slog"
)

// Manager provides a unified interface for all SQL security features
type Manager struct {
	validator *Validator
	acl       *ACLManager
	masking   *MaskingManager
	auditor   *Auditor
}

// ManagerConfig holds configuration for the security manager
type ManagerConfig struct {
	Validator *ValidatorConfig
	ACL       *ACLConfig
	Masking   *MaskingConfig
	Audit     *AuditConfig
	Logger    *slog.Logger
}

// DefaultManagerConfig returns a secure default configuration
func DefaultManagerConfig() *ManagerConfig {
	return &ManagerConfig{
		Validator: DefaultValidatorConfig(),
		ACL:       DefaultACLConfig(),
		Masking:   DefaultMaskingConfig(),
		Audit:     DefaultAuditConfig(),
	}
}

// NewManager creates a new security manager
func NewManager(config *ManagerConfig) *Manager {
	if config == nil {
		config = DefaultManagerConfig()
	}

	return &Manager{
		validator: NewValidator(config.Validator),
		acl:       NewACLManager(config.ACL),
		masking:   NewMaskingManager(config.Masking),
		auditor:   NewAuditor(config.Audit, config.Logger),
	}
}

// ValidateQuery performs AST-based SQL validation
func (m *Manager) ValidateQuery(query string) (*QueryAnalysis, error) {
	return m.validator.Validate(query)
}

// ValidateReadOnlyQuery validates that a query is safe for read-only execution
func (m *Manager) ValidateReadOnlyQuery(query string) (*QueryAnalysis, error) {
	return m.validator.ValidateReadOnly(query)
}

// CheckAccess checks table-level ACL permissions
func (m *Manager) CheckAccess(schema, table string, permission Permission) error {
	return m.acl.CheckAccess(schema, table, permission)
}

// CheckQueryAccess validates access for all tables in a query analysis
func (m *Manager) CheckQueryAccess(analysis *QueryAnalysis, permission Permission) error {
	return m.acl.CheckTableReferences(analysis.Tables, permission)
}

// MaskRow masks sensitive columns in a result row
func (m *Manager) MaskRow(schema, table string, columns []string, row []any) []any {
	return m.masking.MaskRow(schema, table, columns, row)
}

// MaskRows masks sensitive columns in multiple result rows
func (m *Manager) MaskRows(schema, table string, columns []string, rows [][]any) [][]any {
	return m.masking.MaskRows(schema, table, columns, rows)
}

// GetMaskedColumns returns the list of columns that will be masked for a table
func (m *Manager) GetMaskedColumns(schema, table string, columns []string) []string {
	var masked []string
	for _, col := range columns {
		if shouldMask, _ := m.masking.ShouldMask(schema, table, col); shouldMask {
			masked = append(masked, col)
		}
	}
	return masked
}

// StartAudit begins tracking a query execution
func (m *Manager) StartAudit(ctx context.Context, query string, analysis *QueryAnalysis) (*QueryAuditEntry, func(rowCount int, err error)) {
	return m.auditor.StartQuery(ctx, query, analysis)
}

// LogAudit logs a query audit entry
func (m *Manager) LogAudit(entry *QueryAuditEntry) {
	m.auditor.LogQuery(entry)
}

// ValidateAndCheckAccess combines validation and ACL checking
func (m *Manager) ValidateAndCheckAccess(query string, readOnly bool) (*QueryAnalysis, error) {
	var analysis *QueryAnalysis
	var err error

	if readOnly {
		analysis, err = m.validator.ValidateReadOnly(query)
	} else {
		analysis, err = m.validator.Validate(query)
	}

	if err != nil {
		return nil, err
	}

	// Determine required permission based on query type
	permission := PermissionRead
	if !analysis.IsReadOnly {
		if analysis.QueryType == QueryTypeDelete {
			permission = PermissionDelete
		} else {
			permission = PermissionWrite
		}
	}

	// Check ACL for all tables
	if err := m.acl.CheckTableReferences(analysis.Tables, permission); err != nil {
		return nil, err
	}

	return analysis, nil
}

// SecureQuery performs full security validation, ACL checking, and audit tracking
func (m *Manager) SecureQuery(ctx context.Context, query string, readOnly bool) (*QueryAnalysis, func(rowCount int, err error), error) {
	// Validate and check access
	analysis, err := m.ValidateAndCheckAccess(query, readOnly)
	if err != nil {
		// Still audit failed queries
		entry, complete := m.auditor.StartQuery(ctx, query, nil)
		if entry != nil {
			entry.ACLChecked = true
			complete(0, err)
		}
		return nil, nil, err
	}

	// Start audit tracking
	entry, complete := m.auditor.StartQuery(ctx, query, analysis)
	if entry != nil {
		entry.ACLChecked = true
	}

	return analysis, complete, nil
}

// Validator returns the underlying validator
func (m *Manager) Validator() *Validator {
	return m.validator
}

// ACL returns the underlying ACL manager
func (m *Manager) ACL() *ACLManager {
	return m.acl
}

// Masking returns the underlying masking manager
func (m *Manager) Masking() *MaskingManager {
	return m.masking
}

// Auditor returns the underlying auditor
func (m *Manager) Auditor() *Auditor {
	return m.auditor
}

// AddACLRule adds a table-level ACL rule
func (m *Manager) AddACLRule(rule ACLRule) {
	m.acl.AddRule(rule)
}

// AddMaskingRule adds a column masking rule
func (m *Manager) AddMaskingRule(rule ColumnMaskingRule) {
	m.masking.AddRule(rule)
}
