package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Standard errors
var (
	ErrUserNotFound      = errors.New("user not found")
	ErrUserExists        = errors.New("user already exists")
	ErrRoleNotFound      = errors.New("role not found")
	ErrRoleExists        = errors.New("role already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTokenNotFound     = errors.New("token not found")
	ErrTokenExpired      = errors.New("token expired")
)

// Permission represents a single permission
type Permission string

// Standard permissions
const (
	PermissionRead    Permission = "read"
	PermissionWrite   Permission = "write"
	PermissionDelete  Permission = "delete"
	PermissionAdmin   Permission = "admin"
	PermissionDeploy  Permission = "deploy"
	PermissionMigrate Permission = "migrate"
)

// User represents a user in the system
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	Roles        []string  `json:"roles"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastLogin    *time.Time `json:"last_login,omitempty"`
}

// Role represents a role with permissions
type Role struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Permissions []Permission `json:"permissions"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// Session represents an active user session
type Session struct {
	Token     string    `json:"token"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// Config holds identity service configuration
type Config struct {
	// DataPath is the path to persist identity data (optional)
	DataPath string
	// TokenExpiry is how long tokens are valid (default: 24h)
	TokenExpiry time.Duration
	// BcryptCost is the bcrypt cost factor (default: 10)
	BcryptCost int
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		TokenExpiry: 24 * time.Hour,
		BcryptCost:  10,
	}
}

// Service handles identity and user management
type Service struct {
	mu       sync.RWMutex
	users    map[string]*User
	roles    map[string]*Role
	sessions map[string]*Session
	config   *Config
}

// NewService creates a new identity service
func NewService(cfg *Config) *Service {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	if cfg.TokenExpiry == 0 {
		cfg.TokenExpiry = 24 * time.Hour
	}
	if cfg.BcryptCost == 0 {
		cfg.BcryptCost = 10
	}

	s := &Service{
		users:    make(map[string]*User),
		roles:    make(map[string]*Role),
		sessions: make(map[string]*Session),
		config:   cfg,
	}

	// Initialize default roles
	s.initDefaultRoles()

	// Load persisted data if path is configured
	if cfg.DataPath != "" {
		_ = s.loadData()
	}

	return s
}

// initDefaultRoles creates standard roles
func (s *Service) initDefaultRoles() {
	now := time.Now()

	s.roles["admin"] = &Role{
		ID:          "admin",
		Name:        "Administrator",
		Description: "Full system access",
		Permissions: []Permission{PermissionRead, PermissionWrite, PermissionDelete, PermissionAdmin, PermissionDeploy, PermissionMigrate},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.roles["operator"] = &Role{
		ID:          "operator",
		Name:        "Operator",
		Description: "Can deploy and manage resources",
		Permissions: []Permission{PermissionRead, PermissionWrite, PermissionDeploy, PermissionMigrate},
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.roles["viewer"] = &Role{
		ID:          "viewer",
		Name:        "Viewer",
		Description: "Read-only access",
		Permissions: []Permission{PermissionRead},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// generateID creates a new random ID
func generateID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// generateToken creates a new random token
func generateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateUser creates a new user
func (s *Service) CreateUser(ctx context.Context, username, email, password string, roleIDs []string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if username already exists
	for _, u := range s.users {
		if u.Username == username {
			return nil, ErrUserExists
		}
	}

	// Validate roles exist
	for _, roleID := range roleIDs {
		if _, ok := s.roles[roleID]; !ok {
			return nil, fmt.Errorf("role %s: %w", roleID, ErrRoleNotFound)
		}
	}

	// Hash password
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.config.BcryptCost)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	now := time.Now()
	user := &User{
		ID:           generateID(),
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
		Roles:        roleIDs,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	s.users[user.ID] = user
	s.saveData()

	return user, nil
}

// GetUser retrieves a user by ID
func (s *Service) GetUser(ctx context.Context, id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return user, nil
}

// GetUserByUsername retrieves a user by username
func (s *Service) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, u := range s.users {
		if u.Username == username {
			return u, nil
		}
	}
	return nil, ErrUserNotFound
}

// ListUsers returns all users
func (s *Service) ListUsers(ctx context.Context) ([]*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	users := make([]*User, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, u)
	}
	return users, nil
}

// UpdateUser updates a user
func (s *Service) UpdateUser(ctx context.Context, id string, updates map[string]interface{}) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[id]
	if !ok {
		return nil, ErrUserNotFound
	}

	if username, ok := updates["username"].(string); ok && username != "" {
		// Check username uniqueness
		for _, u := range s.users {
			if u.ID != id && u.Username == username {
				return nil, ErrUserExists
			}
		}
		user.Username = username
	}

	if email, ok := updates["email"].(string); ok {
		user.Email = email
	}

	if password, ok := updates["password"].(string); ok && password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), s.config.BcryptCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password: %w", err)
		}
		user.PasswordHash = string(hash)
	}

	if roles, ok := updates["roles"].([]string); ok {
		// Validate roles
		for _, roleID := range roles {
			if _, exists := s.roles[roleID]; !exists {
				return nil, fmt.Errorf("role %s: %w", roleID, ErrRoleNotFound)
			}
		}
		user.Roles = roles
	}

	if enabled, ok := updates["enabled"].(bool); ok {
		user.Enabled = enabled
	}

	user.UpdatedAt = time.Now()
	s.saveData()

	return user, nil
}

// DeleteUser deletes a user
func (s *Service) DeleteUser(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.users[id]; !ok {
		return ErrUserNotFound
	}

	delete(s.users, id)

	// Revoke all sessions for this user
	for token, session := range s.sessions {
		if session.UserID == id {
			delete(s.sessions, token)
		}
	}

	s.saveData()
	return nil
}

// CreateRole creates a new role
func (s *Service) CreateRole(ctx context.Context, name, description string, permissions []Permission) (*Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := generateID()

	// Check if name already exists
	for _, r := range s.roles {
		if r.Name == name {
			return nil, ErrRoleExists
		}
	}

	now := time.Now()
	role := &Role{
		ID:          id,
		Name:        name,
		Description: description,
		Permissions: permissions,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	s.roles[id] = role
	s.saveData()

	return role, nil
}

// GetRole retrieves a role by ID
func (s *Service) GetRole(ctx context.Context, id string) (*Role, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	role, ok := s.roles[id]
	if !ok {
		return nil, ErrRoleNotFound
	}
	return role, nil
}

// ListRoles returns all roles
func (s *Service) ListRoles(ctx context.Context) ([]*Role, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roles := make([]*Role, 0, len(s.roles))
	for _, r := range s.roles {
		roles = append(roles, r)
	}
	return roles, nil
}

// UpdateRole updates a role
func (s *Service) UpdateRole(ctx context.Context, id string, updates map[string]interface{}) (*Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	role, ok := s.roles[id]
	if !ok {
		return nil, ErrRoleNotFound
	}

	if name, ok := updates["name"].(string); ok && name != "" {
		role.Name = name
	}

	if description, ok := updates["description"].(string); ok {
		role.Description = description
	}

	if permissions, ok := updates["permissions"].([]Permission); ok {
		role.Permissions = permissions
	}

	role.UpdatedAt = time.Now()
	s.saveData()

	return role, nil
}

// DeleteRole deletes a role
func (s *Service) DeleteRole(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Don't allow deleting built-in roles
	if id == "admin" || id == "operator" || id == "viewer" {
		return fmt.Errorf("cannot delete built-in role: %s", id)
	}

	if _, ok := s.roles[id]; !ok {
		return ErrRoleNotFound
	}

	delete(s.roles, id)
	s.saveData()

	return nil
}

// AssignRole assigns a role to a user
func (s *Service) AssignRole(ctx context.Context, userID, roleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[userID]
	if !ok {
		return ErrUserNotFound
	}

	if _, ok := s.roles[roleID]; !ok {
		return ErrRoleNotFound
	}

	// Check if already assigned
	for _, r := range user.Roles {
		if r == roleID {
			return nil
		}
	}

	user.Roles = append(user.Roles, roleID)
	user.UpdatedAt = time.Now()
	s.saveData()

	return nil
}

// RemoveRole removes a role from a user
func (s *Service) RemoveRole(ctx context.Context, userID, roleID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[userID]
	if !ok {
		return ErrUserNotFound
	}

	roles := make([]string, 0, len(user.Roles))
	for _, r := range user.Roles {
		if r != roleID {
			roles = append(roles, r)
		}
	}

	user.Roles = roles
	user.UpdatedAt = time.Now()
	s.saveData()

	return nil
}

// ValidateCredentials validates username and password, returns session
func (s *Service) ValidateCredentials(ctx context.Context, username, password string) (*Session, *User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var user *User
	for _, u := range s.users {
		if u.Username == username {
			user = u
			break
		}
	}

	if user == nil {
		return nil, nil, ErrInvalidCredentials
	}

	if !user.Enabled {
		return nil, nil, ErrInvalidCredentials
	}

	err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password))
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	// Update last login
	now := time.Now()
	user.LastLogin = &now

	// Create session
	session := &Session{
		Token:     generateToken(),
		UserID:    user.ID,
		Username:  user.Username,
		ExpiresAt: now.Add(s.config.TokenExpiry),
		CreatedAt: now,
	}

	s.sessions[session.Token] = session
	s.saveData()

	return session, user, nil
}

// ValidateToken validates a session token
func (s *Service) ValidateToken(ctx context.Context, token string) (*Session, *User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[token]
	if !ok {
		return nil, nil, ErrTokenNotFound
	}

	if time.Now().After(session.ExpiresAt) {
		return nil, nil, ErrTokenExpired
	}

	user, ok := s.users[session.UserID]
	if !ok {
		return nil, nil, ErrUserNotFound
	}

	return session, user, nil
}

// RevokeToken revokes a session token
func (s *Service) RevokeToken(ctx context.Context, token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[token]; !ok {
		return ErrTokenNotFound
	}

	delete(s.sessions, token)
	return nil
}

// GetUserPermissions returns all permissions for a user
func (s *Service) GetUserPermissions(ctx context.Context, userID string) ([]Permission, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	user, ok := s.users[userID]
	if !ok {
		return nil, ErrUserNotFound
	}

	permSet := make(map[Permission]bool)
	for _, roleID := range user.Roles {
		if role, ok := s.roles[roleID]; ok {
			for _, perm := range role.Permissions {
				permSet[perm] = true
			}
		}
	}

	perms := make([]Permission, 0, len(permSet))
	for perm := range permSet {
		perms = append(perms, perm)
	}

	return perms, nil
}

// HasPermission checks if a user has a specific permission
func (s *Service) HasPermission(ctx context.Context, userID string, permission Permission) (bool, error) {
	perms, err := s.GetUserPermissions(ctx, userID)
	if err != nil {
		return false, err
	}

	for _, p := range perms {
		if p == permission || p == PermissionAdmin {
			return true, nil
		}
	}

	return false, nil
}

// ListAvailablePermissions returns all available permissions
func (s *Service) ListAvailablePermissions() []Permission {
	return []Permission{
		PermissionRead,
		PermissionWrite,
		PermissionDelete,
		PermissionAdmin,
		PermissionDeploy,
		PermissionMigrate,
	}
}

// persistData is the structure for persisting data
type persistData struct {
	Users    map[string]*User    `json:"users"`
	Roles    map[string]*Role    `json:"roles"`
	Sessions map[string]*Session `json:"sessions"`
}

// saveData persists data to disk
func (s *Service) saveData() {
	if s.config.DataPath == "" {
		return
	}

	data := persistData{
		Users:    s.users,
		Roles:    s.roles,
		Sessions: s.sessions,
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return
	}

	_ = os.WriteFile(s.config.DataPath, jsonData, 0600)
}

// loadData loads data from disk
func (s *Service) loadData() error {
	if s.config.DataPath == "" {
		return nil
	}

	data, err := os.ReadFile(s.config.DataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var pData persistData
	if err := json.Unmarshal(data, &pData); err != nil {
		return err
	}

	if pData.Users != nil {
		s.users = pData.Users
	}
	if pData.Roles != nil {
		// Merge with default roles
		for id, role := range pData.Roles {
			s.roles[id] = role
		}
	}
	if pData.Sessions != nil {
		// Only load non-expired sessions
		now := time.Now()
		for token, session := range pData.Sessions {
			if session.ExpiresAt.After(now) {
				s.sessions[token] = session
			}
		}
	}

	return nil
}
