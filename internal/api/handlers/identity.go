package handlers

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/homeport/homeport/internal/app/identity"
	"github.com/homeport/homeport/internal/pkg/httputil"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

var (
	// usernameRegex validates usernames
	usernameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{2,31}$`)
	// emailRegex validates email addresses (basic validation)
	emailRegex = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)
)

// IdentityHandler handles identity-related HTTP requests
type IdentityHandler struct {
	service *identity.Service
}

// NewIdentityHandler creates a new identity handler
func NewIdentityHandler(service *identity.Service) *IdentityHandler {
	return &IdentityHandler{service: service}
}

// RegisterRoutes registers identity routes
func (h *IdentityHandler) RegisterRoutes(r chi.Router) {
	r.Route("/identity", func(r chi.Router) {
		// User management
		r.Get("/users", h.HandleListUsers)
		r.Post("/users", h.HandleCreateUser)
		r.Get("/users/{id}", h.HandleGetUser)
		r.Put("/users/{id}", h.HandleUpdateUser)
		r.Delete("/users/{id}", h.HandleDeleteUser)

		// Role assignment
		r.Put("/users/{id}/roles/{roleId}", h.HandleAssignRole)
		r.Delete("/users/{id}/roles/{roleId}", h.HandleRemoveRole)

		// Role management
		r.Get("/roles", h.HandleListRoles)
		r.Post("/roles", h.HandleCreateRole)
		r.Get("/roles/{id}", h.HandleGetRole)
		r.Put("/roles/{id}", h.HandleUpdateRole)
		r.Delete("/roles/{id}", h.HandleDeleteRole)

		// Permissions
		r.Get("/permissions", h.HandleListPermissions)

		// Authentication
		r.Post("/login", h.HandleLogin)
		r.Post("/logout", h.HandleLogout)
		r.Get("/me", h.HandleGetCurrentUser)
	})
}

// validateUsername checks if a username is valid
func validateUsername(username string) error {
	if username == "" {
		return errors.New("username is required")
	}
	if !usernameRegex.MatchString(username) {
		return errors.New("username must be 3-32 characters, start with a letter, and contain only letters, digits, underscores, or hyphens")
	}
	return nil
}

// validateEmail checks if an email is valid
func validateEmail(email string) error {
	if email == "" {
		return errors.New("email is required")
	}
	if !emailRegex.MatchString(email) {
		return errors.New("invalid email format")
	}
	return nil
}

// validatePassword checks if a password meets requirements
func validatePassword(password string) error {
	if password == "" {
		return errors.New("password is required")
	}
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	return nil
}

// userResponse is the JSON response for a user
type userResponse struct {
	ID        string   `json:"id"`
	Username  string   `json:"username"`
	Email     string   `json:"email"`
	Roles     []string `json:"roles"`
	Enabled   bool     `json:"enabled"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
	LastLogin *string  `json:"last_login,omitempty"`
}

func toUserResponse(u *identity.User) userResponse {
	resp := userResponse{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		Roles:     u.Roles,
		Enabled:   u.Enabled,
		CreatedAt: u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: u.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if u.LastLogin != nil {
		s := u.LastLogin.Format("2006-01-02T15:04:05Z07:00")
		resp.LastLogin = &s
	}
	return resp
}

// roleResponse is the JSON response for a role
type roleResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

func toRoleResponse(r *identity.Role) roleResponse {
	perms := make([]string, len(r.Permissions))
	for i, p := range r.Permissions {
		perms[i] = string(p)
	}
	return roleResponse{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: perms,
		CreatedAt:   r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   r.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// HandleListUsers handles GET /identity/users
func (h *IdentityHandler) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.service.ListUsers(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	response := make([]userResponse, len(users))
	for i, u := range users {
		response[i] = toUserResponse(u)
	}

	render.JSON(w, r, map[string]interface{}{
		"users": response,
		"count": len(response),
	})
}

// HandleCreateUser handles POST /identity/users
func (h *IdentityHandler) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string   `json:"username"`
		Email    string   `json:"email"`
		Password string   `json:"password"`
		Roles    []string `json:"roles"`
	}

	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if err := validateUsername(req.Username); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validateEmail(req.Email); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	if err := validatePassword(req.Password); err != nil {
		httputil.BadRequest(w, r, err.Error())
		return
	}

	user, err := h.service.CreateUser(r.Context(), req.Username, req.Email, req.Password, req.Roles)
	if err != nil {
		if errors.Is(err, identity.ErrUserExists) {
			httputil.BadRequest(w, r, "username already exists")
			return
		}
		if errors.Is(err, identity.ErrRoleNotFound) {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	render.JSON(w, r, toUserResponse(user))
}

// HandleGetUser handles GET /identity/users/{id}
func (h *IdentityHandler) HandleGetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	user, err := h.service.GetUser(r.Context(), id)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			httputil.NotFound(w, r, "user not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, toUserResponse(user))
}

// HandleUpdateUser handles PUT /identity/users/{id}
func (h *IdentityHandler) HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Username *string  `json:"username,omitempty"`
		Email    *string  `json:"email,omitempty"`
		Password *string  `json:"password,omitempty"`
		Roles    []string `json:"roles,omitempty"`
		Enabled  *bool    `json:"enabled,omitempty"`
	}

	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	updates := make(map[string]interface{})

	if req.Username != nil {
		if err := validateUsername(*req.Username); err != nil {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		updates["username"] = *req.Username
	}

	if req.Email != nil {
		if err := validateEmail(*req.Email); err != nil {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		updates["email"] = *req.Email
	}

	if req.Password != nil && *req.Password != "" {
		if err := validatePassword(*req.Password); err != nil {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		updates["password"] = *req.Password
	}

	if req.Roles != nil {
		updates["roles"] = req.Roles
	}

	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}

	user, err := h.service.UpdateUser(r.Context(), id, updates)
	if err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			httputil.NotFound(w, r, "user not found")
			return
		}
		if errors.Is(err, identity.ErrUserExists) {
			httputil.BadRequest(w, r, "username already exists")
			return
		}
		if errors.Is(err, identity.ErrRoleNotFound) {
			httputil.BadRequest(w, r, err.Error())
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, toUserResponse(user))
}

// HandleDeleteUser handles DELETE /identity/users/{id}
func (h *IdentityHandler) HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.service.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			httputil.NotFound(w, r, "user not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "deleted",
		"id":     id,
	})
}

// HandleAssignRole handles PUT /identity/users/{id}/roles/{roleId}
func (h *IdentityHandler) HandleAssignRole(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	roleID := chi.URLParam(r, "roleId")

	if err := h.service.AssignRole(r.Context(), userID, roleID); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			httputil.NotFound(w, r, "user not found")
			return
		}
		if errors.Is(err, identity.ErrRoleNotFound) {
			httputil.NotFound(w, r, "role not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "assigned",
	})
}

// HandleRemoveRole handles DELETE /identity/users/{id}/roles/{roleId}
func (h *IdentityHandler) HandleRemoveRole(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "id")
	roleID := chi.URLParam(r, "roleId")

	if err := h.service.RemoveRole(r.Context(), userID, roleID); err != nil {
		if errors.Is(err, identity.ErrUserNotFound) {
			httputil.NotFound(w, r, "user not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "removed",
	})
}

// HandleListRoles handles GET /identity/roles
func (h *IdentityHandler) HandleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.service.ListRoles(r.Context())
	if err != nil {
		httputil.InternalError(w, r, err)
		return
	}

	response := make([]roleResponse, len(roles))
	for i, role := range roles {
		response[i] = toRoleResponse(role)
	}

	render.JSON(w, r, map[string]interface{}{
		"roles": response,
		"count": len(response),
	})
}

// HandleCreateRole handles POST /identity/roles
func (h *IdentityHandler) HandleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}

	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if req.Name == "" {
		httputil.BadRequest(w, r, "name is required")
		return
	}

	permissions := make([]identity.Permission, len(req.Permissions))
	for i, p := range req.Permissions {
		permissions[i] = identity.Permission(p)
	}

	role, err := h.service.CreateRole(r.Context(), req.Name, req.Description, permissions)
	if err != nil {
		if errors.Is(err, identity.ErrRoleExists) {
			httputil.BadRequest(w, r, "role name already exists")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	render.JSON(w, r, toRoleResponse(role))
}

// HandleGetRole handles GET /identity/roles/{id}
func (h *IdentityHandler) HandleGetRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	role, err := h.service.GetRole(r.Context(), id)
	if err != nil {
		if errors.Is(err, identity.ErrRoleNotFound) {
			httputil.NotFound(w, r, "role not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, toRoleResponse(role))
}

// HandleUpdateRole handles PUT /identity/roles/{id}
func (h *IdentityHandler) HandleUpdateRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Name        *string  `json:"name,omitempty"`
		Description *string  `json:"description,omitempty"`
		Permissions []string `json:"permissions,omitempty"`
	}

	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	updates := make(map[string]interface{})

	if req.Name != nil {
		updates["name"] = *req.Name
	}

	if req.Description != nil {
		updates["description"] = *req.Description
	}

	if req.Permissions != nil {
		perms := make([]identity.Permission, len(req.Permissions))
		for i, p := range req.Permissions {
			perms[i] = identity.Permission(p)
		}
		updates["permissions"] = perms
	}

	role, err := h.service.UpdateRole(r.Context(), id, updates)
	if err != nil {
		if errors.Is(err, identity.ErrRoleNotFound) {
			httputil.NotFound(w, r, "role not found")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, toRoleResponse(role))
}

// HandleDeleteRole handles DELETE /identity/roles/{id}
func (h *IdentityHandler) HandleDeleteRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.service.DeleteRole(r.Context(), id); err != nil {
		if errors.Is(err, identity.ErrRoleNotFound) {
			httputil.NotFound(w, r, "role not found")
			return
		}
		httputil.BadRequest(w, r, err.Error())
		return
	}

	render.JSON(w, r, map[string]string{
		"status": "deleted",
		"id":     id,
	})
}

// HandleListPermissions handles GET /identity/permissions
func (h *IdentityHandler) HandleListPermissions(w http.ResponseWriter, r *http.Request) {
	perms := h.service.ListAvailablePermissions()
	permissions := make([]string, len(perms))
	for i, p := range perms {
		permissions[i] = string(p)
	}

	render.JSON(w, r, map[string]interface{}{
		"permissions": permissions,
	})
}

// HandleLogin handles POST /identity/login
func (h *IdentityHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if !httputil.DecodeJSON(w, r, &req) {
		return
	}

	if req.Username == "" || req.Password == "" {
		httputil.BadRequest(w, r, "username and password are required")
		return
	}

	session, user, err := h.service.ValidateCredentials(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, identity.ErrInvalidCredentials) {
			httputil.Unauthorized(w, r, "invalid credentials")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, map[string]interface{}{
		"token":      session.Token,
		"user":       toUserResponse(user),
		"expires_at": session.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// HandleLogout handles POST /identity/logout
func (h *IdentityHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	// Get token from Authorization header
	token := r.Header.Get("Authorization")
	if token == "" {
		// Also check X-Auth-Token header
		token = r.Header.Get("X-Auth-Token")
	}

	// Strip "Bearer " prefix if present
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	if token == "" {
		render.JSON(w, r, map[string]string{"status": "ok"})
		return
	}

	_ = h.service.RevokeToken(r.Context(), token)

	render.JSON(w, r, map[string]string{"status": "ok"})
}

// HandleGetCurrentUser handles GET /identity/me
func (h *IdentityHandler) HandleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	// Get token from Authorization header
	token := r.Header.Get("Authorization")
	if token == "" {
		token = r.Header.Get("X-Auth-Token")
	}

	// Strip "Bearer " prefix if present
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	}

	if token == "" {
		httputil.Unauthorized(w, r, "authentication required")
		return
	}

	_, user, err := h.service.ValidateToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, identity.ErrTokenNotFound) || errors.Is(err, identity.ErrTokenExpired) {
			httputil.Unauthorized(w, r, "invalid or expired token")
			return
		}
		httputil.InternalError(w, r, err)
		return
	}

	render.JSON(w, r, toUserResponse(user))
}
