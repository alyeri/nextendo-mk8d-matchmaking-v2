# nextendo-mk8d-matchmaking-v2

Incremental matchmaking reliability work for Mario Kart 8 Deluxe on Nextendo Network.

This repository is no longer a replacement core. It tracks the current public Nextendo mirrors and
ports one narrowly scoped change at a time, with tests, so existing profiles, nicknames, Miis,
friends, tournaments and rankings remain intact.

> **Status: integration branch / review candidate.** The current scope is RMC mutation
> deduplication followed by atomic expiring seat reservations. It is not a production replacement
> and still needs a 6–12 player, multi-network soak test.

## Current upstream baseline

| Component | Revision |
|---|---|
| `NextendoNetwork/mario-kart-8-deluxe` | `5322f40196e9c59385cecf9aff6a04323b3e2e72` |
| `NextendoNetwork/nextendo-nex` | `967c90247487294f52541afc9c7290a5e063f768` |

See [Upstream baseline](docs/UPSTREAM_BASELINE.md) for the exact source and integration rules.

## Included changes

### 1. RMC mutation deduplication

Selected mutating matchmaking calls are keyed by connection ID, authenticated PID, protocol,
method, call ID and SHA-256 request-body hash.

- Completed retries receive a cloned cached response.
- Concurrent duplicates wait for the first execution instead of repeating the mutation.
- Nil responses are not cached.
- Entries expire and the cache has a configurable upper bound.
- Read-only/asynchronous calls such as `GetSessionURLs` are not deduplicated.

This prevents retransmitted `CreateMatchmakeSession` and related mutations from creating duplicate
rooms or applying the same state transition twice.

### 2. Atomic expiring seat reservations

A pending join reserves capacity before it becomes a participant.

- `participants + live reservations` is the effective capacity.
- Reservations are keyed by PID, expire automatically and can be cancelled idempotently.
- A committed reservation is removed before the PID is appended to participants.
- Pending reservations are not exposed as `NumParticipants`.
- Concurrent reservation tests require exactly one winner for one remaining seat.

## Explicitly excluded

The former shared-NAT FIFO identity queue is not present. A public IP address or connection arrival
order is not identity proof and is unsafe under CGNAT. Ticketless identity consumption must not be
reintroduced unless the secure connection carries proof cryptographically bound to the same
account/PID that deposited the signed `nx2` claim. If sufficient proof is unavailable, the safe
behavior is refusal or a client/protocol change—not guessing.

The previous experimental Redis directory, rate limiter, room lifecycle, reconnect lease and host
scoring are also absent from this integration branch. They may be reconsidered independently only
after coordination with Nextendo maintainers.

## Repository layout

```text
.
├── server/          Current MK8D game server plus the dedup wrapper/configuration
├── nextendo-nex/    Current public NEX core plus atomic reservation primitives
├── docs/            Baseline, design, feature status, testing and rollout notes
├── Dockerfile       Reproducible Linux container build
├── compose.yaml     Minimal staging deployment; no Redis or public dashboard
└── go.work          Two-module Go workspace
```

No game files, firmware, title keys, saves, user tokens, private keys, traffic captures or client
builds are included.

## Development

Requirements: Go 1.23 or newer.

```sh
git clone https://github.com/alyeri/nextendo-mk8d-matchmaking-v2.git
cd nextendo-mk8d-matchmaking-v2

cd nextendo-nex && go test -count=1 ./... && go vet ./...
cd ../server && go test -count=1 ./... && go vet ./...
```

Run the race detector on Linux:

```sh
cd nextendo-nex && go test -race -count=1 ./...
cd ../server && go test -race -count=1 ./...
```

## Review strategy

The commit history is intentionally reviewable:

1. Synchronize the current public mirrors.
2. Add RMC deduplication and focused tests.
3. Add atomic reservations and focused tests.
4. Keep later proposals in separate branches/commits.

The preferred upstream path is to cherry-pick or reimplement each accepted commit in the official
repositories rather than replacing the complete core.

## Documentation

- [Upstream baseline](docs/UPSTREAM_BASELINE.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Feature status](docs/FEATURE_STATUS.md)
- [Testing and soak plan](docs/TESTING.md)
- [Configuration](docs/CONFIGURATION.md)
- [Deployment](docs/DEPLOYMENT.md)
- [Integration proposal](docs/PROPOSAL.md)
- [Security](SECURITY.md)

## License and attribution

The source components retain the **PolyForm Shield License 1.0.0** supplied by the Nextendo
projects. See [LICENSE.md](LICENSE.md) and [NOTICE.md](NOTICE.md).

This is an independent community contribution and is not affiliated with or endorsed by Nintendo.
