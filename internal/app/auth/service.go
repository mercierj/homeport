package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

const (
	// SessionIdleTimeout is the sliding window timeout
	SessionIdleTimeout = 24 * time.Hour
	// MaxSessionLifetime is the absolute max session duration
	MaxSessionLifetime = 24 * time.Hour
	// MaxFailedAttempts is the number of failed login attempts before lockout
	MaxFailedAttempts = 5
	// LockoutDuration is how long an account is locked after too many failed attempts
	LockoutDuration = 15 * time.Minute
)

var (
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrUserExists          = errors.New("user already exists")
	ErrUserNotFound        = errors.New("user not found")
	ErrNoAdminConfigured   = errors.New("no admin user configured: set ADMIN_PASSWORD environment variable")
	ErrSessionExpired      = errors.New("session has exceeded maximum lifetime")
	ErrAccountLocked       = errors.New("account temporarily locked due to too many failed login attempts")
	ErrWeakPassword        = errors.New("password must be at least 8 characters and contain uppercase, lowercase, digit, and special character")
)

// ValidatePasswordComplexity checks if a password meets security requirements:
// - At least 8 characters
// - At least one uppercase letter
// - At least one lowercase letter
// - At least one digit
// - At least one special character
func ValidatePasswordComplexity(password string) error {
	if len(password) < 8 {
		return ErrWeakPassword
	}

	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			hasSpecial = true
		}
	}

	if !hasUpper || !hasLower || !hasDigit || !hasSpecial {
		return ErrWeakPassword
	}

	return nil
}

type User struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

type Session struct {
	Token     string    `json:"token"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"` // Absolute creation time
	ExpiresAt time.Time `json:"expires_at"` // Sliding window expiry
}

// loginAttempt tracks failed login attempts for brute force protection
type loginAttempt struct {
	FailedCount int       // Number of consecutive failed attempts
	LockedUntil time.Time // When the lockout expires (zero if not locked)
	LastAttempt time.Time // Time of last failed attempt
}

type Service struct {
	users         map[string]*User
	sessions      map[string]*Session
	loginAttempts map[string]*loginAttempt // Track failed login attempts by username
	mu            sync.RWMutex
	dataPath      string
	encryptionKey []byte // 32-byte key derived from ENCRYPTION_KEY env var (nil if not set)
}

// deriveKey derives a 32-byte AES-256 key from a passphrase using SHA-256
func deriveKey(passphrase string) []byte {
	hash := sha256.Sum256([]byte(passphrase))
	return hash[:]
}

// encrypt encrypts plaintext using AES-GCM
func encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt decrypts ciphertext using AES-GCM
func decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

func NewService(dataPath string) (*Service, error) {
	s := &Service{
		users:         make(map[string]*User),
		sessions:      make(map[string]*Session),
		loginAttempts: make(map[string]*loginAttempt),
		dataPath:      dataPath,
	}

	// Initialize encryption key from environment if set
	if encKey := os.Getenv("ENCRYPTION_KEY"); encKey != "" {
		s.encryptionKey = deriveKey(encKey)
	}

	// Load existing users
	if err := s.loadUsers(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	// Create admin user if no users exist - require secure password from env
	if len(s.users) == 0 {
		adminPassword := os.Getenv("ADMIN_PASSWORD")
		if adminPassword == "" {
			return nil, ErrNoAdminConfigured
		}
		// Use configurable admin username (defaults to "admin")
		adminUsername := os.Getenv("ADMIN_USERNAME")
		if adminUsername == "" {
			adminUsername = "admin"
		}
		// CreateUser validates password complexity
		if err := s.CreateUser(adminUsername, adminPassword); err != nil {
			return nil, err
		}
	}

	return s, nil
}

func (s *Service) loadUsers() error {
	data, err := os.ReadFile(filepath.Join(s.dataPath, "users.json"))
	if err != nil {
		return err
	}

	// If encryption is enabled, try to decrypt first
	if s.encryptionKey != nil {
		// Try base64 decode (encrypted files are base64 encoded)
		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err == nil {
			decrypted, err := decrypt(s.encryptionKey, decoded)
			if err == nil {
				return json.Unmarshal(decrypted, &s.users)
			}
		}
		// If decryption fails, try plain JSON (backwards compatibility)
	}

	// Try plain JSON (unencrypted file or no encryption key)
	return json.Unmarshal(data, &s.users)
}

func (s *Service) saveUsers() error {
	if err := os.MkdirAll(s.dataPath, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s.users, "", "  ")
	if err != nil {
		return err
	}

	// If encryption is enabled, encrypt the data
	if s.encryptionKey != nil {
		encrypted, err := encrypt(s.encryptionKey, data)
		if err != nil {
			return err
		}
		// Base64 encode for safe file storage
		data = []byte(base64.StdEncoding.EncodeToString(encrypted))
	}

	return os.WriteFile(filepath.Join(s.dataPath, "users.json"), data, 0600)
}

func (s *Service) CreateUser(username, password string) error {
	// Validate password complexity before acquiring lock
	if err := ValidatePasswordComplexity(password); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.users[username]; exists {
		return ErrUserExists
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	s.users[username] = &User{
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    time.Now(),
	}

	return s.saveUsers()
}

// isAccountLocked checks if a user account is currently locked due to failed attempts.
// Must be called with the mutex held.
func (s *Service) isAccountLocked(username string) bool {
	attempt, exists := s.loginAttempts[username]
	if !exists {
		return false
	}

	// Check if lockout has expired
	if time.Now().After(attempt.LockedUntil) {
		return false
	}

	return true
}

// recordFailedAttempt records a failed login attempt and potentially locks the account.
// Must be called with the mutex held.
func (s *Service) recordFailedAttempt(username string) {
	attempt, exists := s.loginAttempts[username]
	if !exists {
		attempt = &loginAttempt{}
		s.loginAttempts[username] = attempt
	}

	// If lockout has expired, reset the counter
	if time.Now().After(attempt.LockedUntil) {
		attempt.FailedCount = 0
		attempt.LockedUntil = time.Time{}
	}

	attempt.FailedCount++
	attempt.LastAttempt = time.Now()

	// Lock account if too many failed attempts
	if attempt.FailedCount >= MaxFailedAttempts {
		attempt.LockedUntil = time.Now().Add(LockoutDuration)
	}
}

// clearFailedAttempts clears failed attempts after successful login.
// Must be called with the mutex held.
func (s *Service) clearFailedAttempts(username string) {
	delete(s.loginAttempts, username)
}

// GetLockoutTimeRemaining returns the time remaining until lockout expires, or 0 if not locked.
func (s *Service) GetLockoutTimeRemaining(username string) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	attempt, exists := s.loginAttempts[username]
	if !exists {
		return 0
	}

	remaining := time.Until(attempt.LockedUntil)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (s *Service) Login(username, password string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if account is locked before attempting login
	if s.isAccountLocked(username) {
		return nil, ErrAccountLocked
	}

	user, exists := s.users[username]
	if !exists {
		// Record failed attempt even for non-existent users to prevent user enumeration
		s.recordFailedAttempt(username)
		return nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		s.recordFailedAttempt(username)
		return nil, ErrInvalidCredentials
	}

	// Clear failed attempts on successful login
	s.clearFailedAttempts(username)

	// Generate session token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	now := time.Now()
	session := &Session{
		Token:     token,
		Username:  username,
		CreatedAt: now,
		ExpiresAt: now.Add(SessionIdleTimeout),
	}

	s.sessions[token] = session
	return session, nil
}

func (s *Service) ValidateSession(token string) (*Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[token]
	if !exists {
		return nil, ErrInvalidCredentials
	}

	now := time.Now()

	// Check absolute max lifetime (security: limits stolen token validity)
	if now.After(session.CreatedAt.Add(MaxSessionLifetime)) {
		delete(s.sessions, token)
		return nil, ErrSessionExpired
	}

	// Check sliding window expiry
	if now.After(session.ExpiresAt) {
		delete(s.sessions, token)
		return nil, ErrInvalidCredentials
	}

	// Sliding window: extend but never beyond max lifetime
	newExpiry := now.Add(SessionIdleTimeout)
	maxExpiry := session.CreatedAt.Add(MaxSessionLifetime)
	if newExpiry.After(maxExpiry) {
		session.ExpiresAt = maxExpiry
	} else {
		session.ExpiresAt = newExpiry
	}

	return session, nil
}

func (s *Service) Logout(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

func (s *Service) ChangePassword(username, oldPassword, newPassword string) error {
	// Validate new password complexity before acquiring lock
	if err := ValidatePasswordComplexity(newPassword); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	user, exists := s.users[username]
	if !exists {
		return ErrUserNotFound
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return ErrInvalidCredentials
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user.PasswordHash = string(hash)
	return s.saveUsers()
}
