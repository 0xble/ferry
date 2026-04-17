# Contributing

Thanks for your interest in ferry.

## Development

Requirements:

- Go (see `go.mod` for the minimum version)
- [Tailscale](https://tailscale.com) running locally for end-to-end testing

Common tasks:

```sh
bin/check       # canonical repo gate (vet + test + build)
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

## Pull requests

- Keep changes focused and atomic
- Add tests for new behavior
- Run `make test` and `make lint` before submitting
- Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (`feat:`, `fix:`, `refactor:`, etc.)

## Reporting bugs

Open an issue at https://github.com/0xble/ferry/issues with:

- What you expected vs what happened
- Steps to reproduce
- OS, Go version, and Tailscale version
- Relevant log output from `~/.local/state/ferry/logs/ferryd.log`

## Security issues

See [SECURITY.md](SECURITY.md).
