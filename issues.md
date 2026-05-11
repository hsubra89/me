# Personal Server Provisioning Issues

These issues are written locally instead of being published to an issue tracker. They are dependency-ordered tracer-bullet slices derived from `prd.md`, `CONTEXT.md`, and the accepted ADRs.

## Proposed Breakdown

1. **Persist and gate Personal Server Configuration**
   - **Status**: Done
   - **Type**: AFK
   - **Blocked by**: None
   - **User stories covered**: 1-8, 25-27, 48, 51-52

2. **Verify existing Personal Server state**
   - **Status**: Done
   - **Type**: AFK
   - **Blocked by**: Issue 1
   - **User stories covered**: 9-11, 34-35

3. **Preview Location and Server Type selection**
   - **Status**: Done
   - **Type**: AFK
   - **Blocked by**: Issue 1
   - **User stories covered**: 12-24, 30-32

4. **Collect Personal Server creation inputs and confirmation**
   - **Status**: Done
   - **Type**: AFK
   - **Blocked by**: Issue 3
   - **User stories covered**: 28-29, 41-46, 68-71

5. **Render Personal Server Bootstrap cloud-init**
   - **Status**: Done
   - **Type**: AFK
   - **Blocked by**: Issue 4
   - **User stories covered**: 44-67, 70-71, 73

6. **Create Hetzner resources and save server identity**
   - **Status**: Done
   - **Type**: AFK
   - **Blocked by**: Issues 4 and 5
   - **User stories covered**: 30-40, 47, 72, 78

7. **Poll bootstrap completion and report access**
   - **Status**: Done
   - **Type**: AFK
   - **Blocked by**: Issue 6
   - **User stories covered**: 55-56, 74-80

8. **Harden failure handling and cancellation**
   - **Status**: Done
   - **Type**: AFK
   - **Blocked by**: Issues 6 and 7
   - **User stories covered**: 24, 40, 66-67, 77-78

9. **Validate Personal Server provisioning against live Hetzner**
   - **Status**: Done
   - **Type**: HITL
   - **Blocked by**: Issues 1-8
   - **User stories covered**: All

10. **Document Personal Server provisioning behavior**
   - **Status**: Done
   - **Type**: AFK
   - **Blocked by**: Issues 1-9
   - **User stories covered**: All

## Issue 1: Persist and gate Personal Server Configuration

## What to build

Add the Personal Server Configuration contract and connect it to `me configure` without creating any cloud resources yet. The command should keep its existing local project root, remote project root, and SSH identity behavior, save those values before any Personal Server branch runs, and then decide whether Personal Server creation is eligible.

This slice establishes the safe defaults: Personal Server creation is interactive-only, requires existing Hetzner Credentials, requires a valid SSH identity, and is skipped without blocking local configuration when prerequisites are missing.

## Acceptance criteria

- [x] The config schema supports a top-level Personal Server Configuration with only `serverID`, `ipv4`, and assigned `ipv6`.
- [x] Empty Personal Server Configuration is omitted from saved config.
- [x] Existing local root, remote root, and SSH identity configuration behavior still works.
- [x] Local roots and SSH identity are saved before the Personal Server branch can run.
- [x] Missing Hetzner Credentials do not fail all of `configure`; they skip Personal Server creation with a clear message.
- [x] Missing or invalid SSH identity skips Personal Server creation with a clear message.
- [x] Non-interactive `configure` does not create a Personal Server.
- [x] `configure` does not edit SSH config aliases, clone projects, sync projects, or copy dotfiles.

## Blocked by

None - can start immediately

## Issue 2: Verify existing Personal Server state

**Status**: Done

## What to build

When Personal Server Configuration already contains a server ID, use saved Hetzner Credentials to verify that server before deciding to skip creation. If the server exists, report the server ID and IP addresses instead of prompting for a new server. If the server is missing, ask before clearing stale configuration in interactive mode and fail in non-interactive mode.

This slice prevents duplicate Personal Servers while avoiding stale local state that blocks future provisioning forever.

## Acceptance criteria

- [x] A saved server ID is looked up in Hetzner before creation is skipped.
- [x] If the saved server exists, `configure` reports the server ID and current/saved IP addresses and does not create another server.
- [x] If the saved server is missing, interactive `configure` asks before clearing Personal Server Configuration.
- [x] If the saved server is missing, non-interactive `configure` fails with a clear message and does not clear config.
- [x] Hetzner labels are not used to auto-adopt resources when Personal Server Configuration is missing.
- [x] Verification respects the configured Hetzner endpoint override.

## Blocked by

- Issue 1

## Issue 3: Preview Location and Server Type selection

**Status**: Done

## What to build

Add the interactive pre-creation selection flow for Location and Server Type, ending in a safe declined-create path. Use Hetzner data to list Locations, default to `ash` when available, and show Location labels with the code plus human geography. After Location selection, list only eligible Server Types for that Location.

The Server Type selector should use explicit Hetzner availability metadata, exclude deprecated and non-x86_64 types, ignore Hetzner `Recommended` metadata for defaulting, and default to the option closest to 21 EUR monthly gross price with the agreed tie-breaks.

## Acceptance criteria

- [x] Location prompt uses "Location" terminology throughout.
- [x] Location options show Hetzner code plus human geography and use the code for API calls.
- [x] Location default is `ash` when available.
- [x] Location fallback default is the first available Location sorted by code.
- [x] Server Type options include only non-deprecated x86_64 types explicitly available in the selected Location.
- [x] If a Location has no eligible Server Type, interactive `configure` returns to Location selection.
- [x] Server Type default is closest to 21 EUR monthly gross price, breaking ties by dedicated over shared, then RAM, then vCPU.
- [x] Server Type labels show dedicated/shared, vCPU, RAM, disk size, actual storage type, and API name.
- [x] Server Type selector does not show prices.
- [x] If the user declines creation later, Location and Server Type are not saved.

## Blocked by

- Issue 1

## Issue 4: Collect Personal Server creation inputs and confirmation

**Status**: Done

## What to build

Complete the interactive pre-create flow by collecting the Personal Server User, server name, sudo password, local Git identity values, install plan, and final confirmation. The Personal Server User should be derived from the current local username as lowercase letters, digits, and hyphens, with a prompt fallback when normalization cannot produce a valid username. The server name should default to `<Personal Server User>-personal-server` and be validated locally as an API-safe DNS-label-style value.

The final confirmation should show the selected Location, selected Server Type, server name, Personal Server User, SSH/firewall/network summary, grouped install plan, and a single maximum monthly gross EUR total when live pricing can be fetched. If pricing cannot be fetched, it should explicitly say price is unavailable and still allow creation.

## Acceptance criteria

- [x] Personal Server User normalization handles uppercase, spaces, prefixes, invalid characters, leading digits, and empty normalized output.
- [x] Invalid normalized users cause an interactive prompt for a valid Personal Server User.
- [x] Server name defaults to `<Personal Server User>-personal-server`.
- [x] Server name validation catches invalid names before API creation.
- [x] Personal Server User password is prompted, confirmed, required to be non-empty, hashed locally as SHA-512 crypt, and never saved.
- [x] Available local Git identity values are read from global Git config first and repo-local Git config second.
- [x] Missing Git identity values are reported as skipped; available values are included for bootstrap.
- [x] The install plan is shown before final creation confirmation and grouped into system services, Homebrew tools, and coding agents.
- [x] Final confirmation shows one maximum monthly gross EUR total when pricing is available.
- [x] Final confirmation explicitly says price is unavailable when pricing cannot be fetched.
- [x] Declining final confirmation creates no cloud resources and saves no transient Personal Server choices.

## Blocked by

- Issue 3

## Issue 5: Render Personal Server Bootstrap cloud-init

**Status**: Done

## What to build

Build the typed Personal Server Bootstrap renderer using a YAML library. The renderer should produce cloud-init user data that creates the Personal Server User, keeps key-based root SSH enabled, authorizes the configured SSH identity for root and the user, creates the configured remote project root exactly under the user's home, installs the required system services and user-owned development tools, configures available Git identity values, and writes a completion marker.

Cloud-init should hard-fail essential setup and report coding agent failures as partial failures. It should install security updates, enable unattended security upgrades, and reboot automatically if required.

## Acceptance criteria

- [x] Renderer accepts typed inputs for user, password hash, SSH public key, remote project root, Git identity values, and bootstrap tool plan.
- [x] Generated cloud-init parses as valid YAML in tests.
- [x] Personal Server User is created with Bash, `sudo`, and `docker` group membership.
- [x] Sudo requires the configured password.
- [x] Root SSH remains key-based and enabled after bootstrap.
- [x] Configured SSH public key authorizes both root and the Personal Server User.
- [x] Remote project root is created exactly, including spaces, and owned by the Personal Server User.
- [x] Security updates are applied, unattended security upgrades are enabled, and reboot happens automatically when required.
- [x] Docker Engine and Docker Compose are installed from Docker's official apt repository.
- [x] Homebrew is installed and owned by the Personal Server User.
- [x] Homebrew installs `tmux`, `jq`, `git`, `gh`, `rustup`, `go`, and `nvm`.
- [x] No Rust toolchain is installed.
- [x] Homebrew `nvm` is initialized for the Personal Server User, latest LTS Node.js is installed, and it is set as default.
- [x] Codex is installed with the nvm-managed LTS npm as the Personal Server User.
- [x] Claude Code is installed as the Personal Server User with the official installer.
- [x] GitHub CLI is installed but not authenticated.
- [x] No local dotfiles, shell configuration, GitHub credentials, or project contents are copied.
- [x] Completion marker includes status, timestamp, reboot information, installed tool versions, and partial failure information.
- [x] System update/security setup, user creation, SSH authorization, remote root creation, Homebrew, Docker, core Homebrew tools, nvm, and LTS Node/npm setup are hard failures.
- [x] Codex and Claude Code installation failures are reported as partial failures and do not fail the whole bootstrap.

## Blocked by

- Issue 4

## Issue 6: Create Hetzner resources and save server identity

**Status**: Done

## What to build

Connect the confirmed creation flow to Hetzner Cloud. Create or reuse supporting resources, select the latest Ubuntu image, create the server with public IPv4 and IPv6 enabled, attach the rendered Personal Server Bootstrap user data, wait for Hetzner create actions, and save Personal Server Configuration with server ID and assigned addresses once the server exists.

This slice should create a server but does not need to wait for bootstrap completion beyond handing off user data. Bootstrap polling and final ready-state reporting are handled by a later issue.

## Acceptance criteria

- [x] Creation checks for an existing Hetzner server with the chosen name and fails instead of reusing or suffixing.
- [x] Latest non-deprecated Ubuntu system image is selected by highest Ubuntu version.
- [x] Creation fails clearly if no Ubuntu image can be found.
- [x] Firewall named `me-personal-server` is created or reused.
- [x] Newly created firewall allows inbound TCP 22 from all IPv4 and IPv6 sources and no other inbound access.
- [x] Existing firewall rules are left untouched.
- [x] Hetzner SSH key is created or reused by public key fingerprint.
- [x] Server, firewall, and SSH key created by `me` are labeled `managed_by=me` and `role=personal_server`.
- [x] Server is created with both IPv4 and IPv6 enabled through server creation public network options.
- [x] Server create request includes the selected Location, selected Server Type, latest Ubuntu image, SSH key, firewall, labels, and rendered cloud-init user data.
- [x] Hetzner create actions are waited on before returning from the create step.
- [x] Personal Server Configuration is saved with server ID, IPv4 address, and assigned IPv6 address after server creation succeeds.
- [x] Supporting resources are not automatically cleaned up if server creation fails after they are created.
- [x] Creation respects the configured Hetzner endpoint override.

## Blocked by

- Issue 4
- Issue 5

## Issue 7: Poll bootstrap completion and report access

**Status**: Done

## What to build

After server creation, wait until root SSH first accepts the configured SSH identity, then poll for the Personal Server Bootstrap completion marker over root SSH. The bootstrap timer starts after first root SSH success and runs for at most five minutes, including any automatic reboot downtime. When bootstrap completes, report installed tool versions and SSH commands for both root and the Personal Server User over IPv4 and IPv6.

If bootstrap hard-fails or times out, keep the saved server identity and report enough information for inspection.

## Acceptance criteria

- [x] Root SSH polling uses the configured SSH identity.
- [x] Five-minute bootstrap timeout starts only after root SSH first accepts.
- [x] Temporary SSH disconnects during reboot are tolerated.
- [x] Automatic reboot downtime counts against the five-minute timeout.
- [x] Completion marker is parsed for status, timestamp, reboot information, tool versions, and partial failures.
- [x] Hard failure marker is reported as bootstrap failure while retaining saved server identity.
- [x] Timeout is reported as bootstrap failure while retaining saved server identity.
- [x] Partial coding agent failures are reported without failing the whole bootstrap.
- [x] Successful bootstrap prints installed tool versions from the marker.
- [x] Final output prints user and root SSH commands over IPv4 and IPv6, IPv4 first.
- [x] Printed SSH commands include `-i` with the configured SSH identity.

## Blocked by

- Issue 6

## Issue 8: Harden failure handling and cancellation

**Status**: Done

## What to build

Make the full Personal Server provisioning path robust under cancellation and partial failures. Hetzner API calls, action waiting, root SSH polling, and bootstrap polling should respect command cancellation. Failure modes should preserve the agreed state boundaries: local config remains saved, supporting resources are not automatically cleaned up, created servers are saved even if bootstrap fails, and pricing failure does not block creation.

## Acceptance criteria

- [x] Hetzner API calls respect command cancellation.
- [x] Hetzner action waiting respects command cancellation.
- [x] Root SSH polling respects command cancellation.
- [x] Bootstrap marker polling respects command cancellation.
- [x] Cancellation before server creation does not save Personal Server Configuration.
- [x] Cancellation after server creation preserves saved server ID and IP addresses.
- [x] Bootstrap failure or timeout preserves saved server ID and IP addresses.
- [x] Supporting resources are not automatically cleaned up on cancellation or failure.
- [x] Pricing fetch failure explicitly reports price unavailable and still permits final confirmation.
- [x] Non-interactive mode never creates a Personal Server, including under stale config scenarios.

## Blocked by

- Issue 6
- Issue 7

## Issue 9: Validate Personal Server provisioning against live Hetzner

**Status**: Done

## What to build

Add and run a guarded live validation path for the completed Personal Server provisioning flow. The validation must read a Hetzner API key from `.env.local` as `HETZNER_API_KEY`, map it into the CLI's Hetzner Credentials path for the test run, create an isolated test Personal Server through the real CLI flow, wait for Personal Server Bootstrap completion, verify the server configuration and init setup commands on the live machine, and clean up the live test server afterwards.

This validation must not commit `.env.local`, print the API key, or rely on a developer's real `me` config. It should use an isolated temporary config/home/SSH identity and a unique test server name so the live test does not collide with an existing Personal Server.

## Acceptance criteria

- [x] `.env.local` is loaded only for live validation and `HETZNER_API_KEY` is never printed, committed, or copied into tracked files.
- [x] Live validation fails fast with a clear skip/error message when `.env.local` or `HETZNER_API_KEY` is missing.
- [x] Live validation uses isolated temporary `me` config, home, SSH identity, and server name values.
- [x] Live validation uses the real Hetzner API through the implemented CLI/provisioning path, not the fake Hetzner client.
- [x] Live validation creates a Personal Server with a test-specific name and the standard `managed_by=me` / `role=personal_server` labels.
- [x] Live validation verifies the selected Location and Server Type are accepted by Hetzner and the server is created with both IPv4 and assigned IPv6.
- [x] Live validation verifies the Hetzner SSH key and Personal Server Firewall behavior against live resources without deleting any pre-existing user-managed firewall or SSH key.
- [x] Live validation waits for Hetzner create actions, root SSH readiness, and the Personal Server Bootstrap completion marker.
- [x] Live validation verifies root SSH and Personal Server User SSH both work with the configured test SSH identity.
- [x] Live validation verifies the Personal Server User exists, uses Bash, belongs to `sudo` and `docker`, and has the configured remote project root owned by that user.
- [x] Live validation verifies Docker Engine and the compose plugin are installed and usable enough to report versions.
- [x] Live validation verifies Homebrew is installed for the Personal Server User and reports versions for `tmux`, `jq`, `git`, `gh`, `rustup`, `go`, `node`, `npm`, Codex, and Claude Code when available.
- [x] Live validation verifies nvm defaulted the latest LTS Node.js for the Personal Server User.
- [x] Live validation verifies available local Git identity values were configured on the Personal Server User.
- [x] Live validation verifies hard bootstrap failures fail the live run and coding agent failures are reported as partial failures when they occur.
- [x] Live validation deletes the live test server at the end of the run, including on failure when a server was created.
- [x] Live validation reports any supporting resources it created and intentionally leaves behind according to product behavior.
- [x] Live validation documents the exact command to run, expected cost/risk, and cleanup behavior for maintainers.

## Blocked by

- Issue 1
- Issue 2
- Issue 3
- Issue 4
- Issue 5
- Issue 6
- Issue 7
- Issue 8

## Issue 10: Document Personal Server provisioning behavior

**Status**: Done

## What to build

Update user-facing development documentation for the new `me configure` Personal Server behavior. The documentation should explain the prerequisite auth command, interactive-only creation, saved config shape, Location and Server Type selection, final confirmation, installed software, SSH access, existing-server behavior, and known out-of-scope behaviors.

## Acceptance criteria

- [x] Documentation explains that `me auth hetzner` must be completed before Personal Server creation.
- [x] Documentation explains that non-interactive `configure` does not create a Personal Server.
- [x] Documentation shows the top-level Personal Server Configuration shape with server ID, IPv4, and assigned IPv6 only.
- [x] Documentation describes Location and Server Type selection using project glossary terms.
- [x] Documentation lists the grouped install plan: system services, Homebrew tools, and coding agents.
- [x] Documentation explains that Git identity is copied from available local config values but GitHub auth is not configured.
- [x] Documentation explains that Docker group membership is root-equivalent access even though sudo requires a password.
- [x] Documentation describes the final SSH commands and the use of the configured SSH identity.
- [x] Documentation states that existing firewall rules are not reset and supporting resources are not automatically cleaned up on failure.
- [x] Documentation states that projects, dotfiles, SSH aliases, GitHub credentials, and Rust toolchains are out of scope.

## Blocked by

- Issue 1
- Issue 2
- Issue 3
- Issue 4
- Issue 5
- Issue 6
- Issue 7
- Issue 8
- Issue 9
