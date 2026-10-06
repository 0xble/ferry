# Contributing

Thanks for your interest in ferry.

## Development

Requirements:

- Go (see `go.mod` for the minimum version)
- [Tailscale](https://tailscale.com) running locally for end-to-end testing

Common tasks:

```sh
./bin/ci preflight                        # fast local feedback: format, modules, vet, build, short tests
./bin/ci gate "$(git rev-parse HEAD)"     # merge gate on a clean exact commit: bin/check, race tests, golangci-lint
./bin/ci nightly "$(git rev-parse HEAD)"  # gate plus repeated race runs and govulncheck
./bin/check     # the rendered fleet policy: gofmt, vet, golangci-lint, race tests, build, shellcheck
./bin/setup     # build ferry and ferryd into ~/.local/bin (PREFIX overrides); never restarts the daemon
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

`bin/ci`, `.github/workflows/gate.yml` and `nightly.yml` come from [toolkit](https://github.com/0xble/toolkit)'s `templates/tool`, and the workflows call its reusable `tool-gate.yml` and `tool-nightly.yml` at the toolkit tag `go.mod` requires. `bin/check` and `.golangci.yml` are rendered by the dotfiles `tools/bin/update-policy`; do not edit them by hand. Pull requests run `./bin/ci gate` on GitHub at the exact head commit, and draft pull requests skip it. The `qualification` check is the only merge requirement. A successful local `./bin/ci gate` also writes an exact-SHA receipt to `${XDG_STATE_HOME:-~/.local/state}/ci-receipts/<owner>/<repo>/<sha>.json` (override with `CI_RECEIPT_ROOT`) for local tooling. It is never written on GitHub Actions and is not a merge requirement. `./bin/ci nightly` runs on main every day at 09:17 UTC and can be dispatched manually. Releases stay tag-driven through `.github/workflows/release.yml`.

## Pull requests

- Keep changes focused and atomic
- Add tests for new behavior. Tests run every command with `HOME`, `XDG_*` and `PATH` in a temporary directory, against an in-process daemon on loopback (`internal/ferrytest`), and never reach the live daemon, launchd or Tailscale
- A change to the client's observable output must keep `internal/compat` green, the replay of every known caller against goldens recorded from the pre-toolkit binary (`internal/compat/record.sh`), or document the change in `docs/compatibility.md`
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
