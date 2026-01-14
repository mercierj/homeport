// Package secrets provides pattern matching for detecting sensitive values in resources.
package secrets

import (
	"regexp"
	"strings"
)

// SensitivePatterns contains regex patterns to detect sensitive environment variable names.
var SensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^.*PASSWORD.*$`),
	regexp.MustCompile(`(?i)^.*SECRET.*$`),
	regexp.MustCompile(`(?i)^.*API[_-]?KEY.*$`),
	regexp.MustCompile(`(?i)^.*TOKEN.*$`),
	regexp.MustCompile(`(?i)^.*CREDENTIAL.*$`),
	regexp.MustCompile(`(?i)^.*AUTH.*$`),
	regexp.MustCompile(`(?i)^.*PRIVATE[_-]?KEY.*$`),
	regexp.MustCompile(`(?i)^.*ACCESS[_-]?KEY.*$`),
	regexp.MustCompile(`(?i)^.*CLIENT[_-]?SECRET.*$`),
	regexp.MustCompile(`(?i)^.*ENCRYPTION[_-]?KEY.*$`),
	regexp.MustCompile(`(?i)^.*SIGNING[_-]?KEY.*$`),
	regexp.MustCompile(`(?i)^.*MASTER[_-]?KEY.*$`),
	regexp.MustCompile(`(?i)^.*DB[_-]?PASS.*$`),
	regexp.MustCompile(`(?i)^.*DATABASE[_-]?PASS.*$`),
	regexp.MustCompile(`(?i)^.*CERT.*$`),
	regexp.MustCompile(`(?i)^.*SSH[_-]?KEY.*$`),
	regexp.MustCompile(`(?i)^.*RSA[_-]?KEY.*$`),
	regexp.MustCompile(`(?i)^.*JWT[_-]?SECRET.*$`),
	regexp.MustCompile(`(?i)^.*HMAC.*$`),
	regexp.MustCompile(`(?i)^.*BEARER.*$`),
}

// ExplicitSecretNames are names that are always considered sensitive.
var ExplicitSecretNames = map[string]bool{
	"PASSWORD":          true,
	"SECRET":            true,
	"API_KEY":           true,
	"APIKEY":            true,
	"TOKEN":             true,
	"AUTH_TOKEN":        true,
	"ACCESS_TOKEN":      true,
	"REFRESH_TOKEN":     true,
	"PRIVATE_KEY":       true,
	"SECRET_KEY":        true,
	"ENCRYPTION_KEY":    true,
	"MASTER_PASSWORD":   true,
	"DB_PASSWORD":       true,
	"DATABASE_PASSWORD": true,
	"REDIS_PASSWORD":    true,
	"POSTGRES_PASSWORD": true,
	"MYSQL_PASSWORD":    true,
	"MONGO_PASSWORD":    true,
	"JWT_SECRET":        true,
	"SESSION_SECRET":    true,
	"COOKIE_SECRET":     true,
	"SIGNING_KEY":       true,
	"SSH_KEY":           true,
	"SSH_PRIVATE_KEY":   true,
	"TLS_KEY":           true,
	"SSL_KEY":           true,
}

// IsSensitiveEnvName checks if an environment variable name likely contains a secret.
func IsSensitiveEnvName(name string) bool {
	// Check explicit names first (faster)
	upperName := strings.ToUpper(name)
	if ExplicitSecretNames[upperName] {
		return true
	}

	// Check patterns
	for _, pattern := range SensitivePatterns {
		if pattern.MatchString(name) {
			return true
		}
	}

	return false
}

// InferSecretType guesses the secret type from its name.
func InferSecretType(name string) SecretType {
	upper := strings.ToUpper(name)

	switch {
	case strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "PASSWD"):
		return TypePassword
	case strings.Contains(upper, "API_KEY") || strings.Contains(upper, "APIKEY"):
		return TypeAPIKey
	case strings.Contains(upper, "TOKEN"):
		return TypeAPIKey
	case strings.Contains(upper, "CERT"):
		return TypeCertificate
	case strings.Contains(upper, "PRIVATE_KEY") || strings.Contains(upper, "SSH_KEY") ||
		strings.Contains(upper, "RSA_KEY") || strings.Contains(upper, "TLS_KEY"):
		return TypePrivateKey
	case strings.Contains(upper, "CONNECTION_STRING") || strings.Contains(upper, "DATABASE_URL") ||
		strings.Contains(upper, "DB_URL") || strings.Contains(upper, "REDIS_URL"):
		return TypeConnectionString
	default:
		return TypeGeneric
	}
}

// NormalizeEnvName converts a string to a valid environment variable name.
// Example: "my-secret-key" -> "MY_SECRET_KEY"
func NormalizeEnvName(name string) string {
	// Replace common separators with underscore
	result := strings.ReplaceAll(name, "-", "_")
	result = strings.ReplaceAll(result, ".", "_")
	result = strings.ReplaceAll(result, "/", "_")
	result = strings.ReplaceAll(result, " ", "_")

	// Convert to uppercase
	result = strings.ToUpper(result)

	// Remove any non-alphanumeric characters except underscore
	var cleaned strings.Builder
	for _, r := range result {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			cleaned.WriteRune(r)
		}
	}

	result = cleaned.String()

	// Ensure it starts with a letter
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "VAR_" + result
	}

	// Remove consecutive underscores
	for strings.Contains(result, "__") {
		result = strings.ReplaceAll(result, "__", "_")
	}

	// Trim leading/trailing underscores
	result = strings.Trim(result, "_")

	return result
}

// GenerateSecretName creates a descriptive secret name from resource and field info.
// Example: GenerateSecretName("prod-db", "postgres", "password") -> "PROD_DB_DB_PASSWORD"
func GenerateSecretName(resourceName, resourceType, fieldName string) string {
	// Normalize resource name
	baseName := NormalizeEnvName(resourceName)

	// Add type hint for disambiguation
	typeHint := ""
	switch {
	case strings.Contains(resourceType, "rds") || strings.Contains(resourceType, "postgres") ||
		strings.Contains(resourceType, "mysql") || strings.Contains(resourceType, "sql"):
		typeHint = "DB"
	case strings.Contains(resourceType, "redis") || strings.Contains(resourceType, "elasticache") ||
		strings.Contains(resourceType, "cache"):
		typeHint = "CACHE"
	case strings.Contains(resourceType, "lambda") || strings.Contains(resourceType, "function"):
		typeHint = "FN"
	}

	// Normalize field name
	fieldPart := NormalizeEnvName(fieldName)

	// Build the final name
	parts := []string{baseName}
	if typeHint != "" && !strings.Contains(baseName, typeHint) {
		parts = append(parts, typeHint)
	}
	parts = append(parts, fieldPart)

	return strings.Join(parts, "_")
}

// ExtractSecretKeyFromARN extracts the secret name/path from a Secrets Manager ARN.
// Example: "arn:aws:secretsmanager:us-east-1:123:secret:prod/db-password-xyz" -> "prod/db-password"
func ExtractSecretKeyFromARN(arn string) string {
	// AWS Secrets Manager ARN format:
	// arn:aws:secretsmanager:region:account:secret:name-randomsuffix
	parts := strings.Split(arn, ":")
	if len(parts) < 7 {
		return arn // Return as-is if not a valid ARN
	}

	secretPart := parts[6]

	// Remove the random suffix (last 6 chars after the hyphen)
	if idx := strings.LastIndex(secretPart, "-"); idx > 0 && len(secretPart)-idx == 7 {
		secretPart = secretPart[:idx]
	}

	return secretPart
}
