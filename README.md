# ZeroPhone

A distributed VoIP signaling server with WebRTC voice calling. Nodes on the same ZeroTier network can discover each other and make audio calls.

## Features

- ZeroTier network-based node discovery
- WebRTC voice calling (audio only)
- Real-time signaling via WebSockets + HTTP polling
- SQLite persistence for nodes, calls, and message queues
- Call timeout handling (auto-reject after 60 seconds)
- Docker support for easy deployment
- **Modern dark-themed web UI** with real-time updates
- **TUI CLI client** for terminal-based node management and calling

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

## Clients

### Web Client

Open `http://your-server:8080` in any modern browser:

1. Enter your ZeroTier Network ID (16-digit hex), your Node ID, and your Name
2. Click **Register**
3. Other nodes on the same ZeroTier network will appear in the list
4. Click **Call** to initiate a voice call
5. Accept/Reject incoming calls via on-screen prompts

**Features:**
- Real-time WebSocket connection indicator
- Auto-refresh node list every 5 seconds
- Incoming call ringtone and overlay
- Active call timer and status
- Toast notifications for events

### CLI Client

A terminal-based client built with Go:

```bash
# Clone and build
git clone https://github.com/your-org/zerophone-cli.git
cd zerophone-cli
go build -o zerophone-cli .

# Configure (create ~/.zerophone-cli.json)
cat > ~/.zerophone-cli.json <<EOF
{
  "network_id": "a84ac5c123456789",
  "node_id": "your-zerotier-node-id",
  "name": "Your Name",
  "server_addr": "http://localhost:8080"
}
EOF

# Run
./zerophone-cli
```

**Controls:**
- `↑/↓` — Select node
- `Enter` — Call selected node / End active call
- `a` — Answer incoming call
- `R` — Reject incoming call
- `r` — Refresh node list
- `q` — Quit

The CLI displays online/offline status, auto-refreshes every 5s, and shows call timers.

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

### GET /poll/{node_id}

Long-poll for queued messages (legacy fallback).

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

## WebRTC Call Flow

1. Caller sends `CALL_REQUEST` via `/signal` (or WebSocket)
2. Callee receives `CALL_REQUEST` via WebSocket, UI prompts
3. If callee answers, `CALL_ACCEPT` is sent
4. Both peers exchange SDP offers/answers via `/signal`
5. ICE candidates are exchanged via `/signal`
6. Once connected, audio streams directly peer-to-peer

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

## Development

The project uses Go modules. To add features or fix bugs:

```bash
git clone https://github.com/your-org/zerophone.git
cd zerophone
go mod download
go build -o zerophone .
```

Run tests:

```bash
go test ./...
```

The server serves static files from `static/` — the primary UI is `static/index.html`.

---

*Built with ❤️ using Go, WebRTC, and ZeroTier.*