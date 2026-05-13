# Personal Server Idle Leases

This concept defines how `myn` decides that a Personal Server is idle enough to
hibernate without interrupting deliberate user work.

## Problem

The Personal Server should be able to hibernate after it has been unused for a
while. Hibernation may shut the server down, snapshot it, and delete it, so idle
detection must be conservative about active work.

At the same time, a process existing on the server is not enough evidence that
the server is still busy. A detached `tmux` session, an open Codex prompt, or an
idle Claude Code session should not keep the Personal Server alive forever.

## Principle

Use explicit, renewable leases for user-triggered work.

A lease is not a permanent lock. A lease keeps the Personal Server alive only
while there is recent evidence of activity. Process existence can be part of that
evidence, but interactive workflows also need recent terminal input or terminal
output.

The Personal Server may hibernate only when there are no active leases, no recent
human presence, and no protected system maintenance for the configured idle
window.

## Lease Directory

Runtime leases live under:

```text
/run/myn/idle/leases
```

`/run` is tmpfs, so lease writes do not create durable disk churn. Local
development and tests can override the lease directory:

```sh
MYN_LEASE_DIR=/tmp/myn-leases
```

The CLI should create the lease directory when it is missing and the current
user has permission to create it. On a Personal Server, `/run/myn/idle/leases`
should still be created by Personal Server Bootstrap or a future systemd
tmpfiles configuration with the right owner and permissions, because the
Personal Server User usually cannot create `/run/myn` directly. If directory
creation fails with permission denied, the CLI should report a setup error that
the runtime lease directory must be created by Personal Server Bootstrap or
systemd.

Each lease is a small JSON file named by a generated lease ID with a `.json`
extension:

```json
{
  "id": "018f4f6d-2b41-7e8d-a7da-9f96b78a7a8d",
  "kind": "stdio",
  "rootPid": 12345,
  "processGroup": 12345,
  "user": "harish",
  "workingDirectory": "/home/harish/projects/example",
  "command": "codex",
  "interactive": true,
  "startedAt": "2026-05-10T18:00:00Z",
  "updatedAt": "2026-05-10T18:22:00Z",
  "lastProcessSeenAt": "2026-05-10T18:22:00Z",
  "lastInputAt": "2026-05-10T18:20:00Z",
  "lastOutputAt": "2026-05-10T18:21:30Z",
  "idleAfter": "30m",
  "expiresAt": "2026-05-10T19:00:00Z"
}
```

Lease files should be written with `0644` permissions and contain no secrets.
The `command` field is display metadata and should store only `argv[0]`, not the
full command arguments.

Lease writers should update files atomically by writing a temporary file in the
lease directory, setting the final file permissions, and renaming it over the
lease path. Lease readers should ignore non-`.json` files so leftover temporary
files do not affect status.

The idle agent ignores and removes stale lease files when the root process is
gone, the heartbeat is too old, or the lease has passed `expiresAt`.

Lease writers should update timestamps in memory on every activity event, but
flush the JSON file only on a bounded cadence. A stdio lease can flush every
5-15 seconds, plus immediately when state changes or the wrapped command exits.
This avoids rewriting JSON for every byte or line of terminal output.

## Command Leases

Commands deliberately started through `myn` should run under a command lease:

```sh
myn run -- pnpm test
myn run -- ./ralph.sh
myn run --stdio -- codex
myn run --stdio -- claude
```

For non-interactive commands, an active process tree is enough to renew the
lease. For stdio commands, process existence alone is not enough; the lease
renews only when there is recent terminal input or terminal output.

This handles iterative scripts such as `ralph.sh`: the user wraps the top-level
script, and the lease follows the process group. Recursive calls to tools like
Codex do not need separate leases to protect the workflow.

## Stdio Leases

A stdio lease protects an interactive command while its terminal is active. This
fits Codex and Claude Code, but the concept is not specific to coding agents.
The command should be protected while active, but it should not keep the
Personal Server alive merely because its prompt is still open.

Bootstrap can install shell aliases or shims:

```sh
alias codex='myn run --stdio --idle-after 30m -- codex'
alias claude='myn run --stdio --idle-after 30m -- claude'
```

A stdio lease is active when the command process still exists and either of
these are recent:

- user input into the terminal
- command output on stdout or stderr

A stdio lease is idle when the command process still exists but has had no input
or output for the lease idle window.

Silent long-running work should be wrapped at the command level:

```sh
myn run -- pnpm test
myn run -- ./ralph.sh
```

The stdio lease should not try to infer every subprocess that a command might
launch. Tool output usually flows back through the terminal, and workflows that
need stronger protection should use an explicit command lease.

`myn run --stdio` should run the command under a PTY and proxy terminal input and
output. Plain pipes are not enough for Codex, Claude Code, shells, prompts,
colors, raw mode, window resize behavior, or full-screen terminal applications.
The proxy does not need to inspect terminal contents; it only records that bytes
flowed from user to command or from command to terminal.

## SSH Sessions

Active SSH sessions are human-presence signals and should prevent hibernation.

An SSH session is active when:

- it has recent terminal input or output
- it is running an active remote command
- it is attached to an active `tmux` client or pane

A connected but quiet SSH shell becomes idle after the configured idle window
unless the user creates an explicit manual inhibitor. This keeps a forgotten
terminal from keeping the Personal Server alive forever.

## Tmux Leases

`tmux` sessions should not block hibernation just because they exist.

A `tmux` session is active when:

- a client is attached and has recent input
- a pane contains an active `myn` command lease
- a pane has recent input or output activity

A detached and quiet `tmux` session is idle. An attached but untouched `tmux`
session also becomes idle after the configured idle window.

This means these cases can hibernate:

- an idle detached `tmux` session
- an idle SSH shell
- an idle Codex session
- an idle Codex session inside `tmux`

And these cases should not hibernate:

- a user actively typing or receiving output in an SSH session
- a user actively typing in `tmux`
- Codex or Claude Code actively streaming output
- a `tmux` pane running `myn run -- ./ralph.sh`

## Idle State Machine

A root-owned `myn-idle-agent` runs periodically, for example once per minute.

On each pass it:

1. Reads lease files from `/run/myn/idle/leases`.
2. Removes stale leases.
3. Checks active SSH and login sessions through login/session state and recent
   terminal activity.
4. Checks attached and recently active `tmux` clients and panes.
5. Checks protected system maintenance, such as cloud-init, apt, dpkg,
   unattended upgrades, and reboot work.
6. Records `last_active_at` when any active signal exists.
7. Enters a candidate-idle state only after the configured idle window passes.
8. Waits a short drain period.
9. Rechecks all signals.
10. Requests hibernation only if the Personal Server is still idle.

The effective rule is:

```text
idle =
  no active SSH/login session with recent terminal activity
  and no active command lease
  and no active stdio lease
  and no active tmux pane or client
  and no protected system maintenance
  for the full idle window
```

## User Controls

`myn` should expose explicit controls for exceptional cases:

```sh
myn idle status
myn idle inhibit --for 2h --reason "long build"
myn idle disable --for 1d
myn idle mark-active
```

Manual inhibitors are also leases. They should have explicit expirations so they
cannot accidentally keep the Personal Server alive forever.

## CLI Boundary

This should use the same `myn` CLI locally and on the Personal Server. There
should not be a separate server CLI.

During Personal Server Bootstrap, `myn` installs the same binary on the Personal
Server and wires server-side systemd units, shell aliases, and shims to that
binary. Some commands may only make sense on the Personal Server, such as an idle
agent, but they should still live under `myn`.

This lets the first implementation be developed locally:

```sh
MYN_LEASE_DIR=/tmp/myn-leases myn run --stdio -- bash
MYN_LEASE_DIR=/tmp/myn-leases myn idle status
```

## First Slice

The first implementation should build only stdio leases:

1. `myn run --stdio -- <command...>` starts a command under a PTY.
2. The PTY proxy updates in-memory `lastInputAt` and `lastOutputAt` timestamps
   when bytes flow.
3. The lease file is flushed every 5-15 seconds, on state changes, and on exit.
4. `MYN_LEASE_DIR` can override `/run/myn/idle/leases` for local development and
   tests.
5. `myn idle status` reads leases and reports active, idle, and stale entries.

This first slice should not include hibernation, reaper servers, snapshotting,
tmux detection, SSH-session detection, or systemd idle-agent scheduling. Those
features can consume the lease contract after stdio leases are proven.

## Open Questions

- Which PTY library should `myn run --stdio` use?
- What terminal behavior should be covered by tests before aliases for Codex and
  Claude Code are installed by default?
- Should any connected SSH session block hibernation, or only sessions with
  recent activity?
- Should bootstrap install aliases by default, or should it install command
  shims earlier in `PATH`?
- What exact idle window should be the default: 20 minutes, 30 minutes, or
  something configurable during `myn configure`?
- Should detached `tmux` pane output be treated as activity only when the pane's
  foreground process is known to belong to a protected command lease?
