// Package swarm generates Docker Swarm stack configurations from mapping results.
package swarm

import (
	"context"
	"fmt"
	"time"

	"github.com/homeport/homeport/internal/domain/generator"
	"github.com/homeport/homeport/internal/domain/mapper"
	"github.com/homeport/homeport/internal/domain/target"
)

// Generator generates Docker Swarm stack files from mapping results.
// It implements the generator.TargetGenerator interface.
type Generator struct {
	projectName string
}

// New creates a new Docker Swarm generator.
func New() *Generator {
	return &Generator{
		projectName: "homeport",
	}
}

// NewWithProjectName creates a new Docker Swarm generator with a custom project name.
func NewWithProjectName(projectName string) *Generator {
	return &Generator{
		projectName: projectName,
	}
}

// Platform returns the target platform this generator handles.
func (g *Generator) Platform() target.Platform {
	return target.PlatformDockerSwarm
}

// Name returns the name of this generator.
func (g *Generator) Name() string {
	return "docker-swarm"
}

// Description returns a human-readable description.
func (g *Generator) Description() string {
	return "Generates Docker Swarm stack files with deploy configurations, replicas, update/rollback strategies, and overlay networks"
}

// SupportedHALevels returns the HA levels this generator supports.
func (g *Generator) SupportedHALevels() []target.HALevel {
	return target.SupportedHALevelsForPlatform(target.PlatformDockerSwarm)
}

// RequiresCredentials returns true if the platform needs cloud credentials.
func (g *Generator) RequiresCredentials() bool {
	return false
}

// RequiredCredentials returns the list of required credential keys.
func (g *Generator) RequiredCredentials() []string {
	return nil
}

// Validate checks if the mapping results can be deployed to Docker Swarm.
func (g *Generator) Validate(results []*mapper.MappingResult, config *generator.TargetConfig) error {
	if len(results) == 0 {
		return fmt.Errorf("no mapping results provided")
	}

	if config == nil {
		return fmt.Errorf("target configuration is required")
	}

	// Validate HA level is supported
	supportedLevels := g.SupportedHALevels()
	levelSupported := false
	for _, level := range supportedLevels {
		if level == config.HALevel {
			levelSupported = true
			break
		}
	}
	if !levelSupported {
		return fmt.Errorf("HA level %q is not supported by Docker Swarm (supported: none, basic, multi-server, cluster)", config.HALevel)
	}

	// Validate that at least one result has a Docker service
	hasService := false
	for _, result := range results {
		if result != nil && result.DockerService != nil {
			hasService = true
			break
		}
	}
	if !hasService {
		return fmt.Errorf("no Docker services found in mapping results")
	}

	return nil
}

// Generate produces Docker Swarm stack files for the target platform.
func (g *Generator) Generate(ctx context.Context, results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.TargetOutput, error) {
	if err := g.Validate(results, config); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Set project name from config if provided
	projectName := g.projectName
	if config.ProjectName != "" {
		projectName = config.ProjectName
	}

	output := generator.NewTargetOutput(target.PlatformDockerSwarm)

	// Create stack generator with configuration
	stackGen := NewStackGenerator(projectName, config)

	// Generate the main stack file
	stackContent, err := stackGen.Generate(results)
	if err != nil {
		return nil, fmt.Errorf("failed to generate stack file: %w", err)
	}

	// Add the main stack file
	output.AddDockerFile("docker-stack.yml", []byte(stackContent))
	output.MainFile = "docker-stack.yml"

	// Generate deployment script
	deployScript := g.generateDeployScript(projectName, config)
	output.AddScript("deploy.sh", []byte(deployScript))

	// Generate README
	readme := g.generateReadme(projectName, config, results)
	output.AddDoc("README.md", []byte(readme))

	// Add metadata
	output.Summary = fmt.Sprintf("Generated Docker Swarm stack with %d services at HA level %s",
		countServices(results), config.HALevel)

	// Add manual steps for multi-server setups
	if config.HALevel.RequiresMultiServer() {
		output.AddManualStep("Initialize Docker Swarm on the manager node: docker swarm init")
		output.AddManualStep("Join worker nodes: docker swarm join --token <token> <manager-ip>:2377")
		output.AddManualStep("Deploy the stack: docker stack deploy -c docker-stack.yml " + projectName)
	} else {
		output.AddManualStep("Initialize Docker Swarm (single node): docker swarm init")
		output.AddManualStep("Deploy the stack: docker stack deploy -c docker-stack.yml " + projectName)
	}

	return output, nil
}

// EstimateCost estimates the monthly cost for the deployment.
func (g *Generator) EstimateCost(results []*mapper.MappingResult, config *generator.TargetConfig) (*generator.CostEstimate, error) {
	// Docker Swarm itself is free, cost depends on infrastructure
	estimate := generator.NewCostEstimate("EUR")
	estimate.AddNote("Docker Swarm is open source with no licensing costs")
	estimate.AddNote("Costs depend on the underlying infrastructure (servers, storage, networking)")

	// Estimate based on HA level
	switch config.HALevel {
	case target.HALevelNone:
		estimate.Compute = 10.0 // Single VPS
		estimate.AddNote("Single server setup - minimal costs")
	case target.HALevelBasic:
		estimate.Compute = 15.0
		estimate.Storage = 5.0
		estimate.AddNote("Single server with backup storage")
	case target.HALevelMultiServer:
		estimate.Compute = 40.0 // 2+ servers
		estimate.Storage = 10.0
		estimate.Network = 5.0
		estimate.AddNote("Multi-server setup with floating IP")
	case target.HALevelCluster:
		estimate.Compute = 80.0 // 3+ servers
		estimate.Storage = 20.0
		estimate.Network = 10.0
		estimate.AddNote("Full cluster with load balancing")
	}

	estimate.Calculate()
	return estimate, nil
}

// generateDeployScript generates a bash script for deploying the stack.
func (g *Generator) generateDeployScript(projectName string, config *generator.TargetConfig) string {
	script := `#!/bin/bash
# Docker Swarm Deployment Script
# Generated by Homeport - ` + time.Now().Format(time.RFC3339) + `
# Project: ` + projectName + `
# HA Level: ` + config.HALevel.String() + `

set -e

STACK_NAME="` + projectName + `"
STACK_FILE="docker-stack.yml"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Docker is running
check_docker() {
    if ! docker info >/dev/null 2>&1; then
        log_error "Docker is not running. Please start Docker first."
        exit 1
    fi
}

# Check if Swarm is initialized
check_swarm() {
    if ! docker info 2>/dev/null | grep -q "Swarm: active"; then
        log_warn "Docker Swarm is not initialized."
        read -p "Initialize Swarm now? (y/n) " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            docker swarm init
            log_info "Swarm initialized successfully."
        else
            log_error "Swarm is required for stack deployment."
            exit 1
        fi
    else
        log_info "Swarm is active."
    fi
}

# Create required networks
create_networks() {
    log_info "Creating overlay networks..."

    if ! docker network ls | grep -q "homeport_web"; then
        docker network create --driver overlay --attachable homeport_web
        log_info "Created homeport_web network"
    fi

    if ! docker network ls | grep -q "homeport_internal"; then
        docker network create --driver overlay --attachable --internal homeport_internal
        log_info "Created homeport_internal network"
    fi
}

# Deploy the stack
deploy_stack() {
    log_info "Deploying stack: $STACK_NAME"
    docker stack deploy -c "$STACK_FILE" "$STACK_NAME" --with-registry-auth
    log_info "Stack deployed successfully!"
}

# Show stack status
show_status() {
    log_info "Stack status:"
    docker stack services "$STACK_NAME"
    echo
    log_info "Service replicas:"
    docker service ls --filter "label=com.docker.stack.namespace=$STACK_NAME"
}

# Main execution
main() {
    log_info "Starting deployment for $STACK_NAME..."
    check_docker
    check_swarm
    create_networks
    deploy_stack
    echo
    show_status
    echo
    log_info "Deployment complete!"
    log_info "Monitor services: docker service logs -f ${STACK_NAME}_<service>"
    log_info "Scale a service: docker service scale ${STACK_NAME}_<service>=<replicas>"
}

main "$@"
`
	return script
}

// generateReadme generates a README for the deployment.
func (g *Generator) generateReadme(projectName string, config *generator.TargetConfig, results []*mapper.MappingResult) string {
	haReqs := config.HALevel.Requirements()

	readme := `# ` + projectName + ` - Docker Swarm Stack

Generated by Homeport on ` + time.Now().Format(time.RFC3339) + `

## Overview

This Docker Swarm stack deploys your cloud infrastructure as self-hosted containers.

- **HA Level**: ` + config.HALevel.String() + ` (` + haReqs.Description + `)
- **RTO**: ` + haReqs.RTO + `
- **RPO**: ` + haReqs.RPO + `
- **Services**: ` + fmt.Sprintf("%d", countServices(results)) + `

## Prerequisites

- Docker Engine 20.10+
- Docker Swarm initialized
`

	if config.HALevel.RequiresMultiServer() {
		readme += `- ` + fmt.Sprintf("%d", haReqs.MinServers) + ` servers minimum
- Network connectivity between nodes
`
	}

	readme += `
## Quick Start

1. **Initialize Docker Swarm** (if not already done):
   ` + "```bash" + `
   docker swarm init
   ` + "```" + `

2. **Deploy the stack**:
   ` + "```bash" + `
   ./deploy.sh
   # or manually:
   docker stack deploy -c docker-stack.yml ` + projectName + `
   ` + "```" + `

3. **Check status**:
   ` + "```bash" + `
   docker stack services ` + projectName + `
   ` + "```" + `

## Stack Management

### View logs
` + "```bash" + `
docker service logs -f ` + projectName + `_<service-name>
` + "```" + `

### Scale a service
` + "```bash" + `
docker service scale ` + projectName + `_<service-name>=<replicas>
` + "```" + `

### Update a service
` + "```bash" + `
docker service update --image <new-image> ` + projectName + `_<service-name>
` + "```" + `

### Remove the stack
` + "```bash" + `
docker stack rm ` + projectName + `
` + "```" + `

## HA Configuration

`

	for _, feature := range haReqs.Features {
		readme += "- " + feature + "\n"
	}

	readme += `
## Networking

The stack uses the following overlay networks:
- **homeport_web**: Public-facing services (Traefik ingress)
- **homeport_internal**: Internal service communication (encrypted)

## Secrets and Configs

Sensitive data should be stored in Docker secrets:
` + "```bash" + `
echo "password" | docker secret create db_password -
docker secret create tls_cert ./cert.pem
` + "```" + `

## Monitoring

`
	if config.IncludeMonitoring {
		readme += `Monitoring is enabled with Prometheus and Grafana.
Access Grafana at: http://<manager-ip>:3000
`
	} else {
		readme += `Monitoring can be enabled by setting IncludeMonitoring in the config.
`
	}

	readme += `
## Troubleshooting

### Service not starting
` + "```bash" + `
docker service ps --no-trunc ` + projectName + `_<service-name>
` + "```" + `

### Check node status
` + "```bash" + `
docker node ls
` + "```" + `

### Inspect a service
` + "```bash" + `
docker service inspect ` + projectName + `_<service-name>
` + "```" + `
`

	return readme
}

// countServices counts the total number of services in the results.
func countServices(results []*mapper.MappingResult) int {
	count := 0
	for _, result := range results {
		if result != nil && result.DockerService != nil {
			count++
		}
		if result != nil {
			count += len(result.AdditionalServices)
		}
	}
	return count
}

// init registers the generator with the default registry.
func init() {
	generator.RegisterGenerator(New())
}
