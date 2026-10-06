# Compatibility Inventory

The observable contract of the `ferry` client before the toolkit rewrite, and
where each part lives afterwards. The old side was recorded on 2026-10-06 from
`origin/main` at `6267d4d` (`v2.1.1-31-g6267d4d`), built with `GOWORK=off`, by
reading every file under `cmd/`, `internal/` and `share/`, running `--help` on
every command, and probing the error paths offline with a sandboxed `HOME`, a
`PATH` without `ferryd` or `tailscale`, and no `FERRY_*` variables.

Only the `ferry` client moves to the toolkit. `ferryd`, the daemon run by
launchd, keeps its kong command line, flags, config file, listeners, share
URLs, token scheme, admin API and on-disk state byte for byte. Its one change is
an additive `share.Daemon.Handlers` method that returns the admin and public
handlers without binding listeners, so tests can host a real daemon on random
loopback ports (see [ferryd](#ferryd)).

`internal/compat` replays every caller invocation below against a real
in-process daemon on random loopback ports and compares the result with golden
output recorded from the old binary (see
[Caller Compatibility Test](#caller-compatibility-test)).

## Root Flags

| Flag | Old | New |
| --- | --- | --- |
| `-j, --json` | JSON output | toolkit builtin, same spelling |
| `-V, --version` | prints the version, exit 0 | `--version` is the toolkit builtin. `-V` is kept as a hidden root flag that prints the same string |
| `-h, --help` | kong help | kong help |
| `--agent`, `--fields`, `-y, --yes` | none | toolkit builtins, additive |

Root flags are accepted before or after the command, as before. The show-me
skill puts `--json` before `--`: `ferry get --json -- <id>`.

## Implicit Publish

`ferry <path>` runs `ferry publish <path>`. Before kong parses, the old binary
inserted `publish` before the first argument that was not a flag or a known
command word (`publish list get unshare renew doctor help`). It did not check
the argument, so `ferry no-such-thing` tried to publish a missing path (exit 1)
after starting the daemon.

New: the same rewrite in `cmd/ferry`, with two differences. The known words
also include toolkit's `serve`, `mcp` and `metadata`, and the argument must name
an existing file or directory. Anything else is left to the parser, so an
unknown word is a usage error (exit 2) and never starts the daemon. See C6.

## Environment

| Variable | Effect |
| --- | --- |
| `FERRY_ADMIN_ADDR` | admin API address, trimmed. A path or `unix:PATH` dials a Unix socket, `tcp:HOST:PORT` or `HOST:PORT` dials TCP. Empty means `$HOME/.local/state/ferry/admin.sock` |
| `HOME` | state directory `$HOME/.local/state/ferry` (admin socket, daemon log) and the last `ferryd` lookup, `$HOME/.local/bin/ferryd` |
| `FERRY_LAUNCH_AGENT_LABEL` | on macOS, a down daemon is first restarted with `launchctl kickstart -k gui/<uid>/<label>` |
| `PATH` | finds `ferryd`, `tailscale` (doctor) and `ssh` (`--open`) |

Unchanged in the rewrite. The launchd unit sets `FERRY_LAUNCH_AGENT_LABEL` only
in `ferryd`'s own environment, so a client run from a shell or cron does not
kickstart unless the caller exports it.

## Daemon Start

`publish`, `list`, `get`, `unshare` and `renew` first make sure the daemon is
healthy: `GET /admin/health` on the admin API, then `GET <health_base_url>/healthz`
on the loopback listener, with redirects refused. When that fails they kickstart
the launch agent (above) and poll 10 times 150 ms, then start `ferryd serve`
detached (`setsid`, stdin `/dev/null`, output appended to
`$HOME/.local/state/ferry/logs/ferryd.log`, mode `0600`) from the first of
`PATH`, the client's own directory and `$HOME/.local/bin`, and poll 30 times
150 ms. Failure is `ferryd daemon binary not found in PATH` or
`daemon did not become healthy`, exit 1. `doctor` never starts the daemon.

New: unchanged for every applied call on every surface, moved to
`internal/daemon`. A preview (`--dry-run`, or HTTP and MCP without `apply`)
never starts the daemon: it only reads from one that is already healthy.

## Exit Codes

| Code | Old meaning | New |
| --- | --- | --- |
| 0 | success, help, `--version`, and `doctor --json` even when a check fails | success, help, `--version`. `doctor --json` exits 1 when a check fails (C3) |
| 1 | daemon unavailable, any daemon API error (including `not_found: share not found`), a failed `stat` of the publish path, a failed `ssh`, `doctor` failure (`health_check_failed`) | daemon unavailable, daemon errors other than 400 and 404, `ssh` failure, `doctor` failure |
| 2 | `invalid_args`: `--expires-in` or `--for` not above zero, empty path or target, Markdown assets outside the directory, `--open` host starting with `-` | unchanged, plus every parse error (was 80), invalid durations (was 80), and daemon 400 errors such as an oversized snapshot (was 1). See C4, C5 |
| 3 | `not_found`: `unshare` matched no active share | unchanged, plus daemon 404s (`get`, `renew`, `unshare` by a missing ID is still a path lookup first) and a missing publish path (were 1). See C5 |
| 80 | kong parse errors: missing argument, unknown flag or command, invalid duration | moves to 2 (C4) |

No caller found branches on an exit number other than zero versus non-zero.

## Error Output

Old: an `invalid_args`, `not_found` or `health_check_failed` error printed
`{"error":{"code","message","exit_code"}}` on stderr when stdout was not a
terminal or `--json` was set, and `error: <message>` on a terminal. Every other
error printed its raw text on stderr (for example `not_found: share not found`
or `ferryd daemon binary not found in PATH`). kong parse errors printed the
command's usage and `ferry: error: ...`.

New: every error is one toolkit envelope, `{"error":{"code","message","exit_code"}}`,
under `--json` or `--agent`, and `error: <message>` otherwise, whether or not
stdout is a terminal (C2).

## Commands

| Command | Positional | Own flags (default) | Admin API | JSON shape | New |
| --- | --- | --- | --- | --- | --- |
| `publish` | `<path>` | `--snapshot`, `--expires-in` (`168h`), `--open HOST` | `GET /admin/shares` (live mode), then `POST /admin/shares/{id}/renew` for an active live share of the same path, else `POST /admin/share` | share | op `share.publish`, write, CLI-immediate, `path` and `open` CLI-only |
| `list` | | | `GET /admin/shares` | `[share]`, `[]` when empty | op `shares.list`, read, MCP |
| `get` | `<id>` | | `GET /admin/shares/{id}` | share | op `share.get`, read, MCP |
| `unshare` | `<target>` | | `DELETE /admin/shares/{target}` when the target has no `/`, then on `not_found` `GET /admin/shares` and `DELETE` each active share whose path equals the absolute target | `{id,ok}` or `{ok,path,revoked}` | op `share.unshare`, write, CLI-immediate |
| `renew` | `<id>` | `--for` (`168h`) | `POST /admin/shares/{id}/renew` | share | op `share.renew`, write, CLI-immediate |
| `doctor` | | | `tailscale ip -4`, `tailscale status --json`, admin health | `{daemon_error?,daemon_ok,tailscale_error?,tailscale_ok}` | op `doctor`, read, MCP |

A share is `{id,path,is_dir,mode,url,created_at,expires_at,revoked}`, with RFC
3339 UTC times. The JSON shapes are unchanged in the rewrite, key order
included.

Human output, unchanged:

- A share prints `id`, `kind` (`file` or `directory`), `mode`, `path`, an
  optional `bundle_root`, `created` and `expires` in local RFC 3339, and `url`,
  one `key: value` per line. `publish` prints the requested path, and
  `bundle_root` when a Markdown file was published as its directory.
- `list` separates shares with a blank line, or prints `No active shares`.
- `unshare` prints `revoked share: <id>` or `revoked <n> share(s) for <path>`.
- `doctor` prints `tailscale: ok` or `tailscale: error (<detail>)`, then the
  same for `daemon`.

## Publish Rules

1. The path is made absolute against the working directory and must exist.
2. A Markdown file (`.md`, `.markdown`, `.mdown`, `.mkdn`, `.mkd`, and their
   template forms such as `plan.md.tmpl`) whose
   local links or images point inside its directory is published as that
   directory, and the URL gains the file name: `/s/<id>/<file>?t=<token>`. One
   that points outside its directory is refused (`invalid_args`, exit 2).
3. Live mode reuses an active live share of the same path: it renews it for
   `--expires-in` and prints it, rather than creating a second share.
4. `--open HOST` runs `ssh HOST open <url>` after printing, and refuses a host
   that starts with `-`. With `--json` the old binary returned before opening,
   so `--json --open` never opened (C7).

## Safety Gates

| Gate | Old | New |
| --- | --- | --- |
| Tailnet-only serving, per-share HMAC tokens, admin API on a `0600` Unix socket in a `0700` directory | `ferryd` | unchanged, `ferryd` and `share` are not rewritten |
| Markdown bundle may not escape its directory | `invalid_args`, exit 2 | unchanged |
| `--open` host may not start with `-` | `invalid_args`, exit 2 | unchanged |
| `unshare` revoke failure is not reported as not found | kept the daemon error | unchanged (ported test) |
| Writes on served surfaces | no server existed | `serve` refuses applied writes by default (`op.DenyWrites`). `path` and `open` are refused off the CLI (`cli_only`), so a served caller can neither publish a host path nor run `ssh` |
| Previews | none | `--dry-run` on `publish`, `renew` and `unshare` reads the plan and the daemon's current state, writes nothing and never starts the daemon |

## ferryd

Unchanged: `ferryd serve` (the default command) with `--admin-addr`,
`--public-port` (`39124`), `--token-bytes` (`8`), `--external-url`,
`--snapshot-max-bytes` (`1 GiB`) and `--state-dir`, read from flags or
`$XDG_CONFIG_HOME/ferry/config.toml` (top level or a `[serve]` table), `-V`,
`--version`. It binds the Tailscale IPv4 and `127.0.0.1` on the public port and
the admin socket, and keeps `shares.db`, `secret`, `snapshots/` and `logs/`
under `~/.local/state/ferry`.

toolkit's own `serve --socket` is a `ferry` command, the operation server, and
has nothing to do with `ferryd serve`, the share server. They are different
binaries, so the names do not collide.

Outside readers of the daemon's state, which therefore must not change:

- dotfiles `maintain-macos-storage` reads `~/.local/state/ferry/shares.db`
  read-only (`SELECT id FROM shares WHERE revoked_at IS NULL AND expires_at > ?`)
  and lists `~/.local/state/ferry/snapshots/`.
- dotfiles `healthchecks-heartbeat` checks TCP `127.0.0.1:39124`, and
  `com.brianle.tailscale-serve-health` probes `http://127.0.0.1:39124/` for any
  HTTP reply.

## Callers

| Caller | Invocation | Relies on |
| --- | --- | --- |
| show-me skill `references/ferry.md` | `ferry doctor` | text form, non-zero exit on failure |
| same | `ferry publish --json "/absolute/path"` | share JSON: `id`, `path`, `mode`, `url`, `expires_at` |
| same | `ferry publish --snapshot --json "/absolute/path"` | snapshot mode |
| same | `ferry publish --expires-in=<duration> ...` | `--expires-in` spelling |
| same | `ferry list --json` | array of shares, `path`, `mode`, `expires_at`, `url` |
| same | `ferry get --json -- <id>` | `--` before an ID that may start with `-` |
| same | `ferry renew --for=168h --json -- <id>` | `--for` spelling |
| same | `ferry unshare --json -- <id>`, `ferry unshare --json -- "/exact/source/path"` | ID or path target |
| show-me `SKILL.md`, `references/native.md` | prose only, routes to `references/ferry.md` | |
| Hermes cron `LPG Weekly Business Review` | `ferry publish <doc> --json`, `ferry publish <OUT>/index.html --json`, `ferry publish <OUT>/weekly-business-review-<R>.pdf --json` | flags after the path, `url` (PDF links carry `&pv=native`) |
| Hermes cron `maintain-macos-storage`, dotfiles `maintain-macos-storage` | reads `shares.db` and `snapshots/` | `ferryd` state format |
| dotfiles `healthchecks-heartbeat`, `tailscale-serve-health` | port `39124` | `ferryd` listeners |
| agents evals (`evals/tools/ferry`, four `solution.sh`) | a fake `ferry` script, `ferry publish --json <path>` | not the real binary |

Searched, nothing found: `~/Repos/scripts/src` (only the `ferry-tunnel`
resource name in `accounts/accounts.toml`), `~/Repos/brianle/apps`,
`~/Repos/lpg`. No caller parses `--help`, a `Usage:` line, the version string or
error text, and none branches on a specific exit number.

## Caller Compatibility Test

See `internal/compat`. Filled in with the result once the goldens are recorded.

## Escape Hatches

None planned: all six commands are registry operations with render hooks.

## Intentional Changes

| # | Change | Why | Affected callers |
| --- | --- | --- | --- |
| C1 | `publish`, `renew` and `unshare` gain `--dry-run`, and accept `--apply` as a hidden no-op. They still apply immediately without either | toolkit write model with `CLIImmediate` | none: additive |
| C2 | Every error is the toolkit envelope under `--json`/`--agent` and `error: <message>` otherwise. The old binary chose JSON when stdout was not a terminal and printed most errors as raw text | one error path, and output must not depend on a pipe | none parse stderr |
| C3 | `doctor --json` exits 1 when a check fails, with the report on stdout and `health_check_failed` on stderr. It exited 0 | one result per surface; the text form already exited 1 | none: the show-me skill runs the text form |
| C4 | Parse errors exit 2 with one error line instead of the usage text and exit 80, and an invalid `--expires-in` or `--for` is `invalid_args` | family exit table | none branch on 80 |
| C5 | Daemon 404s and a missing publish path exit 3 `not_found`, and daemon 400s exit 2, instead of 1 | family exit table | none branch on the number |
| C6 | `ferry <word>` publishes only when `<word>` exists, and `serve`, `mcp`, `metadata` are commands, not paths | an unknown command must be a usage error, not a daemon start | none: callers spell `publish` |
| C7 | `publish --json --open HOST` now opens the URL, and `ssh`'s own output goes to stderr | the old early return was a bug, and stdout must stay JSON | none use `--open` |
| C8 | New commands `serve --socket`, `mcp`, `metadata --json` | fleet design | additive |
| C9 | Help text and layout follow toolkit | toolkit help | none parse help |
