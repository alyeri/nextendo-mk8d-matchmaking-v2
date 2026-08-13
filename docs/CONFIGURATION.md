# Configuration reference

The server reads configuration from environment variables. Start with
[`server/example.env`](../server/example.env) for direct execution or [`.env.example`](../.env.example)
for Docker Compose. Never commit a populated `.env` file.

## Network and TLS

| Variable | Purpose | Production guidance |
|---|---|---|
| `NEXTENDO_HOST` | Public IPv4 address advertised to clients. | Use an address only, without a port. |
| `AUTH_PORT` | PRUDP authentication endpoint. | Default deployment uses TCP/443. |
| `SECURE_PORT` | Secure PRUDP/NEX endpoint. | Default deployment uses TCP/60003. |
| `CERT_FILE`, `KEY_FILE` | TLS certificate and private-key paths. | Mount them read-only; never store the key in Git. |
| `NEXTENDO_AUTO_CERT` | Generates a self-signed certificate when files are absent. | Development only; keep `0` in production. |
| `NEXTENDO_PROXY_PROTOCOL` | Accepts upstream PROXY protocol metadata. | Enable only behind a trusted compatible proxy. |

## Account integration

| Variable | Purpose | Production guidance |
|---|---|---|
| `NEXTENDO_ACCOUNT_URL` | Nextendo account service base URL. | Use TLS and the intended account environment. |
| `NEXTENDO_REQUIRE_ACCOUNT` | Rejects unauthenticated guest identities. | Keep `1`. |
| `NEXTENDO_REQUIRE_SIGNED_TOKEN` | Requires a valid signed `nx2` proof in `LoginEx`. | Keep `1`; use `0` only for a controlled migration. |
| `NEXTENDO_INTERNAL_KEY` | Internal service authentication key. | Generate a unique high-entropy value. |
| `NEXTENDO_SECRET` | Inline token verification secret. | Prefer `NEXTENDO_SECRET_FILE`. |
| `NEXTENDO_SECRET_FILE` | File containing the token verification secret. | Mount as a runtime secret. |
| `NEXTENDO_SECURE_PASSWORD` | Shared Kerberos password for auth and secure endpoints. | Generate a unique high-entropy value. |

## Matchmaking policy

| Variable | Purpose | Safe initial value |
|---|---|---|
| `MATCHMAKING_COMPAT_ATTRIBUTES` | Attribute-prefix bytes required to match. | `0` until MK8D attributes are classified. |
| `MATCHMAKING_RESERVATION_SECONDS` | Pending join seat lifetime. | `8` |
| `MATCHMAKING_RECONNECT_GRACE_SECONDS` | Authenticated participant reconnect lease. | `20` |
| `MATCHMAKING_RELAX_AFTER_SECONDS` | Delay before compatible search constraints relax. | `8` |
| `MATCHMAKING_SEARCH_IDLE_SECONDS` | TTL for abandoned searches. | `120` |
| `MATCHMAKING_SESSION_IDLE_SECONDS` | TTL for established inactive sessions. | `1200` |
| `MATCHMAKING_INTERMISSION_GRACE_SECONDS` | Recovery window during results/map selection. | `20` |
| `MATCHMAKING_JANITOR_SECONDS` | Stale-state cleanup interval. | `15` |
| `MATCHMAKING_DEDUP_SECONDS` | Mutating RMC reply cache lifetime. | `20` |
| `MATCHMAKING_QUALITY_WEIGHT` | Weight of NAT/direct readiness in host scoring. | `3` |
| `MATCHMAKING_PRE_RACE_HOST_SELECTION` | Enables experimental pre-race host switching. | `0` |

The first staging rollout should change one policy at a time. In particular, keep pre-race host
selection disabled until the exact MK8D notification sequence has been validated with several
real clients.

## Abuse protection

| Variable | Purpose | Example |
|---|---|---|
| `AUTH_RATE_LIMIT_PER_MINUTE` | Authentication attempts allowed per IP. | `30` |
| `AUTH_RATE_BLOCK_SECONDS` | Temporary authentication block duration. | `30` |
| `MATCHMAKING_RATE_LIMIT_PER_10_SECONDS` | General control-plane operations per IP/PID window. | `300` |
| `MATCHMAKING_EXPENSIVE_LIMIT_PER_10_SECONDS` | Expensive matchmaking mutations per window. | `40` |
| `MATCHMAKING_RATE_BLOCK_SECONDS` | Temporary matchmaking block duration. | `10` |

These controls intentionally exclude peer-to-peer race traffic, NAT probes, keep-alives and server
notifications. Tune them from observed staging traffic; do not raise them merely to hide a client
retry loop.

## Redis and multi-instance state

| Variable | Purpose | Guidance |
|---|---|---|
| `REDIS_URL` | Redis connection URL; empty disables Redis. | Use an authenticated private endpoint. |
| `REDIS_REQUIRED` | Stops startup if Redis is unavailable. | Begin with `0`; consider `1` only after operational validation. |
| `REDIS_KEY_PREFIX` | Namespace for all generated keys. | Use a unique environment prefix. |
| `REDIS_STATE_TTL_SECONDS` | Lifetime of mirrored room and presence state. | Must exceed the sync interval. |
| `REDIS_SYNC_INTERVAL_SECONDS` | Interval between state snapshots. | `5` |
| `NEXTENDO_INSTANCE_ID` | Stable unique server instance identifier. | Never reuse concurrently. |

Redis is currently an expiring coordination and visibility layer. A client still joins through the
instance that owns the in-memory session; the global Redis index is not an authoritative cross-node
transaction database.

## Administration and NNCS

| Variable | Purpose | Production guidance |
|---|---|---|
| `DASH_BIND`, `DASH_PORT` | Administrative listener. | Bind to loopback; use an SSH tunnel. |
| `DASH_TOKEN` | Bearer token for administrative requests. | Generate a unique high-entropy value. |
| `NNCS_LISTEN_IP` | Local NNCS bind address. | Usually `0.0.0.0` in a container. |
| `NNCS_PUBLIC_IP` | Public address returned/observed by NNCS. | Match the reachable VPS address. |
| `NNCS_PROBE_PORTS` | UDP NAT probe ports. | Open the configured ports in both firewalls. |
| `NNCS_SILENT_PORT` | Silent UDP mapping test port. | Open UDP only. |
| `NNCS_FILTER_PROBE_PORT` | UDP filtering test port. | Open UDP only. |
| `NNCS_NAT_FILE` | Local observation log. | Store under a persistent data directory. |

See [Deployment](DEPLOYMENT.md) for firewall rules, secret generation, health checks and rollback.
