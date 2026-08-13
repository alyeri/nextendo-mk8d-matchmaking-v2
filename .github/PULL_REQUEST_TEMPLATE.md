## Summary

Describe the problem and the smallest change that solves it.

## Compatibility impact

- Wire structures changed: yes/no
- Existing clients require changes: yes/no
- Redis required for correctness: yes/no
- P2P race path affected: yes/no

## Verification

- [ ] `gofmt` clean
- [ ] `go test ./...` in both modules
- [ ] `go vet ./...` in both modules
- [ ] New behavior has focused tests
- [ ] No credentials, tokens, IP captures, saves, keys or game assets included

## Staging and rollback

Explain how to test this with real clients and how to disable or roll it back safely.
