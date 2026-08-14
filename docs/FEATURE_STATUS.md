# Feature status

## Included and tested

| Feature | Status | Boundary |
|---|---|---|
| Current public Nextendo core | Synced | Profiles, Miis, friends, tournaments and rankings preserved. |
| Completed RMC retry cache | Active | Mutating matchmaking allowlist only. |
| Concurrent RMC coalescing | Active | One underlying execution per identical in-flight key. |
| Dedup TTL and size bound | Active | Configurable; default 20 seconds / 4096 entries. |
| Atomic seat reservation | Active | PID-keyed, mutex-protected and capacity-aware. |
| Reservation expiry/cancel | Active | Default eight seconds; cancellation is idempotent. |

## Inherited unchanged

- Signed `nx2` extraction and account validation.
- Player profiles, nicknames, Miis and online friends.
- Tournament creation, search, registration and rankings.
- NAT-aware room compatibility and existing participant notifications.
- Current room cleanup/reaper and dashboard behavior.

## Not included

| Earlier experiment | Decision |
|---|---|
| Shared-NAT FIFO identity queue | Removed as unsafe without cryptographic CONNECT binding. |
| Redis room directory | Deferred; unrelated to the first accepted changes. |
| Rate limiting | Deferred to its own coordinated change. |
| Formal room lifecycle | Deferred to its own coordinated change. |
| Reconnect lease / host scoring | Deferred; requires larger protocol validation. |
| Party reservations | Not inferred from IP/friends; no stable client party ID exists. |

## Validation still required

- A real 6–12 player, multi-network soak test.
- Production-volume observation near the reported ~60 concurrent-player workload.
- Packet-loss/retry testing against actual Ryujinx and Atmosphère clients.
- Maintainer review of the exact mutation allowlist and reservation integration points.
