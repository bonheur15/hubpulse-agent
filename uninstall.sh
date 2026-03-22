#!/bin/bash
set -e

# HubPulse Agent Uninstall Script
# Usage: curl -sSL https://install.HubPulse.space/uninstall.sh | sudo bash
# Or run locally: sudo ./uninstall.sh

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}HubPulse Agent Uninstaller${NC}"
echo "--------------------------------"

# Check for root
if [ "$(id -u)" -ne 0 ] && [ "$EUID" -ne 0 ]; then
  echo -e "${RED}Please run as root (use sudo).${NC}"
  exit 1
fi

BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/hubpulse-agent"
BINARY="$BIN_DIR/hubpulse-agent"
CONFIG_FILE="$CONFIG_DIR/config.json"
SERVICE_FILE="/etc/systemd/system/hubpulse-agent.service"

echo -e "${BLUE}Stopping and disabling service...${NC}"
if systemctl is-active --quiet hubpulse-agent 2>/dev/null; then
    systemctl stop hubpulse-agent
    echo "Service stopped."
fi

if systemctl is-enabled --quiet hubpulse-agent 2>/dev/null; then
    systemctl disable hubpulse-agent
    echo "Service disabled."
fi

echo -e "${BLUE}Removing systemd service...${NC}"
if [ -f "$SERVICE_FILE" ]; then
    rm -f "$SERVICE_FILE"
    systemctl daemon-reload
    echo "Service file removed."
fi

echo -e "${BLUE}Removing binary...${NC}"
if [ -f "$BINARY" ]; then
    rm -f "$BINARY"
    echo "Binary removed."
fi

echo -e "${BLUE}Removing configuration...${NC}"
if [ -d "$CONFIG_DIR" ]; then
    rm -rf "$CONFIG_DIR"
    echo "Configuration directory removed."
fi

echo "--------------------------------"
echo -e "${GREEN}HubPulse Agent completely uninstalled!${NC}"
echo "--------------------------------"