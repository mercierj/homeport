// Package runtime provides container runtime detection and abstraction.
// Supports Docker and Podman with automatic detection.
package runtime

import (
	"fmt"
	"os/exec"
	"strings"
)

// ContainerRuntime represents the container runtime to use.
type ContainerRuntime string

const (
	// RuntimeAuto automatically detects the available runtime.
	RuntimeAuto ContainerRuntime = "auto"

	// RuntimeDocker uses Docker as the container runtime.
	RuntimeDocker ContainerRuntime = "docker"

	// RuntimePodman uses Podman as the container runtime.
	RuntimePodman ContainerRuntime = "podman"
)

// String returns the string representation of the runtime.
func (r ContainerRuntime) String() string {
	return string(r)
}

// IsValid checks if the runtime is a valid option.
func (r ContainerRuntime) IsValid() bool {
	switch r {
	case RuntimeAuto, RuntimeDocker, RuntimePodman:
		return true
	default:
		return false
	}
}

// ParseRuntime parses a string into a ContainerRuntime.
func ParseRuntime(s string) (ContainerRuntime, error) {
	switch strings.ToLower(s) {
	case "auto", "":
		return RuntimeAuto, nil
	case "docker":
		return RuntimeDocker, nil
	case "podman":
		return RuntimePodman, nil
	default:
		return "", fmt.Errorf("invalid runtime: %s (valid: auto, docker, podman)", s)
	}
}

// Info contains information about a detected container runtime.
type Info struct {
	// Runtime is the detected runtime type.
	Runtime ContainerRuntime

	// Version is the runtime version string.
	Version string

	// ComposeCommand is the command to use for compose operations.
	// For Docker: "docker compose" or "docker-compose"
	// For Podman: "podman-compose" or "podman compose"
	ComposeCommand string

	// SupportsCompose indicates if compose functionality is available.
	SupportsCompose bool

	// Rootless indicates if the runtime is running in rootless mode.
	Rootless bool
}

// Detect automatically detects the available container runtime.
// It prefers Docker if both are available, unless Podman is explicitly requested.
func Detect() (*Info, error) {
	// Try Docker first
	if info, err := detectDocker(); err == nil {
		return info, nil
	}

	// Try Podman
	if info, err := detectPodman(); err == nil {
		return info, nil
	}

	return nil, fmt.Errorf("no container runtime found: install Docker or Podman")
}

// DetectSpecific detects a specific runtime.
func DetectSpecific(runtime ContainerRuntime) (*Info, error) {
	switch runtime {
	case RuntimeAuto:
		return Detect()
	case RuntimeDocker:
		return detectDocker()
	case RuntimePodman:
		return detectPodman()
	default:
		return nil, fmt.Errorf("invalid runtime: %s", runtime)
	}
}

// detectDocker checks if Docker is available and gets its info.
func detectDocker() (*Info, error) {
	// Check if docker binary exists
	dockerPath, err := exec.LookPath("docker")
	if err != nil {
		return nil, fmt.Errorf("docker not found: %w", err)
	}
	_ = dockerPath

	// Get Docker version
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get docker version: %w", err)
	}

	info := &Info{
		Runtime: RuntimeDocker,
		Version: strings.TrimSpace(string(output)),
	}

	// Check for Docker Compose (v2 plugin or standalone)
	if composeCmd := detectDockerCompose(); composeCmd != "" {
		info.ComposeCommand = composeCmd
		info.SupportsCompose = true
	}

	// Check if running rootless
	info.Rootless = isDockerRootless()

	return info, nil
}

// detectDockerCompose detects the Docker Compose command.
func detectDockerCompose() string {
	// Try "docker compose" (v2 plugin) first
	cmd := exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err == nil {
		return "docker compose"
	}

	// Fall back to standalone "docker-compose"
	if _, err := exec.LookPath("docker-compose"); err == nil {
		return "docker-compose"
	}

	return ""
}

// isDockerRootless checks if Docker is running in rootless mode.
func isDockerRootless() bool {
	cmd := exec.Command("docker", "info", "--format", "{{.SecurityOptions}}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "rootless")
}

// detectPodman checks if Podman is available and gets its info.
func detectPodman() (*Info, error) {
	// Check if podman binary exists
	podmanPath, err := exec.LookPath("podman")
	if err != nil {
		return nil, fmt.Errorf("podman not found: %w", err)
	}
	_ = podmanPath

	// Get Podman version
	cmd := exec.Command("podman", "version", "--format", "{{.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get podman version: %w", err)
	}

	info := &Info{
		Runtime:  RuntimePodman,
		Version:  strings.TrimSpace(string(output)),
		Rootless: true, // Podman is rootless by default
	}

	// Check for Podman Compose
	if composeCmd := detectPodmanCompose(); composeCmd != "" {
		info.ComposeCommand = composeCmd
		info.SupportsCompose = true
	}

	return info, nil
}

// detectPodmanCompose detects the Podman Compose command.
func detectPodmanCompose() string {
	// Try "podman compose" (podman v4+ with compose plugin)
	cmd := exec.Command("podman", "compose", "version")
	if err := cmd.Run(); err == nil {
		return "podman compose"
	}

	// Fall back to standalone "podman-compose"
	if _, err := exec.LookPath("podman-compose"); err == nil {
		return "podman-compose"
	}

	return ""
}

// Commands returns the runtime-specific commands.
type Commands struct {
	// Runtime is the base runtime command (docker or podman).
	Runtime string

	// Compose is the compose command.
	Compose string

	// ComposeUp is the full command to start services.
	ComposeUp string

	// ComposeDown is the full command to stop services.
	ComposeDown string

	// ComposeLogs is the full command to view logs.
	ComposeLogs string

	// ComposePs is the full command to list services.
	ComposePs string

	// NetworkCreate is the command to create a network.
	NetworkCreate string

	// VolumeCreate is the command to create a volume.
	VolumeCreate string
}

// GetCommands returns the commands for a given runtime.
func GetCommands(runtime ContainerRuntime) *Commands {
	switch runtime {
	case RuntimePodman:
		return &Commands{
			Runtime:       "podman",
			Compose:       "podman-compose",
			ComposeUp:     "podman-compose up -d",
			ComposeDown:   "podman-compose down",
			ComposeLogs:   "podman-compose logs -f",
			ComposePs:     "podman-compose ps",
			NetworkCreate: "podman network create",
			VolumeCreate:  "podman volume create",
		}
	default: // Docker
		return &Commands{
			Runtime:       "docker",
			Compose:       "docker compose",
			ComposeUp:     "docker compose up -d",
			ComposeDown:   "docker compose down",
			ComposeLogs:   "docker compose logs -f",
			ComposePs:     "docker compose ps",
			NetworkCreate: "docker network create",
			VolumeCreate:  "docker volume create",
		}
	}
}

// SocketPath returns the default socket path for the runtime.
func SocketPath(runtime ContainerRuntime) string {
	switch runtime {
	case RuntimePodman:
		// Podman rootless socket is typically in XDG_RUNTIME_DIR
		return "${XDG_RUNTIME_DIR}/podman/podman.sock"
	default:
		return "/var/run/docker.sock"
	}
}

// GenerateComposeHeader generates the header comment for compose files.
func GenerateComposeHeader(runtime ContainerRuntime, projectName string) string {
	var sb strings.Builder

	sb.WriteString("# Generated by Homeport\n")
	sb.WriteString(fmt.Sprintf("# Project: %s\n", projectName))
	sb.WriteString("#\n")
	sb.WriteString("# Compatible with Docker and Podman\n")
	sb.WriteString("#\n")

	cmds := GetCommands(runtime)
	if runtime == RuntimePodman {
		sb.WriteString("# Using Podman:\n")
		sb.WriteString(fmt.Sprintf("#   Start:  %s\n", cmds.ComposeUp))
		sb.WriteString(fmt.Sprintf("#   Stop:   %s\n", cmds.ComposeDown))
		sb.WriteString(fmt.Sprintf("#   Logs:   %s\n", cmds.ComposeLogs))
	} else {
		sb.WriteString("# Using Docker:\n")
		sb.WriteString(fmt.Sprintf("#   Start:  %s\n", cmds.ComposeUp))
		sb.WriteString(fmt.Sprintf("#   Stop:   %s\n", cmds.ComposeDown))
		sb.WriteString(fmt.Sprintf("#   Logs:   %s\n", cmds.ComposeLogs))
		sb.WriteString("#\n")
		sb.WriteString("# Or with Podman:\n")
		podmanCmds := GetCommands(RuntimePodman)
		sb.WriteString(fmt.Sprintf("#   Start:  %s\n", podmanCmds.ComposeUp))
		sb.WriteString(fmt.Sprintf("#   Stop:   %s\n", podmanCmds.ComposeDown))
	}

	sb.WriteString("\n")
	return sb.String()
}
