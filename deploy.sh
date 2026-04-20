#!/bin/bash
set -e

echo "=== ZeroPhone Deployment Script ==="

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

cd "$SCRIPT_DIR"

# Stop existing containers
echo "[*] Stopping existing containers..."
docker compose down 2>/dev/null || true

# Build and start
echo "[*] Building and starting ZeroPhone..."
docker compose up -d --build

echo ""
echo "=== ZeroPhone Deployed Successfully! ==="
echo "Web UI: http://localhost:8080"
echo "Status: docker compose ps"
echo "Logs:   docker compose logs -f"