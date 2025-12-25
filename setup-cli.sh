#!/bin/bash

# AgnosTech CLI Setup Script

echo "AgnosTech CLI Setup"
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
go build -o bin/agnostech ./cmd/agnostech
if [ $? -ne 0 ]; then
    echo "Error: Failed to build CLI"
    exit 1
fi

echo ""
echo "Step 3: Testing the CLI..."
./bin/agnostech --help
if [ $? -ne 0 ]; then
    echo "Error: Failed to run CLI"
    exit 1
fi

echo ""
echo "==================="
echo "Setup completed successfully!"
echo ""
echo "You can now run the CLI with: ./bin/agnostech"
echo ""
echo "Available commands:"
echo "  ./bin/agnostech analyze <path>   - Analyze AWS infrastructure"
echo "  ./bin/agnostech migrate <path>   - Generate self-hosted stack"
echo "  ./bin/agnostech validate <path>  - Validate generated stack"
echo "  ./bin/agnostech version          - Show version information"
echo ""
