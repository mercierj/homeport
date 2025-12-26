package security

import (
	"testing"
)

func TestACLManager_CheckAccess(t *testing.T) {
	manager := NewACLManager(nil)

	tests := []struct {
		name       string
		schema     string
		table      string
		permission Permission
		wantErr    bool
	}{
		{
			name:       "public schema allowed",
			schema:     "public",
			table:      "users",
			permission: PermissionRead,
			wantErr:    false,
		},
		{
			name:       "empty schema defaults to public",
			schema:     "",
			table:      "users",
			permission: PermissionRead,
			wantErr:    false,
		},
		{
			name:       "pg_catalog denied",
			schema:     "pg_catalog",
			table:      "pg_tables",
			permission: PermissionRead,
			wantErr:    true,
		},
		{
			name:       "information_schema denied",
			schema:     "information_schema",
			table:      "tables",
			permission: PermissionRead,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.CheckAccess(tt.schema, tt.table, tt.permission)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckAccess() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestACLManager_CheckTableReferences(t *testing.T) {
	manager := NewACLManager(nil)

	tests := []struct {
		name       string
		tables     []TableReference
		permission Permission
		wantErr    bool
	}{
		{
			name: "single allowed table",
			tables: []TableReference{
				{Schema: "public", Table: "users"},
			},
			permission: PermissionRead,
			wantErr:    false,
		},
		{
			name: "multiple allowed tables",
			tables: []TableReference{
				{Schema: "public", Table: "users"},
				{Schema: "public", Table: "orders"},
			},
			permission: PermissionRead,
			wantErr:    false,
		},
		{
			name: "one denied table fails all",
			tables: []TableReference{
				{Schema: "public", Table: "users"},
				{Schema: "pg_catalog", Table: "pg_tables"},
			},
			permission: PermissionRead,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := manager.CheckTableReferences(tt.tables, tt.permission)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckTableReferences() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestACLManager_CustomRules(t *testing.T) {
	config := &ACLConfig{
		DefaultPermission:  PermissionRead,
		DefaultDenySchemas: []string{"pg_catalog"},
		Rules: []ACLRule{
			{
				ID:          "deny-sensitive",
				Description: "Deny access to sensitive tables",
				ACL: TableACL{
					Schema:     "public",
					Table:      "passwords",
					Permission: PermissionNone,
				},
				Priority: 100,
				Enabled:  true,
			},
		},
	}
	manager := NewACLManager(config)

	// Normal table should be accessible
	err := manager.CheckAccess("public", "users", PermissionRead)
	if err != nil {
		t.Errorf("Expected access to users table, got error: %v", err)
	}

	// Sensitive table should be denied
	err = manager.CheckAccess("public", "passwords", PermissionRead)
	if err == nil {
		t.Error("Expected access to passwords table to be denied")
	}
}

func TestACLManager_AddRemoveRule(t *testing.T) {
	manager := NewACLManager(nil)

	// Add a rule
	manager.AddRule(ACLRule{
		ID:          "test-rule",
		Description: "Test rule",
		ACL: TableACL{
			Schema:     "public",
			Table:      "test",
			Permission: PermissionNone,
		},
		Priority: 1000,
		Enabled:  true,
	})

	// Check that the rule is applied
	err := manager.CheckAccess("public", "test", PermissionRead)
	if err == nil {
		t.Error("Expected rule to deny access")
	}

	// Remove the rule
	removed := manager.RemoveRule("test-rule")
	if !removed {
		t.Error("Expected rule to be removed")
	}

	// Check that access is allowed again
	err = manager.CheckAccess("public", "test", PermissionRead)
	if err != nil {
		t.Errorf("Expected access after rule removal, got error: %v", err)
	}
}

func TestPermission_Has(t *testing.T) {
	tests := []struct {
		name     string
		perm     Permission
		check    Permission
		expected bool
	}{
		{"read has read", PermissionRead, PermissionRead, true},
		{"read does not have write", PermissionRead, PermissionWrite, false},
		{"all has read", PermissionAll, PermissionRead, true},
		{"all has write", PermissionAll, PermissionWrite, true},
		{"all has delete", PermissionAll, PermissionDelete, true},
		{"none has nothing", PermissionNone, PermissionRead, false},
		{"read|write has read", PermissionRead | PermissionWrite, PermissionRead, true},
		{"read|write has write", PermissionRead | PermissionWrite, PermissionWrite, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.perm.Has(tt.check)
			if result != tt.expected {
				t.Errorf("Permission.Has() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		pattern  string
		s        string
		expected bool
	}{
		{"*", "anything", true},
		{"user*", "users", true},
		{"user*", "user_data", true},
		{"user*", "data", false},
		{"*_data", "user_data", true},
		{"*_data", "users", false},
		{"us?rs", "users", true},
		{"us?rs", "usars", true},
		{"us?rs", "usfrs", true},
		{"us?rs", "useers", false},
		{"public", "public", true},
		{"public", "private", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern+"_"+tt.s, func(t *testing.T) {
			result := matchWildcard(tt.pattern, tt.s)
			if result != tt.expected {
				t.Errorf("matchWildcard(%q, %q) = %v, want %v", tt.pattern, tt.s, result, tt.expected)
			}
		})
	}
}
