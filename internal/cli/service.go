package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/homeport/homeport/internal/cli/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	serviceStackID  string
	serviceFollow   bool
	serviceTail     int
	serviceAPIHost  string
	serviceAPIPort  int
)

// serviceCmd represents the service command
var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage individual services",
	Long: `Manage individual Docker services within stacks.

Services are individual containers that make up your stack.
Use the subcommands to list, restart, scale, and view logs.

Examples:
  homeport service list my-stack               # List services in stack
  homeport service logs my-service -f          # Stream service logs
  homeport service restart my-service          # Restart a service
  homeport service scale my-service 3          # Scale to 3 replicas`,
}

// serviceListCmd represents the service list command
var serviceListCmd = &cobra.Command{
	Use:   "list [stack-id]",
	Short: "List services in stack",
	Long: `List all services in a stack.

Examples:
  homeport service list my-stack
  homeport service list --stack-id my-stack`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		stackID := serviceStackID
		if len(args) > 0 {
			stackID = args[0]
		}
		if stackID == "" {
			stackID = "default"
		}

		if !IsQuiet() {
			ui.Header("Homeport - Service List")
			ui.Info(fmt.Sprintf("Stack: %s", stackID))
			ui.Divider()
		}

		resp, err := serviceAPICall("GET", fmt.Sprintf("/docker/%s/containers", stackID), nil)
		if err != nil {
			return fmt.Errorf("failed to list services: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return handleServiceAPIError(resp)
		}

		var result struct {
			Containers []struct {
				ID      string   `json:"id"`
				Name    string   `json:"name"`
				Image   string   `json:"image"`
				Status  string   `json:"status"`
				State   string   `json:"state"`
				Ports   []string `json:"ports"`
				Created int64    `json:"created"`
			} `json:"containers"`
			Count int `json:"count"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if len(result.Containers) == 0 {
			ui.Info("No services found")
			return nil
		}

		table := ui.NewTable([]string{"Name", "Image", "Status", "State", "Ports"})
		for _, container := range result.Containers {
			ports := ""
			if len(container.Ports) > 0 {
				for i, p := range container.Ports {
					if i > 0 {
						ports += ", "
					}
					ports += p
				}
			}
			table.AddRow([]string{
				container.Name,
				truncateImage(container.Image, 30),
				container.Status,
				container.State,
				ports,
			})
		}
		fmt.Println(table.Render())

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Found %d service(s)", result.Count))
		}

		return nil
	},
}

// serviceLogsCmd represents the service logs command
var serviceLogsCmd = &cobra.Command{
	Use:   "logs [service]",
	Short: "View service logs",
	Long: `View logs from a service.

Use -f/--follow to stream logs in real-time.
Use --tail to specify the number of lines to show.

Examples:
  homeport service logs my-service
  homeport service logs my-service -f
  homeport service logs my-service --tail 50`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serviceName := args[0]

		if !IsQuiet() {
			ui.Header("Homeport - Service Logs")
			ui.Info(fmt.Sprintf("Service: %s", serviceName))
			if serviceFollow {
				ui.Info("Following logs (Ctrl+C to stop)...")
			}
			ui.Divider()
		}

		if serviceFollow {
			return streamServiceLogs(serviceName)
		}

		return fetchServiceLogs(serviceName)
	},
}

// serviceRestartCmd represents the service restart command
var serviceRestartCmd = &cobra.Command{
	Use:   "restart [service]",
	Short: "Restart a service",
	Long: `Restart a specific service/container.

Examples:
  homeport service restart my-service`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serviceName := args[0]

		if !IsQuiet() {
			ui.Header("Homeport - Restart Service")
			ui.Info(fmt.Sprintf("Restarting service: %s", serviceName))
			ui.Divider()
		}

		resp, err := serviceAPICall("POST", fmt.Sprintf("/docker/containers/%s/restart", serviceName), nil)
		if err != nil {
			return fmt.Errorf("failed to restart service: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return handleServiceAPIError(resp)
		}

		var result struct {
			Status string `json:"status"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Service %s %s", serviceName, result.Status))
		}

		return nil
	},
}

// serviceScaleCmd represents the service scale command
var serviceScaleCmd = &cobra.Command{
	Use:   "scale [service] [replicas]",
	Short: "Scale service replicas",
	Long: `Scale a service to the specified number of replicas.

Note: This requires Docker Swarm mode or docker-compose scale support.

Examples:
  homeport service scale my-service 3`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		serviceName := args[0]
		replicasStr := args[1]

		replicas, err := strconv.Atoi(replicasStr)
		if err != nil || replicas < 0 {
			return fmt.Errorf("invalid replicas count: %s", replicasStr)
		}

		if !IsQuiet() {
			ui.Header("Homeport - Scale Service")
			ui.Info(fmt.Sprintf("Scaling service %s to %d replicas", serviceName, replicas))
			ui.Divider()
		}

		// Scale operation requires a body with the replicas count
		body := map[string]int{"replicas": replicas}
		resp, err := serviceAPICall("POST", fmt.Sprintf("/docker/containers/%s/scale", serviceName), body)
		if err != nil {
			return fmt.Errorf("failed to scale service: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return handleServiceAPIError(resp)
		}

		var result struct {
			Status   string `json:"status"`
			Replicas int    `json:"replicas"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Service %s scaled to %d replicas", serviceName, replicas))
		}

		return nil
	},
}

// serviceStartCmd represents the service start command
var serviceStartCmd = &cobra.Command{
	Use:   "start [service]",
	Short: "Start a service",
	Long: `Start a specific service/container.

Examples:
  homeport service start my-service`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serviceName := args[0]

		if !IsQuiet() {
			ui.Header("Homeport - Start Service")
			ui.Info(fmt.Sprintf("Starting service: %s", serviceName))
			ui.Divider()
		}

		resp, err := serviceAPICall("POST", fmt.Sprintf("/docker/containers/%s/start", serviceName), nil)
		if err != nil {
			return fmt.Errorf("failed to start service: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return handleServiceAPIError(resp)
		}

		var result struct {
			Status string `json:"status"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Service %s %s", serviceName, result.Status))
		}

		return nil
	},
}

// serviceStopCmd represents the service stop command
var serviceStopCmd = &cobra.Command{
	Use:   "stop [service]",
	Short: "Stop a service",
	Long: `Stop a specific service/container.

Examples:
  homeport service stop my-service`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		serviceName := args[0]

		if !IsQuiet() {
			ui.Header("Homeport - Stop Service")
			ui.Info(fmt.Sprintf("Stopping service: %s", serviceName))
			ui.Divider()
		}

		resp, err := serviceAPICall("POST", fmt.Sprintf("/docker/containers/%s/stop", serviceName), nil)
		if err != nil {
			return fmt.Errorf("failed to stop service: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return handleServiceAPIError(resp)
		}

		var result struct {
			Status string `json:"status"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("failed to parse response: %w", err)
		}

		if !IsQuiet() {
			ui.Divider()
			ui.Success(fmt.Sprintf("Service %s %s", serviceName, result.Status))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(serviceCmd)

	// Global flags for service commands
	serviceCmd.PersistentFlags().StringVar(&serviceStackID, "stack-id", "", "stack ID (defaults to 'default')")
	serviceCmd.PersistentFlags().StringVar(&serviceAPIHost, "api-host", "localhost", "API server host")
	serviceCmd.PersistentFlags().IntVar(&serviceAPIPort, "api-port", 8080, "API server port")

	// Logs command flags
	serviceLogsCmd.Flags().BoolVarP(&serviceFollow, "follow", "f", false, "follow log output")
	serviceLogsCmd.Flags().IntVar(&serviceTail, "tail", 100, "number of lines to show from the end of the logs")

	// Bind flags to viper
	viper.BindPFlag("service.stack_id", serviceCmd.PersistentFlags().Lookup("stack-id"))
	viper.BindPFlag("api.host", serviceCmd.PersistentFlags().Lookup("api-host"))
	viper.BindPFlag("api.port", serviceCmd.PersistentFlags().Lookup("api-port"))

	// Add subcommands
	serviceCmd.AddCommand(serviceListCmd)
	serviceCmd.AddCommand(serviceLogsCmd)
	serviceCmd.AddCommand(serviceRestartCmd)
	serviceCmd.AddCommand(serviceScaleCmd)
	serviceCmd.AddCommand(serviceStartCmd)
	serviceCmd.AddCommand(serviceStopCmd)
}

// serviceAPICall makes an HTTP request to the service API
func serviceAPICall(method, path string, body interface{}) (*http.Response, error) {
	host := viper.GetString("api.host")
	if host == "" {
		host = serviceAPIHost
	}
	port := viper.GetInt("api.port")
	if port == 0 {
		port = serviceAPIPort
	}

	url := fmt.Sprintf("http://%s:%d/api/v1%s", host, port, path)

	var bodyReader io.Reader
	if body != nil {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = io.NopCloser(io.NewSectionReader(readerAt(bodyBytes), 0, int64(len(bodyBytes))))
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

// readerAt is a simple wrapper to convert []byte to io.ReaderAt
type readerAt []byte

func (r readerAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(r)) {
		return 0, io.EOF
	}
	n = copy(p, r[off:])
	return n, nil
}

// handleServiceAPIError handles API error responses
func handleServiceAPIError(resp *http.Response) error {
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

// fetchServiceLogs fetches logs without streaming
func fetchServiceLogs(serviceName string) error {
	resp, err := serviceAPICall("GET", fmt.Sprintf("/docker/containers/%s/logs?tail=%d", serviceName, serviceTail), nil)
	if err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return handleServiceAPIError(resp)
	}

	var result struct {
		Logs string `json:"logs"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Println(result.Logs)

	return nil
}

// streamServiceLogs streams logs in real-time using polling
func streamServiceLogs(serviceName string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		cancel()
	}()

	lastLogLine := ""
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Initial fetch
	if err := fetchAndPrintLogs(serviceName, serviceTail, &lastLogLine); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// Fetch new logs
			if err := fetchAndPrintLogs(serviceName, 50, &lastLogLine); err != nil {
				if IsVerbose() {
					ui.Warning(fmt.Sprintf("Error fetching logs: %v", err))
				}
			}
		}
	}
}

// fetchAndPrintLogs fetches logs and prints new lines
func fetchAndPrintLogs(serviceName string, tail int, lastLine *string) error {
	resp, err := serviceAPICall("GET", fmt.Sprintf("/docker/containers/%s/logs?tail=%d", serviceName, tail), nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return handleServiceAPIError(resp)
	}

	var result struct {
		Logs string `json:"logs"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	// Parse and print new lines
	scanner := bufio.NewScanner(io.NopCloser(io.NewSectionReader(readerAt([]byte(result.Logs)), 0, int64(len(result.Logs)))))
	lines := []string{}
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// Find where to start printing
	startIdx := 0
	if *lastLine != "" {
		for i, line := range lines {
			if line == *lastLine {
				startIdx = i + 1
				break
			}
		}
	}

	// Print new lines
	for i := startIdx; i < len(lines); i++ {
		fmt.Println(lines[i])
		if i == len(lines)-1 {
			*lastLine = lines[i]
		}
	}

	return nil
}

// truncateImage truncates an image name to the specified length
func truncateImage(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return "..." + s[len(s)-maxLen+3:]
}
