#!/bin/bash

# CloudExit CLI Setup Script

echo "CloudExit CLI Setup"
echo "==================="
echo ""

echo "Step 1: Installing dependencies..."
go mod tidy
if [ $? -ne 0 ]; then
    echo "Error: Failed to install dependencies"
    exit 1
fi

echo ""
echo "Step 2: Building the CLI..."
go build -o bin/cloudexit ./cmd/cloudexit
if [ $? -ne 0 ]; then
    echo "Error: Failed to build CLI"
    exit 1
fi

echo ""
echo "Step 3: Testing the CLI..."
./bin/cloudexit --help
if [ $? -ne 0 ]; then
    echo "Error: Failed to run CLI"
    exit 1
fi

echo ""
echo "==================="
echo "Setup completed successfully!"
echo ""
echo "You can now run the CLI with: ./bin/cloudexit"
echo ""
echo "Available commands:"
echo "  ./bin/cloudexit analyze <path>   - Analyze AWS infrastructure"
echo "  ./bin/cloudexit migrate <path>   - Generate self-hosted stack"
echo "  ./bin/cloudexit validate <path>  - Validate generated stack"
echo "  ./bin/cloudexit version          - Show version information"
echo ""
