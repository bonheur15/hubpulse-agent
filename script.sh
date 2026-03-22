#!/bin/bash
set -e

# HubPulse Agent Install Script
# Usage: curl -sSL https://install.HubPulse.space/script.sh | bash -s -- --token=YOUR_TOKEN --collector=COLLECTOR_URL

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}HubPulse Agent Installer${NC}"
echo "--------------------------------"

# Default values
TOKEN=""
COLLECTOR="https://collector.hubpulse.space/api/ingest"
BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/hubpulse-agent"
CONFIG_FILE="$CONFIG_DIR/config.json"
GITHUB_REPO="bonheur15/hubpulse-agent"
REPO_URL="https://github.com/bonheur15/hubpulse-agent/releases/latest/download"

# Parse arguments
while [[ $# -gt 0 ]]; do
  case $1 in
    --token=*)
      TOKEN="${1#*=}"
      shift
      ;;
    --collector=*)
      COLLECTOR="${1#*=}"
      shift
      ;;
    *)
      echo "Unknown argument: $1"
      exit 1
      ;;
  esac
done

if [ -z "$TOKEN" ]; then
  echo -e "${RED}Error: --token is required${NC}"
  exit 1
fi

# Detect OS and Arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case $ARCH in
  x86_64)
    ARCH="amd64"
    ;;
  aarch64|arm64)
    ARCH="arm64"
    ;;
  *)
    echo -e "${RED}Unsupported architecture: $ARCH${NC}"
    exit 1
    ;;
esac

if [ "$OS" != "linux" ]; then
  echo -e "${RED}This script only supports Linux.${NC}"
  exit 1
fi

# Check for root using multiple methods
if [ "$(id -u)" -ne 0 ] && [ "$EUID" -ne 0 ]; then
  echo -e "${RED}Please run as root (use sudo).${NC}"
  exit 1
fi

echo -e "Detecting system: ${GREEN}$OS/$ARCH${NC}"

# Ensure bin directory exists
mkdir -p "$BIN_DIR"

# Download binary
BINARY_NAME="hubpulse-agent-$OS-$ARCH"
TMP_FILE=$(mktemp)
DOWNLOAD_URL="$REPO_URL/$BINARY_NAME"
GITHUB_URL="https://github.com/$GITHUB_REPO/releases/latest/download/$BINARY_NAME"

echo -e "Downloading HubPulse Agent..."

echo "Attempting to fetch binary from $DOWNLOAD_URL..."
if curl -sSL --fail "$DOWNLOAD_URL" -o "$TMP_FILE" 2>/dev/null; then
    echo "Download successful, installing..."
    mv "$TMP_FILE" "$BIN_DIR/hubpulse-agent"
elif [ -f "./hubpulse-agent" ]; then
    echo "Using local hubpulse-agent binary for installation."
    cp ./hubpulse-agent "$BIN_DIR/hubpulse-agent"
else
    echo -e "${BLUE}Notice: Failed to fetch from custom URL. Falling back to GitHub...${NC}"
    if curl -sSL --fail "$GITHUB_URL" -o "$TMP_FILE" 2>/dev/null; then
        echo "Download successful, installing..."
        mv "$TMP_FILE" "$BIN_DIR/hubpulse-agent"
    else
        rm -f "$TMP_FILE"
        echo -e "${RED}Error: Failed to download binary from all sources.${NC}"
        exit 1
    fi
fi

chmod +x "$BIN_DIR/hubpulse-agent"

# Set up config
echo -e "Configuring agent..."
mkdir -p "$CONFIG_DIR"

if [ ! -f "$CONFIG_FILE" ]; then
    "$BIN_DIR/hubpulse-agent" print-default-config > "$CONFIG_FILE"
fi

# Update config with token and collector
# We use a simple python/ruby/perl/sed approach since we might not have jq
if command -v jq >/dev/null 2>&1; then
    cat "$CONFIG_FILE" | jq ".token = \"$TOKEN\" | .collector_url = \"$COLLECTOR\"" > "$CONFIG_FILE.tmp" && mv "$CONFIG_FILE.tmp" "$CONFIG_FILE"
else
    # Fallback to sed for basic replacement (assuming standard format)
    sed -i "s|\"token\": \".*\"|\"token\": \"$TOKEN\"|g" "$CONFIG_FILE"
    sed -i "s|\"collector_url\": \".*\"|\"collector_url\": \"$COLLECTOR\"|g" "$CONFIG_FILE"
fi

# Install systemd service
echo -e "Installing systemd service..."
cat <<EOF > /etc/systemd/system/hubpulse-agent.service
[Unit]
Description=HubPulse Monitoring Agent
After=network.target

[Service]
Type=simple
User=root
ExecStart=$BIN_DIR/hubpulse-agent run --config $CONFIG_FILE
Restart=always
RestartSec=10
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable hubpulse-agent
systemctl restart hubpulse-agent

echo "--------------------------------"
echo -e "${GREEN}HubPulse Agent installed and started!${NC}"
echo -e "Service status: ${BLUE}systemctl status hubpulse-agent${NC}"
echo -e "Config file: ${BLUE}$CONFIG_FILE${NC}"
echo "--------------------------------"
