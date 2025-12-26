package security

import (
	"testing"
)

func TestMaskingManager_ShouldMask(t *testing.T) {
	manager := NewMaskingManager(nil)

	tests := []struct {
		name           string
		column         string
		expectMask     bool
		expectStrategy MaskingStrategy
	}{
		{
			name:           "password column masked",
			column:         "password",
			expectMask:     true,
			expectStrategy: MaskingRedact,
		},
		{
			name:           "password_hash column masked",
			column:         "password_hash",
			expectMask:     true,
			expectStrategy: MaskingRedact,
		},
		{
			name:           "api_key column masked",
			column:         "api_key",
			expectMask:     true,
			expectStrategy: MaskingPartial,
		},
		{
			name:           "access_token column masked",
			column:         "access_token",
			expectMask:     true,
			expectStrategy: MaskingPartial,
		},
		{
			name:           "regular column not masked",
			column:         "name",
			expectMask:     false,
			expectStrategy: MaskingNone,
		},
		{
			name:           "id column not masked",
			column:         "id",
			expectMask:     false,
			expectStrategy: MaskingNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shouldMask, strategy := manager.ShouldMask("public", "users", tt.column)
			if shouldMask != tt.expectMask {
				t.Errorf("ShouldMask() = %v, want %v", shouldMask, tt.expectMask)
			}
			if strategy != tt.expectStrategy {
				t.Errorf("ShouldMask() strategy = %v, want %v", strategy, tt.expectStrategy)
			}
		})
	}
}

func TestMaskingManager_MaskValue(t *testing.T) {
	manager := NewMaskingManager(nil)

	tests := []struct {
		name     string
		value    any
		strategy MaskingStrategy
		expected any
	}{
		{
			name:     "mask none returns original",
			value:    "test",
			strategy: MaskingNone,
			expected: "test",
		},
		{
			name:     "mask null returns nil",
			value:    "test",
			strategy: MaskingNull,
			expected: nil,
		},
		{
			name:     "mask redact returns redacted",
			value:    "secret",
			strategy: MaskingRedact,
			expected: "[REDACTED]",
		},
		{
			name:     "mask hash returns hash indicator",
			value:    "password123",
			strategy: MaskingHash,
			expected: "[HASH]",
		},
		{
			name:     "mask nil value returns nil",
			value:    nil,
			strategy: MaskingFull,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.MaskValue(tt.value, tt.strategy, nil)
			if result != tt.expected {
				t.Errorf("MaskValue() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMaskingManager_MaskPartialEmail(t *testing.T) {
	manager := NewMaskingManager(nil)

	tests := []struct {
		name     string
		email    string
		expected string
	}{
		{
			name:     "normal email",
			email:    "john@example.com",
			expected: "j***@example.com", // masks min(len-1, 5) chars
		},
		{
			name:     "short local part",
			email:    "a@example.com",
			expected: "a@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := manager.MaskValue(tt.email, MaskingPartial, nil)
			if result != tt.expected {
				t.Errorf("MaskValue() email = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestMaskingManager_MaskRow(t *testing.T) {
	manager := NewMaskingManager(nil)

	columns := []string{"id", "name", "password", "email"}
	row := []any{1, "John", "secret123", "john@example.com"}

	masked := manager.MaskRow("public", "users", columns, row)

	// id should not be masked
	if masked[0] != 1 {
		t.Errorf("id should not be masked, got %v", masked[0])
	}

	// name should not be masked
	if masked[1] != "John" {
		t.Errorf("name should not be masked, got %v", masked[1])
	}

	// password should be masked
	if masked[2] == "secret123" {
		t.Error("password should be masked")
	}
}

func TestMaskingManager_MaskRows(t *testing.T) {
	manager := NewMaskingManager(nil)

	columns := []string{"id", "password"}
	rows := [][]any{
		{1, "secret1"},
		{2, "secret2"},
		{3, "secret3"},
	}

	masked := manager.MaskRows("public", "users", columns, rows)

	if len(masked) != 3 {
		t.Errorf("Expected 3 rows, got %d", len(masked))
	}

	for i, row := range masked {
		if row[0] != i+1 {
			t.Errorf("Row %d id should not be masked", i)
		}
		if row[1] == rows[i][1] {
			t.Errorf("Row %d password should be masked", i)
		}
	}
}

func TestMaskingManager_Disabled(t *testing.T) {
	config := &MaskingConfig{
		Enabled: false,
	}
	manager := NewMaskingManager(config)

	shouldMask, _ := manager.ShouldMask("public", "users", "password")
	if shouldMask {
		t.Error("Masking should be disabled")
	}

	columns := []string{"password"}
	row := []any{"secret"}
	masked := manager.MaskRow("public", "users", columns, row)

	if masked[0] != "secret" {
		t.Error("Masking should not modify row when disabled")
	}
}

func TestMaskingManager_CustomRule(t *testing.T) {
	manager := NewMaskingManager(nil)

	// Add custom rule for SSN
	manager.AddRule(ColumnMaskingRule{
		Column:      "ssn",
		Strategy:    MaskingFull,
		Description: "Social Security Number",
		Enabled:     true,
	})

	shouldMask, strategy := manager.ShouldMask("public", "users", "ssn")
	if !shouldMask {
		t.Error("SSN should be masked after adding custom rule")
	}
	if strategy != MaskingFull {
		t.Errorf("SSN strategy should be MaskingFull, got %v", strategy)
	}

	// Remove the rule
	removed := manager.RemoveRule("ssn")
	if !removed {
		t.Error("Rule should be removed")
	}
}

func TestMaskingManager_WildcardRules(t *testing.T) {
	manager := NewMaskingManager(nil)

	// Test wildcard pattern matching
	tests := []struct {
		column     string
		shouldMask bool
	}{
		{"user_password", true},    // matches *_password
		{"admin_password", true},   // matches *_password
		{"user_token", true},       // matches *_token
		{"refresh_token", true},    // matches *_token and refresh_token
		{"user_name", false},       // doesn't match any pattern
		{"email_address", false},   // doesn't match any pattern
	}

	for _, tt := range tests {
		t.Run(tt.column, func(t *testing.T) {
			shouldMask, _ := manager.ShouldMask("public", "users", tt.column)
			if shouldMask != tt.shouldMask {
				t.Errorf("ShouldMask(%q) = %v, want %v", tt.column, shouldMask, tt.shouldMask)
			}
		})
	}
}
