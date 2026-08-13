# Proposal for Nextendo review

## Summary

This repository proposes an incremental MK8D matchmaking resilience layer. It does not replace the
P2P race protocol and does not require enabling every feature at once.

## Problems addressed

- stale rooms and dead hosts remaining eligible;
- duplicate mutations caused by retries or client stalls;
- overbooking during concurrent joins;
- fragile reentry around results/map selection;
- incorrect identity inheritance for multiple clients behind one NAT;
- insufficiently private operational endpoints;
- no expiring room directory for future multi-instance routing.

## Integration strategy

Recommended review units:

1. Signed LoginEx proof and FIFO shared-NAT claims.
2. Reservation and lifecycle primitives in `nextendo-nex`.
3. Reconnection/host-loss cleanup.
4. Retry deduplication and conservative rate limits.
5. Redis state mirror/global directory and private dashboard.
6. Guarded future features: pre-race host selection and parties.

Each unit can be reviewed and enabled independently.

## Compatibility principles

- Preserve serialized NEX structures.
- Do not reinterpret unknown attributes by guesswork.
- Do not send undocumented client fields.
- Keep Redis out of the race packet path.
- Default uncertain behavior off.
- Treat explicit exits differently from transient transport loss.

## Evidence available

- Automated unit tests for protocol, NAT mapping, reservations, lifecycle, host selection, Redis
  records, auth proof parsing, rate limits and deduplication.
- Repeat test runs and static analysis.
- Linux reproducible builds.
- Small real-world sessions completing multiple consecutive races on low-resource hardware.

## Evidence still needed before production

- 6–12 player multi-network staging test.
- Concurrent Worldwide/Regional/private rooms.
- Atmosphère and emulator interoperability under strict signed auth.
- Host loss during map selection and long soak sessions.
- Baseline comparison of communication errors and matchmaking time.
- Independent review of the ticketless CONNECT compatibility bridge.

## Suggested rollout

Deploy behind a staging hostname, collect sanitized metrics for one or two weeks, and promote only
the features that demonstrate improvement. Keep `MATCHMAKING_PRE_RACE_HOST_SELECTION=0` until the
required client migration notification is proven.
