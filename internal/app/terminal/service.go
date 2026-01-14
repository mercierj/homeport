package terminal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
)

// Session represents an active terminal session
type Session struct {
	ID           string
	ContainerID  string
	ExecID       string
	HijackedResp types.HijackedResponse
	CreatedAt    time.Time
	LastActivity time.Time
	mu           sync.Mutex
}

// UpdateActivity updates the last activity timestamp
func (s *Session) UpdateActivity() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActivity = time.Now()
}

// Service manages terminal sessions
type Service struct {
	dockerClient *client.Client
	sessions     map[string]*Session
	mu           sync.RWMutex
	stopCleanup  chan struct{}
}

// NewService creates a new terminal service
func NewService() (*Service, error) {
	opts := []client.Opt{client.WithAPIVersionNegotiation()}

	// If DOCKER_HOST is not set, try to find the socket
	if os.Getenv("DOCKER_HOST") == "" {
		if host := findDockerHost(); host != "" {
			opts = append(opts, client.WithHost(host))
		}
	}
	opts = append(opts, client.FromEnv)

	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, err
	}

	svc := &Service{
		dockerClient: cli,
		sessions:     make(map[string]*Session),
		stopCleanup:  make(chan struct{}),
	}

	// Start session cleanup goroutine
	go svc.cleanupSessions()

	return svc, nil
}

// findDockerHost returns the Docker host URI based on the platform
func findDockerHost() string {
	switch runtime.GOOS {
	case "windows":
		return "npipe:////./pipe/docker_engine"
	case "darwin":
		// macOS: Check Docker Desktop socket locations
		home, err := os.UserHomeDir()
		if err == nil {
			// Docker Desktop 4.x+ location
			sock := filepath.Join(home, ".docker", "run", "docker.sock")
			if _, err := os.Stat(sock); err == nil {
				return "unix://" + sock
			}
			// Colima location
			sock = filepath.Join(home, ".colima", "default", "docker.sock")
			if _, err := os.Stat(sock); err == nil {
				return "unix://" + sock
			}
		}
		// Fallback to standard location
		if _, err := os.Stat("/var/run/docker.sock"); err == nil {
			return "unix:///var/run/docker.sock"
		}
	default:
		// Linux and others
		if _, err := os.Stat("/var/run/docker.sock"); err == nil {
			return "unix:///var/run/docker.sock"
		}
		// Rootless Docker
		if xdgRuntime := os.Getenv("XDG_RUNTIME_DIR"); xdgRuntime != "" {
			sock := filepath.Join(xdgRuntime, "docker.sock")
			if _, err := os.Stat(sock); err == nil {
				return "unix://" + sock
			}
		}
	}
	return ""
}

// CreateSession creates a new terminal session for a container
func (s *Service) CreateSession(ctx context.Context, containerID string, cols, rows uint) (*Session, error) {
	// Verify container exists and is running
	info, err := s.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("container not found: %w", err)
	}
	if !info.State.Running {
		return nil, fmt.Errorf("container is not running")
	}

	// Determine shell to use
	shell := s.detectShell(ctx, containerID)

	// Create exec instance
	execConfig := container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          []string{shell},
	}

	execResp, err := s.dockerClient.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %w", err)
	}

	// Attach to exec instance
	attachResp, err := s.dockerClient.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{
		Tty: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to attach to exec: %w", err)
	}

	// Resize terminal
	if cols > 0 && rows > 0 {
		_ = s.dockerClient.ContainerExecResize(ctx, execResp.ID, container.ResizeOptions{
			Height: rows,
			Width:  cols,
		})
	}

	session := &Session{
		ID:           uuid.New().String(),
		ContainerID:  containerID,
		ExecID:       execResp.ID,
		HijackedResp: attachResp,
		CreatedAt:    time.Now(),
		LastActivity: time.Now(),
	}

	s.mu.Lock()
	s.sessions[session.ID] = session
	s.mu.Unlock()

	return session, nil
}

// ResizeSession resizes the terminal
func (s *Service) ResizeSession(ctx context.Context, sessionID string, cols, rows uint) error {
	s.mu.RLock()
	session, ok := s.sessions[sessionID]
	s.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session not found")
	}

	return s.dockerClient.ContainerExecResize(ctx, session.ExecID, container.ResizeOptions{
		Height: rows,
		Width:  cols,
	})
}

// CloseSession closes a terminal session
func (s *Service) CloseSession(sessionID string) error {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if ok {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()

	if !ok {
		return nil
	}

	session.HijackedResp.Close()
	return nil
}

// GetSession returns a session by ID
func (s *Service) GetSession(sessionID string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[sessionID]
	return session, ok
}

// detectShell tries to detect available shell in container
func (s *Service) detectShell(ctx context.Context, containerID string) string {
	shells := []string{"/bin/bash", "/bin/sh", "/bin/ash"}
	for _, shell := range shells {
		execConfig := container.ExecOptions{
			Cmd: []string{"test", "-x", shell},
		}
		resp, err := s.dockerClient.ContainerExecCreate(ctx, containerID, execConfig)
		if err != nil {
			continue
		}
		_ = s.dockerClient.ContainerExecStart(ctx, resp.ID, container.ExecStartOptions{})
		inspect, err := s.dockerClient.ContainerExecInspect(ctx, resp.ID)
		if err != nil {
			continue
		}
		if inspect.ExitCode == 0 {
			return shell
		}
	}
	return "/bin/sh" // fallback
}

// cleanupSessions removes idle sessions
func (s *Service) cleanupSessions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCleanup:
			return
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for id, session := range s.sessions {
				session.mu.Lock()
				idle := now.Sub(session.LastActivity) > 30*time.Minute
				session.mu.Unlock()
				if idle {
					session.HijackedResp.Close()
					delete(s.sessions, id)
				}
			}
			s.mu.Unlock()
		}
	}
}

// Close closes the service and all sessions
func (s *Service) Close() error {
	// Stop cleanup goroutine
	close(s.stopCleanup)

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, session := range s.sessions {
		session.HijackedResp.Close()
	}
	s.sessions = make(map[string]*Session)

	return s.dockerClient.Close()
}
