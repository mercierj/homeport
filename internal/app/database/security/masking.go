package security

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// MaskingStrategy defines how a column value should be masked
type MaskingStrategy int

const (
	// MaskingNone leaves the value unchanged
	MaskingNone MaskingStrategy = iota
	// MaskingFull replaces the entire value with asterisks
	MaskingFull
	// MaskingPartial masks part of the value (e.g., email: j***@example.com)
	MaskingPartial
	// MaskingHash replaces the value with a hash indicator
	MaskingHash
	// MaskingNull replaces the value with null
	MaskingNull
	// MaskingRedact replaces with [REDACTED]
	MaskingRedact
	// MaskingCustom uses a custom masking function
	MaskingCustom
)

// String returns a string representation of MaskingStrategy
func (s MaskingStrategy) String() string {
	switch s {
	case MaskingNone:
		return "NONE"
	case MaskingFull:
		return "FULL"
	case MaskingPartial:
		return "PARTIAL"
	case MaskingHash:
		return "HASH"
	case MaskingNull:
		return "NULL"
	case MaskingRedact:
		return "REDACT"
	case MaskingCustom:
		return "CUSTOM"
	default:
		return "UNKNOWN"
	}
}

// ColumnMaskingRule defines masking rules for a specific column
type ColumnMaskingRule struct {
	Schema      string          // Schema name (empty = match all)
	Table       string          // Table name (empty = match all, supports wildcards)
	Column      string          // Column name (supports wildcards)
	Strategy    MaskingStrategy // How to mask the value
	CustomMask  func(any) any   // Custom masking function (for MaskingCustom)
	Description string          // Description of why this column is masked
	Enabled     bool            // Whether this rule is active
}

// MaskingConfig holds the column masking configuration
type MaskingConfig struct {
	// Enabled controls whether masking is active
	Enabled bool
	// DefaultRules are built-in rules for common sensitive columns
	DefaultRules []ColumnMaskingRule
	// CustomRules are user-defined masking rules
	CustomRules []ColumnMaskingRule
}

// DefaultMaskingConfig returns a secure default masking configuration
func DefaultMaskingConfig() *MaskingConfig {
	return &MaskingConfig{
		Enabled: true,
		DefaultRules: []ColumnMaskingRule{
			// Password-related columns
			{
				Column:      "password",
				Strategy:    MaskingRedact,
				Description: "Password field",
				Enabled:     true,
			},
			{
				Column:      "password_hash",
				Strategy:    MaskingRedact,
				Description: "Password hash field",
				Enabled:     true,
			},
			{
				Column:      "*_password",
				Strategy:    MaskingRedact,
				Description: "Password-related fields",
				Enabled:     true,
			},
			{
				Column:      "*_secret",
				Strategy:    MaskingRedact,
				Description: "Secret fields",
				Enabled:     true,
			},
			// API keys and tokens
			{
				Column:      "api_key",
				Strategy:    MaskingPartial,
				Description: "API key field",
				Enabled:     true,
			},
			{
				Column:      "*_token",
				Strategy:    MaskingPartial,
				Description: "Token fields",
				Enabled:     true,
			},
			{
				Column:      "access_token",
				Strategy:    MaskingPartial,
				Description: "Access token field",
				Enabled:     true,
			},
			{
				Column:      "refresh_token",
				Strategy:    MaskingPartial,
				Description: "Refresh token field",
				Enabled:     true,
			},
			// Credit card numbers
			{
				Column:      "credit_card*",
				Strategy:    MaskingPartial,
				Description: "Credit card fields",
				Enabled:     true,
			},
			{
				Column:      "card_number",
				Strategy:    MaskingPartial,
				Description: "Card number field",
				Enabled:     true,
			},
			{
				Column:      "cvv",
				Strategy:    MaskingFull,
				Description: "CVV field",
				Enabled:     true,
			},
			// Social security numbers
			{
				Column:      "ssn",
				Strategy:    MaskingPartial,
				Description: "Social security number",
				Enabled:     true,
			},
			{
				Column:      "social_security*",
				Strategy:    MaskingPartial,
				Description: "Social security fields",
				Enabled:     true,
			},
			// Private keys
			{
				Column:      "*_key",
				Strategy:    MaskingRedact,
				Description: "Key fields (potential private keys)",
				Enabled:     true,
			},
			{
				Column:      "private_key",
				Strategy:    MaskingRedact,
				Description: "Private key field",
				Enabled:     true,
			},
		},
	}
}

// MaskingManager manages column-level data masking
type MaskingManager struct {
	config *MaskingConfig
	mu     sync.RWMutex
	// Compiled regex patterns for wildcard matching
	compiledPatterns map[string]*regexp.Regexp
}

// NewMaskingManager creates a new masking manager
func NewMaskingManager(config *MaskingConfig) *MaskingManager {
	if config == nil {
		config = DefaultMaskingConfig()
	}
	return &MaskingManager{
		config:           config,
		compiledPatterns: make(map[string]*regexp.Regexp),
	}
}

// ShouldMask checks if a column should be masked
func (m *MaskingManager) ShouldMask(schema, table, column string) (bool, MaskingStrategy) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.config.Enabled {
		return false, MaskingNone
	}

	// Check custom rules first (they take precedence)
	for _, rule := range m.config.CustomRules {
		if rule.Enabled && m.ruleMatches(rule, schema, table, column) {
			return true, rule.Strategy
		}
	}

	// Check default rules
	for _, rule := range m.config.DefaultRules {
		if rule.Enabled && m.ruleMatches(rule, schema, table, column) {
			return true, rule.Strategy
		}
	}

	return false, MaskingNone
}

// MaskValue masks a value according to the given strategy
func (m *MaskingManager) MaskValue(value any, strategy MaskingStrategy, rule *ColumnMaskingRule) any {
	if value == nil {
		return nil
	}

	switch strategy {
	case MaskingNone:
		return value

	case MaskingFull:
		return m.maskFull(value)

	case MaskingPartial:
		return m.maskPartial(value)

	case MaskingHash:
		return "[HASH]"

	case MaskingNull:
		return nil

	case MaskingRedact:
		return "[REDACTED]"

	case MaskingCustom:
		if rule != nil && rule.CustomMask != nil {
			return rule.CustomMask(value)
		}
		return "[REDACTED]"

	default:
		return "[MASKED]"
	}
}

// MaskRow masks sensitive columns in a row
func (m *MaskingManager) MaskRow(schema, table string, columns []string, row []any) []any {
	if !m.config.Enabled || len(columns) != len(row) {
		return row
	}

	masked := make([]any, len(row))
	copy(masked, row)

	for i, col := range columns {
		shouldMask, strategy := m.ShouldMask(schema, table, col)
		if shouldMask {
			masked[i] = m.MaskValue(row[i], strategy, nil)
		}
	}

	return masked
}

// MaskRows masks sensitive columns in multiple rows
func (m *MaskingManager) MaskRows(schema, table string, columns []string, rows [][]any) [][]any {
	if !m.config.Enabled {
		return rows
	}

	masked := make([][]any, len(rows))
	for i, row := range rows {
		masked[i] = m.MaskRow(schema, table, columns, row)
	}
	return masked
}

// AddRule adds a custom masking rule
func (m *MaskingManager) AddRule(rule ColumnMaskingRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.CustomRules = append(m.config.CustomRules, rule)
}

// RemoveRule removes a custom masking rule by column pattern
func (m *MaskingManager) RemoveRule(column string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, rule := range m.config.CustomRules {
		if rule.Column == column {
			m.config.CustomRules = append(m.config.CustomRules[:i], m.config.CustomRules[i+1:]...)
			return true
		}
	}
	return false
}

// SetEnabled enables or disables masking
func (m *MaskingManager) SetEnabled(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.Enabled = enabled
}

// ruleMatches checks if a masking rule matches a column
func (m *MaskingManager) ruleMatches(rule ColumnMaskingRule, schema, table, column string) bool {
	// Match schema (if specified)
	if rule.Schema != "" && !m.matchPattern(rule.Schema, schema) {
		return false
	}

	// Match table (if specified)
	if rule.Table != "" && !m.matchPattern(rule.Table, table) {
		return false
	}

	// Match column (required)
	return m.matchPattern(rule.Column, column)
}

// matchPattern matches a pattern (with wildcards) against a string
func (m *MaskingManager) matchPattern(pattern, s string) bool {
	pattern = strings.ToLower(pattern)
	s = strings.ToLower(s)

	// Check if we have a compiled regex for this pattern
	if re, ok := m.compiledPatterns[pattern]; ok {
		return re.MatchString(s)
	}

	// Convert wildcard pattern to regex
	regexPattern := "^" + regexp.QuoteMeta(pattern) + "$"
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, ".*")
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, ".")

	re, err := regexp.Compile(regexPattern)
	if err != nil {
		// Fallback to simple comparison
		return pattern == s
	}

	m.compiledPatterns[pattern] = re
	return re.MatchString(s)
}

// maskFull replaces the entire value with asterisks
func (m *MaskingManager) maskFull(value any) string {
	str := fmt.Sprintf("%v", value)
	if len(str) == 0 {
		return ""
	}
	return strings.Repeat("*", min(len(str), 8))
}

// maskPartial masks part of the value, keeping some characters visible
func (m *MaskingManager) maskPartial(value any) string {
	str := fmt.Sprintf("%v", value)
	length := len(str)

	if length <= 4 {
		return strings.Repeat("*", length)
	}

	// Check if it looks like an email
	if strings.Contains(str, "@") {
		return m.maskEmail(str)
	}

	// Check if it looks like a credit card (all digits, length 13-19)
	if m.looksLikeCreditCard(str) {
		return m.maskCreditCard(str)
	}

	// Default: show first 2 and last 2 characters
	if length <= 6 {
		return str[:1] + strings.Repeat("*", length-2) + str[length-1:]
	}
	return str[:2] + strings.Repeat("*", length-4) + str[length-2:]
}

// maskEmail masks an email address, keeping domain and first character
func (m *MaskingManager) maskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return m.maskFull(email)
	}

	localPart := parts[0]
	domain := parts[1]

	if len(localPart) <= 1 {
		return localPart + "@" + domain
	}

	masked := string(localPart[0]) + strings.Repeat("*", min(len(localPart)-1, 5)) + "@" + domain
	return masked
}

// maskCreditCard masks a credit card number, keeping last 4 digits
func (m *MaskingManager) maskCreditCard(number string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, number)

	if len(digits) < 4 {
		return strings.Repeat("*", len(number))
	}

	return strings.Repeat("*", len(digits)-4) + digits[len(digits)-4:]
}

// looksLikeCreditCard checks if a string might be a credit card number
func (m *MaskingManager) looksLikeCreditCard(s string) bool {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)

	return len(digits) >= 13 && len(digits) <= 19
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
