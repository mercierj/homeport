package docker

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type Service struct {
	client *client.Client
}

// Client returns the underlying Docker client.
func (s *Service) Client() *client.Client {
	return s.client
}

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
	return &Service{client: cli}, nil
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

func (s *Service) Close() error {
	return s.client.Close()
}

type ContainerInfo struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	Status  string            `json:"status"`
	State   string            `json:"state"`
	Ports   []PortBinding     `json:"ports"`
	Created time.Time         `json:"created"`
	Labels  map[string]string `json:"labels"`
}

type PortBinding struct {
	HostPort      string `json:"host_port"`
	ContainerPort string `json:"container_port"`
	Protocol      string `json:"protocol"`
}

func (s *Service) ListContainers(ctx context.Context, stackID string) ([]ContainerInfo, error) {
	containers, err := s.client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}

	result := make([]ContainerInfo, 0)
	for _, c := range containers {
		// Filter by stack label if provided
		if stackID != "" && stackID != "default" {
			if label, ok := c.Labels["com.docker.compose.project"]; !ok || label != stackID {
				continue
			}
		}

		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		ports := make([]PortBinding, 0)
		for _, p := range c.Ports {
			ports = append(ports, PortBinding{
				HostPort:      fmt.Sprintf("%d", p.PublicPort),
				ContainerPort: fmt.Sprintf("%d", p.PrivatePort),
				Protocol:      p.Type,
			})
		}

		result = append(result, ContainerInfo{
			ID:      c.ID[:12],
			Name:    name,
			Image:   c.Image,
			Status:  c.Status,
			State:   c.State,
			Ports:   ports,
			Created: time.Unix(c.Created, 0),
			Labels:  c.Labels,
		})
	}

	return result, nil
}

func (s *Service) GetContainerLogs(ctx context.Context, containerID string, tail int) (string, error) {
	tailStr := "100"
	if tail > 0 {
		tailStr = fmt.Sprintf("%d", tail)
	}

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tailStr,
		Timestamps: true,
	}

	reader, err := s.client.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(logs), nil
}

func (s *Service) RestartContainer(ctx context.Context, containerID string) error {
	timeout := 10
	return s.client.ContainerRestart(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

func (s *Service) StopContainer(ctx context.Context, containerID string) error {
	timeout := 10
	return s.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

func (s *Service) StartContainer(ctx context.Context, containerID string) error {
	return s.client.ContainerStart(ctx, containerID, container.StartOptions{})
}

func (s *Service) RemoveContainer(ctx context.Context, containerID string, force bool) error {
	return s.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         force,
		RemoveVolumes: false,
	})
}

func (s *Service) RemoveAllContainers(ctx context.Context, stackID string) (int, error) {
	containers, err := s.ListContainers(ctx, stackID)
	if err != nil {
		return 0, err
	}

	removed := 0
	for _, c := range containers {
		if err := s.RemoveContainer(ctx, c.Name, true); err != nil {
			return removed, fmt.Errorf("failed to remove container %s: %w", c.Name, err)
		}
		removed++
	}

	return removed, nil
}
