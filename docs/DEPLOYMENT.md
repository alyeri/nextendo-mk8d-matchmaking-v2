# Deployment

This guide targets an isolated staging server. Do not redirect production users until the test
matrix in [TESTING.md](TESTING.md) has passed.

## Required network ports

| Port | Protocol | Purpose |
|---|---|---|
| 443 | TCP | TLS TicketGranting/LoginEx endpoint |
| 60003 | TCP | Secure NEX/PRUDP-over-WebSocket endpoint |
| 10025, 10125 | UDP | NNCS NAT probes |
| 33334 | UDP | NNCS silent-port behavior |
| 50920 | UDP | NNCS filtering probe source |
| 18082 | TCP | Private admin API; loopback only |
| 6379 | TCP | Redis; private Docker network/loopback only |

Cloud firewall rules should expose only the game TCP and NNCS UDP ports. Never expose 18082 or
6379 to the public Internet.

## Docker Compose staging deployment

1. Install Docker Engine and the Compose plugin.
2. Copy the environment template:

   ```sh
   cp .env.example .env
   chmod 600 .env
   ```

3. Replace every `CHANGE_ME` value. Generate secrets, for example:

   ```sh
   openssl rand -hex 32
   ```

4. Set `NEXTENDO_HOST` and `NNCS_PUBLIC_IP` to the staging server's public IPv4 address.
5. For initial staging, `NEXTENDO_AUTO_CERT=1` creates a self-signed certificate. Production must
   provide a trusted certificate and set it to `0`.
6. Start the stack:

   ```sh
   docker compose up -d --build
   docker compose logs -f server
   ```

The Compose file publishes the dashboard only as `127.0.0.1:18082` on the host and does not publish
Redis at all.

## Dashboard access

Use an SSH tunnel:

```sh
ssh -L 18082:127.0.0.1:18082 user@staging-server
```

Then query with a Bearer token:

```sh
curl -H "Authorization: Bearer $DASH_TOKEN" http://127.0.0.1:18082/api/rooms
```

Eviction is POST-only:

```sh
curl -X POST \
  -H "Authorization: Bearer $DASH_TOKEN" \
  -H "Content-Type: application/json" \
  --data '{"pid":1800000001}' \
  http://127.0.0.1:18082/api/kick
```

## Native systemd deployment

Build a static Linux binary:

```sh
cd server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o mk8d-server .
```

Recommended service properties:

- dedicated unprivileged user;
- `AmbientCapabilities=CAP_NET_BIND_SERVICE` instead of running as root;
- `NoNewPrivileges=true`;
- strict filesystem protection with one writable data directory;
- environment file mode `0600`;
- automatic restart on failure;
- loopback Redis and dashboard binds.

Always keep the prior binary and environment file before replacing them. Check that no players or
rooms are active through `/api/stats` before a planned restart.

## Redis behavior

Redis is optional. With `REDIS_REQUIRED=0`, startup and active rooms continue when Redis is
temporarily unavailable. Use `REDIS_REQUIRED=1` only when infrastructure policy requires Redis to
be healthy before accepting traffic.

Do not treat the room index as authorization or as authoritative cross-instance join state.

## Rollback

1. Stop accepting new traffic at the staging ingress.
2. Confirm whether active rooms remain.
3. Restore the previous binary and environment file.
4. Restart the service.
5. Leave Redis data intact unless schema incompatibility is proven; all state keys expire.
6. Record the failing commit, sanitized logs and reproduction steps.

## Production readiness gate

Do not promote until:

- strict signed auth works for every supported client type;
- 6–12 player and multi-room staging tests pass;
- host-loss and intermission recovery are measured;
- rate limits show no false positives;
- external scans confirm dashboard and Redis are closed;
- a maintainer reviews the ticketless CONNECT risk.
