package secrets

import "testing"

func TestIsSensitiveEnvName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"PASSWORD", "PASSWORD", true},
		{"DB_PASSWORD", "DB_PASSWORD", true},
		{"DATABASE_PASSWORD", "DATABASE_PASSWORD", true},
		{"api_key", "api_key", true},
		{"API_KEY", "API_KEY", true},
		{"secret", "secret", true},
		{"SECRET_KEY", "SECRET_KEY", true},
		{"token", "token", true},
		{"AUTH_TOKEN", "AUTH_TOKEN", true},
		{"access_token", "access_token", true},
		{"PRIVATE_KEY", "PRIVATE_KEY", true},
		{"SSH_KEY", "SSH_KEY", true},
		{"JWT_SECRET", "JWT_SECRET", true},
		{"CREDENTIAL", "CREDENTIAL", true},
		{"APP_SECRET", "APP_SECRET", true},
		{"ENCRYPTION_KEY", "ENCRYPTION_KEY", true},

		// Non-sensitive names
		{"LOG_LEVEL", "LOG_LEVEL", false},
		{"PORT", "PORT", false},
		{"HOST", "HOST", false},
		{"DATABASE_NAME", "DATABASE_NAME", false},
		{"APP_NAME", "APP_NAME", false},
		{"DEBUG", "DEBUG", false},
		{"NODE_ENV", "NODE_ENV", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsSensitiveEnvName(tt.input)
			if result != tt.expected {
				t.Errorf("IsSensitiveEnvName(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestInferSecretType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected SecretType
	}{
		{"password", "PASSWORD", TypePassword},
		{"db_password", "DB_PASSWORD", TypePassword},
		{"passwd", "ADMIN_PASSWD", TypePassword},
		{"api_key", "API_KEY", TypeAPIKey},
		{"token", "AUTH_TOKEN", TypeAPIKey},
		{"certificate", "TLS_CERT", TypeCertificate},
		{"private_key", "SSH_PRIVATE_KEY", TypePrivateKey},
		{"tls_key", "TLS_KEY", TypePrivateKey},
		{"connection_string", "DATABASE_CONNECTION_STRING", TypeConnectionString},
		{"db_url", "DATABASE_URL", TypeConnectionString},
		{"generic", "SOME_SECRET", TypeGeneric},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := InferSecretType(tt.input)
			if result != tt.expected {
				t.Errorf("InferSecretType(%q) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeEnvName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "password", "PASSWORD"},
		{"already_upper", "PASSWORD", "PASSWORD"},
		{"with_hyphen", "my-secret-key", "MY_SECRET_KEY"},
		{"with_dots", "app.database.password", "APP_DATABASE_PASSWORD"},
		{"with_slash", "prod/db/password", "PROD_DB_PASSWORD"},
		{"with_spaces", "my secret", "MY_SECRET"},
		{"starting_with_number", "123_secret", "VAR_123_SECRET"},
		{"special_chars", "my@secret#key!", "MYSECRETKEY"},
		{"double_underscore", "my__secret", "MY_SECRET"},
		{"leading_underscore", "_secret", "SECRET"},
		{"trailing_underscore", "secret_", "SECRET"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeEnvName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeEnvName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGenerateSecretName(t *testing.T) {
	tests := []struct {
		name         string
		resourceName string
		resourceType string
		fieldName    string
		expected     string
	}{
		{
			name:         "rds_password",
			resourceName: "prod-db",
			resourceType: "rds",
			fieldName:    "password",
			expected:     "PROD_DB_PASSWORD", // No duplicate "DB" since base contains "DB"
		},
		{
			name:         "cache_auth",
			resourceName: "cache-cluster",
			resourceType: "elasticache",
			fieldName:    "auth_token",
			expected:     "CACHE_CLUSTER_AUTH_TOKEN", // No "CACHE" hint since base contains "CACHE"
		},
		{
			name:         "lambda_env",
			resourceName: "api-handler",
			resourceType: "lambda",
			fieldName:    "api_key",
			expected:     "API_HANDLER_FN_API_KEY",
		},
		{
			name:         "generic",
			resourceName: "my-service",
			resourceType: "unknown",
			fieldName:    "secret",
			expected:     "MY_SERVICE_SECRET",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateSecretName(tt.resourceName, tt.resourceType, tt.fieldName)
			if result != tt.expected {
				t.Errorf("GenerateSecretName(%q, %q, %q) = %q, expected %q",
					tt.resourceName, tt.resourceType, tt.fieldName, result, tt.expected)
			}
		})
	}
}

func TestExtractSecretKeyFromARN(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "full_arn_with_suffix",
			input:    "arn:aws:secretsmanager:us-east-1:123456789012:secret:prod/db-password-abc123",
			expected: "prod/db-password",
		},
		{
			name:     "arn_with_6char_suffix",
			input:    "arn:aws:secretsmanager:us-east-1:123456789012:secret:simple-secret",
			expected: "simple", // "secret" is 6 chars after hyphen, treated as suffix
		},
		{
			name:     "arn_no_suffix_pattern",
			input:    "arn:aws:secretsmanager:us-east-1:123456789012:secret:my-db-password",
			expected: "my-db-password", // "password" is 8 chars, not a suffix
		},
		{
			name:     "not_an_arn",
			input:    "my-secret-name",
			expected: "my-secret-name",
		},
		{
			name:     "nested_path",
			input:    "arn:aws:secretsmanager:eu-west-1:123:secret:app/prod/db-creds-xyz789",
			expected: "app/prod/db-creds",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSecretKeyFromARN(tt.input)
			if result != tt.expected {
				t.Errorf("ExtractSecretKeyFromARN(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}
