package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type ContainerInfo struct {
	Name   string `json:"Name"`
	State  string `json:"State"`
	Health string `json:"Health"`
	Ports  string `json:"Ports"`
}

type HealthChecker struct {
	maxRetries int
	retryDelay time.Duration
}

func NewHealthChecker() *HealthChecker {
	return &HealthChecker{
		maxRetries: 30,
		retryDelay: 2 * time.Second,
	}
}

func (h *HealthChecker) CheckContainerHealth(ctx context.Context, projectName string) ([]ServiceStatus, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", projectName, "ps", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get container status: %w", err)
	}

	var services []ServiceStatus
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")

	for _, line := range lines {
		if line == "" {
			continue
		}

		var info ContainerInfo
		if err := json.Unmarshal([]byte(line), &info); err != nil {
			// Try alternative parsing
			services = append(services, ServiceStatus{
				Name:    line,
				Healthy: true,
				Ports:   []string{},
			})
			continue
		}

		healthy := info.State == "running" && (info.Health == "" || info.Health == "healthy")

		var ports []string
		if info.Ports != "" {
			ports = strings.Split(info.Ports, ", ")
		}

		services = append(services, ServiceStatus{
			Name:    info.Name,
			Healthy: healthy,
			Ports:   ports,
		})
	}

	return services, nil
}

func (h *HealthChecker) WaitForHealthy(ctx context.Context, d *Deployment, projectName string) ([]ServiceStatus, error) {
	var lastServices []ServiceStatus

	for i := 0; i < h.maxRetries; i++ {
		select {
		case <-ctx.Done():
			return lastServices, ctx.Err()
		default:
		}

		services, err := h.CheckContainerHealth(ctx, projectName)
		if err != nil {
			EmitLog(d, "warn", fmt.Sprintf("Health check attempt %d failed: %v", i+1, err))
			time.Sleep(h.retryDelay)
			continue
		}

		lastServices = services

		allHealthy := true
		for _, svc := range services {
			if !svc.Healthy {
				allHealthy = false
				break
			}
		}

		if allHealthy && len(services) > 0 {
			return services, nil
		}

		EmitLog(d, "info", fmt.Sprintf("Waiting for containers to become healthy (%d/%d)", i+1, h.maxRetries))
		time.Sleep(h.retryDelay)
	}

	return lastServices, fmt.Errorf("containers did not become healthy within timeout")
}

func (h *HealthChecker) GetContainerLogs(ctx context.Context, projectName, serviceName string, lines int) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", projectName, "logs", "--tail", fmt.Sprintf("%d", lines), serviceName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}
	return string(output), nil
}

func (h *HealthChecker) StopContainers(ctx context.Context, workDir, projectName string) error {
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", projectName, "down")
	cmd.Dir = workDir
	return cmd.Run()
}
