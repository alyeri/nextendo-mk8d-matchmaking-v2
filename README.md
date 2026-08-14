# nextendo-mk8d-matchmaking-v2

Experimental, compatibility-preserving matchmaking and session-resilience work for Mario Kart 8
Deluxe on Nextendo Network.

This repository packages the modified MK8D game server and its matching `nextendo-nex` protocol
core in one reproducible Go workspace. The project focuses on control-plane reliability:
authentication, room selection, lifecycle management, reconnection, NAT-aware host choice, abuse
protection and operational visibility. Race-state traffic remains peer-to-peer.

> **Project status: experimental beta / proposal.** The server is running successfully in a small
> test environment, but it has not yet completed a public 6–12 player, multi-network soak test.
> It should be evaluated on a staging endpoint before any production rollout.

## Why this exists

During real MK8D tests, low-resource clients could survive frame drops after client-side continuity
work, yet the surrounding online session still had failure modes:

- repeated RMC calls could create duplicate or inconsistent control-plane state;
- stale hosts and abandoned rooms could remain eligible for matchmaking;
- short disconnects around results or map selection could destroy a usable room;
- multiple clients behind one NAT could inherit the wrong ticketless secure identity;
- monitoring and administrative endpoints needed safer defaults;
- there was no expiring, multi-instance-ready index of live rooms.

This branch addresses those issues without moving race packets through Redis or the VPS.

## Implemented capabilities

| Area | Status | Notes |
|---|---|---|
| Signed account binding | Active | Validates the `nx2` proof embedded in the BAAS `nnex` claim and binds it to the login PID. |
| Shared-NAT identity queue | Active | Fresh ticketless secure connections consume authenticated PID claims in FIFO order. |
| Atomic join reservations | Active | Capacity includes pending reservations; overbooking is rejected. |
| Adaptive matching | Implemented | Attribute compatibility can relax over time; neutral while `MATCHMAKING_COMPAT_ATTRIBUTES=0`. |
| Room lifecycle | Active | Validated searching, forming, ready, racing, results and closing transitions. |
| RMC deduplication | Active | Mutating retries reuse a cached response for a short window. |
| Reconnection lease | Active | Authenticated PID reentry restores the connection ID and participation notifications. |
| Intermission grace | Active | Results/map-selection disconnects receive a configurable recovery window. |
| Host scoring | Partially active | NAT/direct-readiness score chooses a replacement after host loss. Pre-race switching is guarded off. |
| Stale-room janitor | Active | Short TTL for idle searches and a longer TTL for established sessions. |
| Herd protection | Active | Existing-room reuse, reservations and rate limits reduce simultaneous room creation. |
| Party reservation | Server foundation | Atomic multi-seat API exists; current clients do not transmit a party identifier. |
| Redis room directory | Active when Redis is configured | Per-instance snapshots, per-room TTL leases and a global sorted index. Redis is not race-authoritative. |
| Private admin API | Active | Loopback bind, Bearer authentication and POST-only mutations. |
| Rate limiting | Active | Conservative IP/PID control-plane limits; P2P, NAT and keep-alives are excluded. |

See [Feature status](docs/FEATURE_STATUS.md) for exact boundaries and known limitations.

## Repository layout

```text
.
├── server/                 MK8D auth, secure server, matchmaking policy, Redis and dashboard
├── nextendo-nex/           Modified PRUDP/RMC/NEX protocol implementation
├── docs/                   Architecture, security, testing, deployment and proposal notes
├── Dockerfile              Reproducible production container image
├── compose.yaml            Staging stack with private Redis and admin API
├── .github/workflows/      Continuous integration
└── go.work                 Reproducible two-module Go workspace
```

No game files, Nintendo code, firmware, title keys, saves, user tokens, TLS private keys or client
builds are included.

## Quick start for development

Requirements: Go 1.23 or newer.

```sh
git clone https://github.com/alyeri/nextendo-mk8d-matchmaking-v2.git
cd nextendo-mk8d-matchmaking-v2

cd nextendo-nex && go test ./...
cd ../server && go test ./...
```

To run an isolated local server, copy the example configuration and use development-only secrets:

```sh
cp server/example.env .env
# Edit .env. Never commit it.
set -a; . ./.env; set +a
cd server && go run .
```

For a Docker-based staging environment, see [Deployment](docs/DEPLOYMENT.md).

## Safe rollout recommendation

1. Deploy to a separate staging hostname/IP.
2. Keep `MATCHMAKING_PRE_RACE_HOST_SELECTION=0`.
3. Start with one server instance and optional Redis.
4. Test two clients behind one NAT, then clients on separate networks.
5. Run concurrent Worldwide, Regional and private rooms.
6. Test results/map-selection recovery and deliberate host departure.
7. Compare error rates and room cleanup metrics with the current server.
8. Only enable individual features in production after evidence from that stage.

## Validation performed

- Both Go module test suites pass.
- The suites have been repeated five times to catch timing-sensitive failures.
- `go vet ./...` passes for both modules.
- Linux `amd64` builds successfully with `CGO_ENABLED=0`.
- Redis failure is non-fatal unless explicitly configured as required.
- The administrative API has been verified on loopback with Bearer auth and POST-only eviction.
- Small real-world MK8D tests have completed multiple races, including low-resource Ryujinx clients.

The Go race detector has **not** been counted as passed in the original Windows development
environment because no C compiler was installed there. CI includes a Linux race-detector job.

## Important limitations

- MK8D's secure CONNECT can still arrive without a usable Kerberos ticket. The server binds it to a
  short-lived authenticated FIFO claim; full cryptographic CONNECT binding remains future work.
- Server-side ownership migration cannot guarantee continuation of a race whose P2P host vanished.
- Pre-race host replacement is disabled until the client notification sequence is classified.
- Party reservations cannot become end-to-end until a client sends a stable party identifier.
- The Redis global index is coordination infrastructure, not cross-instance authoritative joining.
- P2P participants necessarily learn one another's network endpoints.

## Documentation

- [Architecture](docs/ARCHITECTURE.md)
- [Upstream baseline](docs/UPSTREAM_BASELINE.md)
- [Feature status](docs/FEATURE_STATUS.md)
- [Security model](SECURITY.md)
- [Configuration reference](docs/CONFIGURATION.md)
- [Testing plan](docs/TESTING.md)
- [Deployment](docs/DEPLOYMENT.md)
- [Proposal and integration strategy](docs/PROPOSAL.md)
- [Contributing](CONTRIBUTING.md)

## License and attribution

The source components retain the **PolyForm Shield License 1.0.0** supplied by the Nextendo
projects. See [LICENSE.md](LICENSE.md) and [NOTICE.md](NOTICE.md).

This is an independent community experiment and is not affiliated with, endorsed by or associated
with Nintendo. “Nextendo” is used to identify the community network this proposal targets; this
repository does not claim to be an official Nextendo release.
