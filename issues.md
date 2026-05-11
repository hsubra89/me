# Issues: Stdio Idle Leases for the Personal Server

Source: `prd.md`

Default label: `ready-for-agent`

## Proposed Breakdown

1. **Title**: Report Idle Lease state from JSON lease files
   **Type**: AFK
   **Status**: Done
   **Blocked by**: None
   **User stories covered**: 15-18, 21-31, 35-36, 38

2. **Title**: Render human-readable Idle Lease status
   **Type**: AFK
   **Status**: Done
   **Blocked by**: Issue 1
   **User stories covered**: 22-24, 26-27

3. **Title**: Run stdio commands under a PTY
   **Type**: AFK
   **Status**: Ready
   **Blocked by**: None
   **User stories covered**: 7-10, 13, 33-35, 37

4. **Title**: Create and clean up Stdio Lease files during a PTY run
   **Type**: AFK
   **Status**: Blocked
   **Blocked by**: Issues 1 and 3
   **User stories covered**: 14, 16-21, 28, 30, 32, 35, 37

5. **Title**: Renew Stdio Leases from terminal input and output
   **Type**: AFK
   **Status**: Blocked
   **Blocked by**: Issues 1, 2, and 4
   **User stories covered**: 1-6, 22-25, 30-31, 38

6. **Title**: Preserve interactive terminal controls for stdio sessions
   **Type**: AFK
   **Status**: Blocked
   **Blocked by**: Issue 3
   **User stories covered**: 10-12, 37

## Issue 1: Report Idle Lease State From JSON Lease Files

Type: AFK

Label: `done`

Status: Done

### What to build

Build the read side of the Idle Lease contract by adding `me idle status --json`. The command should resolve the runtime lease directory, read `.json` lease files, ignore non-JSON files, classify each lease as active, idle, or stale, and emit a stable JSON report with the lease directory, current time, counts, and per-lease details.

This slice should be verifiable entirely from fixture lease files. It should not require `me run --stdio` to exist yet.

### Acceptance criteria

- [x] `me idle status --json` reads lease files from the default runtime lease directory.
- [x] `ME_LEASE_DIR` overrides the runtime lease directory.
- [x] A missing lease directory is created when the current user has permission.
- [x] Permission failures while creating the runtime lease directory produce a setup error explaining that Personal Server Bootstrap or systemd must create it.
- [x] Only files ending in `.json` are considered leases.
- [x] Non-JSON files are ignored.
- [x] Valid stdio lease JSON files are decoded into the lease model.
- [x] The lease model uses `workingDirectory`, not `repo`.
- [x] The `command` field is treated as display metadata.
- [x] A lease with a live `rootPid` and recent terminal activity is reported as `active`.
- [x] A lease with a live `rootPid` but old terminal activity is reported as `idle`.
- [x] A lease with a missing root process is reported as `stale`.
- [x] A lease whose `expiresAt` is in the past is reported as `stale`, not `idle`.
- [x] A lease whose heartbeat is too old is reported as `stale`.
- [x] Malformed lease JSON is reported as `stale` with a useful reason.
- [x] The JSON response includes `leaseDirectory`, `now`, state counts, and per-lease details.
- [x] `me idle status --json` exits zero when the status report is produced successfully, even when idle or stale leases exist.
- [x] Operational failures exit non-zero.
- [x] Tests cover active, idle, stale, malformed, ignored temp files, directory override, missing-directory creation, and exit behavior.

### Blocked by

None - can start immediately.

## Issue 2: Render Human-Readable Idle Lease Status

Type: AFK

Label: `done`

Status: Done

### What to build

Add the default human-readable `me idle status` output using the same evaluated lease state as `--json`. The command should summarize active, idle, and stale counts and list enough per-lease detail for a user to understand which interactive sessions are keeping the Personal Server awake or why a lease is stale.

This slice must remain read-only: status may inspect and report lease files, but it must not prune stale leases.

### Acceptance criteria

- [x] `me idle status` renders a concise human-readable summary.
- [x] The summary includes counts for active, idle, and stale leases.
- [x] Per-lease output includes the lease ID, kind, state, command, working directory when present, and a state reason.
- [x] Stale leases include the reason they were classified as stale.
- [x] The command does not remove stale lease files.
- [x] The command exits zero when the status report is produced successfully, even when idle or stale leases exist.
- [x] Operational failures exit non-zero.
- [x] Tests verify representative active, idle, stale, empty-directory, and malformed-lease output.

### Blocked by

- Issue 1

## Issue 3: Run Stdio Commands Under a PTY

Type: AFK

Label: `ready-for-agent`

### What to build

Add the first executable path for `me run --stdio`. The command should validate flags, require a terminal-backed stdin and stdout, start the wrapped command under a PTY on supported Unix-like platforms, proxy input and output, and pass through the wrapped command's exit status.

This slice establishes transparent PTY wrapper behavior without writing Stdio Lease files yet.

### Acceptance criteria

- [ ] `me run --stdio -- <command...>` starts the command under a PTY on supported Unix-like platforms.
- [ ] The PTY implementation uses `github.com/creack/pty`.
- [ ] Unsupported platforms fail clearly rather than falling back to plain pipes.
- [ ] `me run -- <command...>` without `--stdio` fails clearly because non-stdio command leases are not implemented yet.
- [ ] `--idle-after` defaults to `30m`.
- [ ] Valid Go-style duration values are accepted for `--idle-after`.
- [ ] Zero, negative, and malformed idle windows are rejected before the child command starts.
- [ ] Missing command arguments are rejected before child startup.
- [ ] Non-terminal stdin or stdout is rejected before child startup.
- [ ] Child stdout and stderr output reaches the user's terminal.
- [ ] User input reaches the child process through the PTY.
- [ ] The wrapper exits with the wrapped command's exit status when possible.
- [ ] A child that exits due to signal uses conventional shell-style signal mapping where possible.
- [ ] Tests cover flag validation, unsupported non-stdio run mode, terminal requirement, output proxying, input proxying, and exit-status passthrough.

### Blocked by

None - can start immediately.

## Issue 4: Create and Clean Up Stdio Lease Files During a PTY Run

Type: AFK

Label: `ready-for-agent`

### What to build

Connect the PTY runner to the Idle Lease store. While a stdio command is running, `me run --stdio` should create a Stdio Lease JSON file, keep its heartbeat fresh on a bounded cadence, write updates atomically, and remove the lease file on normal wrapper exit.

The command does not need to renew from terminal input or output in this slice beyond initial metadata and heartbeat behavior. Activity-specific renewal is handled by a later slice.

### Acceptance criteria

- [ ] Starting `me run --stdio` creates one Stdio Lease file in the resolved lease directory.
- [ ] Lease file names use generated lease IDs with a `.json` extension.
- [ ] Lease files are written with `0644` permissions.
- [ ] Lease files contain no full command arguments.
- [ ] The `command` field stores only `argv[0]`.
- [ ] The lease records `kind`, `id`, `rootPid`, `processGroup`, `user`, `workingDirectory`, `command`, `interactive`, `startedAt`, `updatedAt`, `idleAfter`, and `expiresAt`.
- [ ] `expiresAt` is refreshed as a crash-safety TTL, not used as the idle deadline.
- [ ] Lease writes are atomic using a temporary file in the same directory and rename.
- [ ] Heartbeat flushes update `updatedAt` on a bounded cadence.
- [ ] A meaningful state change flushes promptly.
- [ ] Normal wrapper exit removes the lease file.
- [ ] If the wrapper or child dies abnormally and leaves a lease behind, `me idle status --json` reports it as stale.
- [ ] Tests verify lease creation, metadata, atomic write behavior, heartbeat update, final cleanup, and stale leftover reporting.

### Blocked by

- Issue 1
- Issue 3

## Issue 5: Renew Stdio Leases From Terminal Input and Output

Type: AFK

Label: `ready-for-agent`

### What to build

Make Stdio Lease activity follow terminal traffic. The PTY proxy should update in-memory `lastInputAt` when bytes flow from the user toward the command and `lastOutputAt` when bytes flow from the command toward the terminal. Lease flushes should persist those timestamps on a bounded cadence and promptly on meaningful state changes.

After this slice, `me idle status` and `me idle status --json` should show a running stdio command as active when either input or output is recent, and idle when the process still exists but both have been quiet longer than `idleAfter`.

### Acceptance criteria

- [ ] User input through the PTY updates `lastInputAt`.
- [ ] Child output through the PTY updates `lastOutputAt`.
- [ ] Input and output timestamps are distinct from `updatedAt`.
- [ ] Output alone is enough to keep a Stdio Lease active.
- [ ] Input alone is enough to keep a Stdio Lease active.
- [ ] A running but quiet stdio command becomes idle after `idleAfter`.
- [ ] A quiet but heartbeating stdio wrapper is reported as `idle`, not `stale`.
- [ ] Activity timestamp updates are flushed on a bounded cadence instead of on every byte.
- [ ] Meaningful activity state changes flush promptly.
- [ ] `me idle status --json` reports active and idle Stdio Leases according to terminal activity.
- [ ] `me idle status` reports the same states and reasons in human-readable form.
- [ ] Tests verify input-only renewal, output-only renewal, quiet-idle classification, heartbeat-versus-activity distinction, and JSON/human status consistency.

### Blocked by

- Issue 1
- Issue 2
- Issue 4

## Issue 6: Preserve Interactive Terminal Controls For Stdio Sessions

Type: AFK

Label: `ready-for-agent`

### What to build

Finish the terminal-fidelity behavior promised by PTY-backed Stdio Leases. `me run --stdio` should behave like a transparent interactive wrapper for common terminal controls: Ctrl-C should reach the child process, the wrapper should not treat Ctrl-C as its own cancellation request while the child is running, and terminal resize events should be forwarded to the child PTY.

This slice is about preserving interactive behavior for shells, Codex, Claude Code, editors, prompts, and full-screen TUIs.

### Acceptance criteria

- [ ] Ctrl-C is passed through to the child process through the PTY.
- [ ] The wrapper does not independently delete or kill the session on Ctrl-C unless the child exits.
- [ ] Lease cleanup still happens when the child exits after Ctrl-C.
- [ ] Terminal resize events are forwarded to the child PTY.
- [ ] Resize forwarding is structured so it can be tested without relying only on manual inspection.
- [ ] Raw terminal mode is restored when the wrapper exits.
- [ ] Tests cover Ctrl-C passthrough where practical.
- [ ] Tests cover resize forwarding through a small integration test or a testable forwarding seam.
- [ ] Tests cover terminal mode restoration on normal exit and child failure where practical.

### Blocked by

- Issue 3
