#!/bin/bash

# AgnosTech CLI Test Script
# This script tests all CLI commands and features

set -e  # Exit on error

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo "================================"
echo "AgnosTech CLI Test Suite"
echo "================================"
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counter
TESTS_PASSED=0
TESTS_FAILED=0

# Test function
test_command() {
    local test_name="$1"
    local command="$2"

    echo -n "Testing: $test_name ... "

    if eval "$command" > /dev/null 2>&1; then
        echo -e "${GREEN}PASS${NC}"
        TESTS_PASSED=$((TESTS_PASSED + 1))
        return 0
    else
        echo -e "${RED}FAIL${NC}"
        TESTS_FAILED=$((TESTS_FAILED + 1))
        return 1
    fi
}

# Clean previous builds
echo "Step 1: Cleaning previous builds..."
make clean > /dev/null 2>&1 || true
echo ""

# Install dependencies
echo "Step 2: Installing dependencies..."
if ! go mod download; then
    echo -e "${RED}Error: Failed to download dependencies${NC}"
    exit 1
fi
echo ""

# Build the CLI
echo "Step 3: Building CLI..."
if ! make build; then
    echo -e "${RED}Error: Failed to build CLI${NC}"
    exit 1
fi
echo ""

# Run tests
echo "Step 4: Running CLI tests..."
echo ""

# Test 1: Help command
test_command "Help command" "./bin/agnostech --help"

# Test 2: Version command
test_command "Version command" "./bin/agnostech version"

# Test 3: Analyze help
test_command "Analyze help" "./bin/agnostech analyze --help"

# Test 4: Migrate help
test_command "Migrate help" "./bin/agnostech migrate --help"

# Test 5: Validate help
test_command "Validate help" "./bin/agnostech validate --help"

# Test 6: Analyze with sample data (if exists)
if [ -f "./test/fixtures/sample.tfstate" ]; then
    test_command "Analyze sample state" "./bin/agnostech analyze ./test/fixtures/sample.tfstate --output /tmp/test-analysis.json"

    # Test 7: Analyze with table format
    test_command "Analyze with table format" "./bin/agnostech analyze ./test/fixtures/sample.tfstate --format table --output /tmp/test-analysis-table.json"

    # Test 8: Migrate sample state
    test_command "Migrate sample state" "./bin/agnostech migrate ./test/fixtures/sample.tfstate --output /tmp/test-output"

    # Test 9: Validate generated stack
    if [ -d "/tmp/test-output" ]; then
        test_command "Validate generated stack" "./bin/agnostech validate /tmp/test-output"
    fi
fi

# Test 10: Verbose flag
test_command "Verbose flag" "./bin/agnostech --verbose version"

# Test 11: Quiet flag
test_command "Quiet flag" "./bin/agnostech --quiet version"

echo ""
echo "================================"
echo "Test Results"
echo "================================"
echo -e "${GREEN}Passed: $TESTS_PASSED${NC}"
echo -e "${RED}Failed: $TESTS_FAILED${NC}"
echo "Total:  $((TESTS_PASSED + TESTS_FAILED))"
echo ""

# Cleanup
echo "Cleaning up test files..."
rm -f /tmp/test-analysis.json /tmp/test-analysis-table.json
rm -rf /tmp/test-output

if [ $TESTS_FAILED -eq 0 ]; then
    echo -e "${GREEN}All tests passed!${NC}"
    echo ""
    echo "You can now use the CLI:"
    echo "  ./bin/agnostech --help"
    exit 0
else
    echo -e "${RED}Some tests failed!${NC}"
    exit 1
fi
