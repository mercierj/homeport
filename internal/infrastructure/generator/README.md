# Infrastructure Generators

This package contains generators that create deployment artifacts from AWS resource mappings.

## Overview

The generators transform mapping results (AWS resources mapped to self-hosted alternatives) into production-ready deployment configurations.

## Generators

### Docker Compose Generator (`compose/`)

Generates `docker-compose.yml` files with:
- Service definitions with proper configuration
- Network configuration (web and internal networks)
- Volume definitions
- Dependency management with topological sorting
- Health checks
- Resource limits

**Usage:**

```go
import "github.com/cloudexit/cloudexit/internal/infrastructure/generator/compose"

gen := compose.NewGenerator("myproject")
output, err := gen.Generate(mappingResults)
if err != nil {
    log.Fatal(err)
}

// output.Files contains "docker-compose.yml"
```

### Traefik Generator (`traefik/`)

Generates Traefik reverse proxy configurations:
- Static configuration (`traefik.yml`)
- Dynamic middleware configuration
- Let's Encrypt ACME setup
- Dashboard with basic auth
- Prometheus metrics endpoint
- Security headers

**Usage:**

```go
import "github.com/cloudexit/cloudexit/internal/infrastructure/generator/traefik"

config := &traefik.Config{
    Email:           "admin@example.com",
    Domain:          "example.com",
    DashboardUser:   "admin",
    DashboardPass:   "secret",
    EnableMetrics:   true,
    EnableDashboard: true,
}

gen := traefik.NewGenerator(config)
output, err := gen.Generate(mappingResults)
```

### Migration Scripts Generator (`scripts/migration.go`)

Generates shell scripts for migrating data from AWS:
- S3 to MinIO migration
- RDS to PostgreSQL/MySQL migration
- DynamoDB migration
- Master migration orchestrator

**Features:**
- Progress indicators
- Error handling
- Data verification
- Backup creation

**Usage:**

```go
import "github.com/cloudexit/cloudexit/internal/infrastructure/generator/scripts"

gen := scripts.NewMigrationGenerator("myproject", "us-east-1")
output, err := gen.Generate(mappingResults)

// Output files:
// - scripts/migrate-s3.sh
// - scripts/migrate-rds.sh
// - scripts/migrate-dynamodb.sh
// - scripts/migrate.sh
```

### Backup Scripts Generator (`scripts/backup.go`)

Generates automated backup and restore scripts:
- Database backups (PostgreSQL, MySQL, MongoDB)
- Redis backups
- MinIO backups
- Systemd timer for automation
- Configurable retention

**Usage:**

```go
gen := scripts.NewBackupGenerator("myproject", 7) // 7 days retention
output, err := gen.Generate(mappingResults)

// Output files:
// - scripts/backup.sh
// - scripts/restore.sh
// - scripts/backup.timer
// - scripts/backup.service
```

### Documentation Generator (`docs/`)

Generates comprehensive documentation:
- `README.md` - Main documentation
- `ARCHITECTURE.md` - System architecture overview
- `MIGRATION.md` - Step-by-step migration guide
- `.env.example` - Environment variables template

**Usage:**

```go
import "github.com/cloudexit/cloudexit/internal/infrastructure/generator/docs"

gen := docs.NewGenerator("myproject", "example.com")
output, err := gen.Generate(mappingResults)
```

## Output Structure

All generators return a `*generator.Output` structure:

```go
type Output struct {
    Files    map[string]string  // filename -> content
    Warnings []string           // warnings generated
    Metadata map[string]string  // additional metadata
}
```

## Network Configuration

The generators use two Docker networks:

- **web**: Public-facing network for services exposed via Traefik
- **internal**: Private network for service-to-service communication

Services are automatically assigned to appropriate networks based on their configuration.

## Complete Example

```go
package main

import (
    "log"
    "os"
    "path/filepath"

    "github.com/cloudexit/cloudexit/internal/domain/mapper"
    "github.com/cloudexit/cloudexit/internal/infrastructure/generator/compose"
    "github.com/cloudexit/cloudexit/internal/infrastructure/generator/traefik"
    "github.com/cloudexit/cloudexit/internal/infrastructure/generator/scripts"
    "github.com/cloudexit/cloudexit/internal/infrastructure/generator/docs"
)

func main() {
    // Get mapping results from mappers
    var results []*mapper.MappingResult
    // ... populate results ...

    // Generate all artifacts
    generators := map[string]interface{}{
        "compose": compose.NewGenerator("myproject"),
        "traefik": traefik.NewGenerator(&traefik.Config{
            Email:  "admin@example.com",
            Domain: "example.com",
        }),
        "migration": scripts.NewMigrationGenerator("myproject", "us-east-1"),
        "backup":    scripts.NewBackupGenerator("myproject", 7),
        "docs":      docs.NewGenerator("myproject", "example.com"),
    }

    outputDir := "./output"
    os.MkdirAll(outputDir, 0755)

    for name, gen := range generators {
        log.Printf("Generating %s...", name)

        var output *generator.Output
        var err error

        switch g := gen.(type) {
        case *compose.Generator:
            output, err = g.Generate(results)
        case *traefik.Generator:
            output, err = g.Generate(results)
        case *scripts.MigrationGenerator:
            output, err = g.Generate(results)
        case *scripts.BackupGenerator:
            output, err = g.Generate(results)
        case *docs.Generator:
            output, err = g.Generate(results)
        }

        if err != nil {
            log.Fatalf("Failed to generate %s: %v", name, err)
        }

        // Write files
        for filename, content := range output.Files {
            path := filepath.Join(outputDir, filename)
            os.MkdirAll(filepath.Dir(path), 0755)

            if err := os.WriteFile(path, []byte(content), 0644); err != nil {
                log.Fatalf("Failed to write %s: %v", path, err)
            }

            log.Printf("  Generated: %s", filename)
        }

        // Print warnings
        for _, warning := range output.Warnings {
            log.Printf("  Warning: %s", warning)
        }
    }

    log.Println("Generation complete!")
}
```

## Templates

Template files are located in `/templates/` and embedded using `go:embed`:

- `templates/compose/service.yml.tmpl`
- `templates/traefik/traefik.yml.tmpl`
- `templates/scripts/migrate-s3.sh.tmpl`
- `templates/scripts/migrate-rds.sh.tmpl`
- `templates/docs/README.md.tmpl`

Templates use Go's `text/template` package.

## Testing

Run tests for all generators:

```bash
go test ./internal/infrastructure/generator/...
```

## Contributing

When adding new generators:

1. Create a new package under `generator/`
2. Implement the generation logic
3. Add templates to `/templates/` if needed
4. Update this README
5. Add tests

## Architecture

```
generator/
├── compose/          # Docker Compose generator
│   ├── compose.go
│   └── networks.go
├── traefik/          # Traefik configuration
│   └── traefik.go
├── scripts/          # Migration and backup scripts
│   ├── migration.go
│   └── backup.go
├── docs/             # Documentation generator
│   └── readme.go
└── example.go        # Usage examples
```

## See Also

- [Domain Generator Interface](/internal/domain/generator/)
- [Mapper Interface](/internal/domain/mapper/)
- [Templates](/templates/)
