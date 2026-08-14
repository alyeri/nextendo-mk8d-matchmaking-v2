# Incremental integration proposal

## Maintainer feedback incorporated

The original repository was reviewed by the Nextendo owner. The review accepted RMC deduplication,
atomic reservations, rate limiting and a formal room lifecycle as useful ideas, but requested that
they be brought into the current core one at a time. It also rejected arrival-order identity
assignment and identified that the public mirrors had gained profiles, friends, tournaments and
rankings.

This branch implements that direction:

1. reset both source modules to the current public mirrors;
2. preserve all newly published functionality;
3. remove the shared-NAT FIFO identity queue;
4. port RMC deduplication as one isolated commit;
5. port atomic reservations as a second isolated commit;
6. defer every other proposal.

## Suggested upstream split

### Change A: RMC deduplication

Review the server wrapper, mutation allowlist, bounded cache and concurrency tests. This change does
not modify NEX wire encoding or matchmaking data structures.

### Change B: atomic reservations

Review the reservation primitive and the small integration points in `matchmaking_handlers.go`.
This change adds no wire fields and preserves client-visible participant counts.

Each change should be accepted, revised or rejected independently. Rate limiting and lifecycle work
should not begin until maintainers confirm that nobody else is implementing them.

## Evidence required before production

Automated tests establish deterministic semantics, not production safety. Before merging at the
reported server scale of roughly sixty concurrent players, run multi-network client tests and
compare duplicate-room, over-capacity and communication-error rates with the current deployment.

## Identity follow-up

Identity is intentionally outside this proposal. Any future design must bind the secure CONNECT to
the account/PID proof itself. IP equality may be a routing signal but is not account identity, and
FIFO ordering is never sufficient proof under CGNAT.
