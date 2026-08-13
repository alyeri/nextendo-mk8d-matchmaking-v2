# Feature status

This document distinguishes working behavior from guarded foundations.

## Active and compatible

### Signed authentication

The server verifies the HMAC and expiry of the `nx2` token and requires its PID to match the account
ID supplied by the game. Production should use `NEXTENDO_REQUIRE_SIGNED_TOKEN=1`.

### Shared-NAT login ordering

Multiple fresh LoginEx claims from one address are queued and consumed in order. Claims are short
lived and single use.

### Reservations and overbooking prevention

Pending reservations count against room capacity. A join commits only while its reservation remains
valid.

### Lifecycle and cleanup

Rooms have validated phases, activity timestamps and separate TTLs for searches versus established
sessions. A background janitor removes stale control-plane state.

### Retry deduplication

Mutating requests are cached briefly. Repeated packets do not create a second room, participant or
ownership migration.

### Intermission recovery

Disconnects in results, ready or search/map-selection phases can receive a longer grace window than
ordinary transport loss.

### Host-loss migration

After the old host's grace expires, the server selects the best live participant using observed
quality, transfers ownership if needed and updates the host connection ID.

This preserves a lobby when possible; it cannot reconstruct an already-running P2P race.

### Abuse protection

Authentication is limited by IP. Matchmaking is limited independently by IP and PID, with a stricter
bucket for expensive create/join/search calls. Blocks are temporary and progressive.

Excluded paths: P2P race packets, NAT probes, PRUDP ping/keep-alive traffic and notifications.

## Implemented but configuration-dependent

### Adaptive compatibility

Enabled when `MATCHMAKING_COMPAT_ATTRIBUTES` is greater than zero. The required prefix is reduced
one attribute per `MATCHMAKING_RELAX_AFTER_SECONDS`. At zero, behavior remains game-mode-only.

### Redis directory

Enabled only with `REDIS_URL`. It publishes expiring per-instance and per-room records. In-memory
matchmaking remains authoritative.

## Guarded or awaiting client support

### Pre-race best-host selection

The scoring and reassignment path exists behind `MATCHMAKING_PRE_RACE_HOST_SELECTION`. Default is
off because changing HostPID before the race may require a client notification sequence that has not
yet been classified for MK8D.

### Party/group matchmaking

The engine can atomically reserve seats for a PID list. Existing retail-compatible clients do not
send a party ID or roster, so the server does not infer parties from IP addresses or friend lists.

### Independent rejoin token

Current reentry is bound to the signed account PID. A separate signed, single-use rejoin token would
require an additional client/server field. No undocumented wire field is repurposed here.

### Authoritative multi-instance joins

Redis provides discovery and ownership leases, but routing a player to another instance still needs
an ingress or handoff mechanism.

## Known protocol debt

The largest remaining issue is a secure CONNECT that may arrive without a usable Kerberos ticket.
The short-lived authenticated FIFO claim is safer than a global last-PID mapping, but complete
cryptographic binding should ultimately be implemented in the ticket/CONNECT exchange.
