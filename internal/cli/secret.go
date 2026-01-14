package cli

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/homeport/homeport/internal/cli/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	secretAPIURL     string
	secretReveal     bool
	secretDesc       string
	secretNewValue   string
	secretConfirm    bool
)

// secretCmd represents the secret command group
var secretCmd = &cobra.Command{
	Use:   "secret",
	Short: "Manage stack secrets",
	Long: `Manage secrets for deployed stacks.

The secret command provides functionality to create, read, update,
delete, and rotate secrets for your infrastructure stacks.

Secrets are stored securely and their values are masked by default
in command output. Use --reveal to show actual values.

Examples:
  # List all secrets for a stack
  homeport secret list my-stack

  # Get a secret value
  homeport secret get my-stack DATABASE_PASSWORD --reveal

  # Set a new secret
  homeport secret set my-stack API_KEY "my-secret-value"

  # Delete a secret
  homeport secret delete my-stack OLD_SECRET

  # Rotate a secret (generates new value)
  homeport secret rotate my-stack SESSION_KEY`,
}

// secretListCmd lists secrets for a stack
var secretListCmd = &cobra.Command{
	Use:   "list <stack-id>",
	Short: "List secrets for a stack",
	Long: `List all secrets for the specified stack.

Only secret names and metadata are shown by default.
Secret values are not displayed unless using the get command with --reveal.

Examples:
  homeport secret list my-stack`,
	Args: cobra.ExactArgs(1),
	RunE: runSecretList,
}

// secretGetCmd gets a secret value
var secretGetCmd = &cobra.Command{
	Use:   "get <stack-id> <key>",
	Short: "Get a secret value",
	Long: `Get the value of a specific secret.

The value is masked by default. Use --reveal to show the actual value.

Examples:
  homeport secret get my-stack DATABASE_PASSWORD
  homeport secret get my-stack API_KEY --reveal`,
	Args: cobra.ExactArgs(2),
	RunE: runSecretGet,
}

// secretSetCmd sets a secret value
var secretSetCmd = &cobra.Command{
	Use:   "set <stack-id> <key> <value>",
	Short: "Set a secret value",
	Long: `Set or update a secret value.

If the secret already exists, it will be updated.
If it doesn't exist, a new secret will be created.

Examples:
  homeport secret set my-stack DATABASE_PASSWORD "new-password"
  homeport secret set my-stack API_KEY "sk-abc123" --description "External API key"`,
	Args: cobra.ExactArgs(3),
	RunE: runSecretSet,
}

// secretDeleteCmd deletes a secret
var secretDeleteCmd = &cobra.Command{
	Use:   "delete <stack-id> <key>",
	Short: "Delete a secret",
	Long: `Delete a secret from the stack.

This action is irreversible.

Examples:
  homeport secret delete my-stack OLD_API_KEY
  homeport secret delete my-stack DEPRECATED_SECRET --yes`,
	Args: cobra.ExactArgs(2),
	RunE: runSecretDelete,
}

// secretRotateCmd rotates a secret
var secretRotateCmd = &cobra.Command{
	Use:   "rotate <stack-id> <key>",
	Short: "Rotate a secret value",
	Long: `Rotate a secret by generating a new random value.

This is useful for periodic credential rotation or after a security incident.
The old value will be replaced with a new randomly generated value.

Examples:
  homeport secret rotate my-stack SESSION_KEY
  homeport secret rotate my-stack ENCRYPTION_KEY --reveal`,
	Args: cobra.ExactArgs(2),
	RunE: runSecretRotate,
}

func init() {
	rootCmd.AddCommand(secretCmd)

	// Add subcommands
	secretCmd.AddCommand(secretListCmd)
	secretCmd.AddCommand(secretGetCmd)
	secretCmd.AddCommand(secretSetCmd)
	secretCmd.AddCommand(secretDeleteCmd)
	secretCmd.AddCommand(secretRotateCmd)

	// Global secret flags
	secretCmd.PersistentFlags().StringVar(&secretAPIURL, "api-url", "http://localhost:8080", "API server URL")
	_ = viper.BindPFlag("api_url", secretCmd.PersistentFlags().Lookup("api-url"))

	// Get command flags
	secretGetCmd.Flags().BoolVarP(&secretReveal, "reveal", "r", false, "reveal the secret value")

	// Set command flags
	secretSetCmd.Flags().StringVarP(&secretDesc, "description", "d", "", "secret description")

	// Delete command flags
	secretDeleteCmd.Flags().BoolVarP(&secretConfirm, "yes", "y", false, "skip confirmation prompt")

	// Rotate command flags
	secretRotateCmd.Flags().BoolVarP(&secretReveal, "reveal", "r", false, "reveal the new secret value")
	secretRotateCmd.Flags().StringVar(&secretNewValue, "value", "", "specific value to set (otherwise auto-generated)")
}

// SecretMetadata represents secret metadata from the API
type SecretMetadata struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SecretValue represents a secret with its value
type SecretValue struct {
	SecretMetadata
	Value string `json:"value"`
}

// SecretListResponse represents the list secrets API response
type SecretListResponse struct {
	Secrets []SecretMetadata `json:"secrets"`
	Count   int              `json:"count"`
}

func getSecretAPIURL() string {
	if url := viper.GetString("api_url"); url != "" {
		return url
	}
	return secretAPIURL
}

func runSecretList(cmd *cobra.Command, args []string) error {
	stackID := args[0]

	if !IsQuiet() {
		ui.Header("Homeport - Secret List")
		ui.Info(fmt.Sprintf("Stack: %s", stackID))
		ui.Divider()
	}

	apiURL := getSecretAPIURL()
	reqURL := fmt.Sprintf("%s/api/v1/stacks/%s/secrets", apiURL, url.PathEscape(stackID))

	resp, err := http.Get(reqURL)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", string(body))
	}

	var result SecretListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Secrets) == 0 {
		ui.Info("No secrets found")
		return nil
	}

	// Display secrets in a table
	table := ui.NewTable([]string{"Name", "Description", "Version", "Updated"})
	for _, secret := range result.Secrets {
		desc := secret.Description
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
		updatedStr := secret.UpdatedAt.Format("2006-01-02 15:04")
		table.AddRow([]string{
			secret.Name,
			desc,
			fmt.Sprintf("v%d", secret.Version),
			updatedStr,
		})
	}
	fmt.Println(table.Render())

	if !IsQuiet() {
		ui.Info(fmt.Sprintf("Total: %d secret(s)", result.Count))
	}

	return nil
}

func runSecretGet(cmd *cobra.Command, args []string) error {
	stackID := args[0]
	key := args[1]

	if !IsQuiet() {
		ui.Header("Homeport - Get Secret")
		ui.Info(fmt.Sprintf("Stack: %s", stackID))
		ui.Info(fmt.Sprintf("Key: %s", key))
		ui.Divider()
	}

	apiURL := getSecretAPIURL()
	reqURL := fmt.Sprintf("%s/api/v1/stacks/%s/secrets/%s", apiURL, url.PathEscape(stackID), url.PathEscape(key))

	resp, err := http.Get(reqURL)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("secret not found: %s", key)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", string(body))
	}

	var secret SecretValue
	if err := json.NewDecoder(resp.Body).Decode(&secret); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Display secret info
	if !IsQuiet() {
		ui.Info(fmt.Sprintf("Name: %s", secret.Name))
		if secret.Description != "" {
			ui.Info(fmt.Sprintf("Description: %s", secret.Description))
		}
		ui.Info(fmt.Sprintf("Version: v%d", secret.Version))
		ui.Info(fmt.Sprintf("Updated: %s", secret.UpdatedAt.Format("2006-01-02 15:04:05")))
		ui.Divider()
	}

	// Display value (masked or revealed)
	if secretReveal {
		fmt.Printf("Value: %s\n", secret.Value)
	} else {
		fmt.Printf("Value: %s\n", maskSecretValue(secret.Value))
		if !IsQuiet() {
			ui.Info("Use --reveal to show the actual value")
		}
	}

	return nil
}

func runSecretSet(cmd *cobra.Command, args []string) error {
	stackID := args[0]
	key := args[1]
	value := args[2]

	if !IsQuiet() {
		ui.Header("Homeport - Set Secret")
		ui.Info(fmt.Sprintf("Stack: %s", stackID))
		ui.Info(fmt.Sprintf("Key: %s", key))
		ui.Divider()
	}

	// Try to get existing secret to determine if this is create or update
	apiURL := getSecretAPIURL()
	getURL := fmt.Sprintf("%s/api/v1/stacks/%s/secrets/%s", apiURL, url.PathEscape(stackID), url.PathEscape(key))

	getResp, err := http.Get(getURL)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	getResp.Body.Close()

	var reqURL string
	var method string
	var payload interface{}

	if getResp.StatusCode == http.StatusOK {
		// Secret exists, update it
		reqURL = fmt.Sprintf("%s/api/v1/stacks/%s/secrets/%s", apiURL, url.PathEscape(stackID), url.PathEscape(key))
		method = http.MethodPut
		payload = map[string]string{
			"value": value,
		}
		if IsVerbose() {
			ui.Info("Updating existing secret...")
		}
	} else {
		// Secret doesn't exist, create it
		reqURL = fmt.Sprintf("%s/api/v1/stacks/%s/secrets", apiURL, url.PathEscape(stackID))
		method = http.MethodPost
		createPayload := map[string]string{
			"name":  key,
			"value": value,
		}
		if secretDesc != "" {
			createPayload["description"] = secretDesc
		}
		payload = createPayload
		if IsVerbose() {
			ui.Info("Creating new secret...")
		}
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	req, err := http.NewRequest(method, reqURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", string(body))
	}

	var result SecretMetadata
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !IsQuiet() {
		ui.Divider()
		if method == http.MethodPost {
			ui.Success("Secret created successfully")
		} else {
			ui.Success("Secret updated successfully")
		}
		ui.Info(fmt.Sprintf("Version: v%d", result.Version))
	}

	return nil
}

func runSecretDelete(cmd *cobra.Command, args []string) error {
	stackID := args[0]
	key := args[1]

	if !IsQuiet() {
		ui.Header("Homeport - Delete Secret")
		ui.Info(fmt.Sprintf("Stack: %s", stackID))
		ui.Info(fmt.Sprintf("Key: %s", key))
		ui.Divider()
	}

	// Confirm deletion unless --yes flag is set
	if !secretConfirm && !IsQuiet() {
		ui.Warning("This action is irreversible. The secret will be permanently deleted.")
		if !ui.PromptYesNo("Delete secret", false) {
			ui.Info("Deletion cancelled")
			return nil
		}
	}

	apiURL := getSecretAPIURL()
	reqURL := fmt.Sprintf("%s/api/v1/stacks/%s/secrets/%s", apiURL, url.PathEscape(stackID), url.PathEscape(key))

	req, err := http.NewRequest(http.MethodDelete, reqURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("secret not found: %s", key)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", string(body))
	}

	if !IsQuiet() {
		ui.Divider()
		ui.Success("Secret deleted successfully")
	}

	return nil
}

func runSecretRotate(cmd *cobra.Command, args []string) error {
	stackID := args[0]
	key := args[1]

	if !IsQuiet() {
		ui.Header("Homeport - Rotate Secret")
		ui.Info(fmt.Sprintf("Stack: %s", stackID))
		ui.Info(fmt.Sprintf("Key: %s", key))
		ui.Divider()
	}

	// Generate a new value if not provided
	newValue := secretNewValue
	if newValue == "" {
		// Generate a random 32-character string
		newValue = generateRandomSecret(32)
		if IsVerbose() {
			ui.Info("Generated new random value")
		}
	}

	// Update the secret with the new value
	apiURL := getSecretAPIURL()
	reqURL := fmt.Sprintf("%s/api/v1/stacks/%s/secrets/%s", apiURL, url.PathEscape(stackID), url.PathEscape(key))

	payload := map[string]string{
		"value": newValue,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPut, reqURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("secret not found: %s", key)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", string(body))
	}

	var result SecretMetadata
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !IsQuiet() {
		ui.Divider()
		ui.Success("Secret rotated successfully")
		ui.Info(fmt.Sprintf("Version: v%d", result.Version))

		if secretReveal {
			fmt.Printf("New value: %s\n", newValue)
		} else {
			fmt.Printf("New value: %s\n", maskSecretValue(newValue))
			ui.Info("Use --reveal to show the new value")
		}
	}

	return nil
}

// maskSecretValue masks a secret value for display
func maskSecretValue(value string) string {
	if len(value) == 0 {
		return ""
	}
	if len(value) <= 4 {
		return strings.Repeat("*", len(value))
	}
	// Show first 2 and last 2 characters, mask the rest
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}

// generateRandomSecret generates a cryptographically secure random secret of the specified length
func generateRandomSecret(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"

	b := make([]byte, length)
	charsetLen := big.NewInt(int64(len(charset)))

	for i := range b {
		n, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			// Fallback to less secure method if crypto/rand fails
			b[i] = charset[i%len(charset)]
			continue
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}
