#!/bin/bash

# AgnosTech CLI - Build Verification Script
# This script verifies that the CLI can be built successfully

echo "================================"
echo "AgnosTech CLI Build Verification"
echo "================================"
echo ""

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo "Error: go.mod not found. Please run this script from the project root."
    exit 1
fi

# Check Go installation
echo "Checking Go installation..."
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed or not in PATH"
    exit 1
fi

GO_VERSION=$(go version)
echo "✓ Found: $GO_VERSION"
echo ""

# Download dependencies
echo "Downloading dependencies..."
if go mod download; then
    echo "✓ Dependencies downloaded"
else
    echo "✗ Failed to download dependencies"
    exit 1
fi
echo ""

# Tidy modules
echo "Tidying modules..."
if go mod tidy; then
    echo "✓ Modules tidied"
else
    echo "✗ Failed to tidy modules"
    exit 1
fi
echo ""

# Verify all files exist
echo "Verifying source files..."
files=(
    "cmd/agnostech/main.go"
    "internal/cli/root.go"
    "internal/cli/analyze.go"
    "internal/cli/migrate.go"
    "internal/cli/validate.go"
    "internal/cli/version.go"
    "internal/cli/ui/progress.go"
    "internal/cli/ui/table.go"
    "internal/cli/ui/prompt.go"
    "pkg/version/version.go"
)

missing_files=0
for file in "${files[@]}"; do
    if [ -f "$file" ]; then
        echo "✓ $file"
    else
        echo "✗ $file (missing)"
        missing_files=$((missing_files + 1))
    fi
done

if [ $missing_files -gt 0 ]; then
    echo ""
    echo "Error: $missing_files source files are missing"
    exit 1
fi
echo ""

# Try to build
echo "Building the CLI..."
echo ""

# Create bin directory
mkdir -p bin

# Build with version info
VERSION="dev"
COMMIT="unknown"
DATE=$(date -u '+%Y-%m-%d_%H:%M:%S')

# Check if git is available
if command -v git &> /dev/null; then
    if git rev-parse --git-dir > /dev/null 2>&1; then
        COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
    fi
fi

LDFLAGS="-X github.com/agnostech/agnostech/pkg/version.Version=$VERSION -X github.com/agnostech/agnostech/pkg/version.Commit=$COMMIT -X github.com/agnostech/agnostech/pkg/version.Date=$DATE"

if go build -ldflags "$LDFLAGS" -o bin/agnostech ./cmd/agnostech; then
    echo ""
    echo "✓ Build successful!"
    echo ""
else
    echo ""
    echo "✗ Build failed!"
    echo ""
    exit 1
fi

# Test the binary
echo "Testing the binary..."
echo ""

if [ -x "bin/agnostech" ]; then
    echo "✓ Binary is executable"
else
    echo "✗ Binary is not executable"
    chmod +x bin/agnostech
    echo "  → Made executable"
fi
echo ""

# Run help command
echo "Running: ./bin/agnostech --help"
echo "---"
if ./bin/agnostech --help; then
    echo "---"
    echo "✓ Help command works"
else
    echo "---"
    echo "✗ Help command failed"
    exit 1
fi
echo ""

# Run version command
echo "Running: ./bin/agnostech version"
echo "---"
if ./bin/agnostech version; then
    echo "---"
    echo "✓ Version command works"
else
    echo "---"
    echo "✗ Version command failed"
    exit 1
fi
echo ""

# Summary
echo "================================"
echo "Build Verification: SUCCESS"
echo "================================"
echo ""
echo "The CLI has been built successfully!"
echo ""
echo "Binary location: ./bin/agnostech"
echo "Binary size: $(du -h bin/agnostech | cut -f1)"
echo ""
echo "You can now run:"
echo "  ./bin/agnostech --help"
echo "  ./bin/agnostech version"
echo "  ./bin/agnostech analyze <path>"
echo "  ./bin/agnostech migrate <path>"
echo "  ./bin/agnostech validate <path>"
echo ""
echo "For more information:"
echo "  - Quick Start: cat QUICKSTART.md"
echo "  - Documentation: cat CLI_README.md"
echo "  - Structure: cat CLI_STRUCTURE.md"
echo ""
