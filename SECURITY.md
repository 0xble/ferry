# Security Policy

## Reporting a vulnerability

Please report security vulnerabilities through GitHub's private vulnerability reporting:

https://github.com/0xble/ferry/security/advisories/new

Do not open a public issue for security concerns.

## Scope

ferry exposes a local daemon (`ferryd`) that serves files over your tailnet with HMAC-SHA256 token authentication. Reports of interest include:

- Authentication bypass or token forgery
- Path traversal outside a share root
- Token leakage through preview rendering or embedded content
- Privilege escalation via the admin API or spawned subprocesses
- Resource exhaustion against the public or admin listeners

## Response

We aim to acknowledge reports within 7 days and publish a GitHub Security Advisory once a fix is available.
