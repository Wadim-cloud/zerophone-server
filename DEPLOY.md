# ZeroPhone - Distributed VoIP over ZeroTier

A peer-to-peer voice calling system that runs on ZeroTier networks without central servers.

## Quick Deploy

### Option 1: Download Binary
```bash
# Download from this repository's releases or build yourself
wget https://github.com/yourusername/zerophone/releases/latest/zerophone
chmod +x zerophone
```

### Option 2: Build from Source
```bash
git clone https://github.com/yourusername/zerophone.git
cd zerophone
go build -o zerophone .
```

### Option 3: Docker
```bash
# Build and run single container
docker build -t zerophone .
docker run -p 8080:8080 zerophone
```

### Option 4: Docker Compose (recommended for cluster)
```bash
# Clone and deploy
git clone https://github.com/Wadim-cloud/zerophone.git
cd zerophone

# Deploy with Docker Compose
docker-compose up -d

# View logs
docker-compose logs -f

# Scale to multiple nodes
docker-compose up -d --scale zerophone-node=2
```

## Running

```bash
# Basic (auto-detects ZeroTier)
./zerophone -addr :8080

# Server mode (binds to ZeroTier IP if detected)
ZEROPHONE_CLUSTER=1 ./zerophone -addr :8080

# With specific peers
./zerophone -addr :8080 -peers "10.121.15.208:8080,10.121.15.223:9090"
```

## Docker Compose Full Deployment

```yaml
# docker-compose.yml
services:
  zerophone:
    build: .
    ports:
      - "8080:8080"
      - "5555-5558:5555-5558"  # ZeroMQ ports
    environment:
      - ZEROPHONE_CLUSTER=1
      - ZEROTIER_IP=${ZEROTIER_IP}  # Set your ZeroTier IP
      - ZEROTIER_NETWORK=your_network_id
    volumes:
      - zerophone-data:/data
    network_mode: host  # Use host network for ZeroTier
    restart: unless-stopped
```

## Running

```bash
# Basic (auto-detects ZeroTier)
./zerophone -addr :8080

# Server mode (binds to ZeroTier IP if detected)
ZEROPHONE_CLUSTER=1 ./zerophone -addr :8080

# With specific peers
./zerophone -addr :8080 -peers "10.121.15.208:8080,10.121.15.223:9090"
```

## Access

- Web UI: http://YOUR_ZEROTIER_IP:8080/
- API: http://YOUR_ZEROTIER_IP:8080/status
- Nodes: http://YOUR_ZEROTIER_IP:8080/nodes
- Call UI: http://YOUR_ZEROTIER_IP:8080/call.html

## Architecture

- **ZeroMQ** for peer discovery and signaling
- **HTTP** for web interface
- **WebRTC-ready** signaling for calls (SDP/ICE exchange)
- Auto-detects ZeroTier network interfaces

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Web UI |
| `/call.html` | GET | Voice call UI |
| `/status` | GET | Server status |
| `/nodes` | GET | List registered nodes |
| `/presence` | GET | Online peers |
| `/register` | POST | Register node |
| `/heartbeat` | POST | Keep alive |
| `/call` | POST | Initiate call |
| `/call/respond` | POST | Accept/reject call |

### Ports Used

- HTTP: 8080 (default)
- ZeroMQ Router: 5555
- ZeroMQ Pub: 5556
- ZeroMQ Discovery: 5557
- ZeroMQ Async: 5558

## Development

```bash
# Run locally for testing
go run .

# Build
go build -o zerophone .

# Test
curl localhost:8080/status
```

## Network Requirements

- ZeroTier network must be active
- Ports 5555-5558 TCP must be open between peers
- Port 8080 for HTTP

## Troubleshooting

```bash
# Check ZeroTier interface
ip addr show | grep "zt"

# List peers
sudo zerotier-cli listpeers

# Check if ports are open
nc -zv 10.121.15.208 8080
```

## License

MIT