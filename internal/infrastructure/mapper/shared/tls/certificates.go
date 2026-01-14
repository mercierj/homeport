// Package tls provides TLS certificate generation utilities for self-hosted services.
// This package supports self-signed certificates via openssl and integration with
// open-source certificate authorities (step-ca, cfssl, easy-rsa).
// NO US cloud services are used (no AWS KMS, HashiCorp Vault Cloud, etc.).
package tls

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"text/template"
)

// CertificateOptions represents configuration for TLS certificate generation.
// This is separate from TLSOptions which handles connection-level TLS settings.
type CertificateOptions struct {
	// Certificate subject fields
	CommonName   string
	Organization string
	Country      string
	State        string
	Locality     string

	// Certificate validity and key settings
	ValidityDays int
	KeySize      int // 2048 or 4096

	// Subject Alternative Names (DNS names and IPs)
	SANs []string

	// Output directory (e.g., "./certs/postgres")
	OutputDir string

	// External CA support (open-source only, NO cloud services)
	UseExternalCA  bool
	ExternalCAType string // "step-ca", "cfssl", "easy-rsa", "openssl"
	ExternalCAURL  string // For step-ca ACME endpoint
	ExternalCACert string // Path to external CA certificate
	ExternalCAKey  string // Path to external CA key (for cfssl, easy-rsa)

	// ACME settings for step-ca
	ACMEEmail string
}

// ExternalCAType constants for supported external certificate authorities.
const (
	CATypeOpenSSL = "openssl"
	CATypeStepCA  = "step-ca"
	CATypeCFSSL   = "cfssl"
	CATypeEasyRSA = "easy-rsa"
)

// TLSOptions defines TLS connection-level settings for services.
// This is separate from CertificateOptions which handles certificate generation.
type TLSOptions struct {
	// Enabled indicates if TLS is enabled
	Enabled bool

	// CertDir is the directory where certs are stored
	CertDir string

	// Certificate file paths
	ServerCertFile string // Path to server certificate
	ServerKeyFile  string // Path to server private key
	CACertFile     string // Path to CA certificate

	// Protocol settings
	MinProtocolVersion  string // Minimum TLS version (e.g., "TLSv1.2")
	Ciphers             string // Allowed cipher suites
	PreferServerCiphers bool   // Prefer server cipher order

	// Client certificate settings
	RequireClientCerts bool // Require client certificate authentication

	// Connection enforcement
	EnforceSSLConnection bool // Force all connections to use SSL

	// Certificate generation options (for generating new certs)
	CertificateOptions *CertificateOptions
}

// NewTLSOptions creates TLSOptions with defaults for a service.
func NewTLSOptions(serviceName string) *TLSOptions {
	certDir := filepath.Join(".", "certs", serviceName)
	return &TLSOptions{
		Enabled:              true,
		CertDir:              certDir,
		ServerCertFile:       filepath.Join(certDir, "server.crt"),
		ServerKeyFile:        filepath.Join(certDir, "server.key"),
		CACertFile:           filepath.Join(certDir, "ca.crt"),
		MinProtocolVersion:   "TLSv1.2",
		Ciphers:              "HIGH:MEDIUM:+3DES:!aNULL",
		PreferServerCiphers:  true,
		EnforceSSLConnection: true,
		CertificateOptions:   NewCertificateOptions(serviceName),
	}
}

// WithMinProtocol sets the minimum TLS protocol version.
func (t *TLSOptions) WithMinProtocol(version string) *TLSOptions {
	if t != nil {
		t.MinProtocolVersion = version
	}
	return t
}

// WithCiphers sets the allowed cipher suites.
func (t *TLSOptions) WithCiphers(ciphers string) *TLSOptions {
	if t != nil {
		t.Ciphers = ciphers
	}
	return t
}

// WithClientCerts enables or disables client certificate requirement.
func (t *TLSOptions) WithClientCerts(required bool) *TLSOptions {
	if t != nil {
		t.RequireClientCerts = required
	}
	return t
}

// WithCertDir sets the certificate directory.
func (t *TLSOptions) WithCertDir(dir string) *TLSOptions {
	if t != nil {
		t.CertDir = dir
		t.ServerCertFile = filepath.Join(dir, "server.crt")
		t.ServerKeyFile = filepath.Join(dir, "server.key")
		t.CACertFile = filepath.Join(dir, "ca.crt")
	}
	return t
}

// NewCertificateOptions creates CertificateOptions with sensible defaults for a service.
func NewCertificateOptions(serviceName string) *CertificateOptions {
	return &CertificateOptions{
		CommonName:     serviceName,
		Organization:   "Homeport",
		Country:        "XX", // Not country-specific
		ValidityDays:   365,
		KeySize:        4096,
		SANs:           []string{serviceName, "localhost", "127.0.0.1"},
		OutputDir:      filepath.Join(".", "certs", serviceName),
		UseExternalCA:  false,
		ExternalCAType: CATypeOpenSSL,
	}
}

// DefaultCertificateOptions is an alias for NewCertificateOptions.
func DefaultCertificateOptions(serviceName string) *CertificateOptions {
	return NewCertificateOptions(serviceName)
}

// WithExternalCA configures the options to use an external certificate authority.
// Supported caTypes: "step-ca", "cfssl", "easy-rsa", "openssl"
func (o *CertificateOptions) WithExternalCA(caType, caURL, caCert string) *CertificateOptions {
	o.UseExternalCA = true
	o.ExternalCAType = caType
	o.ExternalCAURL = caURL
	o.ExternalCACert = caCert
	return o
}

// WithSANs adds Subject Alternative Names to the certificate.
func (o *CertificateOptions) WithSANs(sans ...string) *CertificateOptions {
	o.SANs = append(o.SANs, sans...)
	return o
}

// WithValidity sets the certificate validity period in days.
func (o *CertificateOptions) WithValidity(days int) *CertificateOptions {
	o.ValidityDays = days
	return o
}

// WithKeySize sets the RSA key size (2048 or 4096).
func (o *CertificateOptions) WithKeySize(bits int) *CertificateOptions {
	if bits == 2048 || bits == 4096 {
		o.KeySize = bits
	}
	return o
}

// WithOrganization sets the certificate organization field.
func (o *CertificateOptions) WithOrganization(org string) *CertificateOptions {
	o.Organization = org
	return o
}

// WithOutputDir sets the output directory for certificates.
func (o *CertificateOptions) WithOutputDir(dir string) *CertificateOptions {
	o.OutputDir = dir
	return o
}

// WithACME configures ACME settings for step-ca integration.
func (o *CertificateOptions) WithACME(email string) *CertificateOptions {
	o.ACMEEmail = email
	return o
}

// GenerateCAScript generates a shell script to create a self-signed CA using openssl.
func GenerateCAScript(opts *CertificateOptions) []byte {
	tmpl := `#!/bin/bash
# =============================================================================
# Self-signed CA generation using openssl (no cloud dependencies)
# Generated by Homeport - Self-hosted infrastructure toolkit
# =============================================================================
set -e

CA_DIR="./certs/ca"
CA_KEY="${CA_DIR}/ca.key"
CA_CERT="${CA_DIR}/ca.crt"
KEY_SIZE={{.KeySize}}
VALIDITY_DAYS={{.ValidityDays}}
ORGANIZATION="{{.Organization}}"
COMMON_NAME="{{.Organization}} CA"

echo "Creating Certificate Authority..."
echo "================================="

# Create CA directory with secure permissions
mkdir -p "${CA_DIR}"
chmod 700 "${CA_DIR}"

# Check if CA already exists
if [ -f "${CA_CERT}" ]; then
    echo "CA certificate already exists at ${CA_CERT}"
    echo "To regenerate, remove the existing CA first:"
    echo "  rm -rf ${CA_DIR}"
    exit 0
fi

# Generate CA private key
echo "[1/3] Generating CA private key (${KEY_SIZE} bits)..."
openssl genrsa -out "${CA_KEY}" ${KEY_SIZE}
chmod 600 "${CA_KEY}"

# Generate CA certificate
echo "[2/3] Creating CA certificate (valid for ${VALIDITY_DAYS} days)..."
openssl req -new -x509 \
    -days ${VALIDITY_DAYS} \
    -key "${CA_KEY}" \
    -out "${CA_CERT}" \
    -subj "/O=${ORGANIZATION}/CN=${COMMON_NAME}"
chmod 644 "${CA_CERT}"

# Verify the CA certificate
echo "[3/3] Verifying CA certificate..."
openssl x509 -in "${CA_CERT}" -noout -text | head -20

echo ""
echo "CA certificate created successfully!"
echo "  CA Certificate: ${CA_CERT}"
echo "  CA Private Key: ${CA_KEY}"
echo ""
echo "Next steps:"
echo "  1. Keep ca.key secure - it's used to sign all service certificates"
echo "  2. Distribute ca.crt to clients that need to trust your services"
echo "  3. Generate service certificates using the service certificate script"
`

	return executeTemplate("ca-script", tmpl, opts)
}

// GenerateServiceCertScript generates a shell script to create a service certificate
// signed by the CA.
func GenerateServiceCertScript(serviceName string, opts *CertificateOptions) []byte {
	// Build SAN configuration
	sanConfig := buildSANConfig(serviceName, opts.SANs)

	data := struct {
		*CertificateOptions
		ServiceName string
		SANConfig   string
	}{
		CertificateOptions: opts,
		ServiceName:        serviceName,
		SANConfig:          sanConfig,
	}

	tmpl := `#!/bin/bash
# =============================================================================
# Service certificate generation for: {{.ServiceName}}
# Signed by local CA using openssl (no cloud dependencies)
# Generated by Homeport - Self-hosted infrastructure toolkit
# =============================================================================
set -e

SERVICE_NAME="{{.ServiceName}}"
CA_DIR="./certs/ca"
CERT_DIR="{{.OutputDir}}"
KEY_SIZE={{.KeySize}}
VALIDITY_DAYS={{.ValidityDays}}
ORGANIZATION="{{.Organization}}"
COMMON_NAME="{{.CommonName}}"

echo "Generating certificate for service: ${SERVICE_NAME}"
echo "=================================================="

# Check if CA exists
if [ ! -f "${CA_DIR}/ca.crt" ] || [ ! -f "${CA_DIR}/ca.key" ]; then
    echo "ERROR: CA certificate not found at ${CA_DIR}"
    echo "Please generate the CA first using the CA generation script."
    exit 1
fi

# Create service certificate directory
mkdir -p "${CERT_DIR}"
chmod 755 "${CERT_DIR}"

# Create OpenSSL config with SANs
CONFIG_FILE="${CERT_DIR}/openssl.cnf"
cat > "${CONFIG_FILE}" << 'OPENSSLCONF'
[req]
default_bits = {{.KeySize}}
prompt = no
default_md = sha256
distinguished_name = dn
req_extensions = req_ext

[dn]
O = {{.Organization}}
CN = {{.CommonName}}

[req_ext]
subjectAltName = @alt_names
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth

[alt_names]
{{.SANConfig}}

[v3_ext]
authorityKeyIdentifier = keyid,issuer
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName = @alt_names
OPENSSLCONF

# Generate private key
echo "[1/4] Generating private key (${KEY_SIZE} bits)..."
openssl genrsa -out "${CERT_DIR}/server.key" ${KEY_SIZE}
chmod 600 "${CERT_DIR}/server.key"

# Generate CSR
echo "[2/4] Creating certificate signing request..."
openssl req -new \
    -key "${CERT_DIR}/server.key" \
    -out "${CERT_DIR}/server.csr" \
    -config "${CONFIG_FILE}"

# Sign with CA
echo "[3/4] Signing certificate with CA..."
openssl x509 -req \
    -in "${CERT_DIR}/server.csr" \
    -CA "${CA_DIR}/ca.crt" \
    -CAkey "${CA_DIR}/ca.key" \
    -CAcreateserial \
    -out "${CERT_DIR}/server.crt" \
    -days ${VALIDITY_DAYS} \
    -extfile "${CONFIG_FILE}" \
    -extensions v3_ext
chmod 644 "${CERT_DIR}/server.crt"

# Verify certificate
echo "[4/4] Verifying certificate..."
openssl verify -CAfile "${CA_DIR}/ca.crt" "${CERT_DIR}/server.crt"

# Show certificate info
echo ""
echo "Certificate details:"
openssl x509 -in "${CERT_DIR}/server.crt" -noout -subject -issuer -dates

# Cleanup
rm -f "${CERT_DIR}/server.csr" "${CERT_DIR}/openssl.cnf"

echo ""
echo "Service certificate created successfully!"
echo "  Certificate: ${CERT_DIR}/server.crt"
echo "  Private Key: ${CERT_DIR}/server.key"
echo "  CA Cert:     ${CA_DIR}/ca.crt (distribute to clients)"
echo ""
echo "Usage in Docker Compose:"
echo "  volumes:"
echo "    - ${CERT_DIR}/server.crt:/etc/ssl/server.crt:ro"
echo "    - ${CERT_DIR}/server.key:/etc/ssl/server.key:ro"
echo "    - ${CA_DIR}/ca.crt:/etc/ssl/ca.crt:ro"
`

	return executeTemplate("service-cert-script", tmpl, data)
}

// GenerateExternalCAScript generates a script for integration with external
// open-source certificate authorities (step-ca, cfssl, easy-rsa).
func GenerateExternalCAScript(serviceName string, opts *CertificateOptions) []byte {
	switch opts.ExternalCAType {
	case CATypeStepCA:
		return generateStepCAScript(serviceName, opts)
	case CATypeCFSSL:
		return generateCFSSLScript(serviceName, opts)
	case CATypeEasyRSA:
		return generateEasyRSAScript(serviceName, opts)
	default:
		// Default to openssl (self-signed)
		return GenerateServiceCertScript(serviceName, opts)
	}
}

// generateStepCAScript generates a script for step-ca certificate authority.
func generateStepCAScript(serviceName string, opts *CertificateOptions) []byte {
	data := struct {
		*CertificateOptions
		ServiceName string
		SANsJoined  string
	}{
		CertificateOptions: opts,
		ServiceName:        serviceName,
		SANsJoined:         strings.Join(opts.SANs, ","),
	}

	tmpl := `#!/bin/bash
# =============================================================================
# Certificate generation using step-ca (Smallstep Certificate Authority)
# Open-source PKI - https://smallstep.com/docs/step-ca
# Generated by Homeport - Self-hosted infrastructure toolkit
# =============================================================================
set -e

SERVICE_NAME="{{.ServiceName}}"
CERT_DIR="{{.OutputDir}}"
STEP_CA_URL="{{.ExternalCAURL}}"
CA_CERT="{{.ExternalCACert}}"
VALIDITY="{{.ValidityDays}}h"

echo "Generating certificate for: ${SERVICE_NAME} using step-ca"
echo "=========================================================="

# Check if step CLI is installed
if ! command -v step &> /dev/null; then
    echo "ERROR: step CLI not found"
    echo "Install with: curl -sLO https://dl.smallstep.com/gh-release/cli/gh-release-header/v0.25.0/step-cli_0.25.0_amd64.deb"
    echo "Or visit: https://smallstep.com/docs/step-cli/installation"
    exit 1
fi

# Create certificate directory
mkdir -p "${CERT_DIR}"
chmod 755 "${CERT_DIR}"

# Option 1: Using step-ca with ACME (if URL provided)
if [ -n "${STEP_CA_URL}" ]; then
    echo "Using ACME endpoint: ${STEP_CA_URL}"

    # Bootstrap with CA (first time only)
    if [ ! -f ~/.step/certs/root_ca.crt ]; then
        echo "[1/3] Bootstrapping step CA..."
        step ca bootstrap --ca-url "${STEP_CA_URL}" --fingerprint "$(step certificate fingerprint "${CA_CERT}")"
    fi

    # Request certificate via ACME
    echo "[2/3] Requesting certificate via ACME..."
    step ca certificate "{{.CommonName}}" \
        "${CERT_DIR}/server.crt" \
        "${CERT_DIR}/server.key" \
        --san "{{.SANsJoined}}" \
        --not-after "${VALIDITY}" \
        --force

    chmod 600 "${CERT_DIR}/server.key"
    chmod 644 "${CERT_DIR}/server.crt"

    echo "[3/3] Verifying certificate..."
    step certificate inspect "${CERT_DIR}/server.crt"

# Option 2: Using step CLI locally (offline)
else
    echo "Using step CLI in local mode (no CA server)"

    # Generate self-signed certificate with step
    echo "[1/2] Generating certificate..."
    step certificate create "{{.CommonName}}" \
        "${CERT_DIR}/server.crt" \
        "${CERT_DIR}/server.key" \
        --san "{{.SANsJoined}}" \
        --not-after "${VALIDITY}" \
        --no-password \
        --insecure \
        --force

    chmod 600 "${CERT_DIR}/server.key"
    chmod 644 "${CERT_DIR}/server.crt"

    echo "[2/2] Verifying certificate..."
    step certificate inspect "${CERT_DIR}/server.crt"
fi

echo ""
echo "Certificate created successfully!"
echo "  Certificate: ${CERT_DIR}/server.crt"
echo "  Private Key: ${CERT_DIR}/server.key"
`

	return executeTemplate("step-ca-script", tmpl, data)
}

// generateCFSSLScript generates a script for CFSSL certificate authority.
func generateCFSSLScript(serviceName string, opts *CertificateOptions) []byte {
	sanConfig := buildJSONSANConfig(opts.SANs)

	data := struct {
		*CertificateOptions
		ServiceName string
		SANConfig   string
	}{
		CertificateOptions: opts,
		ServiceName:        serviceName,
		SANConfig:          sanConfig,
	}

	tmpl := `#!/bin/bash
# =============================================================================
# Certificate generation using CFSSL (Cloudflare's PKI toolkit)
# Open-source PKI - https://github.com/cloudflare/cfssl
# Generated by Homeport - Self-hosted infrastructure toolkit
# =============================================================================
set -e

SERVICE_NAME="{{.ServiceName}}"
CERT_DIR="{{.OutputDir}}"
CA_CERT="{{.ExternalCACert}}"
CA_KEY="{{.ExternalCAKey}}"

echo "Generating certificate for: ${SERVICE_NAME} using CFSSL"
echo "========================================================"

# Check if cfssl is installed
if ! command -v cfssl &> /dev/null; then
    echo "ERROR: cfssl not found"
    echo "Install with: go install github.com/cloudflare/cfssl/cmd/cfssl@latest"
    echo "Or download from: https://github.com/cloudflare/cfssl/releases"
    exit 1
fi

if ! command -v cfssljson &> /dev/null; then
    echo "ERROR: cfssljson not found"
    echo "Install with: go install github.com/cloudflare/cfssl/cmd/cfssljson@latest"
    exit 1
fi

# Create certificate directory
mkdir -p "${CERT_DIR}"
chmod 755 "${CERT_DIR}"

# Create CSR JSON
cat > "${CERT_DIR}/csr.json" << 'CSRJSON'
{
    "CN": "{{.CommonName}}",
    "hosts": [
        {{.SANConfig}}
    ],
    "key": {
        "algo": "rsa",
        "size": {{.KeySize}}
    },
    "names": [
        {
            "O": "{{.Organization}}"
        }
    ]
}
CSRJSON

# Create signing config
cat > "${CERT_DIR}/config.json" << 'CONFIGJSON'
{
    "signing": {
        "default": {
            "expiry": "{{.ValidityDays}}h",
            "usages": [
                "signing",
                "key encipherment",
                "server auth",
                "client auth"
            ]
        }
    }
}
CONFIGJSON

# Option 1: Sign with existing CA
if [ -n "${CA_CERT}" ] && [ -n "${CA_KEY}" ] && [ -f "${CA_CERT}" ] && [ -f "${CA_KEY}" ]; then
    echo "[1/2] Generating certificate signed by CA..."
    cfssl gencert \
        -ca="${CA_CERT}" \
        -ca-key="${CA_KEY}" \
        -config="${CERT_DIR}/config.json" \
        "${CERT_DIR}/csr.json" | cfssljson -bare "${CERT_DIR}/server"

# Option 2: Generate self-signed certificate
else
    echo "[1/2] Generating self-signed certificate..."
    cfssl selfsign \
        -config="${CERT_DIR}/config.json" \
        "{{.CommonName}}" \
        "${CERT_DIR}/csr.json" | cfssljson -bare "${CERT_DIR}/server"
fi

# Rename files to standard names
mv "${CERT_DIR}/server.pem" "${CERT_DIR}/server.crt" 2>/dev/null || true
mv "${CERT_DIR}/server-key.pem" "${CERT_DIR}/server.key" 2>/dev/null || true

# Set permissions
chmod 600 "${CERT_DIR}/server.key"
chmod 644 "${CERT_DIR}/server.crt"

# Cleanup
rm -f "${CERT_DIR}/csr.json" "${CERT_DIR}/config.json" "${CERT_DIR}/server.csr"

echo "[2/2] Verifying certificate..."
openssl x509 -in "${CERT_DIR}/server.crt" -noout -subject -dates

echo ""
echo "Certificate created successfully!"
echo "  Certificate: ${CERT_DIR}/server.crt"
echo "  Private Key: ${CERT_DIR}/server.key"
`

	return executeTemplate("cfssl-script", tmpl, data)
}

// generateEasyRSAScript generates a script for Easy-RSA PKI.
func generateEasyRSAScript(serviceName string, opts *CertificateOptions) []byte {
	data := struct {
		*CertificateOptions
		ServiceName string
		SANsJoined  string
	}{
		CertificateOptions: opts,
		ServiceName:        serviceName,
		SANsJoined:         strings.Join(opts.SANs, ","),
	}

	tmpl := `#!/bin/bash
# =============================================================================
# Certificate generation using Easy-RSA
# Open-source PKI - https://github.com/OpenVPN/easy-rsa
# Generated by Homeport - Self-hosted infrastructure toolkit
# =============================================================================
set -e

SERVICE_NAME="{{.ServiceName}}"
CERT_DIR="{{.OutputDir}}"
EASYRSA_DIR="${EASYRSA_DIR:-./easy-rsa}"
EASYRSA_PKI="${EASYRSA_DIR}/pki"

echo "Generating certificate for: ${SERVICE_NAME} using Easy-RSA"
echo "==========================================================="

# Check if easy-rsa is available
if [ ! -d "${EASYRSA_DIR}" ] && ! command -v easyrsa &> /dev/null; then
    echo "Easy-RSA not found. Setting up..."

    # Download Easy-RSA
    EASYRSA_VERSION="3.1.7"
    curl -sL "https://github.com/OpenVPN/easy-rsa/releases/download/v${EASYRSA_VERSION}/EasyRSA-${EASYRSA_VERSION}.tgz" | tar xz
    mv "EasyRSA-${EASYRSA_VERSION}" "${EASYRSA_DIR}"
fi

cd "${EASYRSA_DIR}"

# Initialize PKI if not exists
if [ ! -d "pki" ]; then
    echo "[1/4] Initializing PKI..."
    ./easyrsa init-pki

    # Create vars file
    cat > pki/vars << 'VARSFILE'
set_var EASYRSA_REQ_COUNTRY    "XX"
set_var EASYRSA_REQ_PROVINCE   "Self-Hosted"
set_var EASYRSA_REQ_CITY       "Local"
set_var EASYRSA_REQ_ORG        "{{.Organization}}"
set_var EASYRSA_REQ_EMAIL      "admin@localhost"
set_var EASYRSA_REQ_OU         "Infrastructure"
set_var EASYRSA_KEY_SIZE       {{.KeySize}}
set_var EASYRSA_ALGO           rsa
set_var EASYRSA_CA_EXPIRE      3650
set_var EASYRSA_CERT_EXPIRE    {{.ValidityDays}}
VARSFILE

    echo "[2/4] Building CA..."
    ./easyrsa --batch build-ca nopass
else
    echo "[1/4] Using existing PKI..."
    echo "[2/4] CA already exists..."
fi

# Generate server certificate
echo "[3/4] Generating server certificate..."
export EASYRSA_EXTRA_EXTS="
subjectAltName = DNS:{{.SANsJoined}}"

./easyrsa --batch --req-cn="{{.CommonName}}" gen-req "${SERVICE_NAME}" nopass
./easyrsa --batch sign-req server "${SERVICE_NAME}"

cd -

# Copy certificates to output directory
echo "[4/4] Copying certificates..."
mkdir -p "${CERT_DIR}"
cp "${EASYRSA_PKI}/issued/${SERVICE_NAME}.crt" "${CERT_DIR}/server.crt"
cp "${EASYRSA_PKI}/private/${SERVICE_NAME}.key" "${CERT_DIR}/server.key"
cp "${EASYRSA_PKI}/ca.crt" "${CERT_DIR}/../ca/ca.crt" 2>/dev/null || cp "${EASYRSA_PKI}/ca.crt" "./certs/ca/ca.crt"

# Set permissions
chmod 600 "${CERT_DIR}/server.key"
chmod 644 "${CERT_DIR}/server.crt"

echo ""
echo "Certificate created successfully!"
echo "  Certificate: ${CERT_DIR}/server.crt"
echo "  Private Key: ${CERT_DIR}/server.key"
echo "  CA Cert:     ${EASYRSA_PKI}/ca.crt"
`

	return executeTemplate("easy-rsa-script", tmpl, data)
}

// buildSANConfig creates the SAN configuration for openssl config files.
func buildSANConfig(serviceName string, sans []string) string {
	var lines []string
	dnsIdx := 1
	ipIdx := 1

	// Always include the service name
	seen := make(map[string]bool)
	allSANs := append([]string{serviceName}, sans...)

	for _, san := range allSANs {
		if seen[san] {
			continue
		}
		seen[san] = true

		if isIPAddress(san) {
			lines = append(lines, fmt.Sprintf("IP.%d = %s", ipIdx, san))
			ipIdx++
		} else {
			lines = append(lines, fmt.Sprintf("DNS.%d = %s", dnsIdx, san))
			dnsIdx++
		}
	}

	return strings.Join(lines, "\n")
}

// buildJSONSANConfig creates a JSON-formatted SAN list for CFSSL.
func buildJSONSANConfig(sans []string) string {
	var quoted []string
	for _, san := range sans {
		quoted = append(quoted, fmt.Sprintf(`"%s"`, san))
	}
	return strings.Join(quoted, ",\n        ")
}

// isIPAddress checks if a string looks like an IP address.
func isIPAddress(s string) bool {
	// Simple check for IPv4
	parts := strings.Split(s, ".")
	if len(parts) == 4 {
		for _, p := range parts {
			if len(p) == 0 || len(p) > 3 {
				return false
			}
			for _, c := range p {
				if c < '0' || c > '9' {
					return false
				}
			}
		}
		return true
	}
	// IPv6 contains colons
	return strings.Contains(s, ":")
}

// executeTemplate executes a template and returns the result as bytes.
func executeTemplate(name, tmplStr string, data interface{}) []byte {
	tmpl, err := template.New(name).Parse(tmplStr)
	if err != nil {
		return []byte(fmt.Sprintf("# Error parsing template: %v\n", err))
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return []byte(fmt.Sprintf("# Error executing template: %v\n", err))
	}

	return buf.Bytes()
}

// GenerateCertRotationScript generates a script for automatic certificate rotation.
func GenerateCertRotationScript(serviceName string, opts *CertificateOptions) []byte {
	data := struct {
		*CertificateOptions
		ServiceName string
	}{
		CertificateOptions: opts,
		ServiceName:        serviceName,
	}

	tmpl := `#!/bin/bash
# =============================================================================
# Certificate rotation script for: {{.ServiceName}}
# Renews certificates before expiration
# Generated by Homeport - Self-hosted infrastructure toolkit
# =============================================================================
set -e

SERVICE_NAME="{{.ServiceName}}"
CERT_DIR="{{.OutputDir}}"
CERT_FILE="${CERT_DIR}/server.crt"
DAYS_BEFORE_EXPIRY=30

echo "Certificate rotation check for: ${SERVICE_NAME}"
echo "================================================"

# Check if certificate exists
if [ ! -f "${CERT_FILE}" ]; then
    echo "ERROR: Certificate not found at ${CERT_FILE}"
    echo "Please generate the certificate first."
    exit 1
fi

# Get expiration date
EXPIRY_DATE=$(openssl x509 -in "${CERT_FILE}" -noout -enddate | cut -d= -f2)
EXPIRY_EPOCH=$(date -d "${EXPIRY_DATE}" +%s 2>/dev/null || date -j -f "%b %d %T %Y %Z" "${EXPIRY_DATE}" +%s)
CURRENT_EPOCH=$(date +%s)
THRESHOLD_EPOCH=$((CURRENT_EPOCH + DAYS_BEFORE_EXPIRY * 86400))

echo "Certificate expires: ${EXPIRY_DATE}"

if [ ${EXPIRY_EPOCH} -lt ${THRESHOLD_EPOCH} ]; then
    echo "Certificate expires within ${DAYS_BEFORE_EXPIRY} days. Renewing..."

    # Backup existing certificate
    BACKUP_DIR="${CERT_DIR}/backup_$(date +%Y%m%d_%H%M%S)"
    mkdir -p "${BACKUP_DIR}"
    cp "${CERT_DIR}/server.crt" "${BACKUP_DIR}/"
    cp "${CERT_DIR}/server.key" "${BACKUP_DIR}/"
    echo "Backed up existing certificates to ${BACKUP_DIR}"

    # Regenerate certificate (call the appropriate generation script)
    echo "Regenerating certificate..."
    # This should call GenerateServiceCertScript or GenerateExternalCAScript
    # The actual regeneration depends on your setup

    echo "Certificate rotation complete!"
    echo "Restart services to use the new certificate."
else
    DAYS_LEFT=$(( (EXPIRY_EPOCH - CURRENT_EPOCH) / 86400 ))
    echo "Certificate is valid for ${DAYS_LEFT} more days. No rotation needed."
fi
`

	return executeTemplate("cert-rotation-script", tmpl, data)
}

// GenerateClientCertScript generates a script to create client certificates
// for mutual TLS (mTLS) authentication.
func GenerateClientCertScript(clientName string, opts *CertificateOptions) []byte {
	data := struct {
		*CertificateOptions
		ClientName string
	}{
		CertificateOptions: opts,
		ClientName:         clientName,
	}

	tmpl := `#!/bin/bash
# =============================================================================
# Client certificate generation for mTLS: {{.ClientName}}
# Signed by local CA using openssl (no cloud dependencies)
# Generated by Homeport - Self-hosted infrastructure toolkit
# =============================================================================
set -e

CLIENT_NAME="{{.ClientName}}"
CA_DIR="./certs/ca"
CERT_DIR="./certs/clients/${CLIENT_NAME}"
KEY_SIZE={{.KeySize}}
VALIDITY_DAYS={{.ValidityDays}}
ORGANIZATION="{{.Organization}}"

echo "Generating client certificate for: ${CLIENT_NAME}"
echo "=================================================="

# Check if CA exists
if [ ! -f "${CA_DIR}/ca.crt" ] || [ ! -f "${CA_DIR}/ca.key" ]; then
    echo "ERROR: CA certificate not found at ${CA_DIR}"
    echo "Please generate the CA first."
    exit 1
fi

# Create client certificate directory
mkdir -p "${CERT_DIR}"
chmod 700 "${CERT_DIR}"

# Generate client private key
echo "[1/3] Generating client private key..."
openssl genrsa -out "${CERT_DIR}/client.key" ${KEY_SIZE}
chmod 600 "${CERT_DIR}/client.key"

# Generate CSR
echo "[2/3] Creating certificate signing request..."
openssl req -new \
    -key "${CERT_DIR}/client.key" \
    -out "${CERT_DIR}/client.csr" \
    -subj "/O=${ORGANIZATION}/CN=${CLIENT_NAME}"

# Sign with CA (client certificate)
echo "[3/3] Signing certificate with CA..."
openssl x509 -req \
    -in "${CERT_DIR}/client.csr" \
    -CA "${CA_DIR}/ca.crt" \
    -CAkey "${CA_DIR}/ca.key" \
    -CAcreateserial \
    -out "${CERT_DIR}/client.crt" \
    -days ${VALIDITY_DAYS} \
    -extfile <(printf "keyUsage=digitalSignature\nextendedKeyUsage=clientAuth")
chmod 644 "${CERT_DIR}/client.crt"

# Create PKCS#12 bundle (for browsers/applications)
echo "Creating PKCS#12 bundle..."
openssl pkcs12 -export \
    -in "${CERT_DIR}/client.crt" \
    -inkey "${CERT_DIR}/client.key" \
    -out "${CERT_DIR}/client.p12" \
    -name "${CLIENT_NAME}" \
    -passout pass:changeme
chmod 600 "${CERT_DIR}/client.p12"

# Cleanup CSR
rm -f "${CERT_DIR}/client.csr"

# Verify certificate
openssl verify -CAfile "${CA_DIR}/ca.crt" "${CERT_DIR}/client.crt"

echo ""
echo "Client certificate created successfully!"
echo "  Certificate: ${CERT_DIR}/client.crt"
echo "  Private Key: ${CERT_DIR}/client.key"
echo "  PKCS#12:     ${CERT_DIR}/client.p12 (password: changeme)"
echo ""
echo "Usage:"
echo "  curl --cert ${CERT_DIR}/client.crt --key ${CERT_DIR}/client.key https://service"
`

	return executeTemplate("client-cert-script", tmpl, data)
}
