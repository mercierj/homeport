package providers

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeport/homeport/internal/domain/secrets"
)

// FileProvider resolves secrets from local files.
// Supports reading individual files or parsing .env-style files.
type FileProvider struct {
	// BasePath is the base directory for relative file paths.
	BasePath string

	// EnvFileCache caches parsed .env files.
	envFileCache map[string]map[string]string
}

// NewFileProvider creates a new file provider.
func NewFileProvider() *FileProvider {
	return &FileProvider{
		envFileCache: make(map[string]map[string]string),
	}
}

// WithBasePath sets the base path for file resolution.
func (p *FileProvider) WithBasePath(path string) *FileProvider {
	p.BasePath = path
	return p
}

// Name returns the provider identifier.
func (p *FileProvider) Name() secrets.SecretSource {
	return secrets.SourceFile
}

// CanResolve checks if this provider can handle the secret reference.
func (p *FileProvider) CanResolve(ref *secrets.SecretReference) bool {
	return ref.Source == secrets.SourceFile && ref.Key != ""
}

// Resolve retrieves a secret from a file.
func (p *FileProvider) Resolve(ctx context.Context, ref *secrets.SecretReference) (string, error) {
	if !p.CanResolve(ref) {
		return "", fmt.Errorf("cannot resolve secret %s: invalid source or missing key", ref.Name)
	}

	// Parse the key - formats:
	// - "/path/to/file" - read entire file as secret
	// - "/path/to/file.env:KEY_NAME" - read specific key from env file
	// - "/path/to/file.json:.key.path" - read JSON path (not implemented yet)

	filePath := ref.Key
	keyName := ""

	// Check for .env file format with key
	if idx := strings.LastIndex(filePath, ":"); idx != -1 {
		// Make sure it's not a Windows path (C:\ etc)
		if idx > 0 && (filePath[idx-1] != '\\' && (idx < 2 || filePath[idx-2] != ':')) {
			keyName = filePath[idx+1:]
			filePath = filePath[:idx]
		}
	}

	// Resolve relative paths
	if !filepath.IsAbs(filePath) {
		if p.BasePath != "" {
			filePath = filepath.Join(p.BasePath, filePath)
		} else {
			cwd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("failed to get working directory: %w", err)
			}
			filePath = filepath.Join(cwd, filePath)
		}
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", fmt.Errorf("secret file not found: %s", filePath)
	}

	// If key is specified, parse as env file
	if keyName != "" {
		return p.resolveFromEnvFile(filePath, keyName)
	}

	// Read entire file as secret
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read secret file: %w", err)
	}

	return strings.TrimSpace(string(content)), nil
}

// resolveFromEnvFile reads a specific key from an env file.
func (p *FileProvider) resolveFromEnvFile(filePath, keyName string) (string, error) {
	// Check cache
	if env, ok := p.envFileCache[filePath]; ok {
		if value, ok := env[keyName]; ok {
			return value, nil
		}
		return "", fmt.Errorf("key %s not found in %s", keyName, filePath)
	}

	// Parse env file
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open env file: %w", err)
	}
	defer func() { _ = file.Close() }()

	env := make(map[string]string)
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Find the first =
		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		// Remove quotes
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		env[key] = value
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to parse env file: %w", err)
	}

	// Cache the parsed file
	p.envFileCache[filePath] = env

	if value, ok := env[keyName]; ok {
		return value, nil
	}

	return "", fmt.Errorf("key %s not found in %s", keyName, filePath)
}

// ValidateConfig checks if the base path exists.
func (p *FileProvider) ValidateConfig() error {
	if p.BasePath != "" {
		if _, err := os.Stat(p.BasePath); os.IsNotExist(err) {
			return fmt.Errorf("base path does not exist: %s", p.BasePath)
		}
	}
	return nil
}

// ClearCache clears the env file cache.
func (p *FileProvider) ClearCache() {
	p.envFileCache = make(map[string]map[string]string)
}

// LoadEnvFile pre-loads an env file into the cache.
func (p *FileProvider) LoadEnvFile(filePath string) error {
	// Force a load by reading a dummy key
	_, _ = p.resolveFromEnvFile(filePath, "__dummy__")
	return nil
}

// ListKeysInEnvFile returns all keys in an env file.
func (p *FileProvider) ListKeysInEnvFile(filePath string) ([]string, error) {
	// Ensure file is loaded
	_ = p.LoadEnvFile(filePath)

	if env, ok := p.envFileCache[filePath]; ok {
		keys := make([]string, 0, len(env))
		for k := range env {
			keys = append(keys, k)
		}
		return keys, nil
	}

	return nil, fmt.Errorf("could not load env file: %s", filePath)
}
