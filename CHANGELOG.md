# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Generated release notes for tagged versions are also published on the
[GitHub Releases](https://github.com/0xble/ferry/releases) page.

## [Unreleased]

## [1.0.0]

Initial public release.

### Added

- `ferry publish <path>` publishes a file or directory as a tailnet share with
  a preview-rich URL.
- Live and snapshot modes (`--snapshot` for point-in-time copies).
- Rich preview rendering: markdown (GFM), syntax-highlighted code, CSV, PDF,
  images, audio, and video.
- Directory listing with breadcrumb navigation.
- `ferry list`, `ferry get <id>`, `ferry unshare <target>`, `ferry renew <id>`,
  `ferry doctor` for share lifecycle management.
- HMAC-SHA256 per-share tokens with configurable truncation length
  (`ferryd serve --token-bytes`).
- Automatic share expiry and garbage collection (default lifetime 7 days,
  configurable with `ferry publish --expires-in`).
- Optional failed-auth rate limiter on the public listener.
- `ferryd serve --external-url` for publishing share links behind a custom
  domain (e.g. Tailscale Serve + Funnel with a tailnet cert).
- `ferry publish --open <host>` opens the share URL on a remote machine over
  SSH.
- Opt-in launchd integration on macOS via the `FERRY_LAUNCH_AGENT_LABEL`
  environment variable; the default behavior is to spawn `ferryd` directly.
- `ferry --version` and `ferryd --version` report the embedded build version.

### Security

- HMAC secret persisted at `~/.local/state/ferry/secret` with `0600` mode and a
  private state directory (`0700`).
- Share roots resolved with symlink-aware path escaping checks to prevent
  traversal outside the share root.
- Admin API listens on loopback only (`127.0.0.1:39125`); public listener binds
  to the tailnet interface.

[Unreleased]: https://github.com/0xble/ferry/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/0xble/ferry/releases/tag/v1.0.0
