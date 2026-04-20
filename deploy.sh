#!/bin/bash
set -e

echo "=== ZeroPhone Deployment Script ==="

# Install dependencies (skip update to avoid kernel header errors)
echo "[*] Installing dependencies..."
sudo apt install -y gcc musl-dev pkg-config libzmq3-dev curl 2>/dev/null || true

# Build zerophone
echo "[*] Building zerophone..."
CGO_ENABLED=1 go build -o zerophone .

# Create data directory
echo "[*] Creating data directory..."
sudo mkdir -p /var/lib/zerophone

# Create systemd service
echo "[*] Installing systemd service..."
sudo tee /etc/systemd/system/zerophone.service > /dev/null << 'EOF'
[Unit]
Description=ZeroPhone VoIP Server
After=network.target

[Service]
Type=simple
WorkingDirectory=/var/lib/zerophone
ExecStart=/home/$SUDO_USER/zerophone/zerophone --addr :8080
Restart=always
Environment=ZEROPHONE_CLUSTER=1

[Install]
WantedBy=multi-user.target
EOF

# Stop if running
sudo systemctl stop zerophone 2>/dev/null || true

# Reload systemd and start
echo "[*] Starting service..."
sudo systemctl daemon-reload
sudo systemctl enable zerophone
sudo systemctl restart zerophone

echo ""
echo "=== ZeroPhone Deployed Successfully! ==="
echo "Web UI: http://localhost:8080"
echo "Status: sudo systemctl status zerophone"
echo "Logs:   sudo journalctl -u zerophone -f"