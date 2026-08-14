# Configuration

Copy [`.env.example`](../.env.example) to `.env`; never commit the populated file.

## Network and account variables

| Variable | Purpose |
|---|---|
| `NEXTENDO_HOST` | Public IP/hostname advertised to clients, without a port. |
| `AUTH_PORT` | Authentication listener; default `443`. |
| `SECURE_PORT` | Secure NEX listener; default `60003`. |
| `CERT_FILE`, `KEY_FILE` | TLS certificate and private-key paths. |
| `NEXTENDO_PROXY_PROTOCOL` | Accept PROXY protocol when set to `1`; enable only behind a trusted proxy. |
| `NEXTENDO_ACCOUNT_URL` | Nextendo account-service base URL. |
| `NEXTENDO_REQUIRE_ACCOUNT` | Require a valid Nextendo account when `1`. |
| `NEXTENDO_REQUIRE_SIGNED_TOKEN` | Require signed login proof when `1`; recommended for production. |
| `NEXTENDO_INTERNAL_KEY` | Internal account-service credential. |
| `NEXTENDO_SECRET`, `NEXTENDO_SECRET_FILE` | Shared `nx2` verification secret, inline or file-backed. |

## Integration variables

| Variable | Default | Purpose |
|---|---:|---|
| `MATCHMAKING_DEDUP_SECONDS` | `20` | Completed mutation response lifetime. |
| `MATCHMAKING_DEDUP_MAX_ENTRIES` | `4096` | Upper cache bound before aggressive expiry pruning. |
| `MATCHMAKING_RESERVATION_SECONDS` | `8` | Pending seat lifetime. |

Start with the defaults. Shortening the dedup TTL can miss delayed retransmits; making it too long
retains obsolete responses. Reservation TTL should cover normal join processing but release an
abandoned seat quickly.

## Dashboard and inherited switches

| Variable | Purpose |
|---|---|
| `DASH_PORT` | Dashboard listener inside the process; default `8082`. |
| `DASH_TOKEN` | Query token required by the inherited dashboard. Never leave empty in production. |
| `NEXTENDO_GHOST_IDLE_SECONDS` | Dashboard ghost-player timeout. |
| `NEXTENDO_OPEN_PARTICIPATION` | Inherited title-specific override; keep `0` unless validated. |
| `NEX_SOLO_PIDS` | Inherited test isolation list; leave empty in production. |

This branch intentionally has no Redis, NNCS, rate-limit, host-scoring or lifecycle configuration.
