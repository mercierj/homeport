// Package backup provides volume backup and restore functionality.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/homeport/homeport/internal/pkg/logger"
)

// Standard errors
var (
	ErrBackupNotFound    = errors.New("backup not found")
	ErrBackupExists      = errors.New("backup already exists")
	ErrVolumeNotFound    = errors.New("volume not found")
	ErrBackupInProgress  = errors.New("backup operation in progress")
	ErrRestoreInProgress = errors.New("restore operation in progress")
	ErrInvalidName       = errors.New("invalid backup name")
)

// BackupStatus represents the status of a backup operation.
type BackupStatus string

const (
	BackupStatusPending   BackupStatus = "pending"
	BackupStatusRunning   BackupStatus = "running"
	BackupStatusCompleted BackupStatus = "completed"
	BackupStatusFailed    BackupStatus = "failed"
)

// Backup represents a volume backup.
type Backup struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	StackID     string       `json:"stack_id"`
	Volumes     []string     `json:"volumes"`
	Size        int64        `json:"size"`
	Status      BackupStatus `json:"status"`
	Error       string       `json:"error,omitempty"`
	FilePath    string       `json:"file_path"`
	CreatedAt   time.Time    `json:"created_at"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
}

// VolumeInfo represents information about a Docker volume.
type VolumeInfo struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Labels     map[string]string `json:"labels"`
	StackID    string            `json:"stack_id,omitempty"`
	CreatedAt  string            `json:"created_at"`
}

// Config holds backup service configuration.
type Config struct {
	BackupDir string // Directory to store backups
	DataPath  string // Path for metadata persistence (JSON)
}

// Service handles backup operations.
type Service struct {
	mu           sync.RWMutex
	backups      map[string]*Backup
	dockerClient *client.Client
	config       *Config
	inProgress   map[string]bool // Track in-progress operations
}

// NewService creates a new backup service.
func NewService(cfg *Config) (*Service, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	// Set defaults
	if cfg.BackupDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		cfg.BackupDir = filepath.Join(home, ".homeport", "backups")
	}

	if cfg.DataPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		cfg.DataPath = filepath.Join(home, ".homeport", "backups.json")
	}

	// Create backup directory
	if err := os.MkdirAll(cfg.BackupDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Initialize Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	s := &Service{
		backups:      make(map[string]*Backup),
		dockerClient: dockerClient,
		config:       cfg,
		inProgress:   make(map[string]bool),
	}

	// Load existing data
	if err := s.loadData(); err != nil {
		logger.Warn("Failed to load backup data", "error", err)
	}

	return s, nil
}

// Close closes the service and releases resources.
func (s *Service) Close() error {
	return s.dockerClient.Close()
}

// ListVolumes lists Docker volumes, optionally filtered by stack.
func (s *Service) ListVolumes(ctx context.Context, stackID string) ([]VolumeInfo, error) {
	filterArgs := filters.NewArgs()
	if stackID != "" {
		filterArgs.Add("label", fmt.Sprintf("com.docker.compose.project=%s", stackID))
	}

	volumes, err := s.dockerClient.VolumeList(ctx, volume.ListOptions{Filters: filterArgs})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	result := make([]VolumeInfo, 0, len(volumes.Volumes))
	for _, v := range volumes.Volumes {
		info := VolumeInfo{
			Name:       v.Name,
			Driver:     v.Driver,
			Mountpoint: v.Mountpoint,
			Labels:     v.Labels,
			CreatedAt:  v.CreatedAt,
		}

		// Extract stack ID from labels
		if v.Labels != nil {
			if project, ok := v.Labels["com.docker.compose.project"]; ok {
				info.StackID = project
			}
		}

		result = append(result, info)
	}

	return result, nil
}

// ListBackups returns all backups, optionally filtered by stack.
func (s *Service) ListBackups(ctx context.Context, stackID string) ([]*Backup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Backup, 0, len(s.backups))
	for _, b := range s.backups {
		if stackID != "" && b.StackID != stackID {
			continue
		}
		result = append(result, b)
	}

	return result, nil
}

// GetBackup retrieves a backup by ID.
func (s *Service) GetBackup(ctx context.Context, id string) (*Backup, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	backup, ok := s.backups[id]
	if !ok {
		return nil, ErrBackupNotFound
	}

	return backup, nil
}

// CreateBackup creates a new backup of specified volumes.
// This runs asynchronously - returns immediately with pending status.
func (s *Service) CreateBackup(ctx context.Context, name, description, stackID string, volumes []string) (*Backup, error) {
	if name == "" {
		return nil, ErrInvalidName
	}

	if len(volumes) == 0 {
		return nil, fmt.Errorf("at least one volume is required")
	}

	// Verify volumes exist
	existingVolumes, err := s.ListVolumes(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to verify volumes: %w", err)
	}

	volumeMap := make(map[string]bool)
	for _, v := range existingVolumes {
		volumeMap[v.Name] = true
	}

	for _, vol := range volumes {
		if !volumeMap[vol] {
			return nil, fmt.Errorf("volume %s not found", vol)
		}
	}

	s.mu.Lock()

	// Generate unique ID
	id := generateID()

	backup := &Backup{
		ID:          id,
		Name:        name,
		Description: description,
		StackID:     stackID,
		Volumes:     volumes,
		Status:      BackupStatusPending,
		FilePath:    filepath.Join(s.config.BackupDir, id+".tar.gz"),
		CreatedAt:   time.Now(),
	}

	s.backups[id] = backup
	s.inProgress[id] = true
	s.mu.Unlock()

	// Save metadata
	s.saveData()

	// Run backup in background
	go s.runBackup(backup)

	return backup, nil
}

// runBackup performs the actual backup operation.
func (s *Service) runBackup(backup *Backup) {
	ctx := context.Background()

	// Update status to running
	s.mu.Lock()
	backup.Status = BackupStatusRunning
	s.mu.Unlock()
	s.saveData()

	defer func() {
		s.mu.Lock()
		delete(s.inProgress, backup.ID)
		s.mu.Unlock()
	}()

	// Ensure alpine image is available
	if err := s.ensureAlpineImage(ctx); err != nil {
		s.failBackup(backup, fmt.Errorf("failed to pull alpine image: %w", err))
		return
	}

	// Create tar.gz file
	file, err := os.Create(backup.FilePath)
	if err != nil {
		s.failBackup(backup, fmt.Errorf("failed to create backup file: %w", err))
		return
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	// Backup each volume
	for _, volName := range backup.Volumes {
		if err := s.backupVolume(ctx, volName, tarWriter); err != nil {
			s.failBackup(backup, fmt.Errorf("failed to backup volume %s: %w", volName, err))
			return
		}
	}

	// Close writers to flush data
	tarWriter.Close()
	gzWriter.Close()
	file.Close()

	// Get file size
	info, err := os.Stat(backup.FilePath)
	if err != nil {
		s.failBackup(backup, fmt.Errorf("failed to get backup file info: %w", err))
		return
	}

	// Update backup with success
	s.mu.Lock()
	backup.Status = BackupStatusCompleted
	backup.Size = info.Size()
	now := time.Now()
	backup.CompletedAt = &now
	s.mu.Unlock()
	s.saveData()

	logger.Info("Backup completed", "id", backup.ID, "name", backup.Name, "size", backup.Size)
}

// backupVolume backs up a single volume to the tar archive.
func (s *Service) backupVolume(ctx context.Context, volumeName string, tarWriter *tar.Writer) error {
	// Create a temporary container to access the volume
	containerConfig := &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"tar", "-cf", "-", "-C", "/backup", "."},
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeVolume,
				Source:   volumeName,
				Target:   "/backup",
				ReadOnly: true,
			},
		},
	}

	resp, err := s.dockerClient.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create backup container: %w", err)
	}
	defer func() { _ = s.dockerClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true}) }()

	// Start the container
	if err := s.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start backup container: %w", err)
	}

	// Wait for container to finish
	statusCh, errCh := s.dockerClient.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("error waiting for container: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("backup container exited with code %d", status.StatusCode)
		}
	}

	// Get the tar output from container logs
	logsReader, err := s.dockerClient.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: false,
	})
	if err != nil {
		return fmt.Errorf("failed to get container logs: %w", err)
	}
	defer logsReader.Close()

	// Write volume header to our tar
	header := &tar.Header{
		Name:     volumeName + "/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
		ModTime:  time.Now(),
	}
	if err := tarWriter.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	// Copy the container's tar output into our tar (nested)
	// First, strip the docker multiplexing header
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		_, _ = stdcopy.StdCopy(pw, io.Discard, logsReader)
	}()

	// Read the inner tar and write to our outer tar with prefixed paths
	innerTar := tar.NewReader(pr)
	for {
		innerHeader, err := innerTar.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read inner tar: %w", err)
		}

		// Prefix the path with volume name
		innerHeader.Name = volumeName + "/" + innerHeader.Name
		if err := tarWriter.WriteHeader(innerHeader); err != nil {
			return fmt.Errorf("failed to write inner header: %w", err)
		}

		if _, err := io.Copy(tarWriter, innerTar); err != nil {
			return fmt.Errorf("failed to copy inner tar data: %w", err)
		}
	}

	return nil
}

// RestoreBackup restores volumes from a backup.
func (s *Service) RestoreBackup(ctx context.Context, backupID, targetStackID string, volumes []string) error {
	s.mu.RLock()
	backup, ok := s.backups[backupID]
	if !ok {
		s.mu.RUnlock()
		return ErrBackupNotFound
	}
	s.mu.RUnlock()

	if backup.Status != BackupStatusCompleted {
		return fmt.Errorf("backup is not completed, status: %s", backup.Status)
	}

	// If no volumes specified, restore all
	if len(volumes) == 0 {
		volumes = backup.Volumes
	}

	// Ensure alpine image is available
	if err := s.ensureAlpineImage(ctx); err != nil {
		return fmt.Errorf("failed to pull alpine image: %w", err)
	}

	// Open the backup file
	file, err := os.Open(backup.FilePath)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	// Process each volume
	volumeSet := make(map[string]bool)
	for _, v := range volumes {
		volumeSet[v] = true
	}

	// Group entries by volume
	volumeData := make(map[string][]byte)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// Extract volume name from path
		parts := filepath.SplitList(header.Name)
		if len(parts) == 0 {
			continue
		}

		volName := filepath.Dir(header.Name)
		if idx := filepath.SplitList(volName); len(idx) > 0 {
			volName = idx[0]
		}
		// Get the first path component as volume name
		volName = header.Name
		if idx := len(header.Name); idx > 0 {
			for i, c := range header.Name {
				if c == '/' {
					volName = header.Name[:i]
					break
				}
			}
		}

		if !volumeSet[volName] {
			continue
		}

		// Read file data
		data, err := io.ReadAll(tarReader)
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}
		volumeData[header.Name] = data
	}

	// Restore each volume
	for _, volName := range volumes {
		if err := s.restoreVolume(ctx, volName, backup.FilePath); err != nil {
			return fmt.Errorf("failed to restore volume %s: %w", volName, err)
		}
	}

	logger.Info("Restore completed", "backup_id", backupID, "volumes", volumes)
	return nil
}

// restoreVolume restores a single volume from the backup archive.
func (s *Service) restoreVolume(ctx context.Context, volumeName, backupPath string) error {
	// Open backup file
	file, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup: %w", err)
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzReader.Close()

	// Create a temporary tar file with just this volume's data
	tmpFile, err := os.CreateTemp("", "restore-*.tar")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	tarReader := tar.NewReader(gzReader)
	tarWriter := tar.NewWriter(tmpFile)

	prefix := volumeName + "/"
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// Only include entries for this volume
		if len(header.Name) < len(prefix) {
			continue
		}
		if header.Name[:len(prefix)] != prefix {
			continue
		}

		// Strip volume prefix
		header.Name = header.Name[len(prefix):]
		if header.Name == "" {
			continue
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("failed to write header: %w", err)
		}

		if header.Size > 0 {
			if _, err := io.CopyN(tarWriter, tarReader, header.Size); err != nil {
				return fmt.Errorf("failed to copy data: %w", err)
			}
		}
	}
	tarWriter.Close()
	tmpFile.Close()

	// Ensure volume exists
	_, err = s.dockerClient.VolumeInspect(ctx, volumeName)
	if err != nil {
		// Create volume if it doesn't exist
		_, err = s.dockerClient.VolumeCreate(ctx, volume.CreateOptions{Name: volumeName})
		if err != nil {
			return fmt.Errorf("failed to create volume: %w", err)
		}
	}

	// Create container to restore the volume
	containerConfig := &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"tar", "-xf", "/restore.tar", "-C", "/data"},
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: volumeName,
				Target: "/data",
			},
			{
				Type:     mount.TypeBind,
				Source:   tmpFile.Name(),
				Target:   "/restore.tar",
				ReadOnly: true,
			},
		},
	}

	resp, err := s.dockerClient.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return fmt.Errorf("failed to create restore container: %w", err)
	}
	defer func() { _ = s.dockerClient.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true}) }()

	if err := s.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start restore container: %w", err)
	}

	statusCh, errCh := s.dockerClient.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("error waiting for container: %w", err)
		}
	case status := <-statusCh:
		if status.StatusCode != 0 {
			return fmt.Errorf("restore container exited with code %d", status.StatusCode)
		}
	}

	return nil
}

// DeleteBackup deletes a backup and its archive file.
func (s *Service) DeleteBackup(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	backup, ok := s.backups[id]
	if !ok {
		return ErrBackupNotFound
	}

	// Check if operation is in progress
	if s.inProgress[id] {
		return ErrBackupInProgress
	}

	// Delete the archive file
	if err := os.Remove(backup.FilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete backup file: %w", err)
	}

	delete(s.backups, id)
	s.saveData()

	logger.Info("Backup deleted", "id", id, "name", backup.Name)
	return nil
}

// GetBackupFile returns the path to download a backup file.
func (s *Service) GetBackupFile(ctx context.Context, id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	backup, ok := s.backups[id]
	if !ok {
		return "", ErrBackupNotFound
	}

	if backup.Status != BackupStatusCompleted {
		return "", fmt.Errorf("backup is not completed")
	}

	return backup.FilePath, nil
}

// ensureAlpineImage ensures the alpine image is available locally.
func (s *Service) ensureAlpineImage(ctx context.Context) error {
	_, _, err := s.dockerClient.ImageInspectWithRaw(ctx, "alpine:latest")
	if err == nil {
		return nil
	}

	reader, err := s.dockerClient.ImagePull(ctx, "alpine:latest", image.PullOptions{})
	if err != nil {
		return err
	}
	defer reader.Close()
	_, _ = io.Copy(io.Discard, reader)
	return nil
}

// failBackup marks a backup as failed.
func (s *Service) failBackup(backup *Backup, err error) {
	s.mu.Lock()
	backup.Status = BackupStatusFailed
	backup.Error = err.Error()
	now := time.Now()
	backup.CompletedAt = &now
	s.mu.Unlock()
	s.saveData()

	logger.Error("Backup failed", "id", backup.ID, "name", backup.Name, "error", err)
}

// saveData persists backup metadata to JSON file.
func (s *Service) saveData() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.backups, "", "  ")
	if err != nil {
		logger.Error("Failed to marshal backup data", "error", err)
		return
	}

	if err := os.WriteFile(s.config.DataPath, data, 0600); err != nil {
		logger.Error("Failed to save backup data", "error", err)
	}
}

// loadData loads backup metadata from JSON file.
func (s *Service) loadData() error {
	data, err := os.ReadFile(s.config.DataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &s.backups)
}

// generateID generates a unique ID.
func generateID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
