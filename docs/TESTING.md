# Testing and evaluation plan

## Automated checks

Run from the repository root:

```sh
cd nextendo-nex
go test ./...
go vet ./...

cd ../server
go test ./...
go vet ./...
```

On Linux with CGO and a C compiler:

```sh
cd nextendo-nex && go test -race ./...
cd ../server && go test -race ./...
```

CI runs formatting, unit tests, vet, Linux builds and the race detector.

## Required staging matrix

### Client/network combinations

- Two Ryujinx instances behind the same NAT.
- Ryujinx clients on separate residential networks.
- Ryujinx and Atmosphère together, if the Atmosphère client supports the required signed login.
- At least one restrictive NAT and one direct-ready NAT.
- 6–12 players from at least three networks.

### Game modes

- Worldwide.
- Regional.
- Friends/private room.
- Tournament flow.
- Battle mode, if routed through the same server configuration.

### Lifecycle scenarios

1. Join a newly created room.
2. Join a nearly full room concurrently from multiple clients.
3. Complete at least four consecutive races.
4. Remain through results and map selection.
5. Disconnect a non-host for less than the grace period and reconnect.
6. Disconnect the host and reconnect before expiry.
7. Let the host expire during map selection and observe migration.
8. Exit voluntarily and verify immediate room release.
9. Leave a search room idle and verify janitor cleanup.
10. Restart Redis during a live room and verify the race/server continues.

### Abuse and retry scenarios

- Replay the same mutating RMC request and confirm one state change.
- Exceed auth limits from a test address and verify temporary recovery.
- Exceed expensive matchmaking limits without affecting another PID behind the same NAT.
- Send invalid dashboard query tokens and GET mutations; expect 403/405.
- Confirm dashboard and Redis ports are unreachable externally.

## Success criteria

- No duplicate participant or duplicate room from retry traffic.
- No capacity overbooking.
- Rejoining PID returns to the same room during grace.
- Stale rooms disappear within configured TTL.
- A Redis outage does not end an active room.
- Legitimate MK8D bursts do not trigger rate limits.
- No regression in average time to find a room.
- Communication-error rate is lower than or equal to the baseline.

## Evidence to collect

- Server commit/hash and configuration with secrets removed.
- Number of players, networks and NAT types.
- Room IDs, lifecycle phases and timestamps.
- Reconnect leases created/recovered/expired.
- Reservations rejected and joins committed.
- Rate-limit block counts.
- Communication-error count and phase where each occurred.
- Whether the player or server ended the session.

Do not publish raw tokens, public IP addresses or private station URLs in test reports.
