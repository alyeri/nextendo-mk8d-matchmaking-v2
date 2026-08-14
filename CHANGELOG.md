# Changelog

## Unreleased

### Changed

- Resynchronized the game server to upstream revision `5322f40196e9c59385cecf9aff6a04323b3e2e72`.
- Resynchronized the NEX core to upstream revision `967c90247487294f52541afc9c7290a5e063f768`.
- Preserved the newly published profile, friend, tournament and ranking implementations.
- Reframed the repository as incremental, cherry-pickable integration work rather than a replacement core.

### Added

- Bounded RMC mutation deduplication with completed-response caching and in-flight coalescing.
- Atomic PID-keyed seat reservations with expiration, cancellation and concurrency tests.
- Updated integration, security, configuration, testing and staging documentation.

### Removed from this integration branch

- Shared-NAT FIFO identity assignment.
- Redis coordination, rate limiting, lifecycle, reconnect and host-scoring experiments.
- Claims that the repository is ready for a wholesale production replacement.
