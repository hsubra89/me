# PRD: Stdio Idle Leases for the Personal Server

## Problem Statement

The Personal Server needs a conservative way to know whether interactive user work is still active before later hibernation work is introduced. Today, an open process is too weak a signal: a forgotten Codex prompt, Claude Code prompt, shell, or detached workflow could keep a Personal Server alive forever, while killing an active interactive session would interrupt deliberate work.

For the first slice, the user needs `me` to prove the Stdio Lease contract locally and on Unix-like Personal Server environments without implementing hibernation, SSH detection, tmux detection, systemd scheduling, or non-interactive command leases.

## Solution

Add stdio-only Idle Lease support to the existing Go `me` CLI.

The user can run an interactive command through:

```sh
me run --stdio --idle-after 30m -- codex
me run --stdio --idle-after 30m -- claude
me run --stdio -- bash
```

`me run --stdio` starts the wrapped command under a PTY, proxies terminal input and output, and writes a renewable Stdio Lease JSON file under the runtime lease directory. The lease records heartbeat time separately from terminal activity time. The lease is active while the wrapped process exists and recent terminal input or output happened within the idle window. It is idle when the process still exists but terminal activity is older than the idle window. It is stale when the process is gone, the heartbeat is too old, the lease is expired, or the file is unreadable/malformed.

Add:

```sh
me idle status
me idle status --json
```

`me idle status` is read-only in this slice. It reports active, idle, and stale leases without pruning files.

## User Stories

1. As a Personal Server User, I want to wrap Codex in a Stdio Lease, so that active Codex work keeps my Personal Server awake.
2. As a Personal Server User, I want to wrap Claude Code in a Stdio Lease, so that active Claude work keeps my Personal Server awake.
3. As a Personal Server User, I want an idle Codex prompt to become idle after the configured window, so that forgotten prompts do not keep the Personal Server alive forever.
4. As a Personal Server User, I want terminal output to renew a Stdio Lease, so that streaming agent or command output is treated as active work.
5. As a Personal Server User, I want terminal input to renew a Stdio Lease, so that typing into an interactive session is treated as active work.
6. As a Personal Server User, I want input and output to be tracked separately from heartbeat, so that a quiet but alive wrapper is idle instead of stale.
7. As a Personal Server User, I want a 30 minute default idle window, so that common coding-agent workflows behave conservatively without extra flags.
8. As a Personal Server User, I want to override the idle window per command, so that unusually sensitive or long-lived sessions can use a more appropriate threshold.
9. As a Personal Server User, I want invalid idle windows to be rejected before the command starts, so that the lease state is never ambiguous.
10. As a Personal Server User, I want the wrapper to require a real terminal, so that interactive tools get PTY behavior instead of broken pipe behavior.
11. As a Personal Server User, I want Ctrl-C to reach the wrapped command, so that the wrapper behaves transparently for interactive programs.
12. As a Personal Server User, I want terminal resize events to reach the wrapped command, so that shells, editors, and TUIs render correctly.
13. As a Personal Server User, I want the wrapped command's exit code to pass through, so that using `me run --stdio` does not change command semantics.
14. As a Personal Server User, I want the lease file removed after normal wrapper exit, so that completed interactive sessions do not clutter status output.
15. As a Personal Server User, I want abandoned lease files to be reported as stale, so that crashes and killed wrappers are visible.
16. As a Personal Server User, I want local development to use `ME_LEASE_DIR`, so that I can test leases without needing `/run` permissions.
17. As a Personal Server User, I want the CLI to create a missing writable lease directory, so that local and boot-time runtime directories recover cleanly.
18. As a Personal Server User, I want a clear setup error when the default runtime directory cannot be created due to permissions, so that I know Personal Server Bootstrap or systemd must create it.
19. As a Personal Server User, I want lease files to contain no secrets, so that status inspection is safe.
20. As a Personal Server User, I want the recorded command metadata to avoid full arguments, so that command-line secrets are not written to disk.
21. As a Personal Server User, I want the working directory recorded, so that I can identify which interactive session a lease belongs to.
22. As a Personal Server User, I want `me idle status` to summarize lease counts, so that I can quickly see whether the server would be considered busy.
23. As a Personal Server User, I want `me idle status` to show each lease state, so that I can diagnose active, idle, and stale sessions.
24. As a Personal Server User, I want stale reasons shown in status, so that I can understand whether a process disappeared, a heartbeat expired, or a file is malformed.
25. As an automation author, I want `me idle status --json`, so that tests and later agents can consume lease state without scraping human text.
26. As an automation author, I want `me idle status` to exit zero when it successfully reports idle or stale leases, so that state is data rather than command failure.
27. As an automation author, I want non-zero status only for operational failures, so that scripts can distinguish inability to inspect from an idle result.
28. As a future idle-agent implementer, I want atomic lease writes, so that readers never observe partially written JSON.
29. As a future idle-agent implementer, I want readers to ignore non-JSON temp files, so that interrupted writes do not affect status decisions.
30. As a future idle-agent implementer, I want `expiresAt` to mean stale crash-safety TTL rather than idle deadline, so that active/idle/stale are distinct states.
31. As a future idle-agent implementer, I want process liveness anchored to the root PID for Stdio Leases, so that this slice has a simple and testable liveness rule.
32. As a future command-lease implementer, I want `processGroup` recorded but not used for stdio liveness yet, so that later process-tree semantics have room to grow.
33. As a developer of `me`, I want plain `me run -- <command>` to fail clearly for now, so that users do not think non-stdio command leases are already implemented.
34. As a developer of `me`, I want unsupported platforms to fail clearly, so that there is no silent degradation from PTY behavior to pipe behavior.
35. As a developer of `me`, I want the lease contract to be testable without running a full Personal Server, so that this slice can be developed locally.
36. As a developer of `me`, I want the lease state evaluator isolated from PTY plumbing, so that active/idle/stale behavior can be unit tested thoroughly.
37. As a developer of `me`, I want the PTY runner isolated from status rendering, so that terminal proxy behavior and lease classification do not become entangled.
38. As a developer of `me`, I want the JSON status contract tested, so that future hibernation and idle-agent work can rely on it.

## Implementation Decisions

- The implementation adds `me run` and `me idle status` command groups to the existing Cobra CLI.
- The only supported `me run` mode in this slice is `--stdio`.
- Running `me run` without `--stdio` should fail clearly with a message that non-stdio command leases are not implemented yet.
- `me run --stdio` accepts `--idle-after`, using Go-style duration strings.
- The default Stdio Lease idle window is 30 minutes.
- Zero or negative idle windows are invalid.
- `me run --stdio` requires terminal-backed stdin and stdout.
- `me run --stdio` uses a PTY, not plain pipes, in accordance with the PTY-backed Stdio Leases ADR.
- The first PTY implementation targets Unix-like workflows with `github.com/creack/pty`.
- Unsupported platforms should return a clear error rather than falling back to pipes.
- Ctrl-C should pass through the PTY to the wrapped command.
- Terminal resize events should be forwarded to the child PTY.
- The wrapped command's exit status should pass through exactly when possible.
- If the wrapped command exits due to a signal, use conventional shell-style signal mapping where possible.
- Wrapper setup failures should return normal CLI errors before the child starts.
- A Stdio Lease is active when the root process exists and either recent terminal input or recent terminal output happened within the idle window.
- A Stdio Lease is idle when the root process exists but both terminal input and terminal output are older than the idle window.
- A lease is stale when the root process is gone, the heartbeat is too old, `expiresAt` is past, or the lease file cannot be parsed.
- `updatedAt` is the lease writer heartbeat and is distinct from terminal activity.
- `lastInputAt` advances only when bytes flow from user input toward the command.
- `lastOutputAt` advances only when bytes flow from the command toward the terminal.
- `expiresAt` is a crash-safety TTL, not the idle deadline.
- For Stdio Leases, `expiresAt` should refresh on each flush to a value derived from the idle window, with a minimum long enough to avoid treating quiet but heartbeating wrappers as stale.
- `rootPid` is the only process-liveness anchor used by `me idle status` in this slice.
- `processGroup` may be recorded for future command-lease work, but it does not define stdio liveness yet.
- The runtime lease directory defaults to `/run/me/idle/leases`.
- `ME_LEASE_DIR` overrides the runtime lease directory for local development and tests.
- The CLI should create a missing lease directory when the current user has permission.
- If creating the runtime lease directory fails with permission denied, the CLI should report that Personal Server Bootstrap or systemd must create the runtime lease directory.
- Lease files are JSON files named by generated lease ID with a `.json` extension.
- Lease readers should ignore non-`.json` files.
- Lease writes should be atomic: write a temporary file in the same directory, set final permissions, then rename over the lease path.
- Lease files should be `0644` and contain no secrets.
- The `command` field is display metadata and stores only `argv[0]`, not full arguments.
- The lease records `workingDirectory`, not `repo`.
- The wrapper should flush lease state on a bounded cadence, immediately on meaningful state changes, and at command exit.
- Normal wrapper exit removes the lease file after final cleanup.
- Abnormal leftovers are reported as stale by status.
- `me idle status` is read-only and does not remove stale lease files.
- `me idle status --json` should return a stable object with lease directory, current time, counts by state, and per-lease details.
- `me idle status` exits zero when it successfully reports state, even when idle or stale leases exist.
- Operational failures, invalid flags, and encoding failures should exit non-zero.
- The implementation should be organized around deep modules:
  - Lease model and state evaluator: owns lease schema, time calculations, process-liveness input, and active/idle/stale classification.
  - Lease store: owns lease directory resolution, directory creation, atomic JSON writes, JSON reads, non-JSON filtering, and removal on normal exit.
  - Stdio runner: owns PTY startup, raw terminal mode, input/output proxying, resize forwarding, child exit handling, and activity event emission.
  - Status presenter: owns human and JSON rendering from evaluated lease state.
  - CLI wiring: owns Cobra commands, flag parsing, environment lookup, dependency injection, and error messages.

## Testing Decisions

- Tests should focus on externally observable behavior and contracts rather than implementation details.
- Lease state evaluation should receive a fake clock and fake process-liveness checker so active, idle, and stale states can be tested deterministically.
- Lease store tests should verify directory override behavior, missing-directory creation, permission/setup errors where practical, atomic write/read behavior, non-JSON filtering, malformed JSON classification, and normal removal.
- Status tests should verify human summaries, JSON shape, counts by state, stale reasons, read-only behavior, and zero exit behavior for successful reports.
- CLI tests should follow the existing command-test style in the repo: create a root command, set args, capture output, and assert errors/output.
- Duration parsing tests should cover default `30m`, valid custom values, zero, negative values, and invalid strings.
- Stdio runner tests should be split between unit-testable event handling and narrower integration tests that actually run simple PTY-backed commands on supported platforms.
- PTY integration tests should cover basic command startup, output activity updates, input activity updates, exit status passthrough, and lease cleanup on normal exit.
- Resize forwarding should be covered where practical through a small integration test or a testable resize-forwarding seam.
- Unsupported-platform behavior should be guarded by build tags or platform-specific tests where appropriate.
- Tests should not require a real Personal Server.
- Tests should use temporary lease directories through `ME_LEASE_DIR` or injected dependencies.
- Tests should not assert private helper names or internal goroutine structure.
- Prior art in the repo includes Cobra command tests, config/env override tests, dependency-injected filesystem/process tests, and Personal Server provisioning tests that validate user-visible output and saved state.

## Out of Scope

- Actual hibernation, snapshotting, shutdown, deletion, or resume behavior.
- A root-owned idle agent.
- systemd units or timers for idle checks.
- systemd tmpfiles configuration, except documenting that it will need to create the runtime lease directory on the Personal Server.
- SSH session detection.
- login session detection.
- tmux client or pane detection.
- protected system maintenance detection.
- Manual inhibitors such as `me idle inhibit`, `me idle disable`, or `me idle mark-active`.
- Non-stdio command leases for `me run -- <command>`.
- Process-tree renewal semantics for non-interactive commands.
- Default bootstrap aliases or shims for Codex and Claude Code.
- Windows ConPTY support.
- Durable configuration for idle defaults.
- Pruning stale leases from `me idle status`.
- Storing full command arguments in lease files.

## Further Notes

- The glossary now distinguishes Idle Lease from Stdio Lease and records that heartbeat is distinct from terminal activity.
- The lease concept doc has been sharpened around directory creation, file permissions, command metadata, `workingDirectory`, atomic writes, and `.json` files.
- The PTY-backed Stdio Leases ADR records why `me run --stdio` must use terminal semantics instead of plain pipes.
- This PRD is intentionally limited to proving the stdio lease contract before broader Personal Server idle and hibernation behavior consumes it.
