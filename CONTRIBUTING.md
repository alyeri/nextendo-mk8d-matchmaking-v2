# Contributing

## Principles

- Preserve wire compatibility unless a change is explicitly versioned.
- Do not guess undocumented protocol meanings when an unknown state can be represented honestly.
- Add a focused test for every lifecycle, security or serialization change.
- Keep P2P race traffic out of Redis and control-plane middleware.
- Never commit credentials, user tokens, IP captures, saves, keys or copyrighted game assets.

## Development workflow

1. Create a focused branch.
2. Format modified Go files with `gofmt`.
3. Run tests and vet in both modules.
4. Update documentation and `CHANGELOG.md` for observable behavior.
5. Explain compatibility risk and rollback behavior in the pull request.

## Pull request checklist

- [ ] The change is scoped to one reviewable concern.
- [ ] Existing wire formats remain unchanged or the versioning plan is documented.
- [ ] Unit tests cover success, rejection and expiry/retry behavior.
- [ ] Secrets and personal network data are absent.
- [ ] New environment variables are documented in `server/example.env`.
- [ ] Redis failure behavior is defined.
- [ ] Production defaults are conservative.
- [ ] The proposal includes a staging test and rollback plan.
