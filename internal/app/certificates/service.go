// Package certificates provides Let's Encrypt/ACME certificate management.
package certificates

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
)

// Config holds the certificate service configuration.
type Config struct {
	// DataDir is the directory for storing certificates and account data
	DataDir string
	// Email is the ACME account email for Let's Encrypt notifications
	Email string
	// ACMEDirectory is the ACME server directory URL
	// Use acme.LetsEncryptURL for production or staging URL for testing
	ACMEDirectory string
	// AcceptTOS indicates whether to accept the ACME Terms of Service
	AcceptTOS bool
}

// Certificate represents a managed TLS certificate.
type Certificate struct {
	ID          string    `json:"id"`
	Domain      string    `json:"domain"`
	SANs        []string  `json:"sans,omitempty"`
	Issuer      string    `json:"issuer"`
	NotBefore   time.Time `json:"not_before"`
	NotAfter    time.Time `json:"not_after"`
	Fingerprint string    `json:"fingerprint"`
	Status      string    `json:"status"` // valid, expiring, expired, pending
	AutoRenew   bool      `json:"auto_renew"`
	CreatedAt   time.Time `json:"created_at"`
	RenewedAt   time.Time `json:"renewed_at,omitempty"`
}

// CertificateRequest represents a request for a new certificate.
type CertificateRequest struct {
	Domain    string   `json:"domain"`
	SANs      []string `json:"sans,omitempty"`
	AutoRenew bool     `json:"auto_renew"`
}

// ChallengeInfo holds information about a pending ACME challenge.
type ChallengeInfo struct {
	Domain string `json:"domain"`
	Type   string `json:"type"` // http-01, dns-01, tls-alpn-01
	Token  string `json:"token"`
	Value  string `json:"value"`
}

// Service manages TLS certificates using ACME protocol.
type Service struct {
	config     Config
	client     *acme.Client
	accountKey crypto.Signer
	mu         sync.RWMutex
	challenges map[string]*ChallengeInfo
}

// NewService creates a new certificate management service.
func NewService(cfg Config) (*Service, error) {
	if cfg.DataDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		cfg.DataDir = filepath.Join(homeDir, ".homeport", "certificates")
	}

	// Create data directory
	if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Create subdirectories
	for _, dir := range []string{"certs", "keys", "account"} {
		if err := os.MkdirAll(filepath.Join(cfg.DataDir, dir), 0700); err != nil {
			return nil, fmt.Errorf("failed to create %s directory: %w", dir, err)
		}
	}

	// Set default ACME directory (Let's Encrypt production)
	if cfg.ACMEDirectory == "" {
		cfg.ACMEDirectory = acme.LetsEncryptURL
	}

	s := &Service{
		config:     cfg,
		challenges: make(map[string]*ChallengeInfo),
	}

	// Load or create account key
	accountKey, err := s.loadOrCreateAccountKey()
	if err != nil {
		return nil, fmt.Errorf("failed to load account key: %w", err)
	}
	s.accountKey = accountKey

	// Create ACME client
	s.client = &acme.Client{
		Key:          accountKey,
		DirectoryURL: cfg.ACMEDirectory,
	}

	return s, nil
}

// loadOrCreateAccountKey loads the account private key or creates a new one.
func (s *Service) loadOrCreateAccountKey() (crypto.Signer, error) {
	keyPath := filepath.Join(s.config.DataDir, "account", "account.key")

	// Try to load existing key
	keyData, err := os.ReadFile(keyPath)
	if err == nil {
		block, _ := pem.Decode(keyData)
		if block != nil && block.Type == "EC PRIVATE KEY" {
			key, err := x509.ParseECPrivateKey(block.Bytes)
			if err == nil {
				return key, nil
			}
		}
	}

	// Generate new ECDSA key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate account key: %w", err)
	}

	// Save key
	keyBytes, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal account key: %w", err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})

	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return nil, fmt.Errorf("failed to save account key: %w", err)
	}

	return key, nil
}

// Register registers or retrieves an ACME account.
func (s *Service) Register(ctx context.Context) error {
	if !s.config.AcceptTOS {
		return fmt.Errorf("ACME Terms of Service must be accepted")
	}

	account := &acme.Account{
		Contact: []string{"mailto:" + s.config.Email},
	}

	_, err := s.client.Register(ctx, account, func(tosURL string) bool {
		return s.config.AcceptTOS
	})
	if err != nil && err != acme.ErrAccountAlreadyExists {
		return fmt.Errorf("failed to register ACME account: %w", err)
	}

	return nil
}

// ListCertificates returns all managed certificates.
func (s *Service) ListCertificates(ctx context.Context) ([]Certificate, error) {
	certsDir := filepath.Join(s.config.DataDir, "certs")
	entries, err := os.ReadDir(certsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Certificate{}, nil
		}
		return nil, fmt.Errorf("failed to read certificates directory: %w", err)
	}

	var certs []Certificate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		cert, err := s.loadCertificate(entry.Name())
		if err != nil {
			continue // Skip invalid certificates
		}
		certs = append(certs, *cert)
	}

	return certs, nil
}

// GetCertificate returns a specific certificate by domain.
func (s *Service) GetCertificate(ctx context.Context, domain string) (*Certificate, error) {
	return s.loadCertificate(domain)
}

// loadCertificate loads certificate metadata from disk.
func (s *Service) loadCertificate(domain string) (*Certificate, error) {
	certDir := filepath.Join(s.config.DataDir, "certs", domain)
	metaPath := filepath.Join(certDir, "metadata.json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read certificate metadata: %w", err)
	}

	var cert Certificate
	if err := json.Unmarshal(data, &cert); err != nil {
		return nil, fmt.Errorf("failed to parse certificate metadata: %w", err)
	}

	// Update status based on expiry
	now := time.Now()
	if now.After(cert.NotAfter) {
		cert.Status = "expired"
	} else if now.Add(30 * 24 * time.Hour).After(cert.NotAfter) {
		cert.Status = "expiring"
	} else {
		cert.Status = "valid"
	}

	return &cert, nil
}

// RequestCertificate initiates a new certificate request using ACME.
func (s *Service) RequestCertificate(ctx context.Context, req CertificateRequest) (*Certificate, error) {
	if req.Domain == "" {
		return nil, fmt.Errorf("domain is required")
	}

	// Create certificate directory
	certDir := filepath.Join(s.config.DataDir, "certs", req.Domain)
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create certificate directory: %w", err)
	}

	// Generate private key for the certificate
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate certificate key: %w", err)
	}

	// Save private key
	keyPath := filepath.Join(s.config.DataDir, "keys", req.Domain+".key")
	keyBytes, err := x509.MarshalECPrivateKey(certKey)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal certificate key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyBytes,
	})
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return nil, fmt.Errorf("failed to save certificate key: %w", err)
	}

	// Collect all domains (main + SANs)
	domains := []string{req.Domain}
	domains = append(domains, req.SANs...)

	// Create order
	order, err := s.client.AuthorizeOrder(ctx, acme.DomainIDs(domains...))
	if err != nil {
		return nil, fmt.Errorf("failed to create ACME order: %w", err)
	}

	// Process authorizations
	for _, authURL := range order.AuthzURLs {
		auth, err := s.client.GetAuthorization(ctx, authURL)
		if err != nil {
			return nil, fmt.Errorf("failed to get authorization: %w", err)
		}

		if auth.Status == acme.StatusValid {
			continue
		}

		// Find HTTP-01 challenge
		var challenge *acme.Challenge
		for _, ch := range auth.Challenges {
			if ch.Type == "http-01" {
				challenge = ch
				break
			}
		}

		if challenge == nil {
			return nil, fmt.Errorf("no HTTP-01 challenge available for %s", auth.Identifier.Value)
		}

		// Get challenge response
		response, err := s.client.HTTP01ChallengeResponse(challenge.Token)
		if err != nil {
			return nil, fmt.Errorf("failed to get challenge response: %w", err)
		}

		// Store challenge for HTTP handler
		s.mu.Lock()
		s.challenges[challenge.Token] = &ChallengeInfo{
			Domain: auth.Identifier.Value,
			Type:   "http-01",
			Token:  challenge.Token,
			Value:  response,
		}
		s.mu.Unlock()

		// Accept the challenge
		if _, err := s.client.Accept(ctx, challenge); err != nil {
			s.mu.Lock()
			delete(s.challenges, challenge.Token)
			s.mu.Unlock()
			return nil, fmt.Errorf("failed to accept challenge: %w", err)
		}

		// Wait for authorization
		if _, err := s.client.WaitAuthorization(ctx, authURL); err != nil {
			s.mu.Lock()
			delete(s.challenges, challenge.Token)
			s.mu.Unlock()
			return nil, fmt.Errorf("authorization failed: %w", err)
		}

		// Clean up challenge
		s.mu.Lock()
		delete(s.challenges, challenge.Token)
		s.mu.Unlock()
	}

	// Create CSR
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		DNSNames: domains,
	}, certKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create CSR: %w", err)
	}

	// Finalize order
	der, _, err := s.client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return nil, fmt.Errorf("failed to finalize order: %w", err)
	}

	// Save certificate chain
	var certPEM []byte
	for _, b := range der {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: b,
		})...)
	}

	certPath := filepath.Join(certDir, "cert.pem")
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return nil, fmt.Errorf("failed to save certificate: %w", err)
	}

	// Parse certificate to get metadata
	parsedCert, err := x509.ParseCertificate(der[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse certificate: %w", err)
	}

	// Create certificate metadata
	cert := &Certificate{
		ID:          req.Domain,
		Domain:      req.Domain,
		SANs:        req.SANs,
		Issuer:      parsedCert.Issuer.CommonName,
		NotBefore:   parsedCert.NotBefore,
		NotAfter:    parsedCert.NotAfter,
		Fingerprint: fmt.Sprintf("%x", parsedCert.SerialNumber),
		Status:      "valid",
		AutoRenew:   req.AutoRenew,
		CreatedAt:   time.Now(),
	}

	// Save metadata
	metaPath := filepath.Join(certDir, "metadata.json")
	metaData, err := json.MarshalIndent(cert, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return nil, fmt.Errorf("failed to save metadata: %w", err)
	}

	return cert, nil
}

// RenewCertificate renews an existing certificate.
func (s *Service) RenewCertificate(ctx context.Context, domain string) (*Certificate, error) {
	existing, err := s.loadCertificate(domain)
	if err != nil {
		return nil, fmt.Errorf("certificate not found: %w", err)
	}

	// Request new certificate with same parameters
	newCert, err := s.RequestCertificate(ctx, CertificateRequest{
		Domain:    existing.Domain,
		SANs:      existing.SANs,
		AutoRenew: existing.AutoRenew,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to renew certificate: %w", err)
	}

	newCert.RenewedAt = time.Now()
	return newCert, nil
}

// DeleteCertificate removes a certificate and its associated files.
func (s *Service) DeleteCertificate(ctx context.Context, domain string) error {
	certDir := filepath.Join(s.config.DataDir, "certs", domain)
	keyPath := filepath.Join(s.config.DataDir, "keys", domain+".key")

	// Remove certificate directory
	if err := os.RemoveAll(certDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove certificate directory: %w", err)
	}

	// Remove key file
	if err := os.Remove(keyPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove key file: %w", err)
	}

	return nil
}

// GetChallengeResponse returns the challenge response for HTTP-01 validation.
func (s *Service) GetChallengeResponse(token string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	challenge, ok := s.challenges[token]
	if !ok {
		return "", false
	}
	return challenge.Value, true
}

// GetPendingChallenges returns all pending ACME challenges.
func (s *Service) GetPendingChallenges() []ChallengeInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	challenges := make([]ChallengeInfo, 0, len(s.challenges))
	for _, ch := range s.challenges {
		challenges = append(challenges, *ch)
	}
	return challenges
}

// LoadTLSCertificate loads a certificate as tls.Certificate for use in TLS config.
func (s *Service) LoadTLSCertificate(domain string) (*tls.Certificate, error) {
	certPath := filepath.Join(s.config.DataDir, "certs", domain, "cert.pem")
	keyPath := filepath.Join(s.config.DataDir, "keys", domain+".key")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	return &cert, nil
}

// GetCertificatesNeedingRenewal returns certificates that need renewal (expiring within 30 days).
func (s *Service) GetCertificatesNeedingRenewal(ctx context.Context) ([]Certificate, error) {
	certs, err := s.ListCertificates(ctx)
	if err != nil {
		return nil, err
	}

	var needsRenewal []Certificate
	threshold := time.Now().Add(30 * 24 * time.Hour)

	for _, cert := range certs {
		if cert.AutoRenew && cert.NotAfter.Before(threshold) {
			needsRenewal = append(needsRenewal, cert)
		}
	}

	return needsRenewal, nil
}

// AutoRenewCertificates renews all certificates that need renewal.
func (s *Service) AutoRenewCertificates(ctx context.Context) ([]Certificate, error) {
	needsRenewal, err := s.GetCertificatesNeedingRenewal(ctx)
	if err != nil {
		return nil, err
	}

	var renewed []Certificate
	for _, cert := range needsRenewal {
		newCert, err := s.RenewCertificate(ctx, cert.Domain)
		if err != nil {
			continue // Log error but continue with other renewals
		}
		renewed = append(renewed, *newCert)
	}

	return renewed, nil
}
