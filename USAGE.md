# ZeroPhone v2.0 — Usage Guide

## Getting Started

### 1. Start the Server

```bash
cd zerophone
go run .
```

Server starts on `http://localhost:9443`

### 2. Open Control Terminal

Navigate to `http://localhost:9443/` and click **"Control Terminal"**

## Making Your First Call

### Step 1: Setup Users

1. Open two browser tabs/windows to `http://localhost:9443/call.html`
2. Each tab gets a unique user ID (shown in debug log)

### Step 2: Initiate Call

1. In Tab 1, enter Tab 2's user ID in the input field
2. Click **"INVITE"** button
3. Tab 1 shows: INVITE → RINGING states
4. Tab 2 shows incoming call modal

### Step 3: Accept Call

1. In Tab 2, click **"Accept"** in the modal
2. Both tabs show: OK → ACTIVE states
3. WebRTC connection establishes
4. Audio streams directly between browsers

### Step 4: End Call

1. Click **"End Call"** in either tab
2. BYE signal sent
3. Call state → ENDED
4. Cleanup after 5 seconds

## Understanding the Interface

### Main Panel Layout

```
┌─ User List ──────┬─ Call Panel ───────────────┬─ Signal Log ──────┐
│                 │                            │                   │
│ 🌍 user-abc123  │  📡 ZeroPhone Control       │ [14:32:15] SIGNAL│
│ ● user-def456   │  [ACTIVE]                   │ INVITE user-abc   │
│                 │                             │ → user-def        │
│   IDLE ─ INVITE │ ┌─ FSM States ──────────┐   │                   │
│ → RINGING → ACTIVE │ IDLE INVITE RINGING ACTIVE │                   │
│ → ENDED           │ └─────────────────────┘   │                   │
│                 │                             │                   │
│                 │ ┌─ ICE Strategy ────────┐   │                   │
│                 │ Network: zerotier       │   │                   │
│                 │ Relay: none             │   │                   │
│                 │ Boost: 80               │   │                   │
│                 │ └───────────────────────┘   │                   │
│                 │                             │                   │
│                 │ [target-user] [INVITE]      │                   │
└─────────────────┴─────────────────────────────┴───────────────────┘
```

### User List Panel
- **● Green dot**: Online user
- **🌍 Badge**: Remote cluster node
- **Click user**: Auto-fill target field

### Call Panel
- **Global Status**: Current system state
- **FSM Visualization**: State machine progress
- **ICE Info**: Network routing decisions
- **Controls**: Call management buttons

### Signal Log Panel
- **Real-time signals**: All network activity
- **Call transitions**: FSM state changes
- **ICE decisions**: Backend routing hints
- **Error messages**: Connection issues

## Advanced Features

### Multi-Call Support

ZeroPhone supports multiple simultaneous calls:

1. User A calls User B (Call #1 active)
2. User C calls User A (Call #2 incoming)
3. Accept Call #2 → Both calls active
4. Switch focus between calls
5. Each call has independent WebRTC connection

### Cluster Mode

Enable distributed operation:

```bash
export ZEROPHONE_CLUSTER=1
export ZEROTIER_IP=10.x.x.x
go run .
```

Features:
- **Route Gossip**: Nodes share network metrics
- **Load Balancing**: Calls distributed across nodes
- **Multi-hop Routing**: Calls route through optimal paths
- **NAT Traversal**: TURN/STUN server coordination

### Debug Features

#### Signal Monitoring
- Watch real-time signal flow
- Understand call state transitions
- Debug connection issues

#### FSM Visualization
- See current call state (highlighted)
- View completed states (green)
- Track call progress visually

#### ICE Strategy Display
- **Network**: Preferred routing (zerotier/public)
- **Relay**: Whether TURN server needed
- **Boost**: Priority adjustment (0-100)

## Troubleshooting

### WebRTC Won't Connect

**Check:**
1. Browser permissions (microphone access)
2. Network connectivity
3. ICE servers reachable

**Debug:**
- Check Signal Log for ICE candidates
- Verify ICE strategy shows correct network
- Check browser console for WebRTC errors

### Calls Not Reaching Other Users

**Check:**
1. Both users connected to same server
2. User IDs entered correctly
3. WebSocket connection active (green status)

**Debug:**
- Check Signal Log for INVITE signals
- Verify user appears in User List
- Check server logs for routing errors

### Cluster Issues

**Check:**
1. ZeroTier network configured
2. ZMQ ports accessible
3. Environment variables set

**Debug:**
- Check cluster endpoints: `/cluster/status`
- Verify node discovery: `/cluster/nodes`
- Review gossip logs

## Signal Reference

### Call Flow Signals

```
Caller                  Server                 Callee
  │                       │                      │
  │─── INVITE ───────────▶│                      │
  │                       │─── INVITE ─────────▶│
  │                       │                      │
  │◀── TRYING ────────────│                      │
  │                       │◀── RINGING ─────────│
  │◀── RINGING ───────────│                      │
  │                       │                      │
  │                       │◀── OK ──────────────│
  │◀── OK ────────────────│                      │
  │                       │                      │
  │─── SDP (offer) ──────▶│                      │
  │                       │─── SDP ────────────▶│
  │                       │◀── SDP (answer) ────│
  │◀── SDP ───────────────│                      │
  │                       │                      │
  │─── ICE ──────────────▶│◀────────────────────│
  │◀─────────────────────▶│─── ICE ────────────▶│
  │                       │                      │
  │◀── WebRTC Connected ─▶│◀── Connected ──────│
  │                       │                      │
  │─── BYE ──────────────▶│                      │
  │                       │─── BYE ────────────▶│
```

### Metadata Fields

Signals include optional metadata:

```json
{
  "type": "ICE",
  "call_id": "uuid-123",
  "candidate": "...",
  "_ice": {
    "preferred_network": "zerotier",
    "use_relay": false,
    "priority_boost": 80
  },
  "_route": {
    "path": ["node-a", "node-b"],
    "cost": 25.3
  }
}
```

## Performance Tips

### For Multiple Users
- Use cluster mode for load distribution
- Monitor `/status` endpoint for metrics
- Check signal logs for bottlenecks

### Network Optimization
- Prefer ZeroTier networks (boost: 80)
- Enable TURN servers for NAT traversal
- Monitor load factors in debug panel

### Browser Compatibility
- Chrome/Edge: Full WebRTC support
- Firefox: Good support
- Safari: Limited (check ICE compatibility)

## Development Mode

Enable detailed logging:

```bash
export ZEROPHONE_DEBUG=1
go run .
```

Access debug endpoints:
- `/debug.html` - Full debug interface
- `/status` - System metrics
- `/cluster/status` - Cluster health

## Server Deployment

### Prerequisites
- Go 1.19+ installed
- Domain name pointing to your server
- Firewall allowing ports 80, 443, 9443

### 1. Build and Run Server

```bash
cd zerophone
go build -o zerophone .
./zerophone
```

Server listens on `localhost:9443` (no TLS - Caddy handles it).

### 2. Install and Configure Caddy

```bash
# Install Caddy (Ubuntu/Debian)
sudo apt install caddy

# Copy Caddyfile to /etc/caddy/
sudo cp Caddyfile /etc/caddy/Caddyfile

# Edit domain name
sudo nano /etc/caddy/Caddyfile
# Replace 'yourdomain.com' with your actual domain

# Reload Caddy
sudo systemctl reload caddy
```

### 3. Configure Firewall

```bash
# Allow HTTP/HTTPS (Caddy)
sudo firewall-cmd --add-service=http --permanent
sudo firewall-cmd --add-service=https --permanent

# Allow ZeroPhone port (internal)
sudo firewall-cmd --add-port=9443/tcp --permanent

# Reload firewall
sudo firewall-cmd --reload
```

### 4. Test Deployment

1. Visit `https://yourdomain.com`
2. Open Control Terminal
3. Verify WebSocket connects (check browser console)
4. Make test calls

### Multi-Node Scaling

For load balancing across multiple servers:

```caddyfile
yourdomain.com {
    reverse_proxy {
        to localhost:9443 localhost:9444 localhost:9445
        lb_policy random
    }
}
```

Each node runs ZeroPhone on different ports.

### ZeroTier Integration

For pure ZeroTier internal network:

1. Join ZeroTier network on server
2. Configure Caddy to bind to ZeroTier IP
3. Access via ZeroTier IP instead of public domain

### Monitoring

- `/status` - Server health
- `/debug.html` - Debug dashboard
- `/nodes` - Cluster nodes

## Next Steps

With basic calling working, explore:

1. **Cluster Scaling**: Add more nodes
2. **TURN Servers**: Configure STUN/TURN
3. **Custom Signaling**: Extend signal types
4. **Call Recording**: Add audio persistence
5. **Video Support**: Extend WebRTC to video

The system now provides a complete distributed telecom foundation!