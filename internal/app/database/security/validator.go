// Package security provides SQL security features including AST-based validation,
// table-level ACLs, column masking, and query auditing.
package security

import (
	"fmt"
	"strings"

	"github.com/xwb1989/sqlparser"
)

// QueryType represents the type of SQL query
type QueryType int

const (
	QueryTypeUnknown QueryType = iota
	QueryTypeSelect
	QueryTypeInsert
	QueryTypeUpdate
	QueryTypeDelete
	QueryTypeCreate
	QueryTypeDrop
	QueryTypeAlter
	QueryTypeTruncate
	QueryTypeGrant
	QueryTypeRevoke
	QueryTypeExplain
	QueryTypeShow
	QueryTypeOther
)

// String returns a string representation of QueryType
func (qt QueryType) String() string {
	switch qt {
	case QueryTypeSelect:
		return "SELECT"
	case QueryTypeInsert:
		return "INSERT"
	case QueryTypeUpdate:
		return "UPDATE"
	case QueryTypeDelete:
		return "DELETE"
	case QueryTypeCreate:
		return "CREATE"
	case QueryTypeDrop:
		return "DROP"
	case QueryTypeAlter:
		return "ALTER"
	case QueryTypeTruncate:
		return "TRUNCATE"
	case QueryTypeGrant:
		return "GRANT"
	case QueryTypeRevoke:
		return "REVOKE"
	case QueryTypeExplain:
		return "EXPLAIN"
	case QueryTypeShow:
		return "SHOW"
	case QueryTypeOther:
		return "OTHER"
	default:
		return "UNKNOWN"
	}
}

// TableReference represents a table referenced in a query
type TableReference struct {
	Schema string
	Table  string
	Alias  string
}

// QueryAnalysis contains the result of analyzing a SQL query
type QueryAnalysis struct {
	QueryType   QueryType
	Tables      []TableReference
	Columns     []string
	IsReadOnly  bool
	HasSubquery bool
	HasCTE      bool
	RawQuery    string
}

// ValidationError represents a SQL validation error
type ValidationError struct {
	Code    string
	Message string
	Details string
}

func (e *ValidationError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Error codes
const (
	ErrCodeParseFailed      = "SQL_PARSE_FAILED"
	ErrCodeMultipleStmts    = "SQL_MULTIPLE_STATEMENTS"
	ErrCodeForbiddenStmt    = "SQL_FORBIDDEN_STATEMENT"
	ErrCodeForbiddenTable   = "SQL_FORBIDDEN_TABLE"
	ErrCodeForbiddenSchema  = "SQL_FORBIDDEN_SCHEMA"
	ErrCodeWriteNotAllowed  = "SQL_WRITE_NOT_ALLOWED"
	ErrCodeDangerousPattern = "SQL_DANGEROUS_PATTERN"
)

// ValidatorConfig holds configuration for the SQL validator
type ValidatorConfig struct {
	// AllowedSchemas limits which schemas can be queried (empty = all allowed)
	AllowedSchemas []string
	// DeniedSchemas are schemas that are always blocked
	DeniedSchemas []string
	// AllowedQueryTypes specifies which query types are permitted
	AllowedQueryTypes []QueryType
	// MaxQueryLength is the maximum allowed query length in bytes
	MaxQueryLength int
	// AllowSubqueries controls whether subqueries are permitted
	AllowSubqueries bool
	// AllowCTEs controls whether CTEs (WITH clauses) are permitted
	AllowCTEs bool
}

// DefaultValidatorConfig returns a secure default configuration
func DefaultValidatorConfig() *ValidatorConfig {
	return &ValidatorConfig{
		DeniedSchemas: []string{
			"pg_catalog",
			"information_schema",
			"pg_toast",
		},
		AllowedQueryTypes: []QueryType{
			QueryTypeSelect,
			QueryTypeExplain,
			QueryTypeShow,
		},
		MaxQueryLength:  10000,
		AllowSubqueries: true,
		AllowCTEs:       true,
	}
}

// Validator performs AST-based SQL validation
type Validator struct {
	config *ValidatorConfig
}

// NewValidator creates a new SQL validator with the given configuration
func NewValidator(config *ValidatorConfig) *Validator {
	if config == nil {
		config = DefaultValidatorConfig()
	}
	return &Validator{config: config}
}

// Validate parses and validates a SQL query
func (v *Validator) Validate(query string) (*QueryAnalysis, error) {
	// Check query length
	if v.config.MaxQueryLength > 0 && len(query) > v.config.MaxQueryLength {
		return nil, &ValidationError{
			Code:    ErrCodeDangerousPattern,
			Message: "query exceeds maximum allowed length",
			Details: fmt.Sprintf("max: %d, actual: %d", v.config.MaxQueryLength, len(query)),
		}
	}

	// Normalize the query - remove trailing semicolon for parsing
	trimmed := strings.TrimSpace(query)
	trimmed = strings.TrimSuffix(trimmed, ";")

	// Check for multiple statements by looking for semicolons
	if strings.Contains(trimmed, ";") {
		return nil, &ValidationError{
			Code:    ErrCodeMultipleStmts,
			Message: "multiple statements not allowed",
		}
	}

	// Parse the query using sqlparser
	stmt, err := sqlparser.Parse(trimmed)
	if err != nil {
		// If parsing fails, fall back to basic validation
		return v.basicValidation(query)
	}

	// Analyze the statement
	analysis := &QueryAnalysis{
		RawQuery: query,
	}

	if err := v.analyzeStatement(stmt, analysis); err != nil {
		return nil, err
	}

	// Validate query type
	if !v.isQueryTypeAllowed(analysis.QueryType) {
		return nil, &ValidationError{
			Code:    ErrCodeForbiddenStmt,
			Message: fmt.Sprintf("query type '%s' is not allowed", analysis.QueryType),
		}
	}

	// Validate table references
	for _, table := range analysis.Tables {
		if err := v.validateTableReference(table); err != nil {
			return nil, err
		}
	}

	// Check subquery/CTE restrictions
	if analysis.HasSubquery && !v.config.AllowSubqueries {
		return nil, &ValidationError{
			Code:    ErrCodeDangerousPattern,
			Message: "subqueries are not allowed",
		}
	}
	if analysis.HasCTE && !v.config.AllowCTEs {
		return nil, &ValidationError{
			Code:    ErrCodeDangerousPattern,
			Message: "CTEs (WITH clauses) are not allowed",
		}
	}

	return analysis, nil
}

// basicValidation performs simple string-based validation as fallback
func (v *Validator) basicValidation(query string) (*QueryAnalysis, error) {
	trimmed := strings.TrimSpace(query)
	upper := strings.ToUpper(trimmed)

	analysis := &QueryAnalysis{
		RawQuery: query,
	}

	// Determine query type from prefix
	switch {
	case strings.HasPrefix(upper, "SELECT"):
		analysis.QueryType = QueryTypeSelect
		analysis.IsReadOnly = true
	case strings.HasPrefix(upper, "EXPLAIN"):
		analysis.QueryType = QueryTypeExplain
		analysis.IsReadOnly = true
	case strings.HasPrefix(upper, "SHOW"):
		analysis.QueryType = QueryTypeShow
		analysis.IsReadOnly = true
	case strings.HasPrefix(upper, "INSERT"):
		analysis.QueryType = QueryTypeInsert
		analysis.IsReadOnly = false
	case strings.HasPrefix(upper, "UPDATE"):
		analysis.QueryType = QueryTypeUpdate
		analysis.IsReadOnly = false
	case strings.HasPrefix(upper, "DELETE"):
		analysis.QueryType = QueryTypeDelete
		analysis.IsReadOnly = false
	case strings.HasPrefix(upper, "CREATE"):
		analysis.QueryType = QueryTypeCreate
		analysis.IsReadOnly = false
	case strings.HasPrefix(upper, "DROP"):
		analysis.QueryType = QueryTypeDrop
		analysis.IsReadOnly = false
	case strings.HasPrefix(upper, "ALTER"):
		analysis.QueryType = QueryTypeAlter
		analysis.IsReadOnly = false
	case strings.HasPrefix(upper, "TRUNCATE"):
		analysis.QueryType = QueryTypeTruncate
		analysis.IsReadOnly = false
	default:
		analysis.QueryType = QueryTypeOther
		analysis.IsReadOnly = false
	}

	// Validate query type
	if !v.isQueryTypeAllowed(analysis.QueryType) {
		return nil, &ValidationError{
			Code:    ErrCodeForbiddenStmt,
			Message: fmt.Sprintf("query type '%s' is not allowed", analysis.QueryType),
		}
	}

	// Check for dangerous patterns in the query
	dangerousPatterns := []string{
		"INSERT", "UPDATE", "DELETE", "DROP", "CREATE", "ALTER", "TRUNCATE",
		"GRANT", "REVOKE", "COPY", "DO ", "CALL ", "EXECUTE ",
	}
	for _, pattern := range dangerousPatterns {
		// Don't check for patterns that match the query type
		if analysis.QueryType == QueryTypeSelect && strings.Contains(upper, pattern) {
			// Check if it's in a subquery context (allowed) or top-level (not allowed for SELECT)
			if !strings.HasPrefix(upper, pattern) {
				return nil, &ValidationError{
					Code:    ErrCodeDangerousPattern,
					Message: fmt.Sprintf("query contains forbidden keyword: %s", pattern),
				}
			}
		}
	}

	return analysis, nil
}

// analyzeStatement analyzes a parsed SQL statement
func (v *Validator) analyzeStatement(stmt sqlparser.Statement, analysis *QueryAnalysis) error {
	switch s := stmt.(type) {
	case *sqlparser.Select:
		analysis.QueryType = QueryTypeSelect
		analysis.IsReadOnly = true
		v.extractSelectInfo(s, analysis)

	case *sqlparser.Union:
		analysis.QueryType = QueryTypeSelect
		analysis.IsReadOnly = true
		// Process left and right sides of union
		if s.Left != nil {
			v.extractSelectInfo(s.Left.(*sqlparser.Select), analysis)
		}
		if s.Right != nil {
			v.extractSelectInfo(s.Right.(*sqlparser.Select), analysis)
		}

	case *sqlparser.Insert:
		analysis.QueryType = QueryTypeInsert
		analysis.IsReadOnly = false
		if s.Table.Name.String() != "" {
			analysis.Tables = append(analysis.Tables, TableReference{
				Schema: s.Table.Qualifier.String(),
				Table:  s.Table.Name.String(),
			})
		}

	case *sqlparser.Update:
		analysis.QueryType = QueryTypeUpdate
		analysis.IsReadOnly = false
		for _, expr := range s.TableExprs {
			v.extractTableExpr(expr, analysis)
		}

	case *sqlparser.Delete:
		analysis.QueryType = QueryTypeDelete
		analysis.IsReadOnly = false
		for _, expr := range s.TableExprs {
			v.extractTableExpr(expr, analysis)
		}

	case *sqlparser.DDL:
		switch s.Action {
		case "create":
			analysis.QueryType = QueryTypeCreate
		case "drop":
			analysis.QueryType = QueryTypeDrop
		case "alter":
			analysis.QueryType = QueryTypeAlter
		case "truncate":
			analysis.QueryType = QueryTypeTruncate
		default:
			analysis.QueryType = QueryTypeOther
		}
		analysis.IsReadOnly = false

	case *sqlparser.Show:
		analysis.QueryType = QueryTypeShow
		analysis.IsReadOnly = true

	case *sqlparser.OtherRead:
		analysis.QueryType = QueryTypeOther
		analysis.IsReadOnly = true

	case *sqlparser.OtherAdmin:
		analysis.QueryType = QueryTypeOther
		analysis.IsReadOnly = false

	default:
		analysis.QueryType = QueryTypeOther
		analysis.IsReadOnly = false
	}

	return nil
}

// extractSelectInfo extracts information from a SELECT statement
func (v *Validator) extractSelectInfo(sel *sqlparser.Select, analysis *QueryAnalysis) {
	if sel == nil {
		return
	}

	// Extract FROM clause tables
	for _, tableExpr := range sel.From {
		v.extractTableExpr(tableExpr, analysis)
	}

	// Check for subqueries in WHERE clause
	if sel.Where != nil {
		v.checkForSubqueries(sel.Where.Expr, analysis)
	}

	// Check for subqueries in SELECT expressions
	for _, expr := range sel.SelectExprs {
		if aliasedExpr, ok := expr.(*sqlparser.AliasedExpr); ok {
			v.checkForSubqueries(aliasedExpr.Expr, analysis)
		}
	}
}

// extractTableExpr extracts table references from a table expression
func (v *Validator) extractTableExpr(expr sqlparser.TableExpr, analysis *QueryAnalysis) {
	if expr == nil {
		return
	}

	switch t := expr.(type) {
	case *sqlparser.AliasedTableExpr:
		switch tableRef := t.Expr.(type) {
		case sqlparser.TableName:
			analysis.Tables = append(analysis.Tables, TableReference{
				Schema: tableRef.Qualifier.String(),
				Table:  tableRef.Name.String(),
				Alias:  t.As.String(),
			})
		case *sqlparser.Subquery:
			analysis.HasSubquery = true
			if sel, ok := tableRef.Select.(*sqlparser.Select); ok {
				v.extractSelectInfo(sel, analysis)
			}
		}

	case *sqlparser.JoinTableExpr:
		v.extractTableExpr(t.LeftExpr, analysis)
		v.extractTableExpr(t.RightExpr, analysis)

	case *sqlparser.ParenTableExpr:
		for _, e := range t.Exprs {
			v.extractTableExpr(e, analysis)
		}
	}
}

// checkForSubqueries recursively checks for subqueries in an expression
func (v *Validator) checkForSubqueries(expr sqlparser.Expr, analysis *QueryAnalysis) {
	if expr == nil {
		return
	}

	switch e := expr.(type) {
	case *sqlparser.Subquery:
		analysis.HasSubquery = true
		if sel, ok := e.Select.(*sqlparser.Select); ok {
			v.extractSelectInfo(sel, analysis)
		}

	case *sqlparser.AndExpr:
		v.checkForSubqueries(e.Left, analysis)
		v.checkForSubqueries(e.Right, analysis)

	case *sqlparser.OrExpr:
		v.checkForSubqueries(e.Left, analysis)
		v.checkForSubqueries(e.Right, analysis)

	case *sqlparser.NotExpr:
		v.checkForSubqueries(e.Expr, analysis)

	case *sqlparser.ComparisonExpr:
		v.checkForSubqueries(e.Left, analysis)
		v.checkForSubqueries(e.Right, analysis)

	case *sqlparser.RangeCond:
		v.checkForSubqueries(e.Left, analysis)
		v.checkForSubqueries(e.From, analysis)
		v.checkForSubqueries(e.To, analysis)

	case *sqlparser.IsExpr:
		v.checkForSubqueries(e.Expr, analysis)

	case *sqlparser.ExistsExpr:
		analysis.HasSubquery = true
		if sel, ok := e.Subquery.Select.(*sqlparser.Select); ok {
			v.extractSelectInfo(sel, analysis)
		}

	case *sqlparser.FuncExpr:
		for _, arg := range e.Exprs {
			if aliased, ok := arg.(*sqlparser.AliasedExpr); ok {
				v.checkForSubqueries(aliased.Expr, analysis)
			}
		}

	case *sqlparser.ParenExpr:
		v.checkForSubqueries(e.Expr, analysis)
	}
}

// isQueryTypeAllowed checks if a query type is in the allowed list
func (v *Validator) isQueryTypeAllowed(qt QueryType) bool {
	if len(v.config.AllowedQueryTypes) == 0 {
		return true
	}
	for _, allowed := range v.config.AllowedQueryTypes {
		if allowed == qt {
			return true
		}
	}
	return false
}

// validateTableReference validates a table reference against ACL rules
func (v *Validator) validateTableReference(table TableReference) error {
	schema := table.Schema
	if schema == "" {
		schema = "public"
	}

	// Check denied schemas
	for _, denied := range v.config.DeniedSchemas {
		if strings.EqualFold(schema, denied) {
			return &ValidationError{
				Code:    ErrCodeForbiddenSchema,
				Message: fmt.Sprintf("access to schema '%s' is denied", schema),
			}
		}
	}

	// Check allowed schemas (if specified)
	if len(v.config.AllowedSchemas) > 0 {
		allowed := false
		for _, allowedSchema := range v.config.AllowedSchemas {
			if strings.EqualFold(schema, allowedSchema) {
				allowed = true
				break
			}
		}
		if !allowed {
			return &ValidationError{
				Code:    ErrCodeForbiddenSchema,
				Message: fmt.Sprintf("access to schema '%s' is not allowed", schema),
			}
		}
	}

	return nil
}

// ValidateReadOnly validates that a query is safe for read-only execution
func (v *Validator) ValidateReadOnly(query string) (*QueryAnalysis, error) {
	analysis, err := v.Validate(query)
	if err != nil {
		return nil, err
	}

	if !analysis.IsReadOnly {
		return nil, &ValidationError{
			Code:    ErrCodeWriteNotAllowed,
			Message: fmt.Sprintf("write operations not allowed (found %s)", analysis.QueryType),
		}
	}

	return analysis, nil
}
