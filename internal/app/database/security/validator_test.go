package security

import (
	"testing"
)

func TestValidator_Validate_SelectQuery(t *testing.T) {
	v := NewValidator(nil)

	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:    "simple SELECT",
			query:   "SELECT * FROM users",
			wantErr: false,
		},
		{
			name:    "SELECT with WHERE",
			query:   "SELECT id, name FROM users WHERE id = 1",
			wantErr: false,
		},
		{
			name:    "SELECT with JOIN",
			query:   "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id",
			wantErr: false,
		},
		{
			name:    "SELECT with subquery",
			query:   "SELECT * FROM users WHERE id IN (SELECT user_id FROM orders)",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis, err := v.Validate(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && analysis == nil {
				t.Error("Validate() returned nil analysis")
				return
			}
			if !tt.wantErr && analysis.QueryType != QueryTypeSelect {
				t.Errorf("Validate() QueryType = %v, want %v", analysis.QueryType, QueryTypeSelect)
			}
		})
	}
}

func TestValidator_Validate_BlockedQueries(t *testing.T) {
	v := NewValidator(nil)

	tests := []struct {
		name     string
		query    string
		wantErr  bool
		errCode  string
	}{
		{
			name:    "INSERT query blocked",
			query:   "INSERT INTO users (name) VALUES ('test')",
			wantErr: true,
			errCode: ErrCodeForbiddenStmt,
		},
		{
			name:    "UPDATE query blocked",
			query:   "UPDATE users SET name = 'test' WHERE id = 1",
			wantErr: true,
			errCode: ErrCodeForbiddenStmt,
		},
		{
			name:    "DELETE query blocked",
			query:   "DELETE FROM users WHERE id = 1",
			wantErr: true,
			errCode: ErrCodeForbiddenStmt,
		},
		{
			name:    "DROP query blocked",
			query:   "DROP TABLE users",
			wantErr: true,
			errCode: ErrCodeForbiddenStmt,
		},
		{
			name:    "CREATE query blocked",
			query:   "CREATE TABLE test (id INT)",
			wantErr: true,
			errCode: ErrCodeForbiddenStmt,
		},
		{
			name:    "multiple statements blocked",
			query:   "SELECT * FROM users; DELETE FROM users",
			wantErr: true,
			errCode: ErrCodeMultipleStmts,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.Validate(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err != nil {
				if ve, ok := err.(*ValidationError); ok {
					if ve.Code != tt.errCode {
						t.Errorf("Validate() error code = %v, want %v", ve.Code, tt.errCode)
					}
				}
			}
		})
	}
}

func TestValidator_Validate_SchemaRestrictions(t *testing.T) {
	config := &ValidatorConfig{
		AllowedSchemas: []string{"public"},
		DeniedSchemas:  []string{"pg_catalog", "information_schema"},
		AllowedQueryTypes: []QueryType{
			QueryTypeSelect,
		},
		MaxQueryLength:  10000,
		AllowSubqueries: true,
		AllowCTEs:       true,
	}
	v := NewValidator(config)

	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:    "public schema allowed",
			query:   "SELECT * FROM public.users",
			wantErr: false,
		},
		{
			name:    "implicit public schema allowed",
			query:   "SELECT * FROM users",
			wantErr: false,
		},
		{
			name:    "pg_catalog schema denied",
			query:   "SELECT * FROM pg_catalog.pg_tables",
			wantErr: true,
		},
		{
			name:    "information_schema denied",
			query:   "SELECT * FROM information_schema.tables",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.Validate(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidator_ValidateReadOnly(t *testing.T) {
	config := &ValidatorConfig{
		AllowedQueryTypes: []QueryType{
			QueryTypeSelect,
			QueryTypeInsert,
			QueryTypeUpdate,
		},
		MaxQueryLength:  10000,
		AllowSubqueries: true,
		AllowCTEs:       true,
	}
	v := NewValidator(config)

	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{
			name:    "SELECT is read-only",
			query:   "SELECT * FROM users",
			wantErr: false,
		},
		{
			name:    "INSERT is not read-only",
			query:   "INSERT INTO users (name) VALUES ('test')",
			wantErr: true,
		},
		{
			name:    "UPDATE is not read-only",
			query:   "UPDATE users SET name = 'test'",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.ValidateReadOnly(tt.query)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateReadOnly() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidator_Validate_MaxQueryLength(t *testing.T) {
	config := &ValidatorConfig{
		MaxQueryLength:    50,
		AllowedQueryTypes: []QueryType{QueryTypeSelect},
	}
	v := NewValidator(config)

	shortQuery := "SELECT * FROM users"
	longQuery := "SELECT very_long_column_name_that_exceeds_the_max_length FROM users"

	if _, err := v.Validate(shortQuery); err != nil {
		t.Errorf("Validate() short query should succeed, got error: %v", err)
	}

	if _, err := v.Validate(longQuery); err == nil {
		t.Error("Validate() long query should fail")
	}
}

func TestValidator_TableExtraction(t *testing.T) {
	v := NewValidator(nil)

	query := "SELECT u.name, o.total FROM users u JOIN orders o ON u.id = o.user_id"
	analysis, err := v.Validate(query)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if len(analysis.Tables) < 2 {
		t.Errorf("Expected at least 2 tables, got %d", len(analysis.Tables))
	}

	tableNames := make(map[string]bool)
	for _, table := range analysis.Tables {
		tableNames[table.Table] = true
	}

	if !tableNames["users"] {
		t.Error("Expected 'users' table in analysis")
	}
	if !tableNames["orders"] {
		t.Error("Expected 'orders' table in analysis")
	}
}

func TestValidator_SubqueryDetection(t *testing.T) {
	v := NewValidator(&ValidatorConfig{
		AllowSubqueries:   true,
		AllowedQueryTypes: []QueryType{QueryTypeSelect},
	})

	query := "SELECT * FROM users WHERE id IN (SELECT user_id FROM orders WHERE total > 100)"
	analysis, err := v.Validate(query)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if !analysis.HasSubquery {
		t.Error("Expected HasSubquery to be true")
	}
}

func TestValidator_SubqueryBlocked(t *testing.T) {
	v := NewValidator(&ValidatorConfig{
		AllowSubqueries:   false,
		AllowedQueryTypes: []QueryType{QueryTypeSelect},
	})

	query := "SELECT * FROM users WHERE id IN (SELECT user_id FROM orders)"
	_, err := v.Validate(query)
	if err == nil {
		t.Error("Expected subquery to be blocked")
	}
}
