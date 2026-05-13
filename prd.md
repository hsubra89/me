# PRD: Project-Scoped Personal Server Connection

## Problem Statement

When Myn has configured and provisioned a Personal Server, the user still has to manually choose and run SSH or Mosh commands to enter that server. That loses the local project context the user is already in. If the user is working inside a configured local project root, they should be able to run a short muscle-memory command and land in the matching project-scoped tmux session on the Personal Server.

## Solution

Add `myn connect` as the canonical command, with `myn c` as its short alias, to start a Personal Server Connection.

The command maps the current local working directory lexically under the configured local project root to the matching remote path under the configured remote project root. It connects to the saved Personal Server over SSH, using the saved Personal Server User, saved address, and configured SSH identity. It then attaches to an existing tmux session for the target Project, or creates one when no session exists.

The command is intentionally narrow in the first implementation: it accepts no path arguments, does not use Hetzner Credentials, does not create remote project directories, does not create an Idle Lease, and stays quiet on successful handoff.

## User Stories

1. As a Myn user, I want to run `myn c`, so that I can enter my Personal Server without remembering an SSH command.
2. As a Myn user, I want `myn connect` to also exist, so that the command is discoverable in help output and documentation.
3. As a Myn user, I want `myn c` to work from a subdirectory of my configured local project root, so that my local context determines the remote context.
4. As a Myn user, I want `myn c` from `~/projects/acme/api/src` to target `~/projects/acme/api/src` on the Personal Server, so that I land in the same spot when that remote directory exists.
5. As a Myn user, I want `myn c` from the configured local project root itself to target the configured remote project root, so that the root is still a valid connection target.
6. As a Myn user, I want `myn c` to fail clearly outside the configured local project root, so that Myn does not guess a surprising remote path.
7. As a Myn user, I want the target Project to be derived from the configured project root boundary, not the Git repository root, so that nested repositories do not change connection behavior.
8. As a Myn user, I want path containment to follow the visible shell path, so that symlinked paths under my local project root map predictably.
9. As a Myn user, I want an existing project tmux session to be reused, so that reconnecting returns me to the work already in progress.
10. As a Myn user, I want a missing project tmux session to be created automatically, so that the first connection to a Project is seamless.
11. As a Myn user, I want tmux sessions to be scoped by Project, so that work in different Projects does not collide.
12. As a Myn user, I want tmux session names to be stable and tmux-safe, so that project names with spaces or punctuation still work.
13. As a Myn user, I want remote paths with uppercase letters to produce predictable lowercase session names, so that session identity is stable.
14. As a Myn user, I want the exact mapped remote directory to be used when it exists, so that new sessions start in the most specific matching location.
15. As a Myn user, I want the remote Project root to be used when the exact mapped remote subdirectory is missing, so that a partially created project still opens in the right Project.
16. As a Myn user, I want the Personal Server User home directory to be used when the remote Project root is missing, so that the connection still succeeds without creating directories.
17. As a Myn user, I want only existing remote directories to be valid starting directories, so that files at those paths do not break tmux startup.
18. As a Myn user, I do not want `myn connect` to create missing remote project directories, so that connecting does not become implicit sync or provisioning behavior.
19. As a Myn user, I want directory fallback to affect only newly created tmux sessions, so that existing sessions are attached as-is.
20. As a Myn user, I want `myn connect` to use SSH rather than Mosh, so that the command can reliably run the remote tmux handoff.
21. As a Myn user, I want Mosh Access to remain available separately, so that the new command does not remove the existing access path.
22. As a Myn user, I want `myn connect` to work without Hetzner Credentials, so that day-to-day access is not blocked by missing or expired cloud credentials.
23. As a Myn user, I want the command to prefer the saved IPv4 address and use the saved IPv6 address only when IPv4 is unavailable, so that connection behavior matches existing provisioning output.
24. As a Myn user, I want IPv6 SSH hosts to be passed unbracketed with the Personal Server User supplied separately, so that IPv6-only configurations can connect.
25. As a Myn user, I want the configured SSH identity to always be passed with `-i`, so that SSH targets the key the Personal Server trusts.
26. As a Myn user, I want SSH to use `StrictHostKeyChecking=accept-new`, so that first connection to a newly created Personal Server is smooth while changed host keys are still rejected.
27. As a Myn user, I want `myn connect` to require terminal-backed stdin and stdout, so that tmux starts only in a real interactive terminal.
28. As a Myn user, I want SSH to request one TTY allocation, so that tmux gets the terminal semantics it needs.
29. As a Myn user, I want `myn connect` to rely on the Personal Server User login shell PATH to find tmux, so that it respects the user environment created by bootstrap.
30. As a Myn user, I want the remote handoff to run through Bash login-shell command evaluation, so that PATH and profile initialization are predictable.
31. As a Myn user, I want `myn connect` to fail if tmux is unavailable, so that a broken Personal Server setup is visible instead of silently degrading to plain SSH.
32. As a Myn user, I want successful handoff to be quiet, so that the command feels like a direct terminal transition.
33. As a Myn user, I want local validation failures to be clear, so that I know whether configuration, local roots, SSH identity, or working directory caused the problem.
34. As a Myn user, I want SSH and tmux exit statuses to be preserved, so that failures remain visible to shells and scripts.
35. As a Myn user, I want the Personal Server User saved in Personal Server Configuration, so that future connections know which Linux account to use.
36. As a Myn maintainer, I want no backwards compatibility migration for older Personal Server Configuration shapes, so that this pre-user feature can keep the config contract simple.
37. As a Myn maintainer, I want the connection planning behavior isolated behind a small interface, so that path mapping, session naming, and command construction can be tested without real SSH.
38. As a Myn maintainer, I want Personal Server provisioning to save all fields needed for connection, so that `myn connect` does not depend on transient creation inputs.

## Implementation Decisions

- Add `connect` as the canonical command and `c` as its alias. The command accepts no positional arguments in the initial implementation.
- Extend Personal Server Configuration to store the Personal Server User alongside server ID and saved addresses. A Personal Server Configuration without server ID, Personal Server User, and at least one saved address is incomplete for connection.
- Update the provisioning save path so a newly created Personal Server persists the Personal Server User after server creation succeeds, including the case where Personal Server Bootstrap later fails or times out.
- Add a deep Personal Server Connection planning module with a compact interface that accepts saved configuration, current working directory, user home directory, and local filesystem checks, and returns a connection plan or a local validation error.
- The connection plan should include the SSH user, selected host, SSH identity path, remote exact path, remote Project root, tmux session name, and remote handoff command.
- Validate saved configuration before SSH: configured local project root, configured remote project root, configured SSH identity, saved Personal Server User, and at least one saved Personal Server address are required.
- Validate local project root existence and configured SSH identity existence before SSH.
- Use lexical path containment under the configured local project root. Do not resolve symlink targets for project containment.
- Derive the Project from the first path segment under the configured local project root. If the command is run from the configured local project root itself, the configured root is the Project.
- Map the current working directory to the remote path by applying the local relative path under the configured local project root to the configured remote project root.
- Derive the remote Project root from the configured remote project root plus the first local path segment when the command runs inside a top-level Project. Use the configured remote project root itself when the command runs from the root.
- Build a tmux-safe session name from the remote Project root path by lowercasing, keeping ASCII letters and digits, converting every other character run to one hyphen, trimming edge hyphens, prefixing `myn-`, and using `myn-project` if the normalized project path is empty.
- Select the saved IPv4 address first. Select the saved IPv6 address only when IPv4 is unavailable.
- Pass the Personal Server User to SSH with `-l` and pass the selected host as a separate unbracketed argument, including for IPv6 literals.
- Build SSH with the configured identity explicitly, one TTY allocation, and `StrictHostKeyChecking=accept-new`.
- Do not require Hetzner Credentials and do not verify the saved server through the Hetzner API before connecting.
- Run the remote handoff through Bash login-shell command evaluation so that the Personal Server User login shell PATH is used to find tmux.
- The remote handoff checks for an existing tmux session first. If the session exists, attach to it as-is.
- If no tmux session exists, choose the starting directory by checking the exact mapped remote directory, then the remote Project root, then the Personal Server User home directory. Only directories count as valid starting locations.
- Do not create missing remote project directories.
- Create a missing tmux session in the selected starting directory and attach to it.
- Do not fall back to plain SSH when tmux is unavailable.
- Do not create an Idle Lease in the initial implementation.
- Require terminal-backed stdin and stdout before starting SSH.
- Preserve the SSH/tmux process exit status.
- Stay quiet on successful handoff. Print clear local validation errors before SSH. Let SSH and tmux report their own remote/runtime errors.
- Update user-facing documentation to describe `myn connect` and `myn c`, the path mapping behavior, the project-scoped tmux behavior, and the local validation requirements.

## Testing Decisions

- Good tests should assert external behavior: command shape, configuration validation, path mapping results, tmux session names, SSH argument construction, remote command behavior, output behavior, and exit status preservation. They should not pin private helper boundaries except where a deep module exposes a deliberate stable interface.
- Unit test the Personal Server Connection planning module because it contains most of the feature logic and can be exercised without a real Personal Server.
- Unit test local path mapping from subdirectories, from the configured local project root itself, outside-root failure, roots with spaces, cleaned paths, and lexical symlink-style paths.
- Unit test Project derivation separately from Git repository state to ensure Git roots do not influence connection behavior.
- Unit test remote path fallback command construction for exact path, Project root, and home fallback.
- Unit test tmux session name normalization for lowercase, uppercase, spaces, punctuation, slashes, repeated separators, edge separators, and empty normalized values.
- Unit test SSH host selection for IPv4 preference, IPv6 fallback, missing addresses, unbracketed IPv6 host arguments, and separate `-l` user arguments.
- Unit test configuration validation for missing local root, missing remote root, missing SSH identity, incomplete Personal Server Configuration, missing saved address, missing local root directory, and missing SSH identity file.
- Unit test command registration so `connect` and `c` both route to the same behavior and reject positional arguments.
- Unit test terminal validation so non-terminal stdin or stdout fails before SSH.
- Unit test SSH process execution through a fake runner so the command passes the configured identity, TTY allocation, host key policy, user, host, and remote handoff command.
- Unit test child exit preservation for successful exit, SSH-style failure, and remote-command failure.
- Unit test quiet success by asserting no Myn-specific output is written before a successful fake handoff.
- Unit test local validation error messages for clarity and for including the relevant configured local root or current directory where helpful.
- Update Personal Server provisioning tests so saved Personal Server Configuration includes the Personal Server User.
- Update config serialization tests so the new Personal Server Configuration field is emitted and parsed correctly.
- Use existing configure, Personal Server provisioning, root command, and stdio terminal tests as prior art for table-driven behavior tests, fake dependencies, and command execution tests.
- A live Hetzner validation test is not required for the first implementation because the core behavior can be covered with fakes and command construction tests. Existing live provisioning coverage remains useful for ensuring the Personal Server has tmux and a Personal Server User.

## Out of Scope

- Creating or syncing remote project directories.
- Cloning repositories onto the Personal Server.
- Passing an explicit local path, remote path, or Project argument to `myn connect`.
- Creating an Idle Lease or Stdio Lease for the connection.
- Using Mosh for `myn connect`.
- Falling back to plain SSH when tmux is unavailable.
- Verifying the saved Personal Server through Hetzner before connecting.
- Migrating older Personal Server Configuration files that lack the Personal Server User.
- Creating SSH config aliases.
- Updating or repairing the Personal Server tmux Profile.
- Supporting non-Bash remote shells for the first implementation.
- Creating an ADR for this feature.

## Further Notes

- The current domain model has been updated to define Personal Server Connection, `myn connect`, Project, and the expanded Personal Server Configuration contract.
- The relevant ADRs remain intact: Hetzner is the provisioning boundary, Personal Server Bootstrap happens through cloud-init, and PTY-backed Stdio Leases remain separate from this initial connection implementation.
- The largest implementation risk is shell quoting in the remote Bash handoff. Treat remote command construction as part of the deep planning module and test paths with spaces, quotes, punctuation, and IPv6 hosts.
- Another important risk is preserving terminal behavior while still returning the child process exit status. The command should keep process execution and exit-code translation small and directly tested.
