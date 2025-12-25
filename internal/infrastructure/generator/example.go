// Package generator provides example usage of the generators.
package generator

import (
	"fmt"
	"time"

	"github.com/agnostech/agnostech/internal/domain/mapper"
	"github.com/agnostech/agnostech/internal/infrastructure/generator/compose"
	"github.com/agnostech/agnostech/internal/infrastructure/generator/docs"
	"github.com/agnostech/agnostech/internal/infrastructure/generator/scripts"
	"github.com/agnostech/agnostech/internal/infrastructure/generator/traefik"
)

// Example demonstrates how to use the generators.
func Example() error {
	// Sample mapping results (would come from mappers in real usage)
	results := []*mapper.MappingResult{
		{
			DockerService: &mapper.DockerService{
				Name:  "postgres",
				Image: "postgres:15",
				Environment: map[string]string{
					"POSTGRES_PASSWORD": "changeme",
					"POSTGRES_DB":       "myapp",
				},
				Ports:    []string{"5432:5432"},
				Volumes:  []string{"postgres-data:/var/lib/postgresql/data"},
				Networks: []string{"internal"},
				Restart:  "unless-stopped",
				HealthCheck: &mapper.HealthCheck{
					Test:     []string{"CMD-SHELL", "pg_isready -U postgres"},
					Interval: 10 * time.Second,
					Timeout:  5 * time.Second,
					Retries:  5,
				},
			},
			Configs: map[string][]byte{},
			Scripts: map[string][]byte{
				"migrate-s3.sh": []byte("#!/bin/bash\n# Migrate S3 buckets to MinIO\n"),
			},
			Warnings: []string{
				"PostgreSQL version differences may exist",
			},
			ManualSteps: []string{
				"Review database connection strings in application",
				"Update environment variables with new database endpoint",
			},
		},
		{
			DockerService: &mapper.DockerService{
				Name:  "minio",
				Image: "minio/minio:latest",
				Environment: map[string]string{
					"MINIO_ROOT_USER":     "minioadmin",
					"MINIO_ROOT_PASSWORD": "minioadmin",
				},
				Ports:    []string{"9000:9000", "9001:9001"},
				Volumes:  []string{"minio-data:/data"},
				Networks: []string{"web", "internal"},
				Restart:  "unless-stopped",
				Command:  []string{"server", "/data", "--console-address", ":9001"},
				Labels: map[string]string{
					"traefik.enable":                                        "true",
					"traefik.http.routers.minio.rule":                       "Host(`minio.example.com`)",
					"traefik.http.routers.minio.entrypoints":                "websecure",
					"traefik.http.routers.minio.tls.certresolver":           "letsencrypt",
					"traefik.http.services.minio.loadbalancer.server.port": "9000",
				},
			},
			Configs: map[string][]byte{},
			Scripts: map[string][]byte{},
			Warnings: []string{
				"MinIO does not support all S3 features",
			},
			ManualSteps: []string{
				"Review bucket policies and migrate manually",
				"Update application S3 endpoint configuration",
			},
		},
	}

	// Generate Docker Compose
	fmt.Println("Generating Docker Compose configuration...")
	composeGen := compose.NewGenerator("myproject")
	composeOutput, err := composeGen.Generate(results)
	if err != nil {
		return fmt.Errorf("failed to generate compose: %w", err)
	}
	fmt.Printf("Generated %d files\n", len(composeOutput.Files))

	// Generate Traefik configuration
	fmt.Println("\nGenerating Traefik configuration...")
	traefikConfig := &traefik.Config{
		Email:           "admin@example.com",
		Domain:          "example.com",
		DashboardUser:   "admin",
		DashboardPass:   "changeme",
		EnableMetrics:   true,
		EnableDashboard: true,
	}
	traefikGen := traefik.NewGenerator(traefikConfig)
	traefikOutput, err := traefikGen.Generate(results)
	if err != nil {
		return fmt.Errorf("failed to generate traefik config: %w", err)
	}
	fmt.Printf("Generated %d files\n", len(traefikOutput.Files))

	// Generate migration scripts
	fmt.Println("\nGenerating migration scripts...")
	migrationGen := scripts.NewMigrationGenerator("myproject", "us-east-1")
	migrationOutput, err := migrationGen.Generate(results)
	if err != nil {
		return fmt.Errorf("failed to generate migration scripts: %w", err)
	}
	fmt.Printf("Generated %d files\n", len(migrationOutput.Files))

	// Generate backup scripts
	fmt.Println("\nGenerating backup scripts...")
	backupGen := scripts.NewBackupGenerator("myproject", 7)
	backupOutput, err := backupGen.Generate(results)
	if err != nil {
		return fmt.Errorf("failed to generate backup scripts: %w", err)
	}
	fmt.Printf("Generated %d files\n", len(backupOutput.Files))

	// Generate documentation
	fmt.Println("\nGenerating documentation...")
	docsGen := docs.NewGenerator("myproject", "example.com")
	docsOutput, err := docsGen.Generate(results)
	if err != nil {
		return fmt.Errorf("failed to generate documentation: %w", err)
	}
	fmt.Printf("Generated %d files\n", len(docsOutput.Files))

	// Print warnings
	fmt.Println("\nWarnings:")
	for _, output := range []*mapper.MappingResult{results[0]} {
		for _, warning := range output.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}

	return nil
}
