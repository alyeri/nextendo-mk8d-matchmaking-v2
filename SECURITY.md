# Security policy

## Scope

This branch changes retry handling and room-capacity accounting. It does not claim to solve NEX
transport security, CGNAT traversal or ticketless secure-connection identity binding.

## Identity boundary

The shared-NAT FIFO identity queue from the earlier experiment has been removed. Never assign a PID
because it was the next claim associated with an IP address. Under carrier-grade NAT, unrelated
players share a public address and their authentication/CONNECT operations can interleave.

A future ticketless binding is acceptable only if the CONNECT supplies a nonce/token/proof that:

1. is cryptographically verifiable by the server;
2. identifies the same account and PID as the signed `nx2` claim;
3. is single-use or replay-bounded;
4. cannot be selected by arrival order or source address alone.

If the current protocol cannot carry that evidence, reject the connection or change the client
protocol. Do not silently fall back to identity guessing.

## Deduplication boundary

- Cache keys include PID, connection, protocol, method, call ID and body hash.
- Only an allowlist of mutating matchmaking methods is deduplicated.
- Read-only calls and asynchronous URL retrieval are excluded.
- Responses are cloned to prevent callers from mutating cached state.
- In-flight entries unblock even if the wrapped handler panics; the panic still propagates.
- TTL and maximum-entry controls bound retention.

Deduplication is not authorization. A request must already be authenticated and authorized by the
underlying handler.

## Reservation boundary

- Reservations are keyed by PID and protected by the matchmaking mutex.
- Expired reservations do not consume capacity.
- A reservation does not make a PID a participant and is not reported to clients.
- Cancellation is idempotent.

## Deployment

- Keep `.env`, certificates, signing secrets and dashboard tokens outside Git.
- Require signed account tokens in production.
- Publish only the auth and secure game ports.
- Keep the inherited dashboard on loopback or a private network and require `DASH_TOKEN`.
- Do not deploy this branch as a wholesale replacement before multi-network soak testing.

## Reporting

Do not open a public issue containing credentials, private keys, player IPs, PIDs, Miis, captures or
account tokens. Contact the repository owner privately and include only the minimum reproduction
information needed.
