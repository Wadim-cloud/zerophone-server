#!/usr/bin/env bash
set -e

echo "=================================================="
echo "⚡ ZeroPhone Partner Cluster Node Installer v2.0"
echo "=================================================="

ARCH=$(uname -m)
if [ "$ARCH" != "x86_64" ]; then
    echo "❌ Unsupported architecture: $ARCH (Requires x86_64)"
    exit 1
fi

INSTALL_DIR="/opt/zerophone"
mkdir -p "$INSTALL_DIR"

echo "📥 Fetching ZeroPhone Partner Cluster binary..."
curl -sSL https://dev.wadiem.cloudns.be/downloads/zerophone-cluster-v2.0.0-linux-amd64.tar.gz -o /tmp/zerophone.tar.gz

tar -xzf /tmp/zerophone.tar.gz -C "$INSTALL_DIR"
chmod +x "$INSTALL_DIR/zerophone"

echo "⚙️ Creating systemd service unit..."
cat << 'SERVICE_EOF' > /etc/systemd/system/zerophone.service
[Unit]
Description=ZeroPhone Partner Cluster Node
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/zerophone
ExecStart=/opt/zerophone/zerophone -addr :9443
Restart=always
RestartSec=3s
Environment="ZEROPHONE_CLUSTER=1"
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
SERVICE_EOF

systemctl daemon-reload
systemctl enable --now zerophone.service 2>/dev/null || true

echo "=================================================="
echo "✅ ZeroPhone Partner Cluster Node successfully installed!"
echo "📡 Node running on port 9443 (Cluster Mesh Mode Active)"
echo "=================================================="
