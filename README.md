# ZeroPhone v2.0 — Distributed Telecom System

A complete distributed signaling + routing system for WebRTC communication with cluster-aware call routing.

## Architecture Overview

```
Frontend (Browser) ↔ Backend (Go) ↔ Cluster (ZMQ + ZeroTier)
     ↓                  ↓                    ↓
Control Terminal   Signal Router       Route Gossip
   ↓                 ↓                    ↓
  WebRTC Engine   SIP FSM Mirror     Dijkstra Paths
   ↓                 ↓                    ↓
   Audio           Call States        NAT Awareness
```

## Quick Start

### 1. Build and Run

```bash
cd zerophone
go run .
```

Server starts on `http://localhost:9443`

### 2. Access Control Terminal

Open `http://localhost:9443/` in your browser:

- **Landing Page**: System overview and feature status
- **Control Terminal** (`/call.html`): Multi-call signaling interface
- **Debug Panel** (`/debug.html`): Network observability

### 3. Enable Clustering (Optional)

Set environment variables for cluster mode:

```bash
export ZEROPHONE_CLUSTER=1
export ZEROTIER_IP=10.x.x.x  # Your ZeroTier IP
go run .
```

## Key Features

### 🚀 Multi-Call Support
- Handle multiple simultaneous calls
- Per-call state machines mirroring backend FSM
- Call history and state visualization

### 🧠 Distributed Routing
- Route gossip protocol shares network metrics
- Dijkstra pathfinding for multi-hop routing
- Load-aware call distribution

### 🌍 NAT Awareness
- Automatic NAT type detection
- TURN/STUN capability detection
- ICE strategy optimization per call

### 📡 Real-Time Signaling
- SIP-like state machine (INVITE → RINGING → ACTIVE → BYE)
- WebRTC SDP/ICE exchange
- Cluster-aware routing metadata

## Frontend Architecture

### 5-Layer Design

1. **Transport Layer**: WebSocket connection with reconnect logic
2. **Signal Router**: Frontend mirror of backend signal routing
3. **Call State Store**: Multi-call state management with history
4. **WebRTC Engine**: Peer connection per call with ICE optimization
5. **UI Layer**: Control terminal with FSM visualization

### Signal Flow

```
User Action → Signal Router → State Store → WebRTC Engine → UI Update
        ↓
   WebSocket
        ↓
   Backend Signal Router → SIP FSM → Cluster Resolver → ZMQ Transport
```

## API Endpoints

### Core Endpoints
- `GET /` - Landing page
- `GET /call.html` - Control terminal
- `GET /debug.html` - Debug panel

### Signaling Endpoints
- `WS /ws/{user_id}` - WebSocket signaling connection
- `POST /call/signal` - HTTP signaling fallback

### Status Endpoints
- `GET /status` - System status
- `GET /nodes` - Cluster node list
- `GET /presence` - User presence
- `GET /ice/servers` - ICE server configuration

### Cluster Endpoints
- `GET /cluster/status` - Cluster status
- `GET /cluster/nodes` - Cluster nodes
- `GET /cluster/peers` - Cluster peers

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `ZEROPHONE_CLUSTER` | Enable cluster mode | `0` |
| `ZEROTIER_IP` | ZeroTier IP address | Auto-detected |
| `PUBLIC_IP` | Public IP override | Auto-detected |
| `ZEROPHONE_DATA` | Data directory | `/var/lib/zerophone` |

### Command Line Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-addr` | HTTP listen address | `:9443` |
| `-server` | Server URL for client mode | None |
| `-port` | Cluster port | `9443` |

## Usage Examples

### Basic Call Flow

1. **User A** opens Control Terminal
2. **User B** opens Control Terminal
3. **User A** enters User B's ID and clicks INVITE
4. **User B** receives incoming call modal
5. **User B** accepts → WebRTC connection established
6. Call proceeds with audio streaming
7. Either party clicks "End Call" → BYE signal → cleanup

### Multi-Call Scenario

1. User A has active call with User B
2. User C calls User A (shows in call list)
3. User A can switch between calls
4. Each call has independent WebRTC connection

### Cluster Mode

1. Multiple ZeroPhone nodes on ZeroTier network
2. Nodes discover each other via ZMQ gossip
3. Calls route through optimal paths
4. Load balancing across nodes
5. NAT traversal via TURN nodes

## Signal Types

### Core SIP-like Signals
- `INVITE` - Start call
- `TRYING` - Processing invite
- `RINGING` - Remote ringing
- `OK` - Accept call
- `REJECT` - Decline call
- `BYE` - End call

### WebRTC Signals
- `SDP` - Session description protocol
- `ICE` - Interactive connectivity establishment

### Cluster Signals
- `ROUTE_GOSSIP` - Network metric sharing
- `NODE_ONLINE` - Node discovery
- `USER_ONLINE` - User presence

## Debug Features

### Signal Log
- Real-time signal monitoring
- Call state transitions
- ICE strategy decisions
- Network routing hints

### FSM Visualization
- Current call state highlighting
- State transition history
- Per-call timeline

### Network Awareness
- ICE strategy display (`_ice` metadata)
- Node topology information
- Load factor indicators

## Development

### Project Structure

```
zerophone/
├── main.go              # HTTP server + main loop
├── core/                # Signaling core
│   ├── signal_router.go     # SIP-like routing
│   ├── call_state_machine.go # Call FSM
│   └── call_router.go        # Call handling
├── cluster/             # Distributed features
│   ├── signal_resover.go    # Route resolution + gossip
│   ├── discovery.go          # ZMQ discovery
│   ├── cluster_manager.go    # Cluster orchestration
│   ├── ws_hub.go            # WebSocket hub
│   └── zmq.go               # ZMQ transport
└── static/              # Frontend assets
    ├── index.html          # Landing page
    ├── call.html           # Control terminal
    └── debug.html          # Debug panel
```

### Building

```bash
go mod tidy
go build .
```

### Testing

```bash
go test ./...
```

## Troubleshooting

### WebSocket Connection Issues
- Check browser console for connection errors
- Verify server is running on correct port
- Check firewall settings

### Call Connection Issues
- Check ICE server configuration
- Verify network connectivity
- Check browser WebRTC permissions

### Cluster Issues
- Verify ZeroTier network configuration
- Check ZMQ port accessibility
- Review cluster logs

### Audio Issues
- Check microphone permissions
- Verify WebRTC support
- Check audio device configuration

## Contributing

1. Follow the 5-layer frontend architecture
2. Mirror backend FSM logic exactly
3. Add signal logging for debugging
4. Update documentation for new features

## License

This project implements distributed telecom signaling protocols for educational and research purposes.