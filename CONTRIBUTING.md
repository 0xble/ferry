# Contributing

Thanks for your interest in ferry.

## Development

Requirements:

- Go (see `go.mod` for the minimum version)
- [Tailscale](https://tailscale.com) running locally for end-to-end testing

Common tasks:

```sh
./bin/ci preflight                        # fast local feedback: whitespace, gofmt, vet, build
./bin/ci gate "$(git rev-parse HEAD)"     # merge gate on a clean exact commit: gofmt, vet, tests, build, race
./bin/ci nightly "$(git rev-parse HEAD)"  # gate plus uncached shuffled race tests and release cross-builds
bin/doctor      # machine and runtime diagnostics (never run from hooks)

make build      # build ferry + ferryd into ./bin
make test       # go test ./...
make lint       # go vet ./...
make install    # go install both binaries
```

On first clone, wire the committed git hooks:

```sh
git config --local core.hooksPath git-hooks
```

First inspect `git config --show-scope --get-all core.hooksPath`. If another hook manager is already configured, have it dispatch `git-hooks/pre-push` rather than overwriting it. The pre-push hook runs `./bin/ci preflight` and is bypassable feedback only.

## CI

Pull requests run `./bin/ci gate` on GitHub at the exact head commit. The `qualification` check is the only merge requirement. A successful local `./bin/ci gate` also writes an exact-SHA receipt to `${XDG_STATE_HOME:-~/.local/state}/ci-receipts/<owner>/<repo>/<sha>.json` (override with `CI_RECEIPT_ROOT`) for local tooling. It is never written on GitHub Actions and is not a merge requirement. `./bin/ci nightly` runs on main every day at 06:41 UTC and can be dispatched manually. Releases stay tag-driven through `.github/workflows/release.yml`.

## Pull requests

- Keep changes focused and atomic
- Add tests for new behavior
- Run `./bin/ci gate "$(git rev-parse HEAD)"` on a clean commit before submitting
- Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (`feat:`, `fix:`, `refactor:`, etc.)

## Reporting bugs

Open an issue at https://github.com/0xble/tailscale-ferry/issues with:

- What you expected vs what happened
- Steps to reproduce
- OS, Go version, and Tailscale version
- Relevant log output from `~/.local/state/ferry/logs/ferryd.log`

## Security issues

See [SECURITY.md](SECURITY.md).
