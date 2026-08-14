# Upstream baseline

This integration branch is based on clean snapshots of the public Nextendo mirrors. The copied
source was not taken from the earlier local experimental trees.

| Component | Repository | Revision | Upstream commit |
|---|---|---|---|
| Game server | `NextendoNetwork/mario-kart-8-deluxe` | `5322f40196e9c59385cecf9aff6a04323b3e2e72` | `Sync with the core: profiles, tournaments, rankings` |
| NEX core | `NextendoNetwork/nextendo-nex` | `967c90247487294f52541afc9c7290a5e063f768` | `Sync: player profiles, tournaments and tournament rankings` |

Both snapshots were fetched on 2026-08-13. The server module has one workspace-only adaptation:
its `go.mod` replaces the published `nextendo-nex` pseudo-version with `../nextendo-nex`, ensuring
that tests and builds exercise the exact core stored in this repository.

## Integration rules

- Preserve current player profiles, nicknames, Miis, friend lists, tournaments and rankings.
- Port one reliability feature at a time with focused tests.
- Do not infer identity from public IP addresses or connection arrival order.
- Do not restore the experimental shared-NAT FIFO identity queue.
- Require cryptographic account/PID binding before reconsidering ticketless identity consumption.
- Keep each feature reviewable as an independent commit.
