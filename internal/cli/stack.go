package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/homeport/homeport/internal/cli/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	stackAPIHost string
	stackAPIPort int
)

// stackCmd represents the stack command
var stackCmd = &cobra.Command{
	Use:   "stack",
	Short: "Manage deployment stacks",
	Long: `Manage deployment stacks for your self-hosted infrastructure.

Stacks are collections of Docker services defined by a docker-compose file.
Use the subcommands to list, start, stop, and manage your stacks.

Examples:
  homeport stack list              # List all stacks
  homeport stack status my-stack   # Show stack health status
  homeport stack start my-stack    # Start all services in stack
  homeport stack stop my-stack     # Stop all services in stack
  homeport stack restart my-stack  # Restart all services`,
}

// stackListCmd represents the stack list command
var stackListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all stacks",
	Long: `List all deployment stacks.

Examples:
  homeport stack list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !IsQuiet() {
			ui.Header("Homeport - Stack List")
			ui.Divider()
		}

		resp, err := stackAPICall("GET", "/stacks", nil)
		if err != nil {
			return fmt.Errorf("failed to list stacks: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return handleAPIError(resp)
		}

		var result struct {
			Stacks []struct {
				ID          string    `json:"id"`
				Name        string    `json:"name"`
				Description string    `json:"description"`
				Status      string    `json:"status"`
				CreatedAt   time.Time `json:"created_at"`
				UpdatedAt   time.Time `json:"updated_at"`
			} `json:"stacks"`
			Count int `json:"count"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if len(result.Stacks) == 0 {
			ui.Info("No stacks found")
			return nil
		}

		table := ui.NewTable([]string{"ID", "Name", "Status", "Description", "Updated"})
		for _, stack := range result.Stacks {
			table.AddRow([]string{
				stack.ID,
				stack.Name,
				stack.Status,
				truncate(stack.Description, 30),
				stack.UpdatedAt.Format("2006-01-02 15:04"),
			})
		}
		fmt.Println(table.Render())

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Found %d stack(s)", result.Count))
		}

		return nil
	},
}

// stackStatusCmd represents the stack status command
var stackStatusCmd = &cobra.Command{
	Use:   "status [stack-id]",
	Short: "Show stack health status",
	Long: `Show detailed health status of a stack and its services.

Examples:
  homeport stack status my-stack`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stackID := args[0]

		if !IsQuiet() {
			ui.Header("Homeport - Stack Status")
			ui.Info(fmt.Sprintf("Stack: %s", stackID))
			ui.Divider()
		}

		resp, err := stackAPICall("GET", fmt.Sprintf("/stacks/%s/status", stackID), nil)
		if err != nil {
			return fmt.Errorf("failed to get stack status: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return handleAPIError(resp)
		}

		var result struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Status      string `json:"status"`
			Health      string `json:"health"`
			ServiceCount int   `json:"service_count"`
			Services    []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
				Health string `json:"health"`
				Ports  string `json:"ports"`
			} `json:"services"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		fmt.Printf("Stack: %s\n", result.Name)
		fmt.Printf("Status: %s\n", result.Status)
		fmt.Printf("Health: %s\n", result.Health)
		fmt.Println()

		if len(result.Services) > 0 {
			table := ui.NewTable([]string{"Service", "Status", "Health", "Ports"})
			for _, svc := range result.Services {
				table.AddRow([]string{
					svc.Name,
					svc.Status,
					svc.Health,
					svc.Ports,
				})
			}
			fmt.Println(table.Render())
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Stack has %d service(s)", result.ServiceCount))
		}

		return nil
	},
}

// stackStartCmd represents the stack start command
var stackStartCmd = &cobra.Command{
	Use:   "start [stack-id]",
	Short: "Start all services in stack",
	Long: `Start all services defined in the stack.

Examples:
  homeport stack start my-stack`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stackID := args[0]

		if !IsQuiet() {
			ui.Header("Homeport - Start Stack")
			ui.Info(fmt.Sprintf("Starting stack: %s", stackID))
			ui.Divider()
		}

		resp, err := stackAPICall("POST", fmt.Sprintf("/stacks/%s/start", stackID), nil)
		if err != nil {
			return fmt.Errorf("failed to start stack: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return handleAPIError(resp)
		}

		var result struct {
			Status string `json:"status"`
			ID     string `json:"id"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Stack %s is %s", stackID, result.Status))
		}

		return nil
	},
}

// stackStopCmd represents the stack stop command
var stackStopCmd = &cobra.Command{
	Use:   "stop [stack-id]",
	Short: "Stop all services in stack",
	Long: `Stop all services defined in the stack.

Examples:
  homeport stack stop my-stack`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stackID := args[0]

		if !IsQuiet() {
			ui.Header("Homeport - Stop Stack")
			ui.Info(fmt.Sprintf("Stopping stack: %s", stackID))
			ui.Divider()
		}

		resp, err := stackAPICall("POST", fmt.Sprintf("/stacks/%s/stop", stackID), nil)
		if err != nil {
			return fmt.Errorf("failed to stop stack: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return handleAPIError(resp)
		}

		var result struct {
			Status string `json:"status"`
			ID     string `json:"id"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Stack %s is %s", stackID, result.Status))
		}

		return nil
	},
}

// stackRestartCmd represents the stack restart command
var stackRestartCmd = &cobra.Command{
	Use:   "restart [stack-id]",
	Short: "Restart all services in stack",
	Long: `Restart all services defined in the stack.

Examples:
  homeport stack restart my-stack`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stackID := args[0]

		if !IsQuiet() {
			ui.Header("Homeport - Restart Stack")
			ui.Info(fmt.Sprintf("Restarting stack: %s", stackID))
			ui.Divider()
		}

		resp, err := stackAPICall("POST", fmt.Sprintf("/stacks/%s/restart", stackID), nil)
		if err != nil {
			return fmt.Errorf("failed to restart stack: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return handleAPIError(resp)
		}

		var result struct {
			Status string `json:"status"`
			ID     string `json:"id"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Stack %s is %s", stackID, result.Status))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(stackCmd)

	// Global flags for stack commands
	stackCmd.PersistentFlags().StringVar(&stackAPIHost, "api-host", "localhost", "API server host")
	stackCmd.PersistentFlags().IntVar(&stackAPIPort, "api-port", 8080, "API server port")

	// Bind flags to viper
	_ = viper.BindPFlag("api.host", stackCmd.PersistentFlags().Lookup("api-host"))
	_ = viper.BindPFlag("api.port", stackCmd.PersistentFlags().Lookup("api-port"))

	// Add subcommands
	stackCmd.AddCommand(stackListCmd)
	stackCmd.AddCommand(stackStatusCmd)
	stackCmd.AddCommand(stackStartCmd)
	stackCmd.AddCommand(stackStopCmd)
	stackCmd.AddCommand(stackRestartCmd)
}

// stackAPICall makes an HTTP request to the stack API
func stackAPICall(method, path string, body interface{}) (*http.Response, error) {
	host := viper.GetString("api.host")
	if host == "" {
		host = stackAPIHost
	}
	port := viper.GetInt("api.port")
	if port == 0 {
		port = stackAPIPort
	}

	url := fmt.Sprintf("http://%s:%d/api/v1%s", host, port, path)

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	if IsVerbose() {
		ui.Info(fmt.Sprintf("API Request: %s %s", method, url))
	}

	return client.Do(req)
}

// handleAPIError handles API error responses
func handleAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var errResp struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(body, &errResp); err == nil {
		if errResp.Message != "" {
			return fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Message)
		}
		if errResp.Error != "" {
			return fmt.Errorf("API error (%d): %s", resp.StatusCode, errResp.Error)
		}
	}

	return fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
}

// truncate truncates a string to the specified length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
