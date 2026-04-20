#!/bin/bash
set -e

echo "=== ZeroPhone Deployment Script ==="

# Configuration
REPO_URL="${REPO_URL:-git@github.com:Wadim-cloud/zerophone.git}"
INSTALL_DIR="${INSTALL_DIR:-/opt/zerophone}"
SERVICE_NAME="zerophone"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

log() { echo -e "${GREEN}[+]${NC} $1"; }
error() { echo -e "${RED}[-]${NC} $1"; exit 1; }

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    error "Please run as root (use sudo)"
fi

# Create install directory
log "Creating installation directory..."
mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

# Clone or pull repo
if [ -d ".git" ]; then
    log "Updating existing repository..."
    git pull origin main
else
    log "Cloning repository..."
    git clone "$REPO_URL" .
fi

# Check for Go
if ! command -v go &> /dev/null; then
    error "Go is not installed. Install with: apt install golang-go"
fi

# Check for libzmq
if ! ldconfig -p | grep -q libzmq; then
    log "Installing ZeroMQ library..."
    apt update && apt install -y libzmq3-dev
fi

# Build
log "Building zerophone..."
CGO_ENABLED=1 go build -o zerophone .

# Create data directory
mkdir -p /var/lib/zerophone

# Create systemd service
log "Installing systemd service..."
cat > /etc/systemd/system/${SERVICE_NAME}.service << 'EOF'
[Unit]
Description=ZeroPhone VoIP Server
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/zerophone
ExecStart=/opt/zerophone/zerophone --addr :8080
Restart=always
RestartSec=5
Environment=ZEROPHONE_CLUSTER=1

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd and start
log "Starting service..."
systemctl daemon-reload
systemctl enable ${SERVICE_NAME}
systemctl restart ${SERVICE_NAME}

log "ZeroPhone deployed successfully!"
log "Web UI: http://localhost:8080"
log "Status: systemctl status ${SERVICE_NAME}"
