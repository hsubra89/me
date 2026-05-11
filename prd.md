# Personal Server Provisioning PRD

## Problem Statement

`me configure` currently prepares local project roots and an SSH identity, but it does not provision the cloud-hosted development machine that the user expects to use as their Personal Server. The user must manually choose Hetzner resources, create a server, configure SSH access, create a user, install development tools, and remember the resulting server identity and addresses.

The desired outcome is a single interactive `me configure` flow that keeps the existing local setup behavior, then optionally creates a ready-to-use Hetzner Cloud Personal Server using saved Hetzner Credentials and the configured SSH identity.

## Solution

Extend `me configure` with an interactive Personal Server provisioning flow. After local roots and SSH identity are configured and saved, the command checks for saved Hetzner Credentials and a valid Personal Server Configuration. If no valid server is already configured, it prompts for Location, Server Type, Personal Server name, and Personal Server User password, shows a pre-confirmation summary with the install plan and maximum monthly price when available, then creates and bootstraps the server.

The Personal Server is created on Hetzner Cloud through `hcloud-go/v2`, with both IPv4 and IPv6 enabled, a reusable `me-personal-server` firewall, a Hetzner SSH key resource created from the configured local SSH identity, Hetzner labels for visibility, and cloud-init user data that creates the Personal Server User and installs the development baseline. `me configure` waits for Hetzner create actions, waits until root SSH is reachable, then polls for a cloud-init completion marker for up to five minutes. After creation, it saves only the created server ID and assigned IP addresses.

## User Stories

1. As a `me` user, I want `me configure` to keep configuring my local project root, so that existing local setup behavior continues to work.
2. As a `me` user, I want `me configure` to keep configuring my remote project root, so that future remote workflows know where projects belong.
3. As a `me` user, I want `me configure` to keep configuring my SSH identity before cloud provisioning, so that the Personal Server can trust the correct key.
4. As a `me` user, I want Personal Server prompts to run only after roots and SSH identity are configured, so that cloud setup has the prerequisites it needs.
5. As a `me` user, I want missing Hetzner Credentials to skip only Personal Server creation, so that local configuration is still saved.
6. As a `me` user, I want Personal Server creation to require existing saved Hetzner Credentials, so that `configure` does not duplicate cloud auth setup.
7. As a `me` user, I want non-interactive `configure` to skip Personal Server creation, so that scripts do not accidentally create billable cloud resources.
8. As a `me` user, I want interactive `configure` to explicitly ask before creating cloud resources, so that I approve the final plan.
9. As a `me` user, I want `configure` to detect an existing configured Personal Server, so that rerunning configure does not create duplicates.
10. As a `me` user, I want `configure` to verify the saved server ID in Hetzner, so that stale local config does not hide missing cloud state.
11. As a `me` user, I want stale Personal Server Configuration to be cleared only after an interactive prompt, so that local state is not changed unexpectedly.
12. As a `me` user, I want Location to be the canonical term in prompts and output, so that the CLI matches Hetzner's API language.
13. As a `me` user, I want the Location selector to show code and geography, so that I can choose based on both API identity and physical location.
14. As a `me` user, I want the Location selector to default to `ash` when available, so that US-based provisioning starts from the preferred Location.
15. As a `me` user, I want a deterministic fallback Location default, so that the prompt behaves predictably when `ash` is unavailable.
16. As a `me` user, I want only available Server Types in the selected Location, so that I am not offered choices Hetzner cannot create there.
17. As a `me` user, I want only non-deprecated x86_64 Server Types, so that the Personal Server avoids phased-out or unsupported architecture choices.
18. As a `me` user, I want Server Type defaults chosen near 21 EUR monthly gross price, so that the default lands near the intended budget.
19. As a `me` user, I want dedicated compute preferred over shared compute on default ties, so that the default favors stronger isolation.
20. As a `me` user, I want RAM and then vCPU used as later default tie-breaks, so that equally priced choices prefer useful development capacity.
21. As a `me` user, I want Server Type labels to show dedicated/shared, vCPU, RAM, disk, actual storage type, and API name, so that I understand each option.
22. As a `me` user, I want the Server Type selector not to show prices, so that the selector stays focused on capabilities.
23. As a `me` user, I want final confirmation to show a maximum monthly gross EUR price when available, so that I can approve the cost envelope.
24. As a `me` user, I want final confirmation to say price is unavailable when pricing cannot be fetched, so that omission is not confused with free.
25. As a `me` user, I want the selected cloud choices to remain transient until a server is created, so that config does not imply a server exists.
26. As a `me` user, I want Personal Server Configuration stored in its own top-level section, so that server identity is separate from auth, projects, and SSH.
27. As a `me` user, I want Personal Server Configuration to store only server ID, IPv4, and assigned IPv6 address, so that mutable server details remain customizable in Hetzner.
28. As a `me` user, I want the server name default to be API-safe, so that creation does not fail after confirmation.
29. As a `me` user, I want the server name validated before creation, so that invalid names are caught locally.
30. As a `me` user, I want creation to fail if a server with the chosen name already exists, so that the Personal Server remains unambiguous.
31. As a `me` user, I want Hetzner's latest non-deprecated Ubuntu system image selected dynamically, so that new servers use the latest Ubuntu image available from Hetzner.
32. As a `me` user, I want server creation to fail if no Ubuntu image can be found, so that the CLI does not silently choose a different OS.
33. As a `me` user, I want both IPv4 and IPv6 enabled through server creation public network options, so that both address families work.
34. As a `me` user, I want created Hetzner resources labeled for visibility, so that I can identify resources made by `me`.
35. As a `me` user, I want labels not to auto-adopt missing config, so that old experiments are not silently claimed.
36. As a `me` user, I want the reusable firewall named `me-personal-server`, so that it is clearly created by `me` but not tied only to SSH.
37. As a `me` user, I want a newly created firewall to allow inbound SSH from all IPv4 and IPv6 sources only, so that first access works and no other ports are opened.
38. As a `me` user, I want existing firewall rules left untouched, so that my later firewall customizations are preserved.
39. As a `me` user, I want the Hetzner SSH key resource reused by fingerprint, so that duplicate key resources are avoided.
40. As a `me` user, I want supporting resources left in place if server creation fails, so that reusable resources are not deleted unexpectedly.
41. As a `me` user, I want a Personal Server User derived from my local username, so that the remote account feels natural.
42. As a `me` user, I want invalid local usernames normalized or prompted for, so that Linux user creation is reliable.
43. As a `me` user, I want the Personal Server name derived from the Personal Server User, so that naming is deterministic.
44. As a `me` user, I want the Personal Server User to use Bash initially, so that bootstrap shell setup is predictable.
45. As a `me` user, I want the Personal Server User to have sudo access with a required password, so that privileged commands are intentional.
46. As a `me` user, I want the Personal Server User password confirmed, non-empty, hashed locally, and not saved, so that sudo works without storing plaintext secrets.
47. As a `me` user, I want root SSH and user SSH to both work with the configured SSH identity, so that recovery and normal access are both available.
48. As a `me` user, I want `configure` not to edit my SSH config, so that aliases are not managed implicitly.
49. As a `me` user, I want the remote project root created exactly under the Personal Server User's home, so that the configured remote path exists.
50. As a `me` user, I want remote roots with spaces handled correctly, so that existing configure validation remains meaningful.
51. As a `me` user, I want no project cloning or syncing during provisioning, so that server creation does not move source code unexpectedly.
52. As a `me` user, I want no dotfiles copied during provisioning, so that secrets and personal shell configuration are not transferred implicitly.
53. As a `me` user, I want system security updates applied after creation, so that the new server starts patched.
54. As a `me` user, I want unattended security upgrades enabled, so that the Personal Server continues receiving security fixes.
55. As a `me` user, I want bootstrap to reboot automatically if required, so that security updates can fully apply.
56. As a `me` user, I want reboot downtime to count against the bootstrap timeout, so that broken reboot loops do not hide failure.
57. As a `me` user, I want Docker Engine and Docker Compose installed, so that container-based development works.
58. As a `me` user, I want the Personal Server User in the docker group, so that I can use Docker without sudo.
59. As a `me` user, I want Docker group root-equivalent access treated explicitly, so that the sudo-password boundary is not overstated.
60. As a `me` user, I want Homebrew installed for the Personal Server User, so that development tools are user-owned.
61. As a `me` user, I want `tmux`, `jq`, `git`, `gh`, `rustup`, `go`, and `nvm` installed through Homebrew, so that the baseline toolchain is ready.
62. As a `me` user, I want `rustup` installed without an automatic Rust toolchain, so that Rust toolchain choice remains mine.
63. As a `me` user, I want nvm initialized and latest LTS Node.js installed and defaulted, so that Node tooling is ready.
64. As a `me` user, I want Codex installed with the nvm-managed LTS npm, so that it belongs to the user environment.
65. As a `me` user, I want Claude Code installed as the Personal Server User, so that its profile changes land in the user account.
66. As a `me` user, I want coding agent install failures reported as partial failures, so that the server can still be considered usable when core setup succeeds.
67. As a `me` user, I want hard failures for security setup, user creation, SSH, remote root, Homebrew, Docker, core tools, nvm, and Node, so that essential setup is not reported as successful when broken.
68. As a `me` user, I want Git identity copied from local global config, with repo-local fallback, so that commits on the Personal Server match my local identity where available.
69. As a `me` user, I want partial Git identity values applied and missing values reported, so that available identity setup is not blocked by one missing field.
70. As a `me` user, I want the install plan shown before final confirmation, so that I know what software will be installed.
71. As a `me` user, I want the install plan grouped into system services, Homebrew tools, and coding agents, so that it is easy to scan.
72. As a `me` user, I want Hetzner create actions to finish before SSH polling starts, so that API provisioning failures are distinct from bootstrap failures.
73. As a `me` user, I want bootstrap to run through cloud-init, so that first boot setup is reproducible.
74. As a `me` user, I want `configure` to block until bootstrap completes or times out, so that success means the server is actually ready.
75. As a `me` user, I want bootstrap completion detected through a marker over root SSH, so that the CLI has a concrete readiness signal.
76. As a `me` user, I want a five-minute bootstrap timeout after root SSH first accepts, so that setup does not hang indefinitely.
77. As a `me` user, I want cancellation respected during API calls and polling, so that Ctrl-C behaves predictably.
78. As a `me` user, I want server ID and IPs saved even if bootstrap fails, so that I can inspect the billable server and logs.
79. As a `me` user, I want installed tool versions reported after bootstrap, so that I can verify what was installed.
80. As a `me` user, I want SSH commands printed for user and root over IPv4 and IPv6 with `-i`, so that I can connect immediately.

## Implementation Decisions

- Use the domain terms from the project glossary: Personal Server, Hetzner Credentials, Location, Server Type, Personal Server Configuration, Personal Server Firewall, Personal Server SSH Key, Personal Server User, and Personal Server Bootstrap.
- Respect the accepted ADRs: Hetzner Cloud is the Personal Server provisioning target, and Personal Server Bootstrap runs through cloud-init during server creation.
- Add a separate Personal Server provisioning component instead of expanding local root and SSH identity configuration logic. This component should expose a small orchestration interface that `configure` can call after local config has been saved.
- Add or extend app config schema with a top-level `personalServer` section that stores only:

```json
{
  "serverID": 123456,
  "ipv4": "203.0.113.10",
  "ipv6": "2001:db8::1"
}
```

- Treat Location, Server Type, proposed server name, user password, Git identity inputs, and price calculations as transient provisioning inputs unless a server is actually created.
- Use `github.com/hetznercloud/hcloud-go/v2/hcloud` for Hetzner API access.
- Provisioning must use the saved Hetzner Credentials from auth setup and must respect `HCLOUD_ENDPOINT`.
- Personal Server provisioning must be interactive-only. Non-interactive `configure` continues existing local setup behavior and does not create a server.
- Save local roots and SSH identity before cloud provisioning. Save Personal Server Configuration only after Hetzner server creation succeeds.
- If Personal Server Configuration already stores a server ID, verify it in Hetzner before skipping creation. If it exists, report saved/current server ID and IPs. If it is missing, ask before clearing in interactive mode and fail in non-interactive mode.
- Skip Personal Server creation when Hetzner Credentials are missing or no valid SSH identity is configured; still save local configuration where possible.
- Fetch Locations using `hcloud-go`, show labels as code plus human geography, default to `ash` when available, otherwise first available Location sorted by code.
- Fetch Server Types using `hcloud-go`, filter to non-deprecated x86_64 types explicitly available in the selected Location, and ignore Hetzner `Recommended` metadata for defaulting.
- If a selected Location has no eligible Server Type, return to Location selection in interactive mode.
- Default Server Type to the eligible type closest to 21 EUR monthly gross price, with ties broken by dedicated over shared, then RAM, then vCPU.
- Display Server Type options as dedicated/shared, vCPU, RAM, disk size, actual Hetzner storage type, and Hetzner API name. Do not show price in the selector.
- Fetch live Hetzner pricing for billable resources being provisioned and show one maximum monthly gross EUR total in final confirmation when available. If pricing cannot be fetched, explicitly state that price is unavailable and allow creation.
- Prompt for a Personal Server name, defaulting to `<Personal Server User>-personal-server`, and validate it locally as a DNS-label-style API-safe value.
- Before creation, check whether a Hetzner server with the chosen name already exists and fail rather than reusing or auto-suffixing.
- Select Hetzner's latest non-deprecated Ubuntu system image by highest Ubuntu version. Fail before creation if no Ubuntu image can be found.
- Create the server with both IPv4 and IPv6 enabled through server creation public network options.
- Wait for Hetzner create actions to finish before root SSH polling begins.
- Label created Hetzner server, firewall, and SSH key resources with `managed_by=me` and `role=personal_server`. Labels are for visibility only and are not an adoption mechanism.
- Create or reuse a firewall named `me-personal-server`. If it must be created, create it with inbound TCP 22 from all IPv4 and IPv6 sources and no other inbound access. If it already exists, leave rules untouched.
- Create or reuse a Hetzner SSH key resource by public key fingerprint. The same configured SSH identity must authorize both root and the Personal Server User.
- Do not automatically clean up supporting resources if server creation fails.
- Normalize the local username to a Personal Server User using lowercase letters, digits, and hyphens. If normalization cannot produce a valid Linux username, prompt for one.
- Create the Personal Server User with Bash as the login shell, membership in `sudo` and `docker`, and a non-empty confirmed password. Sudo must require that password.
- Hash the Personal Server User password locally using SHA-512 crypt with a random salt and send only the hash to cloud-init. Do not store the password.
- Keep key-based root SSH enabled after bootstrap.
- Do not create or edit SSH config aliases.
- Render cloud-init user data through a typed Go render function using a YAML library.
- Cloud-init must create the Personal Server User, authorize the SSH key, create the configured remote project root exactly under the user's home, install security updates, enable unattended security upgrades, reboot automatically when required, install Docker Engine and Docker Compose from Docker's official apt repository, install Homebrew for the Personal Server User, install Homebrew tools, initialize nvm, install latest LTS Node.js, install coding agents, configure available Git identity values, and write the bootstrap completion marker.
- Homebrew and Homebrew-installed tools must belong to the Personal Server User, not root.
- Homebrew tools are `tmux`, `jq`, `git`, `gh`, `rustup`, `go`, and `nvm`.
- Do not install a Rust toolchain during bootstrap.
- Initialize Homebrew `nvm` for the Personal Server User, install latest LTS Node.js, and set it as default.
- Install Codex as the Personal Server User using the nvm-managed LTS npm.
- Install Claude Code as the Personal Server User using the official installer script.
- Install GitHub CLI only; do not authenticate `gh`.
- Read Git identity from global Git config first and repo-local Git config second. Set whichever of `user.name` and `user.email` are available and report skipped missing values.
- Do not copy local dotfiles or shell configuration beyond required Homebrew, nvm, and Git identity setup.
- Do not clone or sync local projects during provisioning.
- The bootstrap marker must include status, timestamp, reboot information, installed tool versions, and partial failure information.
- System update/security setup, user creation, SSH authorization, remote project root creation, Homebrew install, Docker install, core Homebrew tools, nvm setup, and LTS Node/npm setup are hard bootstrap failures.
- Claude Code and Codex install failures are soft failures. Report them as partial failures without failing the whole bootstrap.
- `me configure` must poll over root SSH for the bootstrap marker. The five-minute bootstrap timer starts after root SSH first accepts and includes automatic reboot downtime.
- Hetzner API calls, root SSH polling, and bootstrap polling must respect command cancellation.
- After creation, print user and root SSH commands for IPv4 and IPv6, IPv4 first, including `-i` with the configured SSH identity.
- After bootstrap completion, print installed tool versions from the completion marker.

## Testing Decisions

- Tests should verify external behavior and durable contracts: config contents, prompts/defaults, selected API inputs, rendered cloud-init behavior, polling outcomes, final output, and failure handling. They should avoid asserting incidental helper structure.
- Add focused unit tests for config serialization so `personalServer` is omitted when empty and stores only `serverID`, `ipv4`, and assigned `ipv6` when present.
- Add prompt/default tests following the existing fake prompter style: Location defaulting to `ash`, fallback sorting, Server Type labels, Server Type default tie-breaks, server name default/validation, password confirmation, final confirmation, missing credentials skip, and existing server skip.
- Add provisioning service tests with a fake Hetzner client boundary. Cover Location listing, Server Type filtering by explicit availability metadata, deprecated and non-x86_64 exclusions, latest Ubuntu image selection, existing server name failure, firewall create/reuse behavior, SSH key create/reuse by fingerprint, labels, public network options, create action waiting, and saved server identity.
- Add pricing tests with fake pricing data. Cover gross EUR total display, unavailable pricing fallback, and absence of price in the Server Type selector.
- Add cloud-init renderer tests that parse the generated YAML rather than comparing a fragile full string. Cover user creation, SSH authorization, password hash placement, sudo-with-password, docker group, remote project root with spaces, Docker apt repository setup, Homebrew user ownership, nvm initialization, LTS Node install, Codex and Claude Code commands, Git identity handling, completion marker, and hard/soft failure behavior.
- Add Personal Server User normalization tests for local usernames with uppercase, spaces, domain/path prefixes, invalid characters, leading digits, and empty normalized output.
- Add password hashing tests that verify non-empty validation, confirmation behavior, SHA-512 crypt format, randomized salt behavior, and no password persistence.
- Add SSH/bootstrap polling tests with a fake SSH runner. Cover root SSH retry, timer starting after first root SSH success, temporary SSH disconnects during reboot, timeout, cancellation, marker success, marker hard failure, and marker partial failure.
- Add integration-style command tests around `configure` using existing command test patterns. Cover two-phase config save, missing credentials, no SSH identity, stale saved server ID, create declined, create succeeds, create succeeds but bootstrap fails, and non-interactive skip behavior.
- Existing tests for auth, hcloud config parsing, SSH identity selection/generation, config save permissions, and configure path normalization are prior art and should remain passing.

## Out of Scope

- Non-interactive Personal Server creation flags.
- Managing or importing Hetzner credentials inside `me configure`.
- Auto-adopting existing Hetzner resources from labels when Personal Server Configuration is missing.
- Replacing an existing configured Personal Server.
- Automatically deleting the server or supporting resources when provisioning or bootstrap fails.
- Resetting an existing `me-personal-server` firewall to SSH-only rules.
- Creating separately managed Primary IP resources.
- Editing the user's SSH config or creating SSH aliases.
- Copying dotfiles, shell configuration, secrets, or GitHub authentication.
- Running `gh auth login` or transferring GitHub credentials.
- Installing a Rust toolchain.
- Cloning, syncing, or mounting projects from the local project root.
- Supporting ARM Server Types in this first version.
- Showing per-resource price breakdowns in the first version.
- Using Hetzner `Recommended` metadata for defaulting.

## Further Notes

- The current codebase already has strong seams for local configuration, SSH identity discovery/generation, Hetzner token validation, hcloud CLI token import, Cobra command testing, and fake prompters. The new work should preserve those seams and avoid making the configure command itself responsible for detailed cloud orchestration.
- The deepest new modules should be the Personal Server provisioning service, hcloud client adapter, cloud-init renderer, Server Type selector/defaulting logic, Personal Server User normalization/password hashing, pricing calculator, and bootstrap poller. Each has meaningful behavior behind a small interface and can be tested without creating real Hetzner resources.
- The command should be honest about billable resources: show the maximum monthly price when the live pricing API is available, explicitly say price is unavailable otherwise, and always require final confirmation before creation.
- The server is intentionally saved even if bootstrap fails or times out because the Hetzner server exists, may be billable, and may contain the logs needed to debug setup.
