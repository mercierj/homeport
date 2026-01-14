// Package stacks provides multi-stack management for docker-compose deployments.
package stacks

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/pkg/logger"
	"gopkg.in/yaml.v3"
)

// Standard errors
var (
	ErrStackNotFound    = errors.New("stack not found")
	ErrStackExists      = errors.New("stack already exists")
	ErrStackRunning     = errors.New("stack is running")
	ErrInvalidCompose   = errors.New("invalid compose file")
	ErrOperationPending = errors.New("operation already in progress")
	ErrInvalidName      = errors.New("invalid stack name")
)

// StackStatus represents the runtime status of a stack.
type StackStatus string

const (
	StackStatusStopped  StackStatus = "stopped"
	StackStatusRunning  StackStatus = "running"
	StackStatusStarting StackStatus = "starting"
	StackStatusStopping StackStatus = "stopping"
	StackStatusError    StackStatus = "error"
	StackStatusPartial  StackStatus = "partial"
)

// ServiceInfo represents info about a service in the stack.
type ServiceInfo struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	Replicas     int    `json:"replicas"`
	RunningCount int    `json:"running_count"`
	Status       string `json:"status"`
}

// DeploymentConfig holds configuration for a pending deployment.
type DeploymentConfig struct {
	Provider      string                  `json:"provider"`       // e.g., "hetzner", "scaleway", "ovh"
	Region        string                  `json:"region"`         // e.g., "eu-central", "fr-par"
	HALevel       string                  `json:"ha_level"`       // e.g., "none", "basic", "full"
	TerraformPath string                  `json:"terraform_path"` // Path to terraform zip file
	EstimatedCost *generator.CostEstimate `json:"estimated_cost,omitempty"`
}

// Stack represents a docker-compose deployment stack.
type Stack struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description,omitempty"`
	ComposeFile      string            `json:"compose_file"`
	EnvVars          map[string]string `json:"env_vars,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Directory        string            `json:"directory"`
	Status           StackStatus       `json:"status"`
	Services         []ServiceInfo     `json:"services"`
	Error            string            `json:"error,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
	LastStartedAt    *time.Time        `json:"last_started_at,omitempty"`
	LastStoppedAt    *time.Time        `json:"last_stopped_at,omitempty"`
	DeploymentConfig *DeploymentConfig `json:"deployment_config,omitempty"`
	IsPending        bool              `json:"is_pending"`
}

// Config holds stack service configuration.
type Config struct {
	StacksDir string // Base directory for stack files
	DataPath  string // Path for metadata persistence (JSON)
}

// Service handles stack operations.
type Service struct {
	mu           sync.RWMutex
	stacks       map[string]*Stack
	config       *Config
	dockerClient *client.Client
	inProgress   map[string]string // stackID -> operation (start/stop)
}

// NewService creates a new stacks service.
func NewService(cfg *Config) (*Service, error) {
	if cfg == nil {
		cfg = &Config{}
	}

	// Set defaults
	if cfg.StacksDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		cfg.StacksDir = filepath.Join(home, ".homeport", "stacks")
	}

	if cfg.DataPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		cfg.DataPath = filepath.Join(home, ".homeport", "stacks.json")
	}

	// Create stacks directory
	if err := os.MkdirAll(cfg.StacksDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create stacks directory: %w", err)
	}

	// Initialize Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	s := &Service{
		stacks:       make(map[string]*Stack),
		config:       cfg,
		dockerClient: dockerClient,
		inProgress:   make(map[string]string),
	}

	// Load existing data
	if err := s.loadData(); err != nil {
		logger.Warn("Failed to load stacks data", "error", err)
	}

	return s, nil
}

// Close closes the service and releases resources.
func (s *Service) Close() error {
	return s.dockerClient.Close()
}

// ListStacks returns all stacks.
func (s *Service) ListStacks(ctx context.Context) ([]*Stack, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Stack, 0, len(s.stacks))
	for _, stack := range s.stacks {
		// Refresh status
		s.refreshStackStatus(ctx, stack)
		result = append(result, stack)
	}

	return result, nil
}

// GetStack retrieves a stack by ID.
func (s *Service) GetStack(ctx context.Context, id string) (*Stack, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stack, ok := s.stacks[id]
	if !ok {
		return nil, ErrStackNotFound
	}

	// Refresh status
	s.refreshStackStatus(ctx, stack)
	return stack, nil
}

// CreateStack creates a new stack from compose file.
func (s *Service) CreateStack(ctx context.Context, name, description, composeFile string, envVars, labels map[string]string) (*Stack, error) {
	if name == "" {
		return nil, ErrInvalidName
	}

	// Validate compose file
	if err := s.ValidateComposeFile(ctx, composeFile); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate name
	for _, existing := range s.stacks {
		if existing.Name == name {
			return nil, ErrStackExists
		}
	}

	// Generate unique ID
	id := generateID()

	// Create stack directory
	stackDir := filepath.Join(s.config.StacksDir, id)
	if err := os.MkdirAll(stackDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create stack directory: %w", err)
	}

	stack := &Stack{
		ID:          id,
		Name:        name,
		Description: description,
		ComposeFile: composeFile,
		EnvVars:     envVars,
		Labels:      labels,
		Directory:   stackDir,
		Status:      StackStatusStopped,
		Services:    []ServiceInfo{},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Write compose file and env
	if err := s.writeStackFiles(stack); err != nil {
		_ = os.RemoveAll(stackDir)
		return nil, fmt.Errorf("failed to write stack files: %w", err)
	}

	// Parse services from compose file
	stack.Services = s.parseServices(composeFile)

	s.stacks[id] = stack
	s.saveData()

	logger.Info("Stack created", "id", id, "name", name)
	return stack, nil
}

// UpdateStack updates a stack configuration.
func (s *Service) UpdateStack(ctx context.Context, id string, name, description, composeFile *string, envVars, labels map[string]string) (*Stack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stack, ok := s.stacks[id]
	if !ok {
		return nil, ErrStackNotFound
	}

	// Check if operation is in progress
	if op, ok := s.inProgress[id]; ok {
		return nil, fmt.Errorf("%w: %s", ErrOperationPending, op)
	}

	// Update fields if provided
	if name != nil && *name != "" {
		// Check for duplicate name
		for _, existing := range s.stacks {
			if existing.ID != id && existing.Name == *name {
				return nil, ErrStackExists
			}
		}
		stack.Name = *name
	}

	if description != nil {
		stack.Description = *description
	}

	if composeFile != nil && *composeFile != "" {
		// Validate compose file
		if err := s.ValidateComposeFile(ctx, *composeFile); err != nil {
			return nil, err
		}
		stack.ComposeFile = *composeFile
		stack.Services = s.parseServices(*composeFile)
	}

	if envVars != nil {
		stack.EnvVars = envVars
	}

	if labels != nil {
		stack.Labels = labels
	}

	stack.UpdatedAt = time.Now()

	// Rewrite stack files
	if err := s.writeStackFiles(stack); err != nil {
		return nil, fmt.Errorf("failed to write stack files: %w", err)
	}

	s.saveData()

	logger.Info("Stack updated", "id", id, "name", stack.Name)
	return stack, nil
}

// DeleteStack deletes a stack (must be stopped first).
func (s *Service) DeleteStack(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stack, ok := s.stacks[id]
	if !ok {
		return ErrStackNotFound
	}

	// Check if operation is in progress
	if op, ok := s.inProgress[id]; ok {
		return fmt.Errorf("%w: %s", ErrOperationPending, op)
	}

	// Check if stack is running
	s.refreshStackStatus(ctx, stack)
	if stack.Status == StackStatusRunning || stack.Status == StackStatusPartial {
		return ErrStackRunning
	}

	// Delete stack directory
	if err := os.RemoveAll(stack.Directory); err != nil {
		return fmt.Errorf("failed to delete stack directory: %w", err)
	}

	delete(s.stacks, id)
	s.saveData()

	logger.Info("Stack deleted", "id", id, "name", stack.Name)
	return nil
}

// StartStack starts all services in the stack.
func (s *Service) StartStack(ctx context.Context, id string) error {
	s.mu.Lock()
	stack, ok := s.stacks[id]
	if !ok {
		s.mu.Unlock()
		return ErrStackNotFound
	}

	// Check if operation is in progress
	if op, ok := s.inProgress[id]; ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrOperationPending, op)
	}

	s.inProgress[id] = "starting"
	stack.Status = StackStatusStarting
	s.mu.Unlock()
	s.saveData()

	// Run docker compose up
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.inProgress, id)
			s.mu.Unlock()
		}()

		output, err := s.runComposeCommand(context.Background(), stack, "up", "-d")
		s.mu.Lock()
		if err != nil {
			stack.Status = StackStatusError
			stack.Error = fmt.Sprintf("Failed to start: %s - %v", output, err)
			logger.Error("Failed to start stack", "id", id, "error", err, "output", output)
		} else {
			now := time.Now()
			stack.LastStartedAt = &now
			stack.Error = ""
			s.refreshStackStatus(context.Background(), stack)
		}
		s.mu.Unlock()
		s.saveData()
	}()

	return nil
}

// StopStack stops all services in the stack.
func (s *Service) StopStack(ctx context.Context, id string) error {
	s.mu.Lock()
	stack, ok := s.stacks[id]
	if !ok {
		s.mu.Unlock()
		return ErrStackNotFound
	}

	// Check if operation is in progress
	if op, ok := s.inProgress[id]; ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrOperationPending, op)
	}

	s.inProgress[id] = "stopping"
	stack.Status = StackStatusStopping
	s.mu.Unlock()
	s.saveData()

	// Run docker compose down
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.inProgress, id)
			s.mu.Unlock()
		}()

		output, err := s.runComposeCommand(context.Background(), stack, "down")
		s.mu.Lock()
		if err != nil {
			stack.Status = StackStatusError
			stack.Error = fmt.Sprintf("Failed to stop: %s - %v", output, err)
			logger.Error("Failed to stop stack", "id", id, "error", err, "output", output)
		} else {
			stack.Status = StackStatusStopped
			now := time.Now()
			stack.LastStoppedAt = &now
			stack.Error = ""
		}
		s.mu.Unlock()
		s.saveData()
	}()

	return nil
}

// RestartStack restarts all services in the stack.
func (s *Service) RestartStack(ctx context.Context, id string) error {
	s.mu.Lock()
	stack, ok := s.stacks[id]
	if !ok {
		s.mu.Unlock()
		return ErrStackNotFound
	}

	// Check if operation is in progress
	if op, ok := s.inProgress[id]; ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrOperationPending, op)
	}

	s.inProgress[id] = "restarting"
	stack.Status = StackStatusStarting
	s.mu.Unlock()
	s.saveData()

	// Run docker compose restart
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.inProgress, id)
			s.mu.Unlock()
		}()

		output, err := s.runComposeCommand(context.Background(), stack, "restart")
		s.mu.Lock()
		if err != nil {
			stack.Status = StackStatusError
			stack.Error = fmt.Sprintf("Failed to restart: %s - %v", output, err)
			logger.Error("Failed to restart stack", "id", id, "error", err, "output", output)
		} else {
			now := time.Now()
			stack.LastStartedAt = &now
			stack.Error = ""
			s.refreshStackStatus(context.Background(), stack)
		}
		s.mu.Unlock()
		s.saveData()
	}()

	return nil
}

// GetStackStatus refreshes and returns the current status of a stack.
func (s *Service) GetStackStatus(ctx context.Context, id string) (*Stack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stack, ok := s.stacks[id]
	if !ok {
		return nil, ErrStackNotFound
	}

	s.refreshStackStatus(ctx, stack)
	return stack, nil
}

// GetStackLogs retrieves logs from stack services.
func (s *Service) GetStackLogs(ctx context.Context, id, service string, tail int) (string, error) {
	s.mu.RLock()
	stack, ok := s.stacks[id]
	if !ok {
		s.mu.RUnlock()
		return "", ErrStackNotFound
	}
	s.mu.RUnlock()

	args := []string{"logs"}
	if tail > 0 {
		args = append(args, fmt.Sprintf("--tail=%d", tail))
	}
	if service != "" {
		args = append(args, service)
	}

	output, err := s.runComposeCommand(ctx, stack, args...)
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}

	return output, nil
}

// ValidateComposeFile validates a docker-compose.yml file.
func (s *Service) ValidateComposeFile(ctx context.Context, content string) error {
	if content == "" {
		return ErrInvalidCompose
	}

	// Parse YAML to check syntax
	var compose map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &compose); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCompose, err)
	}

	// Check for required fields
	if _, ok := compose["services"]; !ok {
		return fmt.Errorf("%w: missing 'services' section", ErrInvalidCompose)
	}

	services, ok := compose["services"].(map[string]interface{})
	if !ok || len(services) == 0 {
		return fmt.Errorf("%w: no services defined", ErrInvalidCompose)
	}

	return nil
}

// refreshStackStatus queries Docker for current container states.
func (s *Service) refreshStackStatus(ctx context.Context, stack *Stack) {
	// Don't update status if operation is in progress
	if _, ok := s.inProgress[stack.ID]; ok {
		return
	}

	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("com.docker.compose.project=%s", stack.ID))

	containers, err := s.dockerClient.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		logger.Warn("Failed to list containers for stack", "stack", stack.ID, "error", err)
		return
	}

	running := 0
	total := len(containers)

	// Update service info
	serviceMap := make(map[string]*ServiceInfo)
	for _, svc := range stack.Services {
		svc := svc
		serviceMap[svc.Name] = &ServiceInfo{
			Name:         svc.Name,
			Image:        svc.Image,
			Replicas:     svc.Replicas,
			RunningCount: 0,
			Status:       "stopped",
		}
	}

	for _, c := range containers {
		serviceName := c.Labels["com.docker.compose.service"]
		if svc, ok := serviceMap[serviceName]; ok {
			if c.State == "running" {
				svc.RunningCount++
				running++
			}
			svc.Status = c.State
		}
	}

	// Update stack services
	services := make([]ServiceInfo, 0, len(serviceMap))
	for _, svc := range serviceMap {
		services = append(services, *svc)
	}
	stack.Services = services

	// Determine overall status
	if total == 0 {
		stack.Status = StackStatusStopped
	} else if running == total {
		stack.Status = StackStatusRunning
	} else if running == 0 {
		stack.Status = StackStatusStopped
	} else {
		stack.Status = StackStatusPartial
	}
}

// writeStackFiles writes compose file and env to stack directory.
func (s *Service) writeStackFiles(stack *Stack) error {
	// Write docker-compose.yml
	composePath := filepath.Join(stack.Directory, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(stack.ComposeFile), 0644); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	// Write .env file if env vars exist
	if len(stack.EnvVars) > 0 {
		envPath := filepath.Join(stack.Directory, ".env")
		var envContent strings.Builder
		for k, v := range stack.EnvVars {
			envContent.WriteString(fmt.Sprintf("%s=%s\n", k, v))
		}
		if err := os.WriteFile(envPath, []byte(envContent.String()), 0644); err != nil {
			return fmt.Errorf("failed to write env file: %w", err)
		}
	}

	return nil
}

// runComposeCommand executes a docker compose command.
func (s *Service) runComposeCommand(ctx context.Context, stack *Stack, args ...string) (string, error) {
	cmdArgs := []string{"compose", "-p", stack.ID}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	cmd.Dir = stack.Directory

	// Set environment
	env := os.Environ()
	for k, v := range stack.EnvVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// parseServices parses service information from compose file.
func (s *Service) parseServices(composeContent string) []ServiceInfo {
	var compose map[string]interface{}
	if err := yaml.Unmarshal([]byte(composeContent), &compose); err != nil {
		return nil
	}

	services := make([]ServiceInfo, 0)
	if servicesMap, ok := compose["services"].(map[string]interface{}); ok {
		for name, svc := range servicesMap {
			info := ServiceInfo{
				Name:     name,
				Replicas: 1,
				Status:   "stopped",
			}

			if svcMap, ok := svc.(map[string]interface{}); ok {
				if image, ok := svcMap["image"].(string); ok {
					info.Image = image
				}
				if deploy, ok := svcMap["deploy"].(map[string]interface{}); ok {
					if replicas, ok := deploy["replicas"].(int); ok {
						info.Replicas = replicas
					}
				}
			}

			services = append(services, info)
		}
	}

	return services
}

// saveData persists stack metadata to JSON file.
func (s *Service) saveData() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s.stacks, "", "  ")
	if err != nil {
		logger.Error("Failed to marshal stacks data", "error", err)
		return
	}

	if err := os.WriteFile(s.config.DataPath, data, 0600); err != nil {
		logger.Error("Failed to save stacks data", "error", err)
	}
}

// loadData loads stack metadata from JSON file.
func (s *Service) loadData() error {
	data, err := os.ReadFile(s.config.DataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &s.stacks)
}

// CreatePendingStack creates a new stack with pending deployment configuration.
// The stack won't be deployed immediately - it's saved for later deployment.
func (s *Service) CreatePendingStack(ctx context.Context, name, description string, config *DeploymentConfig) (*Stack, error) {
	if name == "" {
		return nil, ErrInvalidName
	}

	if config == nil {
		return nil, fmt.Errorf("deployment config is required")
	}

	// Validate required config fields
	if config.Provider == "" {
		return nil, fmt.Errorf("provider is required")
	}
	if config.Region == "" {
		return nil, fmt.Errorf("region is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate name
	for _, existing := range s.stacks {
		if existing.Name == name {
			return nil, ErrStackExists
		}
	}

	// Generate unique ID
	id := generateID()

	// Create stack directory
	stackDir := filepath.Join(s.config.StacksDir, id)
	if err := os.MkdirAll(stackDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create stack directory: %w", err)
	}

	// Placeholder compose file for pending stacks
	placeholderCompose := `version: "3.8"
services:
  placeholder:
    image: busybox
    command: ["echo", "Pending deployment"]
`

	stack := &Stack{
		ID:               id,
		Name:             name,
		Description:      description,
		ComposeFile:      placeholderCompose,
		Directory:        stackDir,
		Status:           StackStatusStopped,
		Services:         []ServiceInfo{},
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		DeploymentConfig: config,
		IsPending:        true,
	}

	// Write placeholder compose file
	if err := s.writeStackFiles(stack); err != nil {
		_ = os.RemoveAll(stackDir)
		return nil, fmt.Errorf("failed to write stack files: %w", err)
	}

	s.stacks[id] = stack
	s.saveData()

	logger.Info("Pending stack created", "id", id, "name", name, "provider", config.Provider)
	return stack, nil
}

// generateID generates a unique ID.
func generateID() string {
	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
