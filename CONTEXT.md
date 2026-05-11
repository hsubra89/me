# me CLI

The `me` CLI configures and provisions the user's personal development environment across their local machine and a cloud server.

## Language

**Personal Server**:
A Hetzner Cloud server that `me` provisions for the user's cloud-hosted development environment.
_Avoid_: remote machine, default server

**Hetzner Credentials**:
A saved Read & Write Hetzner Cloud API token used by `me` for cloud provisioning.
_Avoid_: hcloud context, cloud login

**Location**:
A Hetzner Cloud location where a **Personal Server** can be created.
_Avoid_: region, datacenter

**Personal Server Configuration**:
The saved identity and addresses of a created **Personal Server**.
_Avoid_: Hetzner config, remote config

**Personal Server Firewall**:
A reusable Hetzner Cloud firewall named `me-personal-server` that is created by `me` for **Personal Server** instances and editable by the user after creation.
_Avoid_: SSH-only firewall, generated firewall

**Personal Server SSH Key**:
A Hetzner Cloud SSH key resource created from the configured local SSH identity for **Personal Server** access.
_Avoid_: remote key, uploaded key

**Personal Server User**:
A Linux user account created on a **Personal Server** from a normalized form of the current local username.
_Avoid_: remote user, cloud user

**Server Type**:
A non-deprecated Hetzner Cloud x86_64 server type available in the selected **Location** for a **Personal Server**.
_Avoid_: size, plan, instance type

**Personal Server Bootstrap**:
The first-boot cloud-init process that creates the **Personal Server User** and installs the expected development tools.
_Avoid_: setup script, install script

## Relationships

- A **Personal Server** trusts the configured SSH identity for both root and user login.
- A **Personal Server** keeps key-based root SSH enabled after bootstrap.
- `me configure` does not modify the user's SSH config for **Personal Server** aliases.
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
- A **Personal Server** name is validated locally as a DNS-label-style value before creation.
- A **Personal Server** is not created when a Hetzner server with the chosen name already exists.
- A **Personal Server** uses Hetzner's latest non-deprecated Ubuntu system image, selected by highest Ubuntu version.
- A **Personal Server** is created with both IPv4 and IPv6 enabled through server creation public network options.
- Hetzner resources created for a **Personal Server** are labeled `managed_by=me` and `role=personal_server`.
- Hetzner labels are for visibility only and are not used to auto-adopt missing **Personal Server Configuration**.
- `me` waits for Hetzner create actions to finish before root SSH and bootstrap polling.
- Hetzner API calls, root SSH polling, and bootstrap polling respect command cancellation.
- A **Personal Server Configuration** belongs in its own top-level config section.
- A **Personal Server Configuration** stores `serverID`, `ipv4`, and the assigned `ipv6` address, not ongoing desired state for mutable server details.
- **Location**, **Server Type**, and proposed server name selections are transient until a **Personal Server** is created.
- When **Personal Server Configuration** already has a server ID, `me configure` skips creation by default and reports the saved server ID and IP addresses.
- A saved **Personal Server** server ID is verified against Hetzner before creation is skipped.
- If a saved **Personal Server** server ID is missing in Hetzner, interactive `configure` asks before clearing it and non-interactive `configure` fails.
- **Personal Server** creation is interactive-only; non-interactive `configure` does not create a server.
- Missing **Hetzner Credentials** do not block saving local configuration; they only skip **Personal Server** creation.
- Interactive `configure` asks for explicit final confirmation before creating a **Personal Server**.
- A created **Personal Server** is saved in **Personal Server Configuration** even if the **Personal Server Bootstrap** fails or times out.
- `me configure` reports user and root SSH commands with `-i` for the configured SSH identity for both IPv4 and IPv6 after **Personal Server** creation, with IPv4 first.
- `me configure` saves local roots and SSH identity before attempting **Personal Server** creation, then saves **Personal Server Configuration** after server creation succeeds.
- After **Personal Server Bootstrap** completes, `me configure` reports installed tool versions from the completion marker.
- **Personal Server** provisioning is implemented separately from local root and SSH identity configuration.
- A **Personal Server Firewall** is created by `me` with an initially minimal rule set and may be expanded by the user.
- A **Personal Server Firewall** can be reused by multiple **Personal Server** instances over time.
- An existing **Personal Server Firewall** is treated as user-editable and is not reset by `me`.
- A newly created **Personal Server Firewall** allows inbound SSH from all IPv4 and IPv6 sources and no other inbound access.
- A **Personal Server SSH Key** is reused when Hetzner already has the same public key fingerprint.
- Supporting resources created for a **Personal Server** are not automatically cleaned up if server creation fails.
- A **Personal Server User** is derived from the current local username as a lowercase letters, digits, and hyphens value.
- If the local username cannot be normalized into a valid **Personal Server User**, `me configure` prompts for one.
- A **Personal Server User** starts with Bash as the login shell.
- A **Personal Server User** belongs to the `sudo` and `docker` groups.
- A **Personal Server User** has sudo access, but sudo requires a password.
- Membership in the `docker` group is accepted as root-equivalent development-machine access.
- A **Personal Server User** password is collected for provisioning, confirmed, required to be non-empty, sent to cloud-init as a SHA-512 crypt hash, and not saved in config.
- A **Server Type** default is the available option closest to 21 EUR monthly gross price, breaking ties by dedicated over shared, then RAM, then vCPU.
- A **Server Type** label reflects Hetzner's actual storage type and includes the Hetzner API name.
- Final **Personal Server** creation confirmation shows one maximum monthly gross EUR total from Hetzner live pricing for billable resources `me` is about to provision when pricing can be fetched, and explicitly says price is unavailable otherwise.
- A **Personal Server Bootstrap** runs through cloud-init during server creation.
- **Personal Server Bootstrap** cloud-init user data is rendered through a typed Go render function using a YAML library.
- A **Personal Server Bootstrap** installs the latest Ubuntu security updates after the **Personal Server** is created, enables unattended security upgrades, and reboots automatically if required.
- A **Personal Server Bootstrap** installs Docker Engine and the compose plugin from Docker's official apt repository.
- **Personal Server** creation is not considered complete until the **Personal Server Bootstrap** finishes.
- The **Personal Server Bootstrap** writes a completion marker with status, timestamp, reboot information, and installed tool versions that `me` polls for over root SSH.
- The **Personal Server Bootstrap** must finish within five minutes after the **Personal Server** first accepts root SSH.
- Automatic reboot downtime counts against the five-minute **Personal Server Bootstrap** timeout.
- The **Personal Server Bootstrap** install plan is shown before final creation confirmation.
- The **Personal Server Bootstrap** install plan groups system services, Homebrew tools, and coding agents separately.
- Homebrew and Homebrew-installed tools belong to the **Personal Server User**, not root.
- **Personal Server Bootstrap** does not copy local dotfiles or shell configuration beyond required Homebrew, nvm, and Git identity setup.
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
> **Domain expert:** "No — store only the created server ID and IP addresses, because the user can customize the instance after creation."
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
> **Dev:** "Should `me` delete the server if bootstrap fails?"
> **Domain expert:** "No — save the created server ID and IP addresses so the user can inspect it."
> **Dev:** "Should the firewall name describe only SSH?"
> **Domain expert:** "No — make it obvious `me` created it, but leave room for the user to expand it later."
> **Dev:** "If the firewall already exists, should `me` restore SSH-only rules?"
> **Domain expert:** "No — existing rules may be intentional user edits."
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
> **Domain expert:** "Yes — show the maximum possible monthly price for the resources `me` is about to provision."
> **Dev:** "Should `me` SSH into the server to install tools?"
> **Domain expert:** "No — install them through the **Personal Server Bootstrap** and wait for cloud-init to finish."
> **Dev:** "Can `configure` return once Hetzner accepts the create request?"
> **Domain expert:** "No — block and poll until the **Personal Server Bootstrap** completes or times out."
> **Dev:** "How should `me` know the **Personal Server Bootstrap** is done?"
> **Domain expert:** "Poll over root SSH for the bootstrap completion marker written by cloud-init."
> **Dev:** "How long should `me` wait for bootstrap?"
> **Domain expert:** "At most five minutes after the **Personal Server** first accepts root SSH."
> **Dev:** "When should users see the software list?"
> **Domain expert:** "Before final creation confirmation, so it is part of what they approve."
> **Dev:** "Should Homebrew run as root?"
> **Domain expert:** "No — install and run Homebrew as the **Personal Server User**."
> **Dev:** "Should `tmux` be installed through apt or Homebrew?"
> **Domain expert:** "Homebrew — it belongs with the user-owned development tools."
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

## Flagged ambiguities

- "remote machine" and "default server" both refer to **Personal Server** in this context.
- "region" was used to mean **Location** — resolved: the CLI uses **Location** everywhere.
