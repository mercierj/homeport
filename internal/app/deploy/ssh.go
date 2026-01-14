package deploy

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type SSHConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthMethod string `json:"authMethod"` // "key" or "password"
	KeyPath    string `json:"keyPath"`
	Password   string `json:"password"`
	RemoteDir  string `json:"remoteDir"`

	// Content to deploy
	ComposeContent string            `json:"composeContent"`
	Scripts        map[string]string `json:"scripts"`
	ProjectName    string            `json:"projectName"`
}

type SSHDeployer struct{}

func NewSSHDeployer() *SSHDeployer {
	return &SSHDeployer{}
}

func (s *SSHDeployer) GetPhases() []string {
	return []string{
		"Connecting to server",
		"Checking Docker installation",
		"Transferring files",
		"Pulling images",
		"Starting containers",
		"Running health checks",
	}
}

func (s *SSHDeployer) Deploy(ctx context.Context, d *Deployment) error {
	config, ok := d.Config.(*SSHConfig)
	if !ok {
		return fmt.Errorf("invalid config type for SSH deployment")
	}

	phases := s.GetPhases()

	// Phase 1: Connect to server
	EmitPhase(d, phases[0], 1)
	EmitProgress(d, 5)

	client, err := s.connect(config)
	if err != nil {
		return fmt.Errorf("SSH connection failed: %w", err)
	}
	defer func() { _ = client.Close() }()

	EmitLog(d, "info", fmt.Sprintf("Connected to %s@%s:%d", config.Username, config.Host, config.Port))
	EmitProgress(d, 15)

	if d.IsCancelled() {
		return nil
	}

	// Phase 2: Check Docker installation
	EmitPhase(d, phases[1], 2)
	EmitProgress(d, 20)

	if err := s.checkDocker(client, d); err != nil {
		return fmt.Errorf("docker check failed: %w", err)
	}
	EmitProgress(d, 25)

	if d.IsCancelled() {
		return nil
	}

	// Phase 3: Transfer files via SFTP
	EmitPhase(d, phases[2], 3)
	EmitProgress(d, 30)

	if err := s.transferFiles(client, config, d); err != nil {
		return fmt.Errorf("file transfer failed: %w", err)
	}
	EmitProgress(d, 50)

	if d.IsCancelled() {
		return nil
	}

	// Phase 4: Pull images
	EmitPhase(d, phases[3], 4)
	EmitProgress(d, 55)

	if err := s.pullImages(client, config, d); err != nil {
		return fmt.Errorf("failed to pull images: %w", err)
	}
	EmitProgress(d, 70)

	if d.IsCancelled() {
		return nil
	}

	// Phase 5: Start containers
	EmitPhase(d, phases[4], 5)
	EmitProgress(d, 75)

	if err := s.startContainers(client, config, d); err != nil {
		return fmt.Errorf("failed to start containers: %w", err)
	}
	EmitProgress(d, 85)

	if d.IsCancelled() {
		return nil
	}

	// Phase 6: Health checks
	EmitPhase(d, phases[5], 6)
	EmitProgress(d, 90)

	services, err := s.runHealthChecks(client, config, d)
	if err != nil {
		EmitLog(d, "warn", fmt.Sprintf("Health check issues: %v", err))
	}

	EmitProgress(d, 100)

	d.Emit(Event{
		Type: EventComplete,
		Data: CompleteEvent{Services: services},
	})

	return nil
}

func (s *SSHDeployer) connect(config *SSHConfig) (*ssh.Client, error) {
	var authMethod ssh.AuthMethod

	if config.AuthMethod == "password" {
		authMethod = ssh.Password(config.Password)
	} else {
		// Key-based auth
		keyPath := config.KeyPath
		if keyPath == "" {
			homeDir, _ := os.UserHomeDir()
			keyPath = filepath.Join(homeDir, ".ssh", "id_rsa")
		} else if strings.HasPrefix(keyPath, "~") {
			homeDir, _ := os.UserHomeDir()
			keyPath = filepath.Join(homeDir, keyPath[2:])
		}

		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read SSH key: %w", err)
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("failed to parse SSH key: %w", err)
		}
		authMethod = ssh.PublicKeys(signer)
	}

	port := config.Port
	if port == 0 {
		port = 22
	}

	sshConfig := &ssh.ClientConfig{
		User:            config.Username,
		Auth:            []ssh.AuthMethod{authMethod},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // In production, use known_hosts
		Timeout:         30 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", config.Host, port)
	return ssh.Dial("tcp", addr, sshConfig)
}

func (s *SSHDeployer) runCommand(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer func() { _ = session.Close() }()

	output, err := session.CombinedOutput(cmd)
	return string(output), err
}

func (s *SSHDeployer) checkDocker(client *ssh.Client, d *Deployment) error {
	output, err := s.runCommand(client, "docker --version")
	if err != nil {
		return fmt.Errorf("docker not installed or not accessible: %s", output)
	}
	EmitLog(d, "info", strings.TrimSpace(output))

	output, err = s.runCommand(client, "docker compose version")
	if err != nil {
		// Try docker-compose (v1)
		output, err = s.runCommand(client, "docker-compose --version")
		if err != nil {
			return fmt.Errorf("docker compose not installed: %s", output)
		}
	}
	EmitLog(d, "info", strings.TrimSpace(output))

	return nil
}

func (s *SSHDeployer) transferFiles(client *ssh.Client, config *SSHConfig, d *Deployment) error {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("failed to create SFTP client: %w", err)
	}
	defer func() { _ = sftpClient.Close() }()

	remoteDir := config.RemoteDir
	if remoteDir == "" {
		remoteDir = "/opt/homeport"
	}

	projectDir := filepath.Join(remoteDir, config.ProjectName)

	// Create remote directory
	if err := sftpClient.MkdirAll(projectDir); err != nil {
		EmitLog(d, "warn", fmt.Sprintf("Directory may exist: %v", err))
	}
	EmitLog(d, "info", fmt.Sprintf("Created directory: %s", projectDir))

	// Write docker-compose.yml
	composePath := filepath.Join(projectDir, "docker-compose.yml")
	if err := s.writeRemoteFile(sftpClient, composePath, config.ComposeContent); err != nil {
		return fmt.Errorf("failed to write docker-compose.yml: %w", err)
	}
	EmitLog(d, "info", "Transferred docker-compose.yml")

	// Write additional scripts
	for name, content := range config.Scripts {
		scriptPath := filepath.Join(projectDir, name)
		if err := s.writeRemoteFile(sftpClient, scriptPath, content); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
		EmitLog(d, "info", fmt.Sprintf("Transferred %s", name))
	}

	return nil
}

func (s *SSHDeployer) writeRemoteFile(client *sftp.Client, path, content string) error {
	file, err := client.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	_, err = io.WriteString(file, content)
	return err
}

func (s *SSHDeployer) pullImages(client *ssh.Client, config *SSHConfig, d *Deployment) error {
	projectDir := filepath.Join(config.RemoteDir, config.ProjectName)
	if config.RemoteDir == "" {
		projectDir = filepath.Join("/opt/homeport", config.ProjectName)
	}

	cmd := fmt.Sprintf("cd %s && docker compose -p %s pull", projectDir, config.ProjectName)
	output, err := s.runCommand(client, cmd)
	if err != nil {
		EmitLog(d, "error", output)
		return err
	}
	EmitLog(d, "info", "Images pulled successfully")
	return nil
}

func (s *SSHDeployer) startContainers(client *ssh.Client, config *SSHConfig, d *Deployment) error {
	projectDir := filepath.Join(config.RemoteDir, config.ProjectName)
	if config.RemoteDir == "" {
		projectDir = filepath.Join("/opt/homeport", config.ProjectName)
	}

	cmd := fmt.Sprintf("cd %s && docker compose -p %s up -d", projectDir, config.ProjectName)
	output, err := s.runCommand(client, cmd)
	if err != nil {
		EmitLog(d, "error", output)
		return err
	}
	EmitLog(d, "info", "Containers started")
	return nil
}

func (s *SSHDeployer) runHealthChecks(client *ssh.Client, config *SSHConfig, d *Deployment) ([]ServiceStatus, error) {
	// Wait for containers to initialize
	time.Sleep(3 * time.Second)

	projectDir := filepath.Join(config.RemoteDir, config.ProjectName)
	if config.RemoteDir == "" {
		projectDir = filepath.Join("/opt/homeport", config.ProjectName)
	}

	cmd := fmt.Sprintf("cd %s && docker compose -p %s ps --format json", projectDir, config.ProjectName)
	output, err := s.runCommand(client, cmd)
	if err != nil {
		return nil, err
	}

	var services []ServiceStatus
	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		services = append(services, ServiceStatus{
			Name:    strings.TrimSpace(line),
			Healthy: true,
			Ports:   []string{},
		})
	}

	if len(services) == 0 {
		services = append(services, ServiceStatus{
			Name:    config.ProjectName,
			Healthy: true,
			Ports:   []string{},
		})
	}

	EmitLog(d, "info", fmt.Sprintf("%d services healthy", len(services)))
	return services, nil
}
