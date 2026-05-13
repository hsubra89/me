# Myn CLI

**Myn** provisions and operates the user's personal development environment across their local machine and a cloud server.

## Language

**Myn**:
The CLI that provisions and operates the user's personal development environment across their local machine and a cloud server.
_Avoid_: generic personal toolbox

**myn**:
The command name for **Myn**, pronounced "mine".
_Avoid_: generic command aliases

**Personal Server**:
A Hetzner Cloud server that **Myn** provisions for the user's cloud-hosted development environment.
_Avoid_: remote machine, default server, Myn Server

**Hetzner Credentials**:
A saved Read & Write Hetzner Cloud API token used by **Myn** for cloud provisioning.
_Avoid_: hcloud context, cloud login

**Location**:
A Hetzner Cloud location where a **Personal Server** can be created.
_Avoid_: region, datacenter

**Personal Server Configuration**:
The saved connection identity and addresses of a created **Personal Server**.
_Avoid_: Hetzner config, remote config

**Personal Server Firewall**:
A reusable Hetzner Cloud firewall named `myn-personal-server` that is created by **Myn** for **Personal Server** instances and editable by the user after creation.
_Avoid_: SSH-only firewall, generated firewall

**Personal Server SSH Key**:
A Hetzner Cloud SSH key resource created from the configured local SSH identity for **Personal Server** access.
_Avoid_: remote key, uploaded key

**Mosh Access**:
UDP-based interactive shell access installed and opened by default for a **Personal Server**.
_Avoid_: optional roaming shell, manual UDP access

**Personal Server Connection**:
An SSH-backed interactive project-scoped tmux session on the configured **Personal Server**.
_Avoid_: raw SSH session, Mosh session, remote shell

**myn connect**:
The canonical command for starting a **Personal Server Connection**, with `myn c` as its short alias.
_Avoid_: myn ssh, myn tmux, myn shell

**Project**:
A connection target rooted at the configured project root itself or at a top-level directory directly under it.
_Avoid_: Git repository, workspace, checkout

**Personal Server User**:
A Linux user account created on a **Personal Server** from a normalized form of the current local username.
_Avoid_: remote user, cloud user

**Server Type**:
A non-deprecated Hetzner Cloud x86_64 server type available in the selected **Location** for a **Personal Server**.
_Avoid_: size, plan, instance type

**Personal Server Bootstrap**:
The first-boot cloud-init process that creates the **Personal Server User** and installs the expected development tools.
_Avoid_: setup script, install script

**Personal Server tmux Profile**:
The standard tmux behavior that **Myn** installs for the **Personal Server User**.
_Avoid_: copied tmux dotfile, local tmux config sync

**Idle Lease**:
A renewable runtime claim that keeps a **Personal Server** awake while there is recent evidence of user-triggered work.
_Avoid_: lock, keepalive, inhibitor

**Stdio Lease**:
An **Idle Lease** for an interactive command whose activity is evidenced by recent terminal input or output.
_Avoid_: prompt lease, terminal lock

## Relationships

- A **Personal Server** trusts the configured SSH identity for both root and user login.
- A **Personal Server** keeps key-based root SSH enabled after bootstrap.
- A **Personal Server Connection** uses SSH rather than **Mosh Access**.
- A **Personal Server Connection** attaches the **Personal Server User** to an existing tmux session for the target project when one exists, otherwise it creates a new tmux session for that project.
- A **Personal Server Connection** names tmux sessions from the remote **Project** root path using a stable `myn-` prefixed tmux-safe name.
- A **Personal Server Connection** tmux session name lowercases the remote **Project** root path, keeps ASCII letters and digits, converts every other character run to one hyphen, trims edge hyphens, prefixes `myn-`, and uses `myn-project` if the normalized project path is empty.
- A **Personal Server Connection** fails rather than falling back to plain SSH when tmux is unavailable.
- A **Personal Server Connection** relies on the **Personal Server User** login shell PATH to find tmux.
- A **Personal Server Connection** runs its remote tmux handoff through Bash login-shell command evaluation.
- A **Personal Server Connection** trusts the saved **Personal Server Configuration** and does not require **Hetzner Credentials** or Hetzner API verification before connecting.
- A **Personal Server Connection** uses the saved IPv4 address first and falls back to the saved IPv6 address only when IPv4 is unavailable.
- A **Personal Server Connection** passes the **Personal Server User** to SSH separately with `-l` and passes IPv6 literals as unbracketed host arguments.
- A **Personal Server Connection** does not create an **Idle Lease** in the initial implementation.
- A **Personal Server Connection** requires terminal-backed stdin and stdout.
- A **Personal Server Connection** requests one SSH TTY allocation.
- A **Personal Server Connection** always passes the configured SSH identity explicitly to SSH.
- A **Personal Server Connection** uses SSH `StrictHostKeyChecking=accept-new`.
- A **Personal Server Connection** preserves the SSH/tmux process exit status.
- A **Personal Server Connection** validates saved configuration, local project root existence, and SSH identity existence before attempting SSH.
- A **Personal Server Connection** stays quiet on successful handoff and prints clear messages only for local validation failures.
- A **Personal Server Connection** derives the target **Project** from the first path segment under the configured local project root, not from the Git repository root.
- A **Personal Server Connection** maps local paths lexically under the configured local project root and does not resolve symlink targets for project containment.
- `myn connect` accepts no path arguments in the initial implementation and maps from the current working directory.
- When `myn connect` is run from the configured local project root itself, the configured local project root is the target **Project**.
- A **Personal Server Connection** starts in the exact mapped remote directory when it exists, otherwise the remote **Project** root when it exists, otherwise the **Personal Server User** home directory.
- A **Personal Server Connection** treats only existing remote directories as valid starting directories.
- A **Personal Server Connection** does not create missing remote project directories.
- Directory fallback affects only newly created **Personal Server Connection** tmux sessions; existing project sessions are attached as-is.
- `myn connect` fails with clear messaging when run outside the configured local project root.
- `myn configure` does not modify the user's SSH config for **Personal Server** aliases.
- All user-visible namespaces use `myn`, including config paths, environment variables, runtime lease directories, cloud resource names, Hetzner labels, bootstrap files, shell profile files, and local development launchers.
- Documentation uses **Myn** in prose and `myn` for the command name; pronunciation belongs in introductory documentation, not command help.
- Planning and decision documents keep their technical rationale but use the **Myn** namespace consistently.
- **Myn** uses the command structure `auth hetzner`, `configure`, `connect` (`c`), `idle status`, `run`, and `version`.
- **Personal Server** prompts run only after local roots and the SSH identity are configured.
- **Personal Server** creation is skipped unless a valid SSH identity is configured.
- **Personal Server** provisioning does not clone or sync projects from the local root.
- **Personal Server Bootstrap** creates the configured remote project root exactly, including spaces, owned by the **Personal Server User**.
- A **Personal Server** can only be provisioned after **Hetzner Credentials** already exist.
- **Personal Server** provisioning respects the `HCLOUD_ENDPOINT` override.
- A **Personal Server** is created in exactly one **Location**.
- A **Location** selector shows the Hetzner code plus human geography and uses the code for API calls.
- The **Location** selector defaults to `ash` when Hetzner offers it, otherwise the first available **Location** sorted by code.
- A **Personal Server** is created with exactly one non-deprecated x86_64 **Server Type** available in its **Location**, based on explicit Hetzner availability metadata.
- If the selected **Location** has no eligible **Server Type**, interactive `configure` returns to **Location** selection.
- Hetzner `Recommended` metadata is ignored for **Location** and **Server Type** defaulting.
- A **Personal Server** defaults its API-safe name to `<Personal Server User>-personal-server`.
- A **Personal Server** default server name does not include `myn`; supporting cloud resources use `myn` because they are product-managed automation artifacts.
- A **Personal Server** name is validated locally as a DNS-label-style value before creation.
- A **Personal Server** is not created when a Hetzner server with the chosen name already exists.
- A **Personal Server** uses Hetzner's latest non-deprecated Ubuntu system image, selected by highest Ubuntu version.
- A **Personal Server** is created with both IPv4 and IPv6 enabled through server creation public network options.
- Hetzner resources created for a **Personal Server** are labeled `managed_by=myn` and `role=personal_server`.
- Hetzner labels are for visibility only and are not used to auto-adopt missing **Personal Server Configuration**.
- **Myn** waits for Hetzner create actions to finish before root SSH and bootstrap polling.
- Hetzner API calls, root SSH polling, and bootstrap polling respect command cancellation.
- A **Personal Server Configuration** belongs in its own top-level config section.
- A **Personal Server Configuration** stores `serverID`, **Personal Server User**, `ipv4`, and the assigned `ipv6` address, not ongoing desired state for mutable server details.
- A **Personal Server Configuration** is incomplete without the **Personal Server User**.
- **Location**, **Server Type**, and proposed server name selections are transient until a **Personal Server** is created.
- When **Personal Server Configuration** already has a server ID, `myn configure` skips creation by default and reports the saved server ID and IP addresses.
- A saved **Personal Server** server ID is verified against Hetzner before creation is skipped.
- If a saved **Personal Server** server ID is missing in Hetzner, interactive `configure` asks before clearing it and non-interactive `configure` fails.
- **Personal Server** creation is interactive-only; non-interactive `configure` does not create a server.
- Missing **Hetzner Credentials** do not block saving local configuration; they only skip **Personal Server** creation.
- Interactive `configure` asks for explicit final confirmation before creating a **Personal Server**.
- A created **Personal Server** is saved in **Personal Server Configuration** even if the **Personal Server Bootstrap** fails or times out.
- `myn configure` reports user and root SSH commands with `-i` for the configured SSH identity for both IPv4 and IPv6 after **Personal Server** creation, with IPv4 first.
- `myn configure` reports **Mosh Access** commands for the **Personal Server User** after **Personal Server** creation, with IPv4 first and the configured SSH identity passed explicitly.
- `myn configure` reports **Mosh Access** commands only after successful **Personal Server Bootstrap**.
- `myn configure` saves local roots and SSH identity before attempting **Personal Server** creation, then saves **Personal Server Configuration** after server creation succeeds.
- After **Personal Server Bootstrap** completes, `myn configure` reports installed tool versions from the completion marker.
- **Personal Server** provisioning is implemented separately from local root and SSH identity configuration.
- A **Personal Server Firewall** is created by **Myn** with an initially minimal rule set and may be expanded by the user.
- A **Personal Server Firewall** can be reused by multiple **Personal Server** instances over time.
- An existing **Personal Server Firewall** is treated as user-editable and is not reset by **Myn**.
- A newly created **Personal Server Firewall** allows inbound SSH and **Mosh Access** from all IPv4 and IPv6 sources and no other inbound access.
- **Mosh Access** uses Mosh's default UDP `60000-61000` port range.
- Existing **Personal Server Firewalls** are not modified to add **Mosh Access**.
- When `myn configure` reuses an existing **Personal Server Firewall**, it reports that firewall rules are left untouched and **Mosh Access** may require the user-managed UDP rule.
- A **Personal Server SSH Key** is reused when Hetzner already has the same public key fingerprint.
- Supporting resources created for a **Personal Server** are not automatically cleaned up if server creation fails.
- A **Personal Server User** is derived from the current local username as a lowercase letters, digits, and hyphens value.
- If the local username cannot be normalized into a valid **Personal Server User**, `myn configure` prompts for one.
- A **Personal Server User** starts with Bash as the login shell.
- A **Personal Server User** belongs to the `sudo` and `docker` groups.
- A **Personal Server User** has sudo access, but sudo requires a password.
- Membership in the `docker` group is accepted as root-equivalent development-machine access.
- A **Personal Server User** password is collected for provisioning, confirmed, required to be non-empty, sent to cloud-init as a SHA-512 crypt hash, and not saved in config.
- A **Server Type** default is the available option closest to 21 EUR monthly gross price, breaking ties by dedicated over shared, then RAM, then vCPU.
- A **Server Type** label reflects Hetzner's actual storage type and includes the Hetzner API name.
- Final **Personal Server** creation confirmation shows one maximum monthly gross EUR total from Hetzner live pricing for billable resources **Myn** is about to provision when pricing can be fetched, and explicitly says price is unavailable otherwise.
- A **Personal Server Bootstrap** runs through cloud-init during server creation.
- **Personal Server Bootstrap** cloud-init user data is rendered through a typed Go render function using a YAML library.
- A **Personal Server Bootstrap** installs the latest Ubuntu security updates after the **Personal Server** is created, enables unattended security upgrades, and reboots automatically if required.
- **Personal Server Bootstrap** installs `mosh` as an apt-managed system package for **Mosh Access**.
- A **Personal Server Bootstrap** installs Docker Engine and the compose plugin from Docker's official apt repository.
- **Personal Server** creation is not considered complete until the **Personal Server Bootstrap** finishes.
- The **Personal Server Bootstrap** writes a completion marker with status, timestamp, reboot information, and installed tool versions that **Myn** polls for over root SSH.
- The **Personal Server Bootstrap** completion marker reports the installed `mosh` version.
- The **Personal Server Bootstrap** must finish within five minutes after the **Personal Server** first accepts root SSH.
- Automatic reboot downtime counts against the five-minute **Personal Server Bootstrap** timeout.
- The **Personal Server Bootstrap** install plan is shown before final creation confirmation.
- The **Personal Server Bootstrap** install plan groups system services, Homebrew tools, and coding agents separately.
- Homebrew and Homebrew-installed tools belong to the **Personal Server User**, not root.
- **Personal Server Bootstrap** does not copy local dotfiles or shell configuration beyond required Homebrew, nvm, Git identity setup, and the repo-owned **Personal Server tmux Profile**.
- **Personal Server Bootstrap** installs the **Personal Server tmux Profile** for the **Personal Server User** instead of reading the user's local `~/.tmux.conf` at provisioning time.
- Every new **Personal Server** receives the **Personal Server tmux Profile** without an extra prompt or saved configuration flag.
- **Personal Server Bootstrap** writes the **Personal Server tmux Profile** during creation; this does not define a later repair or update policy for user-edited tmux configuration.
- The **Personal Server tmux Profile** is installed as `/home/<Personal Server User>/.tmux.conf`, owned by the **Personal Server User**, not as system-wide tmux configuration.
- The **Personal Server tmux Profile** is a literal snapshot of the intended tmux settings, not a curated subset.
- The repo-owned **Personal Server tmux Profile** is the source of truth after it is seeded; tests and provisioning do not compare against the user's live local `~/.tmux.conf`.
- The **Personal Server Bootstrap** install plan does not call out the **Personal Server tmux Profile** separately from the standard tool setup.
- `tmux` is installed through Homebrew as part of the **Personal Server Bootstrap**.
- `rustup` is installed through Homebrew, but no Rust toolchain is installed during the **Personal Server Bootstrap**.
- Go is installed through the Homebrew `go` formula during the **Personal Server Bootstrap**.
- GitHub CLI is installed through Homebrew, but GitHub authentication is not configured by **Personal Server Bootstrap**.
- Git identity on the **Personal Server** matches available local Git identity values, read from global Git config first and repo-local Git config second, and reports skipped missing values.
- `nvm` is installed through Homebrew, initialized for the **Personal Server User**, and used to install and default the latest LTS Node.js.
- Codex is installed as the **Personal Server User** with the nvm-managed LTS Node.js npm.
- Claude Code is installed as the **Personal Server User** with its official installer script.
- Coding agent installation failures do not fail the **Personal Server Bootstrap**; they are reported as partial failures.
- **Personal Server Bootstrap** hard-fails system update/security setup, user creation, SSH authorization, remote project root creation, Homebrew, Docker, core Homebrew tools, nvm, and LTS Node/npm setup.
- **Personal Server Bootstrap** hard-fails writing the **Personal Server tmux Profile**.
- **Personal Server Bootstrap** hard-fails `mosh` installation because **Mosh Access** is default Personal Server access.
- A **Stdio Lease** is active only while the wrapped command still exists and terminal input or output is recent.
- A quiet **Stdio Lease** can become idle while the wrapped command is still waiting at a prompt.
- A completed **Stdio Lease** is removed on normal wrapper exit.
- An expired **Idle Lease** is stale, not merely idle.
- **Idle Lease** heartbeat is distinct from terminal activity.
- A **Stdio Lease** defaults to a 30 minute idle window.
- `myn idle status` reports **Idle Lease** state without removing lease files.
- A **Stdio Lease** requires a terminal-backed stdin and stdout.

## Example dialogue

> **Dev:** "Should `configure` create the **Personal Server** immediately?"
> **Domain expert:** "Interactive setup can create it after confirmation, but non-interactive setup must opt in explicitly."
> **Dev:** "Should `configure` ask for the Hetzner token?"
> **Domain expert:** "No — run authentication first, then `configure` can use the saved **Hetzner Credentials**."
> **Dev:** "Should the prompt ask for a region?"
> **Domain expert:** "No — Hetzner calls this a **Location**, so the CLI should too."
> **Dev:** "Where should selected cloud details be saved?"
> **Domain expert:** "In a top-level **Personal Server Configuration**, separate from auth, project roots, and SSH identity."
> **Dev:** "Should **Personal Server Configuration** store selected Location and Server Type?"
> **Domain expert:** "No — store only the connection identity and addresses needed to reconnect, because the user can customize the instance after creation."
> **Dev:** "Should cloud choices be saved if final creation is declined?"
> **Domain expert:** "No — they are transient unless a **Personal Server** is actually created."
> **Dev:** "Should `configure` replace an existing configured **Personal Server**?"
> **Domain expert:** "No — skip creation by default and report the saved server ID and IP addresses."
> **Dev:** "Should `configure` trust a saved server ID?"
> **Domain expert:** "No — verify it exists in Hetzner before skipping creation."
> **Dev:** "Should stale **Personal Server Configuration** be cleared automatically?"
> **Domain expert:** "No — ask in interactive mode and fail in non-interactive mode."
> **Dev:** "Should non-interactive `configure` create a **Personal Server** with flags?"
> **Domain expert:** "No — skip non-interactive creation for this flow."
> **Dev:** "Should missing **Hetzner Credentials** fail all of `configure`?"
> **Domain expert:** "No — save local configuration and skip **Personal Server** creation with a clear message."
> **Dev:** "Should `configure` create a **Personal Server** automatically after prompts?"
> **Domain expert:** "No — ask explicitly before creating cloud resources."
> **Dev:** "Should `myn configure` delete the server if bootstrap fails?"
> **Domain expert:** "No — save the created server ID and IP addresses so the user can inspect it."
> **Dev:** "Should the firewall name describe only SSH?"
> **Domain expert:** "No — make it obvious **Myn** created it, but leave room for the user to expand it later."
> **Dev:** "If the firewall already exists, should **Myn** restore SSH-only rules?"
> **Domain expert:** "No — existing rules may be intentional user edits."
> **Dev:** "Should **Mosh Access** be a manual post-provisioning step?"
> **Domain expert:** "No — every newly created **Personal Server** should support **Mosh Access** by default."
> **Dev:** "Should reusing an existing **Personal Server Firewall** ask for another confirmation because **Mosh Access** may need a UDP rule?"
> **Domain expert:** "No — mention the unchanged firewall rules in the creation plan instead of adding another prompt."
> **Dev:** "Should initial SSH access be source-restricted?"
> **Domain expert:** "No — allow SSH from all IPv4 and IPv6 sources."
> **Dev:** "Can we rely only on cloud-init for SSH access?"
> **Domain expert:** "No — create or reuse a **Personal Server SSH Key** for root login and also authorize the key for the user account."
> **Dev:** "Should root SSH be disabled after bootstrap?"
> **Domain expert:** "No — the configured SSH identity should continue to work for both root and the **Personal Server User**."
> **Dev:** "Does 'same name as the local user' mean verbatim?"
> **Domain expert:** "No — create a **Personal Server User** from a Linux-safe normalized local username."
> **Dev:** "Which login shell should the **Personal Server User** start with?"
> **Domain expert:** "Bash; the user can change it later."
> **Dev:** "Should the **Personal Server User** have passwordless sudo?"
> **Domain expert:** "No — sudo must require a password."
> **Dev:** "Where do we save the **Personal Server User** password?"
> **Domain expert:** "We do not save it — collect it during provisioning and send only a password hash to cloud-init."
> **Dev:** "Can the **Personal Server User** password be empty?"
> **Domain expert:** "No — require confirmation and a non-empty password."
> **Dev:** "Should cloud-init receive the plaintext password?"
> **Domain expert:** "No — hash it locally with SHA-512 crypt and send only the hash."
> **Dev:** "Can the user choose any Hetzner server type?"
> **Domain expert:** "No — list only **Server Type** options available in the selected **Location**."
> **Dev:** "Should every **Server Type** label say NVMe SSD?"
> **Domain expert:** "No — display the storage type Hetzner reports for that **Server Type**."
> **Dev:** "Should the **Server Type** selector show price?"
> **Domain expert:** "No — price is only used internally to choose the default."
> **Dev:** "Should final confirmation show the monthly price?"
> **Domain expert:** "Yes — show the maximum possible monthly price for the resources **Myn** is about to provision."
> **Dev:** "Should **Myn** SSH into the server to install tools?"
> **Domain expert:** "No — install them through the **Personal Server Bootstrap** and wait for cloud-init to finish."
> **Dev:** "Can `configure` return once Hetzner accepts the create request?"
> **Domain expert:** "No — block and poll until the **Personal Server Bootstrap** completes or times out."
> **Dev:** "How should **Myn** know the **Personal Server Bootstrap** is done?"
> **Domain expert:** "Poll over root SSH for the bootstrap completion marker written by cloud-init."
> **Dev:** "How long should **Myn** wait for bootstrap?"
> **Domain expert:** "At most five minutes after the **Personal Server** first accepts root SSH."
> **Dev:** "When should users see the software list?"
> **Domain expert:** "Before final creation confirmation, so it is part of what they approve."
> **Dev:** "Should Homebrew run as root?"
> **Domain expert:** "No — install and run Homebrew as the **Personal Server User**."
> **Dev:** "Should `tmux` be installed through apt or Homebrew?"
> **Domain expert:** "Homebrew — it belongs with the user-owned development tools."
> **Dev:** "Should the **Personal Server tmux Profile** come from the user's live local `~/.tmux.conf`?"
> **Domain expert:** "No — **Myn** should install a repo-owned tmux profile so Personal Server provisioning stays reproducible."
> **Dev:** "Should the **Personal Server tmux Profile** be curated or a literal snapshot of the intended local tmux settings?"
> **Domain expert:** "Use a literal snapshot."
> **Dev:** "Should installing the **Personal Server tmux Profile** be optional during creation?"
> **Domain expert:** "No — every new Personal Server should receive it as part of the standard development environment."
> **Dev:** "If a later bootstrap-like flow finds an existing tmux config, should this decision require preserving it?"
> **Domain expert:** "No — this decision only says creation writes the standard profile; future repair or update behavior is separate."
> **Dev:** "Should the **Personal Server tmux Profile** be system-wide?"
> **Domain expert:** "No — install it as the Personal Server User's own `.tmux.conf`."
> **Dev:** "Should the creation plan mention the **Personal Server tmux Profile** separately?"
> **Domain expert:** "No — it does not need to mention the specific tmux configuration."
> **Dev:** "Should failing to write the **Personal Server tmux Profile** be treated as a partial setup issue?"
> **Domain expert:** "No — bootstrap should hard-fail because the expected development environment was not created."
> **Dev:** "After seeding the snapshot, should tests keep comparing it to the user's live local `~/.tmux.conf`?"
> **Domain expert:** "No — the repo-owned snapshot is canonical."
> **Dev:** "Should bootstrap install a Rust stable toolchain?"
> **Domain expert:** "No — install only the Homebrew `rustup` package."
> **Dev:** "Should the package be called `golang`?"
> **Domain expert:** "No — use the Homebrew formula name `go`."
> **Dev:** "Should `nvm` use the upstream install script?"
> **Domain expert:** "No — install Homebrew `nvm`, initialize it for the user, then install and default the latest LTS Node.js."
> **Dev:** "Should Codex be installed with system npm?"
> **Domain expert:** "No — install it with the **Personal Server User**'s nvm-managed LTS npm."
> **Dev:** "Should Claude Code be installed as root?"
> **Domain expert:** "No — run the official installer as the **Personal Server User**."
> **Dev:** "Should the image be limited to Ubuntu LTS?"
> **Domain expert:** "No — use Hetzner's latest Ubuntu image and apply the latest security updates during bootstrap."
> **Dev:** "Should an open Codex prompt keep the **Personal Server** awake forever?"
> **Domain expert:** "No — protect it with a **Stdio Lease**, and let the lease become idle when terminal input and output have both been quiet long enough."

## Flagged ambiguities

- "remote machine" and "default server" both refer to **Personal Server** in this context.
- "region" was used to mean **Location** — resolved: the CLI uses **Location** everywhere.
- "lease" does not mean a permanent lock — resolved: an **Idle Lease** is renewable evidence of recent activity.
- "same tmux settings" does not mean live dotfile sync — resolved: **Personal Server Bootstrap** installs a repo-owned **Personal Server tmux Profile**.
