# AgnosTech CLI - File Structure

This document shows the complete file structure of the CLI layer.

## Directory Tree

```
exit_gafam/
├── cmd/
│   └── agnostech/
│       └── main.go                    # Entry point
│
├── internal/
│   └── cli/
│       ├── root.go                    # Root command with global flags
│       ├── analyze.go                 # Analyze command
│       ├── migrate.go                 # Migrate command
│       ├── validate.go                # Validate command
│       ├── version.go                 # Version command
│       └── ui/
│           ├── progress.go            # Progress bar utilities
│           ├── table.go               # Table output utilities
│           └── prompt.go              # Interactive prompts
│
├── pkg/
│   └── version/
│       └── version.go                 # Version info for ldflags
│
├── test/
│   └── fixtures/
│       └── sample.tfstate             # Sample Terraform state
│
├── go.mod                             # Go module file
├── Makefile                           # Build automation
├── setup-cli.sh                       # Setup script
├── test-cli.sh                        # Test script
├── .gitignore                         # Git ignore rules
├── .agnostech.example.yaml            # Example configuration
├── CLI_README.md                      # CLI documentation
└── QUICKSTART.md                      # Quick start guide
```

## File Descriptions

### Core CLI Files

#### cmd/agnostech/main.go
- Entry point for the CLI application
- Initializes and executes the root command
- Handles fatal errors

#### internal/cli/root.go
- Defines the root command
- Manages global flags: --config, --verbose, --quiet
- Loads configuration from file or environment
- Provides helper functions for verbose/quiet mode

#### internal/cli/analyze.go
- Implements the `analyze` command
- Parses AWS infrastructure from Terraform state/config files
- Supports multiple output formats: JSON, YAML, Table
- Flags: --output, --format

#### internal/cli/migrate.go
- Implements the `migrate` command
- Generates self-hosted Docker stack from AWS infrastructure
- Creates Docker Compose, Traefik config, env files, documentation
- Flags: --output, --domain, --include-migration, --include-monitoring

#### internal/cli/validate.go
- Implements the `validate` command
- Validates generated stack configuration
- Checks Docker Compose syntax, files, networks, volumes
- Provides detailed validation results

#### internal/cli/version.go
- Implements the `version` command
- Displays version, commit, and build date
- Uses values from pkg/version package

### UI Components

#### internal/cli/ui/progress.go
- Progress bar implementation using Charm's Bubbles
- Provides both interactive and simple progress indicators
- Uses lipgloss for styling

#### internal/cli/ui/table.go
- Table rendering utilities
- Supports styled and simple ASCII tables
- Automatically calculates column widths

#### internal/cli/ui/prompt.go
- Interactive prompts for user input
- Yes/No prompts with defaults
- Selection prompts
- Styled messages: Error, Success, Warning, Info

### Version Package

#### pkg/version/version.go
- Version information variables
- Set via ldflags during build
- Variables: Version, Commit, Date

### Test Files

#### test/fixtures/sample.tfstate
- Sample Terraform state file
- Contains EC2, RDS, and S3 resources
- Used for testing and examples

### Build Files

#### Makefile
- Build automation
- Commands: build, install, test, clean, run
- Cross-platform builds
- Version injection via ldflags

#### setup-cli.sh
- Automated setup script
- Installs dependencies
- Builds and tests the CLI

#### test-cli.sh
- Comprehensive test script
- Tests all commands and flags
- Validates functionality

### Configuration

#### .agnostech.example.yaml
- Example configuration file
- Shows all available options
- Can be copied to ~/.agnostech.yaml

#### .gitignore
- Ignores binaries, build output
- Ignores temporary and generated files

## Dependencies

The CLI uses the following main dependencies:

- **github.com/spf13/cobra** - CLI framework
- **github.com/spf13/viper** - Configuration management
- **github.com/charmbracelet/lipgloss** - Terminal styling
- **github.com/charmbracelet/bubbles** - Terminal UI components
- **gopkg.in/yaml.v3** - YAML parsing

## Build Process

```bash
# Install dependencies
go mod download

# Build with version info
go build -ldflags "-X github.com/agnostech/agnostech/pkg/version.Version=1.0.0 \
  -X github.com/agnostech/agnostech/pkg/version.Commit=$(git rev-parse --short HEAD) \
  -X github.com/agnostech/agnostech/pkg/version.Date=$(date -u '+%Y-%m-%d_%H:%M:%S')" \
  -o bin/agnostech ./cmd/agnostech
```

## Command Flow

### Analyze Command
```
User Input → root.go (global flags) → analyze.go → performAnalysis()
→ outputAnalysisResults() → JSON/YAML/Table output
```

### Migrate Command
```
User Input → root.go (global flags) → migrate.go → performMigration()
→ performAnalysis() → generateDockerCompose() → generateTraefikConfig()
→ generateEnvFiles() → generateDocumentation()
```

### Validate Command
```
User Input → root.go (global flags) → validate.go → performValidation()
→ multiple validation checks → displayValidationResults()
```

## Extension Points

The CLI is designed to be extended:

1. **New Commands**: Add new commands in `internal/cli/`
2. **New Parsers**: Extend `performAnalysis()` for new input types
3. **New Generators**: Add generators in migrate.go
4. **New Validators**: Add validation checks in validate.go
5. **New UI Components**: Add components in `internal/cli/ui/`

## Integration with Core Logic

The CLI layer integrates with the core application:

```
CLI Layer (internal/cli/)
    ↓
Application Layer (internal/app/)
    ↓
Domain Layer (internal/domain/)
    ↓
Infrastructure Layer (internal/infrastructure/)
```

The CLI commands will call application services which use the domain models and infrastructure implementations.
