package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/homeport/homeport/internal/cli/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	backupAPIURL     string
	backupName       string
	backupDesc       string
	backupVolumes    []string
	backupOutputFile string
)

// backupCmd represents the backup command group
var backupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Manage stack backups",
	Long: `Manage backups for deployed stacks.

The backup command provides functionality to create, restore, list,
download, and delete backups for your infrastructure stacks.

Examples:
  # List all backups for a stack
  homeport backup list my-stack

  # Create a new backup
  homeport backup create my-stack --name daily-backup

  # Restore from a backup
  homeport backup restore my-stack backup-123

  # Download a backup file
  homeport backup download backup-123 --output backup.tar.gz

  # Delete a backup
  homeport backup delete backup-123`,
}

// backupListCmd lists backups for a stack
var backupListCmd = &cobra.Command{
	Use:   "list [stack-id]",
	Short: "List backups for a stack",
	Long: `List all backups for the specified stack.

If no stack-id is provided, lists all backups across all stacks.

Examples:
  homeport backup list
  homeport backup list my-stack`,
	Args: cobra.MaximumNArgs(1),
	RunE: runBackupList,
}

// backupCreateCmd creates a new backup
var backupCreateCmd = &cobra.Command{
	Use:   "create <stack-id>",
	Short: "Create a new backup",
	Long: `Create a new backup for the specified stack.

The backup will include all configured volumes for the stack unless
specific volumes are specified with the --volumes flag.

Examples:
  homeport backup create my-stack --name daily-backup
  homeport backup create my-stack --name weekly --description "Weekly full backup"
  homeport backup create my-stack --volumes postgres-data,redis-data`,
	Args: cobra.ExactArgs(1),
	RunE: runBackupCreate,
}

// backupRestoreCmd restores from a backup
var backupRestoreCmd = &cobra.Command{
	Use:   "restore <stack-id> <backup-id>",
	Short: "Restore from a backup",
	Long: `Restore a stack from a backup.

This will restore the backed up volumes to the target stack.

Examples:
  homeport backup restore my-stack backup-123
  homeport backup restore my-stack backup-123 --volumes postgres-data`,
	Args: cobra.ExactArgs(2),
	RunE: runBackupRestore,
}

// backupDownloadCmd downloads a backup file
var backupDownloadCmd = &cobra.Command{
	Use:   "download <backup-id>",
	Short: "Download a backup file",
	Long: `Download a backup archive to the local filesystem.

The backup will be downloaded as a gzipped tar archive.

Examples:
  homeport backup download backup-123
  homeport backup download backup-123 --output /path/to/backup.tar.gz`,
	Args: cobra.ExactArgs(1),
	RunE: runBackupDownload,
}

// backupDeleteCmd deletes a backup
var backupDeleteCmd = &cobra.Command{
	Use:   "delete <backup-id>",
	Short: "Delete a backup",
	Long: `Delete a backup and its associated files.

This action is irreversible.

Examples:
  homeport backup delete backup-123`,
	Args: cobra.ExactArgs(1),
	RunE: runBackupDelete,
}

func init() {
	rootCmd.AddCommand(backupCmd)

	// Add subcommands
	backupCmd.AddCommand(backupListCmd)
	backupCmd.AddCommand(backupCreateCmd)
	backupCmd.AddCommand(backupRestoreCmd)
	backupCmd.AddCommand(backupDownloadCmd)
	backupCmd.AddCommand(backupDeleteCmd)

	// Global backup flags
	backupCmd.PersistentFlags().StringVar(&backupAPIURL, "api-url", "http://localhost:8080", "API server URL")
	_ = viper.BindPFlag("api_url", backupCmd.PersistentFlags().Lookup("api-url"))

	// Create command flags
	backupCreateCmd.Flags().StringVarP(&backupName, "name", "n", "", "backup name (required)")
	backupCreateCmd.Flags().StringVarP(&backupDesc, "description", "d", "", "backup description")
	backupCreateCmd.Flags().StringSliceVarP(&backupVolumes, "volumes", "V", nil, "volumes to backup (comma-separated)")
	_ = backupCreateCmd.MarkFlagRequired("name")

	// Restore command flags
	backupRestoreCmd.Flags().StringSliceVarP(&backupVolumes, "volumes", "V", nil, "volumes to restore (comma-separated)")

	// Download command flags
	backupDownloadCmd.Flags().StringVarP(&backupOutputFile, "output", "o", "", "output file path (default: backup-<id>.tar.gz)")
}

// BackupInfo represents backup information from the API
type BackupInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	StackID     string    `json:"stack_id"`
	Volumes     []string  `json:"volumes"`
	Size        int64     `json:"size"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// BackupListResponse represents the list backups API response
type BackupListResponse struct {
	Backups []BackupInfo `json:"backups"`
	Count   int          `json:"count"`
}

func getAPIURL() string {
	if url := viper.GetString("api_url"); url != "" {
		return url
	}
	return backupAPIURL
}

func runBackupList(cmd *cobra.Command, args []string) error {
	if !IsQuiet() {
		ui.Header("Homeport - Backup List")
		ui.Divider()
	}

	apiURL := getAPIURL()
	url := fmt.Sprintf("%s/api/v1/backups", apiURL)

	if len(args) > 0 {
		url = fmt.Sprintf("%s?stack_id=%s", url, args[0])
		if IsVerbose() {
			ui.Info(fmt.Sprintf("Listing backups for stack: %s", args[0]))
		}
	}

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", string(body))
	}

	var result BackupListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.Backups) == 0 {
		ui.Info("No backups found")
		return nil
	}

	// Display backups in a table
	table := ui.NewTable([]string{"ID", "Name", "Stack", "Status", "Size", "Created"})
	for _, backup := range result.Backups {
		sizeStr := formatBytes(backup.Size)
		createdStr := backup.CreatedAt.Format("2006-01-02 15:04")
		table.AddRow([]string{
			backup.ID,
			backup.Name,
			backup.StackID,
			backup.Status,
			sizeStr,
			createdStr,
		})
	}
	fmt.Println(table.Render())

	if !IsQuiet() {
		ui.Info(fmt.Sprintf("Total: %d backup(s)", result.Count))
	}

	return nil
}

func runBackupCreate(cmd *cobra.Command, args []string) error {
	stackID := args[0]

	if !IsQuiet() {
		ui.Header("Homeport - Create Backup")
		ui.Info(fmt.Sprintf("Stack: %s", stackID))
		ui.Info(fmt.Sprintf("Name: %s", backupName))
		ui.Divider()
	}

	// Build request payload
	payload := map[string]interface{}{
		"name":        backupName,
		"description": backupDesc,
		"stack_id":    stackID,
	}

	if len(backupVolumes) > 0 {
		payload["volumes"] = backupVolumes
	} else {
		// If no volumes specified, we need to get them from the API
		// For now, require volumes to be specified
		return fmt.Errorf("at least one volume is required; use --volumes flag")
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	apiURL := getAPIURL()
	url := fmt.Sprintf("%s/api/v1/backups", apiURL)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", string(body))
	}

	var backup BackupInfo
	if err := json.NewDecoder(resp.Body).Decode(&backup); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !IsQuiet() {
		ui.Divider()
		ui.Success("Backup created successfully")
		ui.Info(fmt.Sprintf("Backup ID: %s", backup.ID))
		ui.Info(fmt.Sprintf("Status: %s", backup.Status))
	}

	return nil
}

func runBackupRestore(cmd *cobra.Command, args []string) error {
	stackID := args[0]
	backupID := args[1]

	if !IsQuiet() {
		ui.Header("Homeport - Restore Backup")
		ui.Info(fmt.Sprintf("Stack: %s", stackID))
		ui.Info(fmt.Sprintf("Backup: %s", backupID))
		ui.Divider()
	}

	// Confirm restore action
	if !IsQuiet() {
		ui.Warning("This will restore data from the backup, potentially overwriting existing data.")
		if !ui.PromptYesNo("Continue with restore", false) {
			ui.Info("Restore cancelled")
			return nil
		}
	}

	// Build request payload
	payload := map[string]interface{}{
		"target_stack_id": stackID,
	}

	if len(backupVolumes) > 0 {
		payload["volumes"] = backupVolumes
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	apiURL := getAPIURL()
	url := fmt.Sprintf("%s/api/v1/backups/%s/restore", apiURL, backupID)

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", string(body))
	}

	if !IsQuiet() {
		ui.Divider()
		ui.Success("Backup restored successfully")
	}

	return nil
}

func runBackupDownload(cmd *cobra.Command, args []string) error {
	backupID := args[0]

	// Determine output file
	outputFile := backupOutputFile
	if outputFile == "" {
		outputFile = fmt.Sprintf("backup-%s.tar.gz", backupID)
	}

	if !IsQuiet() {
		ui.Header("Homeport - Download Backup")
		ui.Info(fmt.Sprintf("Backup: %s", backupID))
		ui.Info(fmt.Sprintf("Output: %s", outputFile))
		ui.Divider()
	}

	apiURL := getAPIURL()
	url := fmt.Sprintf("%s/api/v1/backups/%s/download", apiURL, backupID)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("backup not found: %s", backupID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", string(body))
	}

	// Create output file
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Copy response body to file
	written, err := io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write backup file: %w", err)
	}

	if !IsQuiet() {
		ui.Divider()
		ui.Success("Backup downloaded successfully")
		ui.Info(fmt.Sprintf("File: %s", outputFile))
		ui.Info(fmt.Sprintf("Size: %s", formatBytes(written)))
	}

	return nil
}

func runBackupDelete(cmd *cobra.Command, args []string) error {
	backupID := args[0]

	if !IsQuiet() {
		ui.Header("Homeport - Delete Backup")
		ui.Info(fmt.Sprintf("Backup: %s", backupID))
		ui.Divider()
	}

	// Confirm deletion
	if !IsQuiet() {
		ui.Warning("This action is irreversible. The backup will be permanently deleted.")
		if !ui.PromptYesNo("Delete backup", false) {
			ui.Info("Deletion cancelled")
			return nil
		}
	}

	apiURL := getAPIURL()
	url := fmt.Sprintf("%s/api/v1/backups/%s", apiURL, backupID)

	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("backup not found: %s", backupID)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s", string(body))
	}

	if !IsQuiet() {
		ui.Divider()
		ui.Success("Backup deleted successfully")
	}

	return nil
}

// formatBytes converts bytes to human-readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
