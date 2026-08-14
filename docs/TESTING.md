# Testing and soak plan

## Automated checks

Run both modules independently:

```sh
cd nextendo-nex
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...

cd ../server
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
```

The reservation suite covers capacity accounting, expiry, idempotent cancellation and 64 concurrent
PIDs competing for one seat. The dedup suite covers completed retries, cloned response bodies,
in-flight concurrency, identity/payload separation, nil responses, expiry and read-only exclusion.

## RMC retry integration test

1. Instrument the underlying create handler with an execution counter.
2. Send the same mutating RMC request repeatedly with the same connection/PID/call ID/body.
3. Send several copies concurrently before the first handler returns.
4. Confirm one mutation, one room ID and byte-equivalent responses.
5. Reuse the call ID with a different body; confirm a separate mutation.
6. Repeat from another PID/connection; confirm isolation.
7. Wait beyond the TTL; confirm a new execution is allowed.

## Reservation integration test

1. Create a room with one remaining seat.
2. Race simultaneous join attempts from different authenticated PIDs.
3. Confirm one reservation succeeds and every other attempt receives session-full behavior.
4. Confirm `NumParticipants` excludes the reservation before commit.
5. Commit the winner and confirm the room reaches exactly its maximum.
6. Repeat with an abandoned reservation and verify expiry frees the seat.

## Real-client soak matrix

Test Worldwide, Regional, private rooms and tournaments separately. Use:

- 2 clients behind the same household NAT;
- clients on at least three unrelated networks;
- a mix of Ryujinx and Atmosphère where available;
- 6, 8 and 12-player rooms;
- 60–120 minute sessions including map selection and results transitions.

Record sanitized counts only: underlying mutations, dedup hits/waiters, reservation accepts/rejects,
room count, communication errors and server panics. Do not publish IP addresses, account tokens,
Miis or packet captures.

## Acceptance criteria

- No duplicate room from identical retransmitted create calls.
- No room exceeds `MaxParticipants` including pending reservations.
- No cross-PID or cross-connection cached response reuse.
- No regressions in profiles, friends, tournaments, rankings or NAT matchmaking tests.
- No data races in either module.
- No statistically meaningful increase in communication errors versus the current server.
