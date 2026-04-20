#!/bin/bash
set -e

echo "=== ZeroPhone Deployment Script (Docker) ==="

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Check for Docker
if ! command -v docker &> /dev/null; then
    echo "[*] Installing Docker..."
    curl -fsSL https://get.docker.com | sh
    sudo usermod -aG docker $USER
fi

cd "$SCRIPT_DIR"

# Build Docker image
echo "[*] Building Docker image..."
docker build -t zerophone:latest .

# Stop existing container if running
docker stop zerophone 2>/dev/null || true
docker rm zerophone 2>/dev/null || true

# Run container
echo "[*] Starting ZeroPhone container..."
docker run -d \
    --name zerophone \
    --network host \
    -e ZEROPHONE_CLUSTER=1 \
    -v zerophone-data:/data \
    zerophone:latest

echo ""
echo "=== ZeroPhone Deployed Successfully! ==="
echo "Web UI: http://localhost:8080"
echo "Status: docker ps"
echo "Logs:   docker logs zerophone"
