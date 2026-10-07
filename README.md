# ZeroPhone v2.0

A distributed softphone platform built on WebRTC and ZeroTier mesh networking.

## Quick Start

```bash
go run .
```

Open http://localhost:9443

## Features

- **WebRTC Audio**: Peer-to-peer voice calls
- **Signaling FSM**: SIP-compatible state machine
- **Cluster Mode**: Distributed operation across nodes
- **ZeroTier Integration**: Mesh networking support

## Documentation

See [USAGE.md](USAGE.md) for complete usage guide and deployment instructions.

## Architecture

Browser (WebRTC) ←→ ZeroPhone Server (Signaling) ←→ Peer-to-Peer Audio

Built with Go, Gorilla WebSocket, and ZMQ for clustering.

## SIP Bridge (inbound MVP)

ZeroPhone now includes a basic SIP bridge API for inbound call control.
This lets an external SIP engine (for example `go-b2bua`) inject SIP call events
into the existing ZeroPhone WebRTC signaling flow.

### Endpoints

- `POST /sip/map` maps a SIP extension to a ZeroPhone user id.
- `GET /sip/map` lists current extension mappings.
- `POST /sip/register` self-registers extension to a ZeroPhone user id.
- `POST /sip/unregister` removes extension mapping.
- `GET /sip/sessions` shows active SIP bridge sessions.
- `GET /sip/media/capabilities` shows media-bridge capability metadata.
- `GET /sip/media/sessions` shows SDP/media profile per active SIP bridge session.
- `GET /sip/media/workers` shows runtime media worker state/ports per call.
- `POST /sip/invite` injects an inbound SIP INVITE into ZeroPhone.
- `POST /sip/bye` injects SIP BYE for an active bridged call.

Pickup policy lever:
- `single` (default): extension can ring multiple users, but only first `OK` is forwarded.
- `shared`: multiple mapped users can answer/join (events are forwarded to bridge callback).

### Quick test

1) Map extension `1001` to a browser user id:

```bash
curl -X POST http://localhost:9443/sip/map \
  -H "Content-Type: application/json" \
  -d '{"extension":"1001","user_id":"user-abc123","policy":"single"}'
```

2) Inject an INVITE (with optional SDP):

```bash
curl -X POST http://localhost:9443/sip/invite \
  -H "Content-Type: application/json" \
  -d '{"call_id":"call-1","from":"2001","to_ext":"1001","sdp":"v=0..."}'
```

3) End the call from SIP side:

```bash
curl -X POST http://localhost:9443/sip/bye \
  -H "Content-Type: application/json" \
  -d '{"call_id":"call-1","from":"2001"}'
```

4) Unregister extension mapping:

```bash
curl -X POST http://localhost:9443/sip/unregister \
  -H "Content-Type: application/json" \
  -d '{"extension":"1001"}'
```

### Optional callback to SIP core

Set `ZEROPHONE_SIP_CALLBACK_URL` to receive call events (`TRYING`, `RINGING`,
`OK`, `REJECT`, `BYE`) from ZeroPhone back to your SIP service:

```bash
export ZEROPHONE_SIP_CALLBACK_URL=http://127.0.0.1:18080/sip/events
```