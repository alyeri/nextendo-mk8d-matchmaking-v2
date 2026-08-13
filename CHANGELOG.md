# Changelog

## Unreleased

### Added

- Signed `nx2` account binding from LoginEx BAAS extra data.
- FIFO shared-NAT identity claims for ticketless secure CONNECT compatibility.
- Atomic join and party-seat reservations.
- Server-owned room phases and epochs.
- Adaptive attribute-prefix compatibility.
- NAT/direct-readiness host scoring.
- Reconnection leases and host connection restoration.
- Host-loss migration to the best observed live participant.
- Mutating RMC response deduplication.
- Intermission-specific disconnect grace.
- Stale-room janitor with phase-sensitive TTLs.
- Conservative IP/PID rate limiting.
- Redis presence, room snapshots, global room index and ownership leases.
- Private Bearer-authenticated dashboard and POST-only eviction endpoint.
- Docker, CI, architecture, testing, security and proposal documentation.

### Guarded

- Pre-race host replacement is disabled by default.
- Party reservation awaits a client-provided party identifier.

### Known limitations

- Secure CONNECT may still require temporal PID binding when no usable ticket is presented.
- Redis discovery does not yet provide authoritative cross-instance join routing.
