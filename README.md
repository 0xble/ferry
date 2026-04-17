# tailscale-ferry

[![CI](https://github.com/0xble/tailscale-ferry/actions/workflows/ci.yml/badge.svg)](https://github.com/0xble/tailscale-ferry/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/0xble/tailscale-ferry?include_prereleases&sort=semver)](https://github.com/0xble/tailscale-ferry/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/0xble/ferry.svg)](https://pkg.go.dev/github.com/0xble/ferry)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

Tailscale-first file sharing CLI. Publish files and directories as
preview-rich URLs scoped to your tailnet, authenticated with per-share
tokens.

![Markdown article rendered with frontmatter, tags, and metadata](docs/assets/hero-markdown.png)

## Previews

| | |
|---|---|
| [![Code preview with syntax highlighting](docs/assets/preview-code.png)](docs/assets/preview-code.png) | [![CSV rendered as a sortable table](docs/assets/preview-csv.png)](docs/assets/preview-csv.png) |
| [![PDF rendered with pdf.js](docs/assets/preview-pdf.png)](docs/assets/preview-pdf.png) | [![Directory listing with breadcrumbs](docs/assets/preview-directory.png)](docs/assets/preview-directory.png) |

## Use it to

- Share a draft markdown with your team. It opens as a polished article,
  not a download.
- Preview a PDF on your phone without an iCloud handoff. Tap, scroll, done.
- Post a CSV link in chat that opens sortable in the browser. No
  spreadsheet app required.

## Features

- Share files and directories as live or snapshot links
- Rich previews: markdown (GFM), code (highlight.js), CSV (Tabulator),
  PDF (pdf.js), images, audio, video
- HMAC-SHA256 token auth per share
- Directory listing with breadcrumb navigation
- Automatic share expiry and garbage collection
- Snapshot mode for point-in-time copies

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

## Requirements

- [Tailscale](https://tailscale.com)

## Install

```sh
go install github.com/0xble/ferry/cmd/ferry@latest
go install github.com/0xble/ferry/cmd/ferryd@latest
```

## How it compares

| Tool                    | Tailnet-scoped | Rich previews | Per-share token | Snapshot | Auto expiry |
|-------------------------|----------------|---------------|-----------------|----------|-------------|
| ferry                   | yes            | yes           | yes             | yes      | yes         |
| Taildrop                | yes            | no            | identity        | no       | no          |
| Tailscale Serve         | optional       | no            | optional        | no       | no          |
| caddy file-server       | no             | no            | no              | no       | no          |
| python -m http.server   | no             | no            | no              | no       | no          |

## Architecture

`ferry` is the client CLI. `ferryd` is the background daemon that runs
three HTTP listeners.

![Architecture: ferry CLI talks to ferryd over the loopback admin API; tailnet peers hit the public listener](docs/assets/architecture.svg)

- **Public** (tailnet IP, port 39124): preview and raw file endpoints
- **Loopback** (127.0.0.1, port 39124): same as public, for local access
- **Admin** (127.0.0.1, port 39125): share CRUD API used by the `ferry`
  client

State is stored in `~/.local/state/ferry/` with the HMAC secret in a
0600-mode file under a 0700-mode directory.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

See [SECURITY.md](SECURITY.md). Report vulnerabilities privately through
GitHub Security Advisories.

## License

MIT
