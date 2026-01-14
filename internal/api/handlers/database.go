package handlers

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/homeport/homeport/internal/api/middleware"
	"github.com/homeport/homeport/internal/app/database"
	"github.com/homeport/homeport/internal/app/database/security"
	"github.com/homeport/homeport/internal/pkg/httputil"
)

// Context key type for database handler audit values
type dbContextKey string

const (
	dbContextKeyRequestID dbContextKey = "request_id"
	dbContextKeyUsername  dbContextKey = "username"
	dbContextKeySessionID dbContextKey = "session_id"
	dbContextKeyIPAddress dbContextKey = "ip_address"
)

const (
	// MaxQueryLimit is the maximum number of rows that can be retrieved
	MaxQueryLimit = 1000
	// MaxQueryLength is the maximum length of a SQL query
	MaxQueryLength = 10000
	// MaxIdentifierLength is the maximum length of schema/table names
	MaxIdentifierLength = 128
)

var (
	// identifierRegex validates PostgreSQL identifiers (schema, table names)
	// Allows letters, digits, underscores, starting with letter or underscore
	identifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// DatabaseHandler handles database-related HTTP requests.
type DatabaseHandler struct{}

// NewDatabaseHandler creates a new database handler.
func NewDatabaseHandler() *DatabaseHandler {
	return &DatabaseHandler{}
}

// validateIdentifier checks if a PostgreSQL identifier is valid
func validateIdentifier(name, fieldName string) error {
	if name == "" {
		return fmt.Errorf("%s is required", fieldName)
	}
	if len(name) > MaxIdentifierLength {
		return fmt.Errorf("%s must be at most %d characters", fieldName, MaxIdentifierLength)
	}
	if !identifierRegex.MatchString(name) {
		return fmt.Errorf("%s must start with a letter or underscore and contain only letters, digits, and underscores", fieldName)
	}
	return nil
}

// validateLimit checks if a limit value is within acceptable bounds
func validateLimit(limit int) (int, error) {
	if limit < 0 {
		return 0, fmt.Errorf("limit must be non-negative")
	}
	if limit == 0 || limit > MaxQueryLimit {
		return MaxQueryLimit, nil
	}
	return limit, nil
}

// validateQuery checks if a SQL query is within acceptable bounds
func validateQuery(query string) error {
	if query == "" {
		return fmt.Errorf("query is required")
	}
	if len(query) > MaxQueryLength {
		return fmt.Errorf("query must be at most %d characters", MaxQueryLength)
	}
	return nil
}

// validatePort checks if a port is valid (1-65535)
func validatePort(port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

// isSecurityError checks if an error is a security-related error
func isSecurityError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	// Check for security error types
	_, isValidationErr := err.(*security.ValidationError)
	_, isACLErr := err.(*security.ACLError)
	if isValidationErr || isACLErr {
		return true
	}
	// Also check error message patterns
	return strings.Contains(errStr, "SQL_") ||
		strings.Contains(errStr, "ACL_") ||
		strings.Contains(errStr, "denied") ||
		strings.Contains(errStr, "not allowed")
}

// handleSecurityError handles security-related errors appropriately
func handleSecurityError(w http.ResponseWriter, r *http.Request, err error) {
	errStr := err.Error()
	// For ACL and access errors, return 403 Forbidden
	if strings.Contains(errStr, "ACL_") || strings.Contains(errStr, "denied") {
		httputil.Forbidden(w, r, errStr)
		return
	}
	// For validation errors, return 400 Bad Request
	httputil.BadRequest(w, r, errStr)
}

// addAuditContext adds user and request info to context for audit logging
func addAuditContext(r *http.Request) context.Context {
	ctx := r.Context()

	// Add request ID
	if reqID := chimiddleware.GetReqID(ctx); reqID != "" {
		ctx = context.WithValue(ctx, dbContextKeyRequestID, reqID)
	}

	// Add username from session
	if session := middleware.GetSession(r); session != nil {
		ctx = context.WithValue(ctx, dbContextKeyUsername, session.Username)
		ctx = context.WithValue(ctx, dbContextKeySessionID, session.Token[:8]+"...") // Truncated for privacy
	}

	// Add client IP
	if ip := r.RemoteAddr; ip != "" {
		// Strip port from address
		if colonIdx := strings.LastIndex(ip, ":"); colonIdx != -1 {
			ip = ip[:colonIdx]
		}
		ctx = context.WithValue(ctx, dbContextKeyIPAddress, ip)
	}

	return ctx
}

// getDatabaseService creates a database service from request session credentials.
func (h *DatabaseHandler) getDatabaseService(r *http.Request) (*database.Service, error) {
	// Get session from context
	session := middleware.GetSession(r)
	if session == nil {
		return nil, fmt.Errorf("no session found")
	}

	// Get credential store from context
	credStore := middleware.GetCredentialStore(r)
	if credStore == nil {
		return nil, fmt.Errorf("credential store not available")
	}

	// Get credentials for this session
	creds := credStore.GetDatabaseCredentials(session.Token)
	if creds == nil {
		return nil, fmt.Errorf("database credentials not configured")
	}

	cfg := database.Config{
		Host:     creds.Host,
		User:     creds.User,
		Password: creds.Password,
		Database: creds.Database,
		SSLMode:  creds.SSLMode,
	}

	// Apply defaults
	if cfg.Host == "" {
		cfg.Host = "localhost"
	}
	if creds.Port == 0 {
		cfg.Port = 5432
	} else {
		if err := validatePort(creds.Port); err != nil {
			return nil, err
		}
		cfg.Port = creds.Port
	}
	if cfg.Database == "" {
		cfg.Database = "postgres"
	}

	return database.NewService(r.Context(), cfg)
}

// HandleListDatabases handles GET /database/databases
func (h *DatabaseHandler) HandleListDatabases(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getDatabaseService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Database connection failed", err)
		return
	}
	defer svc.Close()

	databases, err := svc.ListDatabases(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"databases": databases,
		"count":     len(databases),
	})
}

// HandleListTables handles GET /database/tables
func (h *DatabaseHandler) HandleListTables(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getDatabaseService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Database connection failed", err)
		return
	}
	defer svc.Close()

	schema := r.URL.Query().Get("schema")
	if schema == "" {
		schema = "public"
	}

	if err := validateIdentifier(schema, "schema"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Use audit context for logging
	ctx := addAuditContext(r)

	tables, err := svc.ListTables(ctx, schema)
	if err != nil {
		if isSecurityError(err) {
			handleSecurityError(w, r, err)
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"tables": tables,
		"count":  len(tables),
		"schema": schema,
	})
}

// HandleGetTableSchema handles GET /database/tables/{table}/schema
func (h *DatabaseHandler) HandleGetTableSchema(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getDatabaseService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Database connection failed", err)
		return
	}
	defer svc.Close()

	table := chi.URLParam(r, "table")
	schema := r.URL.Query().Get("schema")
	if schema == "" {
		schema = "public"
	}

	if err := validateIdentifier(schema, "schema"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateIdentifier(table, "table"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Use audit context for logging
	ctx := addAuditContext(r)

	columns, err := svc.GetTableSchema(ctx, schema, table)
	if err != nil {
		if isSecurityError(err) {
			handleSecurityError(w, r, err)
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"columns": columns,
		"table":   table,
		"schema":  schema,
	})
}

// HandleGetTableData handles GET /database/tables/{table}/data
func (h *DatabaseHandler) HandleGetTableData(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getDatabaseService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Database connection failed", err)
		return
	}
	defer svc.Close()

	table := chi.URLParam(r, "table")
	schema := r.URL.Query().Get("schema")
	if schema == "" {
		schema = "public"
	}

	if err := validateIdentifier(schema, "schema"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateIdentifier(table, "table"); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		parsedLimit, err := strconv.Atoi(l)
		if err != nil {
			httputil.BadRequest(w, r, "invalid limit parameter")
			return
		}
		limit, err = validateLimit(parsedLimit)
		if err != nil {
			httputil.BadRequest(w, r, err.Error())
			return
		}
	}

	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		parsedOffset, err := strconv.Atoi(o)
		if err != nil {
			httputil.BadRequest(w, r, "invalid offset parameter")
			return
		}
		if parsedOffset < 0 {
			httputil.BadRequest(w, r, "offset must be non-negative")
			return
		}
		offset = parsedOffset
	}

	// Use audit context for logging
	ctx := addAuditContext(r)

	result, err := svc.GetTableData(ctx, schema, table, limit, offset)
	if err != nil {
		if isSecurityError(err) {
			handleSecurityError(w, r, err)
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, result)
}

// HandleExecuteQuery handles POST /database/query
func (h *DatabaseHandler) HandleExecuteQuery(w http.ResponseWriter, r *http.Request) {
	svc, err := h.getDatabaseService(r)
	if err != nil {
		httputil.BadGateway(w, r, "Database connection failed", err)
		return
	}
	defer svc.Close()

	var req struct {
		Query    string `json:"query"`
		ReadOnly bool   `json:"read_only"`
	}
	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if err := validateQuery(req.Query); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	// Default to read-only for safety
	if !req.ReadOnly {
		req.ReadOnly = true
	}

	// Use audit context for logging
	ctx := addAuditContext(r)

	result, err := svc.ExecuteQuery(ctx, req.Query, req.ReadOnly)
	if err != nil {
		if isSecurityError(err) {
			handleSecurityError(w, r, err)
			return
		}
		httputil.BadRequest(w, r, err.Error())
		return
	}

	render.JSON(w, r, result)
}
