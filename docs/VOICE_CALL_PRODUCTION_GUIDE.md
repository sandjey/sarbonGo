# Voice Call Production Guide

This guide describes how to prepare the server for production voice calls and how to run end-to-end API/WS tests from two devices.

## 1) What backend already does

- Stores call lifecycle state in PostgreSQL (`RINGING`, `ACTIVE`, `ENDED`, etc.).
- Handles call REST APIs under `/v1/calls`.
- Relays signaling messages (`webrtc.offer`, `webrtc.answer`, `webrtc.ice`, `call.end`) via `/v1/chat/ws`.
- Applies create-call rate limit and sweeps expired ringing calls to `MISSED`.

Backend does **not** transport audio/video media itself. Media is WebRTC peer-to-peer, typically with TURN fallback.

## 2) Server prerequisites

- Ubuntu server with public IP and DNS (example: `api.example.com`, `turn.example.com`).
- Open ports:
  - API: `80/tcp`, `443/tcp`
  - TURN: `3478/tcp+udp`, `5349/tcp`, plus relay UDP range (for example `49152-65535/udp`)
- PostgreSQL and Redis available to API.
- TLS certificate (Let's Encrypt or equivalent).

## 3) API host setup (Nginx + WebSocket)

For the API location, ensure websocket upgrade headers are present:

```nginx
proxy_http_version 1.1;
proxy_set_header Upgrade $http_upgrade;
proxy_set_header Connection "upgrade";
proxy_set_header Host $host;
proxy_read_timeout 3600;
proxy_send_timeout 3600;
```

Without these headers, `/v1/chat/ws` will fail or disconnect quickly.

## 4) TURN server setup (coturn)

Install:

```bash
sudo apt update
sudo apt install -y coturn
```

Enable service:

```bash
sudo sed -i 's/^#TURNSERVER_ENABLED=1/TURNSERVER_ENABLED=1/' /etc/default/coturn
```

Minimal `/etc/turnserver.conf`:

```conf
listening-port=3478
tls-listening-port=5349
realm=turn.example.com
fingerprint
use-auth-secret
static-auth-secret=CHANGE_ME_LONG_RANDOM_SECRET
no-cli
stale-nonce=600
cert=/etc/letsencrypt/live/turn.example.com/fullchain.pem
pkey=/etc/letsencrypt/live/turn.example.com/privkey.pem
```

Firewall example:

```bash
sudo ufw allow 3478/tcp
sudo ufw allow 3478/udp
sudo ufw allow 5349/tcp
sudo ufw allow 49152:65535/udp
```

Start:

```bash
sudo systemctl enable coturn
sudo systemctl restart coturn
sudo systemctl status coturn
```

## 5) App environment checklist

Set and verify:

- `DATABASE_URL`
- `REDIS_ADDR`, `REDIS_PASSWORD`, `REDIS_DB`
- `JWT_SIGNING_KEY`
- `CLIENT_TOKEN_EXPECTED`
- `CALLS_RINGING_TIMEOUT_SECONDS` (default `30`)
- `CALLS_CREATE_LIMIT` (default `6`)
- `CALLS_CREATE_WINDOW_SECONDS` (default `60`)

Optional hardening:

- Disable WS auth by `user_id` in production and keep JWT-only WS auth.

## 6) New test tools in project

- Swagger docs: `/docs`
- Generic WS tester: `/ws-test`
- Voice-call test lab (REST + WS): `/calls-test`
- Bootstrap helper API: `GET /v1/calls/test/bootstrap`

These let you test calls quickly from two devices/accounts.

## 7) Two-device end-to-end test plan

Prerequisites:

- User A and user B accounts with valid JWT access tokens.
- Both devices can access API host over HTTPS/WSS.

Steps:

1. Open `/calls-test` on both devices.
2. Fill base URL + required headers + each user JWT.
3. On both devices, press `WS Connect`.
4. On device A:
   - set `peer_id` to user B UUID
   - press `Create` (`POST /v1/calls`)
   - copy generated `call_id`
5. On device B:
   - paste `call_id`
   - press `Accept`
6. Signaling test:
   - A sends `webrtc.offer` template
   - B receives it in log, sends `webrtc.answer`
   - both exchange `webrtc.ice`
7. End call:
   - either side presses `End`
8. Validate state:
   - `GET /v1/calls/{id}` should show final status
   - `GET /v1/calls` should include the call

Negative tests:

- Busy conflict: create second call while active -> expect `409`.
- Rate limit: burst create calls -> expect `429`.
- Timeout: leave `RINGING` unaccepted -> should transition to `MISSED`.

## 8) Production readiness checklist

- [ ] WSS works through Nginx with stable reconnect behavior.
- [ ] TURN works from different networks (Wi-Fi vs mobile).
- [ ] Call state transitions verified (`RINGING -> ACTIVE -> ENDED`, etc.).
- [ ] 409/429 paths tested and expected.
- [ ] Monitoring enabled for call create/accept/end and error rates.
- [ ] Backup strategy for PostgreSQL and basic Redis observability in place.

