#!/bin/bash

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Project root directory (parent of scripts/)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Current values
CURRENT_MODULE="github.com/homeport/homeport"
CURRENT_NAME="homeport"
CURRENT_NAME_CAPITALIZED="Homeport"

echo -e "${BLUE}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║           Project Rename Tool                               ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
echo -e "Current project name: ${YELLOW}${CURRENT_NAME}${NC}"
echo -e "Current Go module: ${YELLOW}${CURRENT_MODULE}${NC}"
echo ""

# Ask for new GitHub organization/username
read -p "Enter new GitHub organization/username (e.g., 'myorg'): " NEW_ORG
if [ -z "$NEW_ORG" ]; then
    echo -e "${RED}Error: Organization/username cannot be empty${NC}"
    exit 1
fi

# Ask for new project name
read -p "Enter new project name (e.g., 'myproject'): " NEW_NAME
if [ -z "$NEW_NAME" ]; then
    echo -e "${RED}Error: Project name cannot be empty${NC}"
    exit 1
fi

# Derive capitalized version
NEW_NAME_CAPITALIZED="$(echo "${NEW_NAME:0:1}" | tr '[:lower:]' '[:upper:]')${NEW_NAME:1}"
NEW_MODULE="github.com/${NEW_ORG}/${NEW_NAME}"

echo ""
echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
echo -e "Summary of changes:"
echo -e "  Module: ${YELLOW}${CURRENT_MODULE}${NC} → ${GREEN}${NEW_MODULE}${NC}"
echo -e "  Name: ${YELLOW}${CURRENT_NAME}${NC} → ${GREEN}${NEW_NAME}${NC}"
echo -e "  Capitalized: ${YELLOW}${CURRENT_NAME_CAPITALIZED}${NC} → ${GREEN}${NEW_NAME_CAPITALIZED}${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
echo ""

read -p "Proceed with renaming? (y/N): " CONFIRM
if [[ ! "$CONFIRM" =~ ^[Yy]$ ]]; then
    echo -e "${YELLOW}Aborted.${NC}"
    exit 0
fi

echo ""
echo -e "${BLUE}Starting rename process...${NC}"
echo ""

cd "$PROJECT_ROOT"

# Function to perform sed replacement across files
replace_in_files() {
    local pattern="$1"
    local replacement="$2"
    local file_pattern="$3"
    local description="$4"

    echo -e "  ${YELLOW}→${NC} $description"

    # Find files matching pattern and perform replacement
    find . -type f \( -name "*.go" -o -name "*.mod" -o -name "*.yaml" -o -name "*.yml" \
        -o -name "*.json" -o -name "*.md" -o -name "*.tsx" -o -name "*.ts" \
        -o -name "*.html" -o -name "Makefile" -o -name "Dockerfile" \
        -o -name "*.sh" -o -name ".goreleaser*" \) \
        ! -path "./.git/*" \
        ! -path "./node_modules/*" \
        ! -path "./web/node_modules/*" \
        ! -path "./bin/*" \
        ! -path "./scripts/rename-project.sh" \
        -exec grep -l "$pattern" {} \; 2>/dev/null | while read -r file; do
        if [[ "$OSTYPE" == "darwin"* ]]; then
            sed -i '' "s|${pattern}|${replacement}|g" "$file"
        else
            sed -i "s|${pattern}|${replacement}|g" "$file"
        fi
    done
}

# Derive uppercase versions for env vars
CURRENT_NAME_UPPER=$(echo "${CURRENT_NAME}" | tr '[:lower:]' '[:upper:]')
NEW_NAME_UPPER=$(echo "${NEW_NAME}" | tr '[:lower:]' '[:upper:]')

# Step 1: Replace Go module path
echo -e "${GREEN}[1/10]${NC} Updating Go module path..."
replace_in_files "$CURRENT_MODULE" "$NEW_MODULE" "" "Replacing module imports"

# Step 2: Replace all Docker labels (name.* patterns)
echo -e "${GREEN}[2/10]${NC} Updating Docker labels..."
replace_in_files "${CURRENT_NAME}\\." "${NEW_NAME}." "" "Replacing all Docker labels (${CURRENT_NAME}.*)"
replace_in_files "com\\.${CURRENT_NAME}\\." "com.${NEW_NAME}." "" "Replacing com.${CURRENT_NAME}.* labels"

# Step 3: Replace CLI command examples and cobra Use field
echo -e "${GREEN}[3/10]${NC} Updating CLI examples..."
replace_in_files "${CURRENT_NAME} " "${NEW_NAME} " "" "Replacing CLI command examples"
replace_in_files "Use:   \\\"${CURRENT_NAME}\\\"" "Use:   \\\"${NEW_NAME}\\\"" "" "Replacing cobra root command Use field"
replace_in_files "help for ${CURRENT_NAME}" "help for ${NEW_NAME}" "" "Replacing help text"

# Step 4: Replace network names
echo -e "${GREEN}[4/10]${NC} Updating network names..."
replace_in_files "\\\"${CURRENT_NAME}\\\"" "\\\"${NEW_NAME}\\\"" "" "Replacing network names in Go strings"
replace_in_files "network: ${CURRENT_NAME}" "network: ${NEW_NAME}" "" "Replacing network names in YAML"
replace_in_files "networks: \\[${CURRENT_NAME}\\]" "networks: [${NEW_NAME}]" "" "Replacing network arrays"

# Step 5: Replace capitalized version (display text, comments, headers)
echo -e "${GREEN}[5/10]${NC} Updating capitalized project name..."
replace_in_files "$CURRENT_NAME_CAPITALIZED" "$NEW_NAME_CAPITALIZED" "" "Replacing display names and comments"

# Step 6: Replace remaining lowercase instances
echo -e "${GREEN}[6/10]${NC} Updating binary and remaining references..."
replace_in_files "${CURRENT_NAME}-stack" "${NEW_NAME}-stack" "" "Replacing stack names"
replace_in_files "${CURRENT_NAME}-cluster" "${NEW_NAME}-cluster" "" "Replacing cluster names"
replace_in_files "${CURRENT_NAME}-nats" "${NEW_NAME}-nats" "" "Replacing NATS names"
# Environment variables (uppercase)
replace_in_files "${CURRENT_NAME_UPPER}_" "${NEW_NAME_UPPER}_" "" "Replacing env var prefixes"
replace_in_files "bin/${CURRENT_NAME}" "bin/${NEW_NAME}" "" "Replacing binary paths"
replace_in_files "BINARY_NAME=${CURRENT_NAME}" "BINARY_NAME=${NEW_NAME}" "" "Replacing Makefile binary name"
replace_in_files "-o ${CURRENT_NAME}" "-o ${NEW_NAME}" "" "Replacing build output names"
replace_in_files "adduser -D -u 1000 ${CURRENT_NAME}" "adduser -D -u 1000 ${NEW_NAME}" "" "Replacing Dockerfile user"
replace_in_files "USER ${CURRENT_NAME}" "USER ${NEW_NAME}" "" "Replacing Dockerfile USER"
replace_in_files "\\.${CURRENT_NAME}" ".${NEW_NAME}" "" "Replacing config file references"

# Step 7: Rename directories
echo -e "${GREEN}[7/10]${NC} Renaming directories..."
if [ -d "cmd/${CURRENT_NAME}" ]; then
    echo -e "  ${YELLOW}→${NC} Renaming cmd/${CURRENT_NAME} → cmd/${NEW_NAME}"
    mv "cmd/${CURRENT_NAME}" "cmd/${NEW_NAME}"
fi

# Step 8: Rename config example files
echo -e "${GREEN}[8/10]${NC} Renaming config files..."
if [ -f ".${CURRENT_NAME}.example.yaml" ]; then
    echo -e "  ${YELLOW}→${NC} Renaming .${CURRENT_NAME}.example.yaml → .${NEW_NAME}.example.yaml"
    mv ".${CURRENT_NAME}.example.yaml" ".${NEW_NAME}.example.yaml"
fi

# Update web UI index.html title
if [ -f "web/index.html" ]; then
    echo -e "  ${YELLOW}→${NC} Updating web/index.html title"
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' "s|<title>.*</title>|<title>${NEW_NAME_CAPITALIZED}</title>|g" "web/index.html"
    else
        sed -i "s|<title>.*</title>|<title>${NEW_NAME_CAPITALIZED}</title>|g" "web/index.html"
    fi
fi

# Step 9: Update go.mod first line
echo -e "${GREEN}[9/10]${NC} Finalizing go.mod..."
if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "1s|module .*|module ${NEW_MODULE}|" go.mod
else
    sed -i "1s|module .*|module ${NEW_MODULE}|" go.mod
fi

# Step 10: Update this script with new current values for future renames
echo -e "${GREEN}[10/10]${NC} Updating rename script for future use..."
SCRIPT_PATH="${SCRIPT_DIR}/rename-project.sh"
if [[ "$OSTYPE" == "darwin"* ]]; then
    sed -i '' "s|^CURRENT_MODULE=.*|CURRENT_MODULE=\"${NEW_MODULE}\"|" "$SCRIPT_PATH"
    sed -i '' "s|^CURRENT_NAME=.*|CURRENT_NAME=\"${NEW_NAME}\"|" "$SCRIPT_PATH"
    sed -i '' "s|^CURRENT_NAME_CAPITALIZED=.*|CURRENT_NAME_CAPITALIZED=\"${NEW_NAME_CAPITALIZED}\"|" "$SCRIPT_PATH"
else
    sed -i "s|^CURRENT_MODULE=.*|CURRENT_MODULE=\"${NEW_MODULE}\"|" "$SCRIPT_PATH"
    sed -i "s|^CURRENT_NAME=.*|CURRENT_NAME=\"${NEW_NAME}\"|" "$SCRIPT_PATH"
    sed -i "s|^CURRENT_NAME_CAPITALIZED=.*|CURRENT_NAME_CAPITALIZED=\"${NEW_NAME_CAPITALIZED}\"|" "$SCRIPT_PATH"
fi

echo ""
echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ Rename complete!${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════════════════════${NC}"
echo ""
echo -e "Next steps:"
echo -e "  1. Review changes: ${YELLOW}git diff${NC}"
echo -e "  2. Run: ${YELLOW}go mod tidy${NC}"
echo -e "  3. Build: ${YELLOW}make build${NC}"
echo -e "  4. Test: ${YELLOW}make test${NC}"
echo -e "  5. Build web: ${YELLOW}cd web && npm run build${NC}"
echo ""
echo -e "If you need to update the remote repository:"
echo -e "  ${YELLOW}git remote set-url origin git@github.com:${NEW_ORG}/${NEW_NAME}.git${NC}"
echo ""
