# ZeroPhone Technical Overview

## System Architecture

ZeroPhone is a distributed VoIP signaling server with WebRTC voice calling, designed to run over ZeroTier networks.

## Components

### Server (main.go)
- HTTP server on port 8080
- ZeroMQ for peer discovery and signaling
- SQLite for persistence (optional)
- WebRTC for audio calls

### Docker Deployment

**Image:** `zerophone:latest`
- Base: Alpine Linux
- Build: Go 1.21 with CGO (ZeroMQ support)

**Services:**
- `zerophone` - Main server
- `zerophone-node` - Additional node for scaling

**Network:** Host mode (for ZeroTier access)

### Ports
- 8080 - HTTP API/Web UI
- 5555-5558 - ZeroMQ (discovery, routing, async)

## Environment Variables

| Variable | Description | Default |
|----------|-------------|----------|
| ZEROPHONE_CLUSTER | Enable cluster mode | - |
| ZEROTIER_IP | ZeroTier IP | auto-detect |
| ZEROTIER_NETWORK | ZeroTier network ID | flgubtoy |
| ZEROPHONE_DATA | Data directory | /data |

## API Endpoints

### Core
- `GET /` - Web UI
- `GET /status` - Server status
- `GET /nodes` - List peers
- `POST /register` - Register node

### Call Signaling
- `POST /call` - Initiate/answer call
- `POST /call/signal` - Call signaling
- `GET /call` - Poll pending calls

### WebRTC
- `POST /sdp/offer` - WebRTC offer
- `POST /sdp/answer` - WebRTC answer
- `POST /ice/candidate` - ICE candidate

### Debug
- `GET /debug` - Debug dashboard
- `GET /stats` - Server statistics

## WebRTC Configuration

### ICE Servers
```
stun:stun.l.google.com:19302
stun:stun1.l.google.com:19302  
stun:stun2.l.google.com:19302
```

### Audio Codecs
- Opus (48kHz, recommended)
- PCMU
- PCMA

### Audio Constraints
- Echo cancellation: enabled
- Noise suppression: enabled
- Auto gain control: enabled

## Call Flow

```
Caller                           Callee
   |                               |
   |-------- INVITE --------------->
   |          (100 TRYING)         |
   |        <----------- 180 RINGING
   |                              |
   |------- SDP OFFER ----------->
   |          (200 OK)           |
   |<------- SDP ANSWER ----------
   |                              |
   |------ ACK ----------------->
   |                              |
   |======= WebRTC ============|
   |    (peer-to-peer audio)     |
   |                              |
   |-------- BYE --------------->
   |                              |
```

## Known Issues

1. **HTTPS** - Self-signed certs required for mic on non-localhost
2. **NAT Traversal** - ICE may timeout on symmetric NAT
3. **One-way audio** - Possible codec mismatch

## Dependencies

### Go
- github.com/gorilla/mux
- github.com/gorilla/websocket
- github.com/mattn/go-sqlite3
- github.com/pebbe/zmq4

### System
- gcc
- musl-dev
- libzmq3-dev (Alpine: zeromq)

## Build

```bash
# Local
CGO_ENABLED=1 go build -o zerophone .

# Docker
docker build -t zerophone .
docker run -d --network host zerophone
```

## Configuration Files

- `main.go` - Server
- `Dockerfile` - Container build
- `docker-compose.yml` - Multi-container
- `static/index.html` - Web UI
- `.env.example` - Environment template