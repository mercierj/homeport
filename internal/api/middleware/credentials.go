package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// CredentialStoreContextKey is the context key for the credential store
type credentialStoreContextKey struct{}

// CredentialStoreMiddleware adds the credential store to request context
func CredentialStoreMiddleware(store *CredentialStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), credentialStoreContextKey{}, store)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetCredentialStore retrieves the credential store from request context
func GetCredentialStore(r *http.Request) *CredentialStore {
	if store, ok := r.Context().Value(credentialStoreContextKey{}).(*CredentialStore); ok {
		return store
	}
	return nil
}

// StorageCredentials holds MinIO/S3-compatible storage credentials
type StorageCredentials struct {
	Endpoint  string `json:"endpoint"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

// DatabaseCredentials holds PostgreSQL database credentials
type DatabaseCredentials struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
	SSLMode  string `json:"sslMode,omitempty"`
}

// CredentialStore manages credentials keyed by session token
type CredentialStore struct {
	mu       sync.RWMutex
	storage  map[string]*StorageCredentials
	database map[string]*DatabaseCredentials
}

// NewCredentialStore creates a new credential store
func NewCredentialStore() *CredentialStore {
	return &CredentialStore{
		storage:  make(map[string]*StorageCredentials),
		database: make(map[string]*DatabaseCredentials),
	}
}

// SetStorageCredentials stores storage credentials for a session
func (cs *CredentialStore) SetStorageCredentials(sessionToken string, creds *StorageCredentials) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.storage[sessionToken] = creds
}

// GetStorageCredentials retrieves storage credentials for a session
func (cs *CredentialStore) GetStorageCredentials(sessionToken string) *StorageCredentials {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.storage[sessionToken]
}

// DeleteStorageCredentials removes storage credentials for a session
func (cs *CredentialStore) DeleteStorageCredentials(sessionToken string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.storage, sessionToken)
}

// SetDatabaseCredentials stores database credentials for a session
func (cs *CredentialStore) SetDatabaseCredentials(sessionToken string, creds *DatabaseCredentials) error {
	// Validate port number
	if creds.Port != 0 && (creds.Port < 1 || creds.Port > 65535) {
		return fmt.Errorf("invalid port number: %d (must be 1-65535)", creds.Port)
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.database[sessionToken] = creds
	return nil
}

// GetDatabaseCredentials retrieves database credentials for a session
func (cs *CredentialStore) GetDatabaseCredentials(sessionToken string) *DatabaseCredentials {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return cs.database[sessionToken]
}

// DeleteDatabaseCredentials removes database credentials for a session
func (cs *CredentialStore) DeleteDatabaseCredentials(sessionToken string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.database, sessionToken)
}

// ClearSessionCredentials removes all credentials for a session (called on logout)
func (cs *CredentialStore) ClearSessionCredentials(sessionToken string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.storage, sessionToken)
	delete(cs.database, sessionToken)
}

// CredentialsHandler provides HTTP handlers for credential management
type CredentialsHandler struct {
	store *CredentialStore
}

// NewCredentialsHandler creates a new credentials handler
func NewCredentialsHandler(store *CredentialStore) *CredentialsHandler {
	return &CredentialsHandler{store: store}
}

// HandleSetStorageCredentials stores storage credentials for the current session
func (h *CredentialsHandler) HandleSetStorageCredentials(w http.ResponseWriter, r *http.Request) {
	session := GetSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var creds StorageCredentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.store.SetStorageCredentials(session.Token, &creds)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// HandleDeleteStorageCredentials removes storage credentials for the current session
func (h *CredentialsHandler) HandleDeleteStorageCredentials(w http.ResponseWriter, r *http.Request) {
	session := GetSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.store.DeleteStorageCredentials(session.Token)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// HandleSetDatabaseCredentials stores database credentials for the current session
func (h *CredentialsHandler) HandleSetDatabaseCredentials(w http.ResponseWriter, r *http.Request) {
	session := GetSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var creds DatabaseCredentials
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.store.SetDatabaseCredentials(session.Token, &creds); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

// HandleDeleteDatabaseCredentials removes database credentials for the current session
func (h *CredentialsHandler) HandleDeleteDatabaseCredentials(w http.ResponseWriter, r *http.Request) {
	session := GetSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	h.store.DeleteDatabaseCredentials(session.Token)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}
