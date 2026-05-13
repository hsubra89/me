# Issues: Project-Scoped Personal Server Connection

Source: PRD: Project-Scoped Personal Server Connection

## Proposed Breakdown

1. **Title**: Persist the Personal Server User in Personal Server Configuration  
   **Type**: AFK  
   **Status**: Done
   **Blocked by**: None  
   **User stories covered**: 35, 36, 38

2. **Title**: Connect from the Configured Project Root  
   **Type**: AFK  
   **Status**: Done
   **Blocked by**: Issue 1 (done)
   **User stories covered**: 1, 2, 5, 20, 22, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 38

3. **Title**: Map Local Subdirectories to Project-Scoped Remote Targets  
   **Type**: AFK  
   **Status**: Done
   **Blocked by**: Issue 2 (done)
   **User stories covered**: 3, 4, 6, 7, 8, 14, 33

4. **Title**: Reuse and Create Project-Scoped tmux Sessions  
   **Type**: AFK  
   **Status**: Ready
   **Blocked by**: Issue 3 (done)
   **User stories covered**: 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 31

5. **Title**: Complete Host Selection and Connection Documentation  
   **Type**: AFK  
   **Status**: Blocked
   **Blocked by**: Issue 4
   **User stories covered**: 21, 23, 24, 32, 33, 34

## Issue 1: Persist the Personal Server User in Personal Server Configuration

**Status**: Done

## What to build

Persist the Personal Server User as part of the saved Personal Server Configuration so future Personal Server Connections know which Linux user account to SSH into. The saved configuration should represent the connection identity and addresses needed to reconnect, not mutable desired state for server provisioning.

This slice should update the provisioning path end to end: when a Personal Server is created, Myn saves the server ID, Personal Server User, IPv4 address, and IPv6 address before waiting for Personal Server Bootstrap to complete. This keeps the billable server inspectable and reconnectable even when bootstrap later fails or times out.

## Acceptance criteria

- [x] Personal Server Configuration stores and reloads the Personal Server User alongside server ID, IPv4, and IPv6.
- [x] A newly created Personal Server saves the Personal Server User before bootstrap polling can fail or time out.
- [x] Existing configure behavior still skips creation when a saved Personal Server exists and still reports saved and current addresses.
- [x] Personal Server Configuration without a Personal Server User is treated as incomplete for connection purposes.
- [x] Serialization, parsing, and provisioning tests cover the new Personal Server User field.

## Blocked by

None - can start immediately.

## Issue 2: Connect from the Configured Project Root

**Status**: Done

## What to build

Add `myn connect` as the canonical command and `myn c` as its short alias. In this first vertical slice, support the complete handoff when the command is run from the configured local project root itself.

The command should load saved configuration, validate the local preconditions, require terminal-backed stdin and stdout, and start an SSH-backed tmux handoff to the Personal Server. It should trust the saved Personal Server Configuration and must not require Hetzner Credentials or Hetzner API verification before connecting.

On successful handoff, Myn should stay quiet and let SSH and tmux own the terminal. Local validation failures should be clear. SSH and remote command exit statuses should be preserved.

## Acceptance criteria

- [x] `myn connect` and `myn c` route to the same command behavior.
- [x] The command rejects positional path arguments.
- [x] Running from the configured local project root maps to the configured remote project root.
- [x] The command validates saved local project root, remote project root, configured SSH identity, Personal Server User, and at least one saved Personal Server address before SSH.
- [x] The command validates that the local project root and configured SSH identity exist locally before SSH.
- [x] The command requires terminal-backed stdin and stdout before starting SSH.
- [x] The command starts SSH with the saved Personal Server User, configured SSH identity, one TTY allocation, and `StrictHostKeyChecking=accept-new`.
- [x] The remote handoff runs through Bash login-shell command evaluation and relies on the Personal Server User login shell PATH to find tmux.
- [x] The command fails rather than falling back to plain SSH when tmux is unavailable.
- [x] Successful handoff emits no Myn-specific success output before SSH takes over the terminal.
- [x] SSH and tmux process exit statuses are preserved.
- [x] Tests use fake dependencies for config loading, filesystem checks, terminal detection, and process execution.

## Blocked by

- Issue 1: Persist the Personal Server User in Personal Server Configuration (done)

## Issue 3: Map Local Subdirectories to Project-Scoped Remote Targets

**Status**: Done

## What to build

Extend Personal Server Connection planning so `myn connect` maps the current working directory lexically under the configured local project root to the corresponding remote path under the configured remote project root.

The target Project is the configured project root itself when the command runs from the root. Otherwise, the target Project is the first path segment under the configured local project root. This must not depend on the Git repository root. A command run outside the configured local project root should fail before SSH with clear messaging.

## Acceptance criteria

- [x] A current working directory below the configured local project root maps to the matching remote path below the configured remote project root.
- [x] The configured local project root itself maps to the configured remote project root.
- [x] The target Project is derived from the first path segment under the configured local project root.
- [x] Git repository roots do not affect target Project derivation or remote path mapping.
- [x] Local path containment is lexical and does not resolve symlink targets.
- [x] Running outside the configured local project root fails before SSH.
- [x] Outside-root errors clearly include enough context to identify the configured local root and current working directory.
- [x] Tests cover roots and subdirectories with spaces, cleaned path segments, lexical symlink-style paths, and outside-root failure.

## Blocked by

- Issue 2: Connect from the Configured Project Root (done)

## Issue 4: Reuse and Create Project-Scoped tmux Sessions

**Status**: Ready

## What to build

Make the remote tmux handoff project-scoped. A Personal Server Connection should attach the Personal Server User to an existing tmux session for the target Project when one exists. If the session does not exist, Myn should create a new tmux session for that Project and attach to it.

tmux session names should be derived from the remote Project root path with the agreed stable normalization: lowercase the path, keep ASCII letters and digits, convert every other character run to one hyphen, trim edge hyphens, prefix `myn-`, and use `myn-project` if the normalized project path is empty.

When creating a new session, choose the starting directory on the Personal Server by trying the exact mapped remote directory, then the remote Project root, then the Personal Server User home directory. Only existing remote directories count. Missing directories must not be created. Directory fallback must not alter existing sessions, which are attached as-is.

## Acceptance criteria

- [ ] Existing target Project tmux sessions are attached as-is.
- [ ] Missing target Project tmux sessions are created and attached.
- [ ] tmux session names are derived from the remote Project root path using the agreed normalization.
- [ ] Session naming tests cover uppercase letters, spaces, punctuation, slashes, repeated separators, edge separators, and empty normalized values.
- [ ] New sessions start in the exact mapped remote directory when it exists.
- [ ] New sessions start in the remote Project root when the exact mapped remote directory does not exist but the Project root exists.
- [ ] New sessions start in the Personal Server User home directory when neither mapped remote directory exists.
- [ ] Files at mapped remote paths are treated as invalid starting directories.
- [ ] The remote handoff does not create missing remote project directories.
- [ ] Existing sessions are attached without applying directory fallback.
- [ ] Tests cover remote command construction and shell quoting for paths with spaces and punctuation.

## Blocked by

- Issue 3: Map Local Subdirectories to Project-Scoped Remote Targets (done)

## Issue 5: Complete Host Selection and Connection Documentation

**Status**: Blocked

## What to build

Finish the Personal Server Connection behavior by covering saved address selection, IPv6 SSH target formatting, and user-facing documentation.

The command should prefer the saved IPv4 address. If IPv4 is unavailable, it should use the saved IPv6 address and bracket IPv6 literals when building SSH targets. Mosh Access remains available separately, but `myn connect` is SSH-backed and should not use Mosh. The initial implementation should not create an Idle Lease or Stdio Lease.

Update documentation so users can discover `myn connect` and `myn c`, understand the project-scoped tmux behavior, and know the important limits: no path arguments, no remote directory creation, no Hetzner verification, no Mosh use, and no Idle Lease in the initial implementation.

## Acceptance criteria

- [ ] Saved IPv4 is selected before saved IPv6.
- [ ] Saved IPv6 is selected when IPv4 is unavailable.
- [ ] IPv6 literals are bracketed correctly in SSH targets.
- [ ] Missing saved addresses fail before SSH with clear messaging.
- [ ] `myn connect` does not require Hetzner Credentials and does not call the Hetzner API.
- [ ] `myn connect` does not create an Idle Lease or Stdio Lease.
- [ ] User-facing documentation describes `myn connect` and `myn c`.
- [ ] User-facing documentation describes local-to-remote path mapping and project-scoped tmux sessions.
- [ ] User-facing documentation describes the initial limits and failure behavior.
- [ ] Tests cover IPv4 preference, IPv6 fallback, IPv6 bracket formatting, missing addresses, and no Hetzner dependency.

## Blocked by

- Issue 4: Reuse and Create Project-Scoped tmux Sessions
