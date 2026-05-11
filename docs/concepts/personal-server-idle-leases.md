# Personal Server Idle Leases

This concept defines how `me` decides that a Personal Server is idle enough to
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
/run/me/idle/leases
```

Each lease is a small JSON file named by a generated lease ID:

```json
{
  "id": "018f4f6d-2b41-7e8d-a7da-9f96b78a7a8d",
  "kind": "command",
  "rootPid": 12345,
  "processGroup": 12345,
  "user": "harish",
  "repo": "/home/harish/projects/example",
  "command": "codex",
  "interactive": true,
  "startedAt": "2026-05-10T18:00:00Z",
  "lastProcessSeenAt": "2026-05-10T18:22:00Z",
  "lastInputAt": "2026-05-10T18:20:00Z",
  "lastOutputAt": "2026-05-10T18:21:30Z",
  "idleAfter": "30m",
  "expiresAt": "2026-05-10T19:00:00Z"
}
```

The idle agent ignores and removes stale lease files when the root process is
gone, the heartbeat is too old, or the lease has passed `expiresAt`.

## Command Leases

Commands deliberately started through `me` should run under a command lease:

```sh
me run -- pnpm test
me run -- ./ralph.sh
me run --interactive -- codex
me run --interactive -- claude
```

For non-interactive commands, an active process tree is enough to renew the
lease. For interactive commands, process existence alone is not enough; the lease
renews only when there is recent terminal input or terminal output.

This handles iterative scripts such as `ralph.sh`: the user wraps the top-level
script, and the lease follows the process group. Recursive calls to tools like
Codex do not need separate leases to protect the workflow.

## Agent Session Leases

Codex and Claude Code are interactive agent sessions. They should be protected
while active, but they should not keep the Personal Server alive merely because
their prompt is still open.

Bootstrap can install shell aliases or shims:

```sh
alias codex='me run --interactive --idle-after 30m -- codex'
alias claude='me run --interactive --idle-after 30m -- claude'
```

An agent session lease is active when the agent process still exists and either
of these are recent:

- user input into the terminal
- agent output on stdout or stderr

An agent session lease is idle when the agent process still exists but has had no
input or output for the lease idle window.

Silent long-running work should be wrapped at the command level:

```sh
me run -- pnpm test
me run -- ./ralph.sh
```

The agent lease should not try to infer every subprocess that an agent might
launch. Tool output usually flows back through the agent terminal, and workflows
that need stronger protection should use an explicit command lease.

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
- a pane contains an active `me` command lease
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
- a `tmux` pane running `me run -- ./ralph.sh`

## Idle State Machine

A root-owned `me-idle-agent` runs periodically, for example once per minute.

On each pass it:

1. Reads lease files from `/run/me/idle/leases`.
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
  and no active agent session lease
  and no active tmux pane or client
  and no protected system maintenance
  for the full idle window
```

## User Controls

`me` should expose explicit controls for exceptional cases:

```sh
me idle status
me idle inhibit --for 2h --reason "long build"
me idle disable --for 1d
me idle mark-active
```

Manual inhibitors are also leases. They should have explicit expirations so they
cannot accidentally keep the Personal Server alive forever.

## Open Questions

- How should `me run --interactive` observe terminal input/output without
  changing the behavior of full-screen terminal applications?
- Should any connected SSH session block hibernation, or only sessions with
  recent activity?
- Should bootstrap install aliases by default, or should it install command
  shims earlier in `PATH`?
- What exact idle window should be the default: 20 minutes, 30 minutes, or
  something configurable during `me configure`?
- Should detached `tmux` pane output be treated as activity only when the pane's
  foreground process is known to belong to a protected command lease?
