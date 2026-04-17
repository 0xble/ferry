# ferry

[![CI](https://github.com/0xble/ferry/actions/workflows/ci.yml/badge.svg)](https://github.com/0xble/ferry/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/0xble/ferry?include_prereleases&sort=semver)](https://github.com/0xble/ferry/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/0xble/ferry.svg)](https://pkg.go.dev/github.com/0xble/ferry)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Tailscale-first file sharing CLI. Share files and directories with anyone on
your tailnet through preview-rich URLs with token-based access control.

## Features

- Share files and directories as live or snapshot links
- Rich previews: markdown (GFM), code (highlight.js), CSV (Tabulator),
  PDF (pdf.js), images, audio, video
- HMAC-SHA256 token auth per share
- Directory listing with breadcrumb navigation
- Automatic share expiry and garbage collection
- Snapshot mode for point-in-time copies

## Requirements

- [Tailscale](https://tailscale.com) running on the host machine

## Install

```sh
go install github.com/0xble/ferry/cmd/ferry@latest
go install github.com/0xble/ferry/cmd/ferryd@latest
```

## Usage

```sh
# Publish a file (starts daemon automatically)
ferry publish ~/Desktop/report.pdf

# Publish a directory
ferry publish ~/Projects/docs/

# Snapshot mode (point-in-time copy)
ferry publish --snapshot ~/Desktop/draft.md

# Custom expiry (default: 7d)
ferry publish --expires-in 24h ~/Desktop/notes.txt

# List active shares
ferry list

# Get a share by id
ferry get <id>

# Extend share expiry
ferry renew <id>

# Revoke a share by id or exact path
ferry unshare <target>

# Check daemon and Tailscale health
ferry doctor
```

## Architecture

`ferry` is the client CLI. `ferryd` is the background daemon that runs three
HTTP servers:

- **Public** (tailnet IP): serves preview and raw file endpoints
- **Loopback** (127.0.0.1): same as public, for local access
- **Admin** (127.0.0.1): share CRUD API used by the `ferry` client

State is stored in `~/.local/state/ferry/`.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

See [SECURITY.md](SECURITY.md). Report vulnerabilities privately through
GitHub Security Advisories.

## License

MIT
