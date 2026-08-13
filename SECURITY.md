# Security policy and model

## Reporting a vulnerability

Do not open a public issue containing credentials, private keys, access tokens, exploitable player
data or a working denial-of-service procedure. Contact the repository owner privately and include:

- affected commit and component;
- reproduction conditions;
- expected impact;
- whether active exploitation was observed;
- a minimal proof of concept with secrets removed.

No bug bounty is promised by this experimental project.

## Current protections

- HMAC-signed, expiring `nx2` account proof bound to the requested PID.
- Optional strict rejection of unsigned/bare account PIDs.
- One-time, short-lived FIFO identity claims for ticketless secure CONNECT compatibility.
- IP/PID rate limits and progressive temporary blocks on control-plane calls.
- Atomic room reservations and bounded in-memory caches.
- Redis bound privately, authenticated and treated as non-authoritative.
- Dashboard bound to loopback by default.
- `Authorization: Bearer` instead of query-string credentials.
- Administrative mutations require POST and bounded JSON bodies.
- No IP addresses, tickets, station URLs or session keys in Redis room snapshots.
- Expiring room, presence and service keys.

## Production requirements

- Set `NEXTENDO_REQUIRE_ACCOUNT=1` and `NEXTENDO_REQUIRE_SIGNED_TOKEN=1`.
- Generate independent high-entropy values for every secret.
- Never copy `server/example.env` unchanged into production.
- Use a trusted TLS certificate and protect the private key.
- Keep `DASH_BIND=127.0.0.1`; access the API through an SSH tunnel or authenticated proxy.
- Bind Redis to a private interface or Docker network; do not publish port 6379 publicly.
- Restrict ingress to the exact TCP/UDP ports the service requires.
- Run as a dedicated non-root user with systemd or container hardening.
- Rotate secrets and invalidate sessions after suspected exposure.
- Retain bounded logs and avoid logging raw tokens or complete authorization headers.

## Trust boundaries

- The account service is trusted to issue `nx2` proofs.
- The outer BAAS JWT is decoded only as a container; its authenticity is not assumed by this server.
- Clients and peers are untrusted.
- Redis is operational infrastructure, not an authorization authority.
- The dashboard token grants administrative visibility and eviction capability.
- P2P participants necessarily receive peer network endpoints.

## Known risks

1. A stolen valid `nx2` token can be replayed until expiry.
2. Ticketless CONNECT binding is temporal rather than end-to-end cryptographic.
3. A determined distributed denial-of-service attack requires upstream/cloud mitigation.
4. Host migration cannot preserve all in-race P2P state.
5. This code has not undergone an independent security audit or penetration test.

## Supported versions

Only the latest commit on the default branch is intended to receive security fixes during the
experimental phase.
