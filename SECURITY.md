# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities through GitHub's private vulnerability reporting:

https://github.com/0xble/tailscale-ferry/security/advisories/new

Do not open a public issue for security concerns.

## Scope

ferry exposes a local daemon (`ferryd`) that serves files over your tailnet with HMAC-SHA256 token authentication. Reports of interest include:

- Authentication bypass or token forgery
- Path traversal outside a share root
- Token leakage through preview rendering or embedded content
- Privilege escalation via the admin API or spawned subprocesses
- Resource exhaustion against the public or admin listeners

## HTML artifact isolation

HTML previews run in an iframe and response-level CSP sandbox that allow inline
scripts but deliberately omit `allow-same-origin`. The artifact receives an
opaque origin and cannot access Ferry storage, its parent preview, or another
share. Fetch/XHR, external runtime subresources, forms, nested frames, plugins, and
parent/top navigation are blocked. An artifact can navigate its own sandboxed
frame, so Ferry strips the capability token into an HttpOnly, path-scoped cookie
before artifact code runs and suppresses referrers. The raw `/r/` route remains
download-only.

## Response

We aim to acknowledge reports within 7 days and publish a GitHub Security Advisory once a fix is available.
