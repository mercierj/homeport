package providers

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/homeport/homeport/internal/domain/secrets"
	"golang.org/x/term"
)

// ManualProvider prompts users for secret values interactively.
type ManualProvider struct {
	// Reader is the input reader (default: os.Stdin).
	Reader *bufio.Reader

	// Writer is the output writer (default: os.Stdout).
	Writer *os.File

	// cache stores already-entered secrets to avoid re-prompting.
	cache map[string]string

	// maskInput controls whether to hide input for passwords.
	maskInput bool

	// nonInteractive disables prompting (for testing).
	nonInteractive bool

	// defaultValues provides default values for secrets.
	defaultValues map[string]string
}

// NewManualProvider creates a new manual provider.
func NewManualProvider() *ManualProvider {
	return &ManualProvider{
		Reader:    bufio.NewReader(os.Stdin),
		Writer:    os.Stdout,
		cache:     make(map[string]string),
		maskInput: true,
	}
}

// WithMasking enables or disables input masking.
func (p *ManualProvider) WithMasking(mask bool) *ManualProvider {
	p.maskInput = mask
	return p
}

// WithDefaults sets default values for secrets.
func (p *ManualProvider) WithDefaults(defaults map[string]string) *ManualProvider {
	p.defaultValues = defaults
	return p
}

// SetNonInteractive disables interactive prompts.
func (p *ManualProvider) SetNonInteractive(nonInteractive bool) {
	p.nonInteractive = nonInteractive
}

// Name returns the provider identifier.
func (p *ManualProvider) Name() secrets.SecretSource {
	return secrets.SourceManual
}

// CanResolve checks if this provider can handle the secret reference.
func (p *ManualProvider) CanResolve(ref *secrets.SecretReference) bool {
	// Manual provider is the fallback for any secret
	return ref.Source == secrets.SourceManual || ref.Name != ""
}

// Resolve prompts the user for a secret value.
func (p *ManualProvider) Resolve(ctx context.Context, ref *secrets.SecretReference) (string, error) {
	// Check cache first
	if value, ok := p.cache[ref.Name]; ok {
		return value, nil
	}

	// Check defaults
	if p.defaultValues != nil {
		if value, ok := p.defaultValues[ref.Name]; ok {
			p.cache[ref.Name] = value
			return value, nil
		}
	}

	if p.nonInteractive {
		return "", fmt.Errorf("non-interactive mode: cannot prompt for secret %s", ref.Name)
	}

	// Prompt user
	value, err := p.prompt(ref)
	if err != nil {
		return "", err
	}

	// Cache the value
	p.cache[ref.Name] = value

	return value, nil
}

// prompt displays a prompt and reads user input.
func (p *ManualProvider) prompt(ref *secrets.SecretReference) (string, error) {
	// Display prompt
	promptText := p.buildPrompt(ref)
	_, _ = fmt.Fprint(p.Writer, promptText)

	var value string
	var err error

	// Determine if we should mask input
	shouldMask := p.maskInput && p.isSecretType(ref)

	if shouldMask {
		// Read password without echo
		bytePassword, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return "", fmt.Errorf("failed to read password: %w", err)
		}
		value = string(bytePassword)
		_, _ = fmt.Fprintln(p.Writer) // Add newline after hidden input
	} else {
		value, err = p.Reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}
		value = strings.TrimSpace(value)
	}

	if value == "" && ref.Required {
		return "", fmt.Errorf("secret %s is required but no value provided", ref.Name)
	}

	return value, nil
}

// buildPrompt creates the prompt text for a secret.
func (p *ManualProvider) buildPrompt(ref *secrets.SecretReference) string {
	var sb strings.Builder

	// Add description if available
	if ref.Description != "" {
		sb.WriteString(fmt.Sprintf("\n# %s\n", ref.Description))
	}

	// Add required/optional indicator
	if ref.Required {
		sb.WriteString("[REQUIRED] ")
	} else {
		sb.WriteString("[OPTIONAL] ")
	}

	// Add the actual prompt
	sb.WriteString(fmt.Sprintf("Enter value for %s", ref.Name))

	// Add type hint
	if ref.Type != "" && ref.Type != secrets.TypeGeneric {
		sb.WriteString(fmt.Sprintf(" (%s)", ref.Type))
	}

	sb.WriteString(": ")

	return sb.String()
}

// isSecretType determines if a secret should be masked.
func (p *ManualProvider) isSecretType(ref *secrets.SecretReference) bool {
	// Mask based on type
	switch ref.Type {
	case secrets.TypePassword, secrets.TypeAPIKey, secrets.TypePrivateKey:
		return true
	}

	// Mask based on name patterns
	nameLower := strings.ToLower(ref.Name)
	maskPatterns := []string{
		"password", "passwd", "secret", "key", "token",
		"credential", "auth", "private", "apikey", "api_key",
	}

	for _, pattern := range maskPatterns {
		if strings.Contains(nameLower, pattern) {
			return true
		}
	}

	return false
}

// ValidateConfig checks if the terminal supports interactive input.
func (p *ManualProvider) ValidateConfig() error {
	if p.nonInteractive {
		return nil
	}

	// Check if stdin is a terminal
	if !term.IsTerminal(int(syscall.Stdin)) {
		return fmt.Errorf("stdin is not a terminal, cannot prompt for secrets")
	}

	return nil
}

// ClearCache clears all cached secret values.
func (p *ManualProvider) ClearCache() {
	// Overwrite values before clearing
	for k, v := range p.cache {
		p.cache[k] = strings.Repeat("0", len(v))
	}
	p.cache = make(map[string]string)
}

// PreloadCache adds values to the cache without prompting.
func (p *ManualProvider) PreloadCache(values map[string]string) {
	for k, v := range values {
		p.cache[k] = v
	}
}

// PromptFunc returns a function suitable for use with Resolver.SetPromptFunc.
func (p *ManualProvider) PromptFunc() func(*secrets.SecretReference) (string, error) {
	return func(ref *secrets.SecretReference) (string, error) {
		return p.Resolve(context.Background(), ref)
	}
}

// PromptBatch prompts for multiple secrets at once.
func (p *ManualProvider) PromptBatch(ctx context.Context, refs []*secrets.SecretReference) (*secrets.ResolvedSecrets, error) {
	resolved := secrets.NewResolvedSecrets()

	_, _ = fmt.Fprintln(p.Writer, "")
	_, _ = fmt.Fprintln(p.Writer, "=== Secret Entry ===")
	_, _ = fmt.Fprintf(p.Writer, "Please provide values for %d secrets:\n", len(refs))

	for _, ref := range refs {
		value, err := p.Resolve(ctx, ref)
		if err != nil {
			if ref.Required {
				return resolved, fmt.Errorf("failed to get required secret %s: %w", ref.Name, err)
			}
			continue
		}
		resolved.Add(ref.Name, value, "manual", ref)
	}

	_, _ = fmt.Fprintln(p.Writer, "=== Secret Entry Complete ===")
	_, _ = fmt.Fprintln(p.Writer, "")

	return resolved, nil
}

// ConfirmSecrets displays entered secrets (masked) and asks for confirmation.
func (p *ManualProvider) ConfirmSecrets(refs []*secrets.SecretReference) (bool, error) {
	_, _ = fmt.Fprintln(p.Writer, "\nSecrets to be used:")

	for _, ref := range refs {
		value, ok := p.cache[ref.Name]
		if !ok {
			_, _ = fmt.Fprintf(p.Writer, "  %s: (not set)\n", ref.Name)
			continue
		}

		masked := maskValue(value)
		_, _ = fmt.Fprintf(p.Writer, "  %s: %s\n", ref.Name, masked)
	}

	_, _ = fmt.Fprint(p.Writer, "\nProceed with these secrets? [y/N]: ")

	answer, err := p.Reader.ReadString('\n')
	if err != nil {
		return false, err
	}

	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

// maskValue returns a masked version of a secret value.
func maskValue(value string) string {
	if len(value) == 0 {
		return "(empty)"
	}
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}
