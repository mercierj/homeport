// Package httputil provides HTTP utilities including credential sanitization.
package httputil

import (
	"regexp"
)

var sensitivePatterns = []*regexp.Regexp{
	// PostgreSQL connection string: postgres://user:PASSWORD@host:port/db
	regexp.MustCompile(`(postgres://[^:]+:)[^@]+(@)`),
	// MySQL connection string: user:PASSWORD@tcp(host:port)/db
	regexp.MustCompile(`([a-zA-Z0-9_]+:)[^@]+(@tcp\()`),
	// Redis connection string: redis://user:PASSWORD@host:port
	regexp.MustCompile(`(redis://[^:]*:)[^@]+(@)`),
	// MongoDB connection string: mongodb://user:PASSWORD@host:port
	regexp.MustCompile(`(mongodb(\+srv)?://[^:]+:)[^@]+(@)`),
	// Generic URL with credentials: protocol://user:PASSWORD@host
	regexp.MustCompile(`(://[^:]+:)[^@]+(@)`),
	// Generic password in key=value format
	regexp.MustCompile(`(?i)(password\s*[=:]\s*)[^\s&;,}]+`),
	// Generic pwd in key=value format
	regexp.MustCompile(`(?i)(pwd\s*[=:]\s*)[^\s&;,}]+`),
	// Secret key patterns
	regexp.MustCompile(`(?i)(secret[_-]?key\s*[=:]\s*)[^\s&;,}]+`),
	// Access key patterns
	regexp.MustCompile(`(?i)(access[_-]?key[_-]?id\s*[=:]\s*)[^\s&;,}]+`),
	// AWS secret patterns
	regexp.MustCompile(`(?i)(aws[_-]?secret[_-]?access[_-]?key\s*[=:]\s*)[^\s&;,}]+`),
	// API key patterns
	regexp.MustCompile(`(?i)(api[_-]?key\s*[=:]\s*)[^\s&;,}]+`),
	// Token patterns
	regexp.MustCompile(`(?i)(auth[_-]?token\s*[=:]\s*)[^\s&;,}]+`),
	regexp.MustCompile(`(?i)(bearer\s+)[^\s]+`),
	// Connection strings with password parameter
	regexp.MustCompile(`(?i)([\?&]password=)[^&\s]+`),
	// Private key patterns (JSON)
	regexp.MustCompile(`(?i)("private[_-]?key"\s*:\s*")[^"]+(")`),
	// Credential patterns (JSON)
	regexp.MustCompile(`(?i)("credentials?"\s*:\s*")[^"]+(")`),
}

const maskedValue = "***MASKED***"

// SanitizeError masks sensitive data in error messages.
func SanitizeError(err error) string {
	if err == nil {
		return ""
	}
	return SanitizeString(err.Error())
}

// SanitizeString masks sensitive data in a string.
func SanitizeString(s string) string {
	if s == "" {
		return s
	}
	result := s
	for _, pattern := range sensitivePatterns {
		result = pattern.ReplaceAllString(result, "${1}"+maskedValue+"${2}")
	}
	return result
}
