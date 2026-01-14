package cutover

import (
	"regexp"
	"time"
)

// HealthCheckType represents the type of health check to perform.
type HealthCheckType string

const (
	// HealthCheckHTTP performs an HTTP/HTTPS request and validates the response.
	HealthCheckHTTP HealthCheckType = "http"

	// HealthCheckTCP checks if a TCP port is open and accepting connections.
	HealthCheckTCP HealthCheckType = "tcp"

	// HealthCheckDNS validates DNS resolution returns expected values.
	HealthCheckDNS HealthCheckType = "dns"

	// HealthCheckDatabase checks database connectivity and optionally runs a query.
	HealthCheckDatabase HealthCheckType = "database"

	// HealthCheckCommand runs a shell command and checks the exit code.
	HealthCheckCommand HealthCheckType = "command"
)

// String returns the string representation of the health check type.
func (t HealthCheckType) String() string {
	return string(t)
}

// IsValid checks if the health check type is recognized.
func (t HealthCheckType) IsValid() bool {
	switch t {
	case HealthCheckHTTP, HealthCheckTCP, HealthCheckDNS,
		HealthCheckDatabase, HealthCheckCommand:
		return true
	default:
		return false
	}
}

// DisplayName returns a human-friendly display name for the health check type.
func (t HealthCheckType) DisplayName() string {
	switch t {
	case HealthCheckHTTP:
		return "HTTP"
	case HealthCheckTCP:
		return "TCP"
	case HealthCheckDNS:
		return "DNS"
	case HealthCheckDatabase:
		return "Database"
	case HealthCheckCommand:
		return "Command"
	default:
		return string(t)
	}
}

// HealthCheck defines a health check to validate service availability.
// Health checks are used both before cutover (pre-checks) to ensure the
// target is ready, and after cutover (post-checks) to verify success.
type HealthCheck struct {
	// ID is the unique identifier for this health check.
	ID string `json:"id"`

	// Name is a human-readable name for the check.
	Name string `json:"name"`

	// Description provides additional context about what this check validates.
	Description string `json:"description,omitempty"`

	// Type specifies the type of health check (http, tcp, dns, database).
	Type HealthCheckType `json:"type"`

	// Endpoint is the target address (URL, host:port, domain, connection string).
	Endpoint string `json:"endpoint"`

	// Method is the HTTP method (GET, POST, etc.). Only for HTTP checks.
	Method string `json:"method,omitempty"`

	// Headers are HTTP headers to include. Only for HTTP checks.
	Headers map[string]string `json:"headers,omitempty"`

	// Body is the request body. Only for HTTP POST/PUT checks.
	Body string `json:"body,omitempty"`

	// ExpectedStatus is the expected HTTP status code. Only for HTTP checks.
	ExpectedStatus int `json:"expected_status,omitempty"`

	// ExpectedBody is a regex or exact match for response body. Only for HTTP checks.
	ExpectedBody string `json:"expected_body,omitempty"`

	// ExpectedBodyIsRegex indicates if ExpectedBody is a regex pattern.
	ExpectedBodyIsRegex bool `json:"expected_body_is_regex,omitempty"`

	// Query is a SQL query to execute. Only for database checks.
	Query string `json:"query,omitempty"`

	// Command is a shell command to execute. Only for command checks.
	Command string `json:"command,omitempty"`

	// ExpectedOutput is the expected command output (for command checks).
	ExpectedOutput string `json:"expected_output,omitempty"`

	// ExpectedDNSValue is the expected DNS resolution value. Only for DNS checks.
	ExpectedDNSValue string `json:"expected_dns_value,omitempty"`

	// Timeout is the maximum duration for this check.
	Timeout time.Duration `json:"timeout"`

	// Retries is the number of retry attempts on failure.
	Retries int `json:"retries"`

	// RetryDelay is the delay between retry attempts.
	RetryDelay time.Duration `json:"retry_delay"`

	// Critical indicates if this check failing should abort the cutover.
	Critical bool `json:"critical"`

	// SkipTLSVerify skips TLS certificate verification (HTTP checks).
	SkipTLSVerify bool `json:"skip_tls_verify,omitempty"`

	// Enabled indicates if this check is enabled.
	Enabled bool `json:"enabled"`

	// Tags are labels for categorizing checks.
	Tags []string `json:"tags,omitempty"`

	// CreatedAt is when this check was created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when this check was last modified.
	UpdatedAt time.Time `json:"updated_at"`
}

// NewHealthCheck creates a new HealthCheck with default values.
func NewHealthCheck(id, name string, checkType HealthCheckType, endpoint string) *HealthCheck {
	now := time.Now()
	return &HealthCheck{
		ID:         id,
		Name:       name,
		Type:       checkType,
		Endpoint:   endpoint,
		Method:     "GET",
		Headers:    make(map[string]string),
		Timeout:    30 * time.Second,
		Retries:    3,
		RetryDelay: 5 * time.Second,
		Critical:   true,
		Enabled:    true,
		Tags:       make([]string, 0),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
}

// NewHTTPHealthCheck creates an HTTP health check.
func NewHTTPHealthCheck(id, name, url string, expectedStatus int) *HealthCheck {
	check := NewHealthCheck(id, name, HealthCheckHTTP, url)
	check.ExpectedStatus = expectedStatus
	return check
}

// NewTCPHealthCheck creates a TCP health check.
func NewTCPHealthCheck(id, name, hostPort string) *HealthCheck {
	return NewHealthCheck(id, name, HealthCheckTCP, hostPort)
}

// NewDNSHealthCheck creates a DNS health check.
func NewDNSHealthCheck(id, name, domain, expectedValue string) *HealthCheck {
	check := NewHealthCheck(id, name, HealthCheckDNS, domain)
	check.ExpectedDNSValue = expectedValue
	return check
}

// NewDatabaseHealthCheck creates a database health check.
func NewDatabaseHealthCheck(id, name, connectionString string) *HealthCheck {
	check := NewHealthCheck(id, name, HealthCheckDatabase, connectionString)
	check.Query = "SELECT 1"
	return check
}

// Validate checks if the health check configuration is valid.
func (h *HealthCheck) Validate() []string {
	var errors []string

	if h.ID == "" {
		errors = append(errors, "health check ID is required")
	}

	if h.Name == "" {
		errors = append(errors, "health check name is required")
	}

	if !h.Type.IsValid() {
		errors = append(errors, "invalid health check type: "+string(h.Type))
	}

	if h.Endpoint == "" {
		errors = append(errors, "endpoint is required")
	}

	if h.Timeout <= 0 {
		errors = append(errors, "timeout must be positive")
	}

	if h.Retries < 0 {
		errors = append(errors, "retries must be non-negative")
	}

	if h.RetryDelay < 0 {
		errors = append(errors, "retry delay must be non-negative")
	}

	// Validate type-specific fields
	switch h.Type {
	case HealthCheckHTTP:
		if h.ExpectedStatus <= 0 && h.ExpectedBody == "" {
			errors = append(errors, "HTTP check requires expected status or body")
		}
		if h.ExpectedBodyIsRegex && h.ExpectedBody != "" {
			if _, err := regexp.Compile(h.ExpectedBody); err != nil {
				errors = append(errors, "invalid regex pattern: "+err.Error())
			}
		}
	case HealthCheckDNS:
		if h.ExpectedDNSValue == "" {
			errors = append(errors, "DNS check requires expected value")
		}
	}

	return errors
}

// HealthCheckResult represents the result of executing a health check.
type HealthCheckResult struct {
	// CheckID links to the health check that was executed.
	CheckID string `json:"check_id"`

	// CheckName is the name of the health check.
	CheckName string `json:"check_name"`

	// Passed indicates if the check passed.
	Passed bool `json:"passed"`

	// StatusCode is the HTTP status code (for HTTP checks).
	StatusCode int `json:"status_code,omitempty"`

	// Response is the response body or output from the check.
	Response string `json:"response,omitempty"`

	// Duration is how long the check took to execute.
	Duration time.Duration `json:"duration"`

	// Attempts is the number of attempts made (including retries).
	Attempts int `json:"attempts"`

	// Error contains the error message if the check failed.
	Error string `json:"error,omitempty"`

	// Timestamp is when the check was executed.
	Timestamp time.Time `json:"timestamp"`

	// Details contains additional check-specific information.
	Details map[string]interface{} `json:"details,omitempty"`
}

// NewHealthCheckResult creates a new result for a health check.
func NewHealthCheckResult(checkID, checkName string) *HealthCheckResult {
	return &HealthCheckResult{
		CheckID:   checkID,
		CheckName: checkName,
		Attempts:  1,
		Timestamp: time.Now(),
		Details:   make(map[string]interface{}),
	}
}

// MarkPassed marks the result as passed.
func (r *HealthCheckResult) MarkPassed(duration time.Duration, response string) {
	r.Passed = true
	r.Duration = duration
	r.Response = response
}

// MarkFailed marks the result as failed.
func (r *HealthCheckResult) MarkFailed(duration time.Duration, err string) {
	r.Passed = false
	r.Duration = duration
	r.Error = err
}

// RollbackTrigger defines a condition that triggers automatic rollback.
// When a trigger's condition evaluates to true, the cutover should be
// rolled back to restore the original state.
type RollbackTrigger struct {
	// ID is the unique identifier for this trigger.
	ID string `json:"id"`

	// Name is a human-readable name for the trigger.
	Name string `json:"name,omitempty"`

	// Description explains what this trigger monitors.
	Description string `json:"description"`

	// Condition is an expression to evaluate (e.g., "error_rate > 5%").
	Condition string `json:"condition"`

	// ConditionType specifies how the condition is evaluated.
	ConditionType RollbackConditionType `json:"condition_type"`

	// Threshold is the numeric threshold for comparison conditions.
	Threshold float64 `json:"threshold,omitempty"`

	// ThresholdUnit is the unit for the threshold (%, ms, count, etc.).
	ThresholdUnit string `json:"threshold_unit,omitempty"`

	// MetricName is the metric to monitor (for metric-based triggers).
	MetricName string `json:"metric_name,omitempty"`

	// HealthCheckID links to a health check (for check-based triggers).
	HealthCheckID string `json:"health_check_id,omitempty"`

	// ConsecutiveFailures is how many consecutive failures trigger rollback.
	ConsecutiveFailures int `json:"consecutive_failures,omitempty"`

	// WindowDuration is the time window for evaluating the condition.
	WindowDuration time.Duration `json:"window_duration,omitempty"`

	// AutoRollback indicates if this trigger should automatically rollback.
	// If false, it only alerts but doesn't rollback.
	AutoRollback bool `json:"auto_rollback"`

	// Enabled indicates if this trigger is active.
	Enabled bool `json:"enabled"`

	// Priority determines the order of trigger evaluation (lower = first).
	Priority int `json:"priority"`

	// Triggered indicates if this trigger has been activated.
	Triggered bool `json:"triggered"`

	// TriggeredAt is when the trigger was activated.
	TriggeredAt *time.Time `json:"triggered_at,omitempty"`

	// TriggeredReason explains why the trigger was activated.
	TriggeredReason string `json:"triggered_reason,omitempty"`
}

// RollbackConditionType specifies the type of rollback condition.
type RollbackConditionType string

const (
	// RollbackConditionHealthCheck triggers on health check failures.
	RollbackConditionHealthCheck RollbackConditionType = "health_check"

	// RollbackConditionErrorRate triggers on error rate threshold.
	RollbackConditionErrorRate RollbackConditionType = "error_rate"

	// RollbackConditionLatency triggers on latency threshold.
	RollbackConditionLatency RollbackConditionType = "latency"

	// RollbackConditionTimeout triggers if cutover exceeds timeout.
	RollbackConditionTimeout RollbackConditionType = "timeout"

	// RollbackConditionManual is a manual trigger (user-initiated).
	RollbackConditionManual RollbackConditionType = "manual"

	// RollbackConditionCustom is a custom expression-based trigger.
	RollbackConditionCustom RollbackConditionType = "custom"
)

// String returns the string representation of the condition type.
func (c RollbackConditionType) String() string {
	return string(c)
}

// NewRollbackTrigger creates a new rollback trigger with defaults.
func NewRollbackTrigger(id, description string, autoRollback bool) *RollbackTrigger {
	return &RollbackTrigger{
		ID:                  id,
		Description:         description,
		ConditionType:       RollbackConditionHealthCheck,
		ConsecutiveFailures: 3,
		WindowDuration:      5 * time.Minute,
		AutoRollback:        autoRollback,
		Enabled:             true,
		Priority:            10,
	}
}

// NewHealthCheckTrigger creates a rollback trigger based on a health check.
func NewHealthCheckTrigger(id, healthCheckID string, consecutiveFailures int, autoRollback bool) *RollbackTrigger {
	trigger := NewRollbackTrigger(id, "Rollback on health check failure", autoRollback)
	trigger.ConditionType = RollbackConditionHealthCheck
	trigger.HealthCheckID = healthCheckID
	trigger.ConsecutiveFailures = consecutiveFailures
	trigger.Condition = "health_check.failures >= " + string(rune(consecutiveFailures+'0'))
	return trigger
}

// NewErrorRateTrigger creates a rollback trigger based on error rate.
func NewErrorRateTrigger(id string, thresholdPercent float64, autoRollback bool) *RollbackTrigger {
	trigger := NewRollbackTrigger(id, "Rollback on high error rate", autoRollback)
	trigger.ConditionType = RollbackConditionErrorRate
	trigger.Threshold = thresholdPercent
	trigger.ThresholdUnit = "%"
	trigger.MetricName = "error_rate"
	trigger.Condition = "error_rate > " + string(rune(int(thresholdPercent)+'0')) + "%"
	return trigger
}

// NewLatencyTrigger creates a rollback trigger based on latency.
func NewLatencyTrigger(id string, thresholdMs float64, autoRollback bool) *RollbackTrigger {
	trigger := NewRollbackTrigger(id, "Rollback on high latency", autoRollback)
	trigger.ConditionType = RollbackConditionLatency
	trigger.Threshold = thresholdMs
	trigger.ThresholdUnit = "ms"
	trigger.MetricName = "p99_latency"
	return trigger
}

// Validate checks if the rollback trigger is valid.
func (t *RollbackTrigger) Validate() []string {
	var errors []string

	if t.ID == "" {
		errors = append(errors, "rollback trigger ID is required")
	}

	if t.Description == "" && t.Name == "" {
		errors = append(errors, "rollback trigger description or name is required")
	}

	switch t.ConditionType {
	case RollbackConditionHealthCheck:
		if t.HealthCheckID == "" {
			errors = append(errors, "health check ID is required for health check triggers")
		}
		if t.ConsecutiveFailures <= 0 {
			errors = append(errors, "consecutive failures must be positive")
		}
	case RollbackConditionErrorRate, RollbackConditionLatency:
		if t.Threshold <= 0 {
			errors = append(errors, "threshold must be positive")
		}
		if t.MetricName == "" {
			errors = append(errors, "metric name is required for metric-based triggers")
		}
	case RollbackConditionCustom:
		if t.Condition == "" {
			errors = append(errors, "condition expression is required for custom triggers")
		}
	}

	return errors
}

// ValidationRule represents a rule for validating migration success.
type ValidationRule struct {
	// ID is the unique identifier for this rule.
	ID string `json:"id"`

	// Name is a human-readable name for the rule.
	Name string `json:"name"`

	// Description explains what this rule validates.
	Description string `json:"description,omitempty"`

	// Type specifies the type of validation.
	Type ValidationType `json:"type"`

	// Expected is the expected value or pattern.
	Expected string `json:"expected"`

	// Actual is the actual value found during validation.
	Actual string `json:"actual,omitempty"`

	// Operator is the comparison operator (equals, contains, regex, etc.).
	Operator ValidationOperator `json:"operator"`

	// Passed indicates if the validation passed.
	Passed bool `json:"passed"`

	// Error contains the error message if validation failed.
	Error string `json:"error,omitempty"`

	// ValidatedAt is when the validation was performed.
	ValidatedAt *time.Time `json:"validated_at,omitempty"`

	// Critical indicates if failing this rule should fail the migration.
	Critical bool `json:"critical"`
}

// ValidationType specifies the type of validation.
type ValidationType string

const (
	// ValidationTypeDataIntegrity validates data was migrated correctly.
	ValidationTypeDataIntegrity ValidationType = "data_integrity"

	// ValidationTypeRowCount validates row counts match.
	ValidationTypeRowCount ValidationType = "row_count"

	// ValidationTypeSchema validates schema matches.
	ValidationTypeSchema ValidationType = "schema"

	// ValidationTypeEndpoint validates an endpoint is accessible.
	ValidationTypeEndpoint ValidationType = "endpoint"

	// ValidationTypeFile validates file existence or content.
	ValidationTypeFile ValidationType = "file"

	// ValidationTypeCustom is a custom validation.
	ValidationTypeCustom ValidationType = "custom"
)

// String returns the string representation of the validation type.
func (t ValidationType) String() string {
	return string(t)
}

// ValidationOperator specifies how to compare expected and actual values.
type ValidationOperator string

const (
	// ValidationOperatorEquals checks exact equality.
	ValidationOperatorEquals ValidationOperator = "equals"

	// ValidationOperatorNotEquals checks inequality.
	ValidationOperatorNotEquals ValidationOperator = "not_equals"

	// ValidationOperatorContains checks if actual contains expected.
	ValidationOperatorContains ValidationOperator = "contains"

	// ValidationOperatorRegex checks if actual matches expected regex.
	ValidationOperatorRegex ValidationOperator = "regex"

	// ValidationOperatorGreaterThan checks if actual > expected (numeric).
	ValidationOperatorGreaterThan ValidationOperator = "greater_than"

	// ValidationOperatorLessThan checks if actual < expected (numeric).
	ValidationOperatorLessThan ValidationOperator = "less_than"

	// ValidationOperatorWithinPercent checks if values are within percentage.
	ValidationOperatorWithinPercent ValidationOperator = "within_percent"
)

// String returns the string representation of the validation operator.
func (o ValidationOperator) String() string {
	return string(o)
}

// NewValidationRule creates a new validation rule.
func NewValidationRule(id, name string, validationType ValidationType, expected string) *ValidationRule {
	return &ValidationRule{
		ID:       id,
		Name:     name,
		Type:     validationType,
		Expected: expected,
		Operator: ValidationOperatorEquals,
		Critical: true,
	}
}

// Validate checks if the validation rule configuration is valid.
func (r *ValidationRule) Validate() []string {
	var errors []string

	if r.ID == "" {
		errors = append(errors, "validation rule ID is required")
	}

	if r.Name == "" {
		errors = append(errors, "validation rule name is required")
	}

	if r.Expected == "" {
		errors = append(errors, "expected value is required")
	}

	if r.Operator == ValidationOperatorRegex {
		if _, err := regexp.Compile(r.Expected); err != nil {
			errors = append(errors, "invalid regex pattern: "+err.Error())
		}
	}

	return errors
}

// SetResult sets the validation result.
func (r *ValidationRule) SetResult(actual string, passed bool, err string) {
	now := time.Now()
	r.Actual = actual
	r.Passed = passed
	r.Error = err
	r.ValidatedAt = &now
}

// ValidationSummary provides a summary of all validation results.
type ValidationSummary struct {
	// TotalRules is the total number of validation rules.
	TotalRules int `json:"total_rules"`

	// PassedRules is the number of rules that passed.
	PassedRules int `json:"passed_rules"`

	// FailedRules is the number of rules that failed.
	FailedRules int `json:"failed_rules"`

	// CriticalFailures is the number of critical rules that failed.
	CriticalFailures int `json:"critical_failures"`

	// AllPassed indicates if all rules passed.
	AllPassed bool `json:"all_passed"`

	// Failures contains details of failed validations.
	Failures []*ValidationRule `json:"failures,omitempty"`

	// Duration is how long validation took.
	Duration time.Duration `json:"duration"`

	// CompletedAt is when validation completed.
	CompletedAt time.Time `json:"completed_at"`
}

// NewValidationSummary creates a summary from validation rules.
func NewValidationSummary(rules []*ValidationRule, duration time.Duration) *ValidationSummary {
	summary := &ValidationSummary{
		TotalRules:  len(rules),
		Failures:    make([]*ValidationRule, 0),
		Duration:    duration,
		CompletedAt: time.Now(),
	}

	for _, rule := range rules {
		if rule.Passed {
			summary.PassedRules++
		} else {
			summary.FailedRules++
			summary.Failures = append(summary.Failures, rule)
			if rule.Critical {
				summary.CriticalFailures++
			}
		}
	}

	summary.AllPassed = summary.FailedRules == 0

	return summary
}
