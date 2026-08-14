# Staging deployment

Do not replace the production MK8D server directly. Deploy this branch to an isolated staging
endpoint first.

## Exposed ports

| Port | Protocol | Exposure |
|---:|---|---|
| 443 | TCP | Public authentication endpoint. |
| 60003 | TCP | Public secure NEX endpoint. |
| 18082 | TCP | Host loopback only; maps to dashboard port 8082. |

No Redis or NNCS ports are used by this branch.

## Docker Compose

```sh
cp .env.example .env
mkdir -p secrets
# Install cert.pem and key.pem in ./secrets with restrictive permissions.
# Replace every CHANGE_ME value in .env.
docker compose config
docker compose up -d --build
```

The container runs as a non-root user with only `NET_BIND_SERVICE`, a read-only root filesystem and
the TLS directory mounted read-only. The dashboard is published only on `127.0.0.1:18082`.

Health check through an SSH tunnel or locally on the host:

```sh
curl http://127.0.0.1:18082/healthz
```

Access statistics only with the configured inherited dashboard token; do not expose the dashboard
directly to the Internet.

## Rollout

1. Record current production error and room-creation baselines.
2. Validate two clients, including separate accounts behind one NAT.
3. Validate profiles, friends, tournaments and rankings.
4. Force duplicate RMC retries and concurrent final-seat joins.
5. Run the multi-network soak matrix in [Testing](TESTING.md).
6. Compare results before proposing an upstream cherry-pick.

## Rollback

This work is split into independent commits. Roll back reservations separately from deduplication;
do not replace the official core with an older copy. Preserve sanitized logs long enough to identify
whether a failure occurred in the wrapper, capacity accounting or unchanged upstream behavior.
