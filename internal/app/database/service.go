package database

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/app/database/security"
	"github.com/homeport/homeport/internal/pkg/logger"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	Host           string
	Port           int
	User           string
	Password       string
	Database       string
	SSLMode        string
	ConnectTimeout int // Connection timeout in seconds (default: 10)
}

// ConnectionString returns the connection string with credentials.
// WARNING: Do not log this - use SanitizedConnectionString() for logging.
func (c Config) ConnectionString() string {
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = "require" // Secure default
	}
	timeout := c.ConnectTimeout
	if timeout <= 0 {
		timeout = 10 // Default 10 seconds
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s&connect_timeout=%d",
		c.User, c.Password, c.Host, c.Port, c.Database, sslMode, timeout,
	)
}

// SanitizedConnectionString returns a connection string safe for logging.
func (c Config) SanitizedConnectionString() string {
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}
	timeout := c.ConnectTimeout
	if timeout <= 0 {
		timeout = 10
	}
	return fmt.Sprintf(
		"postgres://%s:****@%s:%d/%s?sslmode=%s&connect_timeout=%d",
		c.User, c.Host, c.Port, c.Database, sslMode, timeout,
	)
}

// SecurityConfig holds security-related configuration
type SecurityConfig struct {
	// AllowedSchemas limits which schemas can be queried (empty = public only)
	AllowedSchemas []string
	// EnableMasking enables column-level masking
	EnableMasking bool
	// EnableAudit enables query audit logging
	EnableAudit bool
	// CustomMaskingRules are additional column masking rules
	CustomMaskingRules []security.ColumnMaskingRule
	// CustomACLRules are additional table access rules
	CustomACLRules []security.ACLRule
}

// DefaultSecurityConfig returns secure defaults
func DefaultSecurityConfig() *SecurityConfig {
	return &SecurityConfig{
		AllowedSchemas: []string{"public"},
		EnableMasking:  true,
		EnableAudit:    true,
	}
}

type Service struct {
	pool     *pgxpool.Pool
	security *security.Manager
}

// NewService creates a new database service with default security settings
func NewService(ctx context.Context, cfg Config) (*Service, error) {
	return NewServiceWithSecurity(ctx, cfg, nil)
}

// NewServiceWithSecurity creates a new database service with custom security settings
func NewServiceWithSecurity(ctx context.Context, cfg Config, secCfg *SecurityConfig) (*Service, error) {
	pool, err := pgxpool.New(ctx, cfg.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Initialize security manager
	if secCfg == nil {
		secCfg = DefaultSecurityConfig()
	}

	secMgrConfig := &security.ManagerConfig{
		Validator: &security.ValidatorConfig{
			AllowedSchemas: secCfg.AllowedSchemas,
			DeniedSchemas: []string{
				"pg_catalog",
				"information_schema",
				"pg_toast",
			},
			AllowedQueryTypes: []security.QueryType{
				security.QueryTypeSelect,
				security.QueryTypeExplain,
				security.QueryTypeShow,
			},
			MaxQueryLength:  10000,
			AllowSubqueries: true,
			AllowCTEs:       true,
		},
		ACL: security.DefaultACLConfig(),
		Masking: &security.MaskingConfig{
			Enabled:      secCfg.EnableMasking,
			DefaultRules: security.DefaultMaskingConfig().DefaultRules,
			CustomRules:  secCfg.CustomMaskingRules,
		},
		Audit: &security.AuditConfig{
			Enabled:              secCfg.EnableAudit,
			LogQueries:           true,
			LogQueryParams:       false,
			MaxQueryLength:       1000,
			SampleRate:           1.0,
			IncludeSelectQueries: true,
			IncludeSlowQueries:   true,
			SlowQueryThreshold:   time.Second,
		},
		Logger: logger.Default(),
	}

	// Add custom ACL rules
	for _, rule := range secCfg.CustomACLRules {
		secMgrConfig.ACL.Rules = append(secMgrConfig.ACL.Rules, rule)
	}

	secMgr := security.NewManager(secMgrConfig)

	return &Service{
		pool:     pool,
		security: secMgr,
	}, nil
}

func (s *Service) Close() {
	s.pool.Close()
}

type DatabaseInfo struct {
	Name  string `json:"name"`
	Owner string `json:"owner"`
	Size  string `json:"size"`
}

func (s *Service) ListDatabases(ctx context.Context) ([]DatabaseInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT datname, pg_catalog.pg_get_userbyid(datdba) as owner,
		       pg_catalog.pg_size_pretty(pg_catalog.pg_database_size(datname)) as size
		FROM pg_catalog.pg_database
		WHERE datistemplate = false
		ORDER BY datname
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var databases []DatabaseInfo
	for rows.Next() {
		var db DatabaseInfo
		if err := rows.Scan(&db.Name, &db.Owner, &db.Size); err != nil {
			return nil, err
		}
		databases = append(databases, db)
	}
	return databases, nil
}

type TableInfo struct {
	Schema   string `json:"schema"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Owner    string `json:"owner"`
	RowCount int64  `json:"row_count"`
	Size     string `json:"size"`
}

func (s *Service) ListTables(ctx context.Context, schema string) ([]TableInfo, error) {
	if schema == "" {
		schema = "public"
	}

	// Check if schema is allowed before listing tables
	if err := s.security.CheckAccess(schema, "", security.PermissionRead); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT schemaname, tablename, tableowner,
		       pg_catalog.pg_size_pretty(pg_catalog.pg_table_size(schemaname || '.' || tablename)) as size
		FROM pg_catalog.pg_tables
		WHERE schemaname = $1
		ORDER BY tablename
	`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		t.Type = "table"
		if err := rows.Scan(&t.Schema, &t.Name, &t.Owner, &t.Size); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, nil
}

type ColumnInfo struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Nullable   bool    `json:"nullable"`
	Default    *string `json:"default,omitempty"`
	PrimaryKey bool    `json:"primary_key"`
}

func (s *Service) GetTableSchema(ctx context.Context, schema, table string) ([]ColumnInfo, error) {
	// Check ACL before returning schema information
	if err := s.security.CheckAccess(schema, table, security.PermissionRead); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		var nullable string
		if err := rows.Scan(&c.Name, &c.Type, &nullable, &c.Default); err != nil {
			return nil, err
		}
		c.Nullable = nullable == "YES"
		columns = append(columns, c)
	}
	return columns, nil
}

type QueryResult struct {
	Columns       []string `json:"columns"`
	Rows          [][]any  `json:"rows"`
	RowCount      int      `json:"row_count"`
	Duration      string   `json:"duration"`
	MaskedColumns []string `json:"masked_columns,omitempty"`
}

// ExecuteQuery executes a SQL query with full security validation, ACL checking,
// column masking, and audit logging
func (s *Service) ExecuteQuery(ctx context.Context, query string, readOnly bool) (*QueryResult, error) {
	start := time.Now()

	// Perform AST-based validation and ACL checking
	analysis, auditComplete, err := s.security.SecureQuery(ctx, query, readOnly)
	if err != nil {
		return nil, err
	}

	// Use read-only transaction for additional safety
	accessMode := pgx.ReadWrite
	if readOnly || (analysis != nil && analysis.IsReadOnly) {
		accessMode = pgx.ReadOnly
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{
		AccessMode: accessMode,
	})
	if err != nil {
		if auditComplete != nil {
			auditComplete(0, err)
		}
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, query)
	if err != nil {
		if auditComplete != nil {
			auditComplete(0, err)
		}
		return nil, err
	}
	defer rows.Close()

	// Get column names
	fields := rows.FieldDescriptions()
	columns := make([]string, len(fields))
	for i, f := range fields {
		columns[i] = string(f.Name)
	}

	// Collect rows
	var resultRows [][]any
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			if auditComplete != nil {
				auditComplete(len(resultRows), err)
			}
			return nil, err
		}
		resultRows = append(resultRows, values)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		if auditComplete != nil {
			auditComplete(len(resultRows), err)
		}
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Get the primary table for masking (use first table in analysis)
	schema := "public"
	table := ""
	if analysis != nil && len(analysis.Tables) > 0 {
		schema = analysis.Tables[0].Schema
		if schema == "" {
			schema = "public"
		}
		table = analysis.Tables[0].Table
	}

	// Apply column masking
	maskedColumns := s.security.GetMaskedColumns(schema, table, columns)
	if len(maskedColumns) > 0 {
		resultRows = s.security.MaskRows(schema, table, columns, resultRows)
	}

	// Complete audit logging
	if auditComplete != nil {
		auditComplete(len(resultRows), nil)
	}

	duration := time.Since(start)

	return &QueryResult{
		Columns:       columns,
		Rows:          resultRows,
		RowCount:      len(resultRows),
		Duration:      duration.String(),
		MaskedColumns: maskedColumns,
	}, nil
}

func (s *Service) GetTableData(ctx context.Context, schema, table string, limit, offset int) (*QueryResult, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	// Check ACL before proceeding
	if err := s.security.CheckAccess(schema, table, security.PermissionRead); err != nil {
		return nil, err
	}

	// Use pgx.Identifier for safe quoting
	schemaIdent := pgx.Identifier{schema}.Sanitize()
	tableIdent := pgx.Identifier{table}.Sanitize()
	query := fmt.Sprintf("SELECT * FROM %s.%s LIMIT %d OFFSET %d", schemaIdent, tableIdent, limit, offset)
	return s.ExecuteQuery(ctx, query, true)
}

// Security returns the security manager for advanced configuration
func (s *Service) Security() *security.Manager {
	return s.security
}

// AddACLRule adds a table-level access control rule
func (s *Service) AddACLRule(rule security.ACLRule) {
	s.security.AddACLRule(rule)
}

// AddMaskingRule adds a column masking rule
func (s *Service) AddMaskingRule(rule security.ColumnMaskingRule) {
	s.security.AddMaskingRule(rule)
}

// IsSchemaAllowed checks if a schema is allowed for querying
func (s *Service) IsSchemaAllowed(schema string) bool {
	err := s.security.CheckAccess(schema, "", security.PermissionRead)
	return err == nil
}
