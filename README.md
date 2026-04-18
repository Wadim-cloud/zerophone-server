# ZeroPhone

A distributed VoIP signaling server with WebRTC voice calling. Nodes on the same ZeroTier network can discover each other and make audio calls.

## Features

- ZeroTier network-based node discovery
- WebRTC voice calling (audio only)
- Real-time signaling via WebSockets + HTTP polling
- SQLite persistence for nodes, calls, and message queues
- Call timeout handling (auto-reject after 60 seconds)
- Docker support for easy deployment

## Quick Start

### Using Docker Compose (Recommended)

```bash
docker-compose up -d
```

The server will be available at `http://localhost:8080`

### Using Docker

### Manual Build

```bash
go build -o zerophone .
./zerophone --db zerophone.db
```

## Usage

1. Open the web UI at `http://your-server:8080`
2. Enter your ZeroTier Network ID (16-digit hex), your Node ID, and your Name
3. Click Register
4. Other nodes on the same ZeroTier network will appear in the list
5. Click "Call" to initiate a voice call

### Adding New Users

Any new user on the same ZeroTier network can register by:
1. Opening the web UI
2. Entering the same Network ID
3. Entering their ZeroTier Node ID and Name
4. Clicking Register

They will automatically see and can call other registered nodes on the network.

## API Reference

### POST /register

Register a new node (requires ZeroTier network ID):

```json
{
  "id": "node-1",
  "name": "My Node",
  "network_id": "a84ac5c123456789",
  "capabilities": ["audio"]
}
```

### GET /nodes?network_id=xxx

List nodes on a specific ZeroTier network.

### POST /signal

Send signaling messages (call, WebRTC SDP, ICE candidates):

```json
{
  "type": "CALL_REQUEST",
  "from_id": "node-1",
  "to_id": "node-2",
  "call_id": "uuid-1234"
}
```

Types: `CALL_REQUEST`, `CALL_ACCEPT`, `CALL_REJECT`, `CALL_END`, `SDP_OFFER`, `SDP_ANSWER`, `ICE_CANDIDATE`

### GET /ws/{node_id}

WebSocket endpoint for real-time signaling.

## Configuration

- `--addr`: Listen address (default: `:8080`)
- `--db`: SQLite database path (default: `zerophone.db`)

## Docker Deployment

### Using Docker Compose (Recommended)

```bash
# Start the server
docker-compose up -d

# View logs
docker-compose logs -f

# Stop the server
docker-compose down
```

### Using Docker Directly

```bash
docker run -d \
  --name zerophone \
  -p 8080:8080 \
  -v zerophone-data:/root \
  zerophone
```

## Systemd Service

Create `/etc/systemd/system/zerophone.service`:

```ini
[Unit]
Description=ZeroPhone VoIP Server
After=network.target

[Service]
Type=simple
ExecStart=/opt/zerophone/zerophone --db /var/lib/zerophone/zerophone.db
Restart=always

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable zerophone
sudo systemctl start zerophone
```

## Architecture

```
┌─────────┐     ┌──────────┐     ┌─────────┐
│ Node A  │────▶│  Server  │────▶│ Node B  │
│ (WebRTC)│◀────│ (Signaling)◀───│ (WebRTC)│
└─────────┘     └──────────┘     └─────────┘
                    │
              ┌─────────┐
              │ SQLite  │
              └─────────┘
```

## Troubleshooting

- Ensure all nodes are on the same ZeroTier network
- Check firewall allows port 8080
- For WebRTC to work, STUN servers must be accessible