#!/bin/bash
set -e

echo "=== ZeroPhone Deployment Script ==="

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Check for Docker
if ! command -v docker &> /dev/null; then
    echo "[*] Installing Docker..."
    curl -fsSL https://get.docker.com | sh
fi

cd "$SCRIPT_DIR"

# Build and start using docker compose v2
echo "[*] Building and starting ZeroPhone..."
docker compose up -d --build

echo ""
echo "=== ZeroPhone Deployed Successfully! ==="
echo "Web UI: http://localhost:8080"
echo "Status: docker compose ps"
echo "Logs:   docker compose logs -f"
