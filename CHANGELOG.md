# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Generated release notes for tagged versions are also published on the
[GitHub Releases](https://github.com/0xble/tailscale-ferry/releases) page.

## [Unreleased]

### Added

- Markdown previews now render `mermaid` fenced code blocks as responsive diagrams,
  with copyable source and a readable fallback when the renderer is unavailable.
- Self-contained HTML artifacts now render interactively in a script-only sandbox.
  Inline CSS and JavaScript work, while CSP blocks fetch/XHR, external runtime subresources,
  forms, nested frames, storage, parent/top navigation, and access to Ferry's
  origin. The inner frame receives a token-free URL before artifact code runs.

### Fixed

- Interactive HTML previews now hard-contain the document viewport horizontally
  and clamp embedded frames to the visible device width when iOS reports an
  oversized layout viewport, preventing clipped content and rubber-band drag
  into blank space while preserving vertical scrolling and pinch zoom.
- CLI health checks now use a daemon-provided, loopback-only probe URL instead
  of requiring the local MagicDNS name to resolve. Redirects are rejected, and
  older daemons retain the existing public-URL fallback.
- Wide Markdown tables now retain table semantics while scrolling horizontally on
  narrow screens.

## [2.1.1] - 2026-05-02

### Fixed

- PDF preview links now use the browser's native PDF viewer instead of the
  Ferry PDF.js canvas renderer, avoiding incorrect image rendering on PDFs with
  embedded color profiles.
- Public share responses now send no-cache headers, and directory PDF links use
  a fresh native-viewer cache key so stale preview HTML is not reused.

## [2.1.0] - 2026-04-29

### Added

- Markdown previews now render GitHub-style alerts for `[!NOTE]`, `[!TIP]`,
  `[!IMPORTANT]`, `[!WARNING]`, and `[!CAUTION]` blockquotes instead of showing
  the alert marker as plain quote text.

## [2.0.0] - 2026-04-28

This release moves the admin API off TCP and adds defense-in-depth on the
preview pages. Anyone hitting the loopback admin port directly will need to
switch to the CLI or the new Unix domain socket.

### Changed

- **BREAKING:** The admin API now listens on a Unix domain socket at
  `~/.local/state/ferry/admin.sock` (mode `0600` in a `0700` parent) instead
  of `127.0.0.1:39125`. The CLI auto-resolves the socket path; set
  `FERRY_ADMIN_ADDR` or pass `--admin-addr tcp:127.0.0.1:39125` to opt back
  into TCP. The `share.DefaultAdminAddr` Go constant has been removed; use
  `share.DefaultStatePaths().AdminSocket` instead.
- `pdfjs-dist` is now loaded from `cdn.jsdelivr.net` instead of `esm.sh` so
  Subresource Integrity hashes apply to the actual served bytes.
- `diff2html` is now pinned to `3.4.56` (previously floated to npm latest).

### Added

- Subresource Integrity (`integrity="sha384-..."`, `crossorigin="anonymous"`,
  `referrerpolicy="no-referrer"`) on every CDN `<script>` and `<link>` in
  preview pages. A pinned `<link rel="modulepreload">` covers the PDF.js
  module so the browser validates SRI before the inline `import` runs.
- `Content-Security-Policy` on `/s/` preview responses with `default-src
  'none'`, `frame-ancestors 'none'`, `base-uri 'none'`, `form-action 'none'`,
  and script/style/font/worker sources restricted to the known CDN hosts.
- `Content-Security-Policy: default-src 'none'; sandbox` on `/r/` raw
  responses as defense-in-depth on top of the existing
  `Content-Disposition: attachment` for HTML.
- `X-Content-Type-Options: nosniff` and `X-Frame-Options: DENY` on every
  public response.

### Security

- Admin API is no longer reachable from any browser tab on the host; the
  kernel's socket file mode is the auth, which closes CSRF and DNS-rebinding
  attacks against the admin endpoints.
- `ferryd serve --token-bytes` now rejects values below `8` instead of
  silently accepting them, so a misconfiguration surfaces as a startup error
  rather than weakening authentication.
- `ferry publish --open <host>` rejects host arguments that begin with `-`
  to block ssh option injection (e.g. `-oProxyCommand=...`).

### Fixed

- `Client.Health()` no longer routes the public-health probe through the
  admin transport when the admin client is dialing a Unix domain socket.

## [1.2.0] - 2026-04-28

### Added

- Inline SVG blocks in markdown previews now render. The sanitization policy
  allows the safe subset of SVG elements and attributes; `<script>`,
  `<foreignObject>`, and `on*` handlers remain stripped.
- Video and audio embedded in markdown previews now play inline. Markdown
  image syntax pointing at a media file (e.g. `![demo](demo.mp4)`,
  `![track](song.mp3)`) is converted to a `<video>` or `<audio>` element with
  controls, and hand-authored `<video>`/`<audio>`/`<source>`/`<track>` tags
  survive sanitization. Share-aware URL rewriting now covers media `src`
  attributes alongside `<img>` sources.

## [1.1.0] - 2026-04-24

### Added

- Preview common template files, including chezmoi-style Markdown templates
  such as `.md.tmpl`, and use the underlying extension for code highlighting.

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

[Unreleased]: https://github.com/0xble/tailscale-ferry/compare/v2.1.1...HEAD
[2.1.1]: https://github.com/0xble/tailscale-ferry/compare/v2.1.0...v2.1.1
[2.1.0]: https://github.com/0xble/tailscale-ferry/compare/v2.0.0...v2.1.0
[2.0.0]: https://github.com/0xble/tailscale-ferry/compare/v1.2.0...v2.0.0
[1.2.0]: https://github.com/0xble/tailscale-ferry/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/0xble/tailscale-ferry/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/0xble/tailscale-ferry/releases/tag/v1.0.0
