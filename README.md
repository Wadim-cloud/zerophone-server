# ZeroPhone

A distributed VoIP signaling server built with Go. Enables real-time call signaling between nodes using WebSockets and HTTP long-polling.

## Features

- Node registration and heartbeat-based presence detection
- Real-time call signaling (REQUEST, ACCEPT, REJECT, END)
- WebSocket push + HTTP polling for real-time messaging
- SQLite persistence for nodes, calls, and message queues
- Auto-cleanup of old messages and ended calls
- Call timeout handling (auto-reject after 60 seconds)
- Web-based UI for testing and monitoring
- Docker support for easy deployment

## Quick Start

### Using Docker Compose (Recommended)

```bash
git clone <your-repo>
cd zerophone
docker-compose up
```

Server runs on `http://localhost:8080`

### Manual Build

```bash
# Install dependencies
go mod download

# Build
make build

# Run
./zerophone --db zerophone.db --addr :8080
```

### Using Make

```bash
make run          # Build and run
make test         # Run tests
make clean        # Clean build artifacts
make docker-build # Build Docker image
```

## API Reference

### POST /register

Register a new node.

```json
{
  "id": "node-1",
  "name": "My Node",
  "capabilities": ["audio"]
}
```

### POST /heartbeat?node_id=x

Send heartbeat to indicate node is alive.

### GET /nodes

List all registered nodes with online status.

### POST /signal

Send a signaling message.

```json
{
  "type": "CALL_REQUEST",
  "from_id": "node-1",
  "to_id": "node-2",
  "call_id": "uuid-1234"
}
```

Types: `CALL_REQUEST`, `CALL_ACCEPT`, `CALL_REJECT`, `CALL_END`, `MESSAGE`

### GET /poll/{node_id}

Long-poll for pending messages.

### GET /ws/{node_id}

WebSocket endpoint for real-time messages.

## Configuration

Command-line flags:

- `--addr`: Listen address (default: `:8080`)
- `--db`: SQLite database path (default: `zerophone.db`)

## Data Model

### Node

- `id`: Unique node identifier
- `name`: Display name
- `last_seen`: Unix timestamp of last heartbeat
- `status`: "online" or "offline"
- `capabilities`: JSON array of supported features

### Call

- `call_id`: Unique call identifier
- `a`, `b`: Participant node IDs
- `state`: "ringing", "active", or "ended"

### Message

- Queued per recipient
- Automatically marked delivered when fetched
- Periodic cleanup of old messages

## Deployment

### Server Setup

1. Clone repository on server
2. Install Go 1.21+ (or use Docker)
3. Build: `make build`
4. Configure: `./zerophone --db /var/lib/zerophone/zerophone.db --addr :8080`
5. Set up systemd service (see below)

### Systemd Service

Create `/etc/systemd/system/zerophone.service`:

```ini
[Unit]
Description=ZeroPhone VoIP Signaling Server
After=network.target

[Service]
Type=simple
User=zerophone
WorkingDirectory=/opt/zerophone
ExecStart=/opt/zerophone/zerophone --db /var/lib/zerophone/zerophone.db --addr :8080
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable zerophone
sudo systemctl start zerophone
```

### Docker Deployment

```bash
# Build image
docker build -t zerophone:latest .

# Run container
docker run -d \
  --name zerophone \
  -p 8080:8080 \
  -v zerophone-data:/root \
  zerophone:latest
```

### Using with systemd-nspawn or LXC

The Docker image can be used with any container runtime.

## Architecture

```
┌─────────┐     ┌──────────┐     ┌──────────┐
│ Node A  │────▶│  Store   │────▶│ Node B   │
│ (WebUI) │     │  (SQLite)│     │ (WebUI)  │
└─────────┘     └──────────┘     └──────────┘
                    │  ▲
                    ▼  │
              ┌──────────┐
              │ WS Hub   │ (WebSocket)
              └──────────┘
```

## Testing

Open `http://your-server:8080` in two browser windows. Enter different node IDs and test calling between them.

## Troubleshooting

### Port already in use

Change `--addr` flag to use a different port.

### Database locked

Ensure only one instance runs per database file. Use separate DB files for multiple instances.

### Messages not delivered

Check WebSocket connection status. Messages fall back to HTTP polling.

## License

MIT
