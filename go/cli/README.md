# Myn

<p align="center">
  <img src="../../logos/myn-seagull-variation-5-icon-palette-04-m-blue-dark-electric-blue.svg" alt="Myn seagull logo" width="180">
</p>

Myn ("mine") provisions and operates your personal development environment.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/hsubra89/myn/master/go/cli/install.sh | sh
```

This installs the latest stable release to `~/.local/bin/myn` and verifies the
release checksum before replacing any existing binary at that path. To install
a specific release:

```sh
curl -fsSL https://raw.githubusercontent.com/hsubra89/myn/master/go/cli/install.sh | MYN_VERSION=0.1.0 sh
```

## Development

Common development commands are defined in [`justfile`](justfile):

```sh
just run version
just fmt
just test
just build
just tidy
```

## Run

```sh
go run ./cmd/myn version
```

## Local Dev Launcher

```sh
../../scripts/mount-myn-cli
myn version
```

This creates `~/.local/bin/myn` as a launcher for this checkout. It runs
a temporary dev binary built from this checkout, so local source changes are
picked up automatically while cwd-sensitive commands still see the directory
where `myn` was invoked. Use `../../scripts/mount-myn-cli --unmount` to remove
it.

## Configure

```sh
go run ./cmd/myn configure
```

The command configures project roots for this machine and the remote machine,
plus the Ed25519 SSH identity that future `myn`-provisioned servers should
trust. It stores normalized home-relative paths in the `myn` config:

```json
{
  "projects": {
    "localRoot": "projects",
    "remoteRoot": "projects"
  },
  "ssh": {
    "identityFile": ".ssh/id_ed25519"
  }
}
```

The local root must be an existing directory under the current user's home
directory. The prompt accepts `projects`, `~/projects`, or an absolute path
under the user's home directory, and stores all of them as `projects`.

The remote root must be relative to the remote user's home directory. The
prompt accepts `projects` or `~/projects` and stores `projects`; absolute
remote paths are rejected.

The SSH identity must be an existing Ed25519 private key under the current
user's home directory. Interactive configuration scans `~/.ssh/*.pub` for
Ed25519 keypairs, lets you select the current configured identity when it is
valid, and always offers to generate a new keypair. Generated keys use
`~/.ssh/id_ed25519` first and fall back to `~/.ssh/id_myn_25519` when the
default path is occupied. If an existing private key is missing its `.pub`
file, `configure` regenerates it with `ssh-keygen -y`.

Generated keys prompt once for a masked passphrase, which may be empty, and use
`<user>@<hostname>` as the key comment. When the selected key is not already
loaded in `ssh-agent`, `configure` asks whether to add it; this is recommended
for remote server access. Agent-add failures are warnings and do not block
config saving.

Non-interactive options:

```sh
go run ./cmd/myn configure \
  --local-root projects \
  --remote-root projects \
  --ssh-identity-file ~/.ssh/id_ed25519
```

Non-interactive configuration accepts an existing `ssh.identityFile` from the
config or a `--ssh-identity-file` value. It does not generate SSH keys.
Non-interactive `configure` also never creates a Personal Server.

## Hetzner Authentication

```sh
go run ./cmd/myn auth hetzner
```

The command validates that a Hetzner Cloud API token is a Read & Write token and
saves it in the `myn` config. It first checks `/locations`, then probes
`DELETE /ssh_keys/0` against a non-existent key so no key is created. By
default the config path is
`${XDG_CONFIG_HOME}/myn/config.json` or the platform equivalent from Go's
`os.UserConfigDir`; set `MYN_CONFIG` to override it.

Non-interactive options:

```sh
go run ./cmd/myn auth hetzner --token "$HCLOUD_TOKEN"
go run ./cmd/myn auth hetzner --from-hcloud-context warptech
```

When no token is supplied, the command checks an existing `myn` token first, then
looks for hcloud contexts in `~/.config/hcloud/cli.toml` or `HCLOUD_CONFIG`.
Set `HCLOUD_ENDPOINT` to override the validation endpoint.

## Personal Server Provisioning

Run `myn auth hetzner` before provisioning a Personal Server. `myn configure`
uses saved Hetzner Credentials; it does not collect or import a Hetzner token
inside the configure flow.

Interactive `myn configure` configures the local project root, remote project
root, and SSH identity first. After those values are saved, it can offer to
create a Hetzner Cloud Personal Server when Hetzner Credentials and a valid
SSH identity are available. Non-interactive `configure` skips Personal Server
creation so scripts do not create billable cloud resources.

When a Personal Server already exists in config, `configure` verifies the saved
server ID against Hetzner. If the server still exists, it reports the saved and
current addresses and does not create another server. If the saved server is
missing, interactive `configure` asks before clearing the stale Personal Server
Configuration; non-interactive `configure` fails without clearing it.

The saved Personal Server Configuration is a top-level config section that
stores only the created server identity, Personal Server User, and assigned
addresses:

```json
{
  "personalServer": {
    "serverID": 123456,
    "user": "harish",
    "ipv4": "203.0.113.10",
    "ipv6": "2001:db8::1"
  }
}
```

The saved section is incomplete for connection unless it has `serverID`, the
Personal Server User, and at least one saved address.

Location, Server Type, server name, password, install choices, and pricing are
transient provisioning inputs. They are not saved as desired state.

During creation, `configure` prompts for a Hetzner Location, then lists eligible
non-deprecated x86_64 Server Types available in that Location. The Location
selector defaults to `ash` when Hetzner offers it, otherwise the first Location
sorted by code. Server Type options show dedicated/shared compute, vCPU, RAM,
disk, storage type, and API name. Prices are not shown in the Server Type
selector; the final confirmation shows one maximum monthly gross EUR price when
pricing is available, or clearly says the price is unavailable.

Before creating cloud resources, `configure` shows an install plan grouped as:

- System services: security updates, unattended security upgrades, Docker
  Engine, Docker Compose, Mosh access, the Personal Server User, SSH access,
  and the remote project root.
- Homebrew tools: `tmux`, `jq`, `git`, `gh`, `rustup`, `go`, `nvm`, latest LTS
  Node.js, and npm.
- Coding agents: Codex and Claude Code.

Git identity values are copied from available local Git config values, with
global config preferred over repo-local config. Missing `user.name` or
`user.email` values are reported and skipped. GitHub CLI is installed, but
GitHub authentication and credentials are not configured.

The Personal Server User has password-backed sudo and belongs to the `docker`
group. Docker group membership is root-equivalent access on the server even
though sudo itself requires the password collected during provisioning. That
password is hashed locally for cloud-init and is not saved in config.

After Hetzner accepts the create request, `configure` waits for create actions,
root SSH readiness, and the cloud-init Personal Server Bootstrap completion
marker. A created server ID, Personal Server User, and assigned IP addresses are
saved even if bootstrap fails or times out, so the billable server can be
inspected.

When provisioning finishes successfully, `configure` prints SSH commands for
the Personal Server User and root over IPv4 and IPv6, IPv4 first. Each SSH
command includes `-i` with the configured SSH identity and `-l` with the login
user, so IPv6 addresses are passed as unbracketed host arguments. It also
prints Mosh commands for the Personal Server User with the configured SSH
identity passed through `--ssh`.

`myn` creates or reuses the `myn-personal-server` firewall and a Hetzner SSH key
resource for the configured SSH identity. A newly created firewall allows
inbound SSH and Mosh UDP `60000-61000` from all IPv4 and IPv6 sources only.
Existing firewall rules are not reset, so reused firewalls may need the Mosh UDP
rule added manually. Supporting resources are not automatically cleaned up on
server creation, cancellation, or bootstrap failure.

Personal Server provisioning does not create SSH config aliases, clone or sync
projects, copy dotfiles, copy GitHub credentials, authenticate `gh`, or install
a Rust toolchain.

## Connect

```sh
go run ./cmd/myn connect
go run ./cmd/myn c
```

`myn connect` starts a Personal Server Connection using the saved Personal
Server Configuration. It accepts no path arguments; the current working
directory is the input. The command requires a configured local project root,
remote project root, SSH identity, Personal Server User, and at least one saved
Personal Server address. The local project root and SSH identity file must exist
locally, and stdin and stdout must be terminal-backed.

The current working directory is mapped lexically under the configured local
project root to the matching path under the configured remote project root. If
the command runs from the local project root itself, it targets the remote
project root. If it runs from a subdirectory, the target Project is the first
path segment under the configured local project root; Git repository boundaries
do not affect this. Running outside the configured local project root fails
before SSH.

The command connects over SSH, preferring the saved IPv4 address and falling
back to the saved IPv6 address when IPv4 is unavailable. The Personal Server
User is passed to SSH with `-l`, so IPv6 addresses are passed as unbracketed
host arguments. The configured SSH identity is passed with `-i`, SSH requests
one TTY allocation, and host key checking uses
`StrictHostKeyChecking=accept-new`.

On the Personal Server, `myn connect` runs a Bash login-shell tmux handoff. It
attaches to an existing project-scoped tmux session when one exists; otherwise
it creates one. New sessions start in the exact mapped remote directory when
that directory exists, then fall back to the remote Project root, then the
Personal Server User home directory. Only existing directories are used, and the
command does not create missing remote project directories.

Mosh Access remains available through the commands printed after successful
provisioning, but this first `myn connect` implementation does not use Mosh
Access, does not require Hetzner Credentials, does not verify the saved server
through the Hetzner API, and does not create an Idle Lease or Stdio Lease.
Successful handoff prints no Myn-specific output; local validation failures are
reported before SSH, and SSH or tmux exit statuses are preserved.

## Test

```sh
just test
# or
go test ./...
```

### Live Hetzner Validation

The live Personal Server provisioning validation is opt-in because it creates a
billable Hetzner Cloud server. Put a Read & Write token in the repository-root
`.env.local` file as `HETZNER_API_KEY`; that file is ignored by git and the test
does not print the token.

```sh
just live-hetzner
# or
MYN_LIVE_HETZNER=1 go test ./internal/cli -run TestLivePersonalServerProvisioning -count=1 -timeout=30m -v
```

The test uses isolated temporary `myn` config, home, known_hosts, and SSH identity
values. It creates a uniquely named `myn-live-*` server through the real
configure provisioning path, waits for bootstrap, verifies SSH/user/tool setup,
then deletes the test server on success or failure. It may create and intentionally
leave behind the reusable `myn-personal-server` firewall and matching SSH key,
which is the product behavior for supporting resources. By default it chooses
the cheapest eligible offered Server Type in the selected Location to limit
prorated test cost; set `MYN_LIVE_HETZNER_LOCATION` or
`MYN_LIVE_HETZNER_SERVER_TYPE` in `.env.local` to force a specific choice.

## Format

```sh
just fmt
# or
go fmt ./...
```

## Build

```sh
just build
# or
mkdir -p ./bin
go build -o ./bin/myn ./cmd/myn
```

## Release Metadata

```sh
just release 0.1.0
# or
mkdir -p ./bin
go build \
  -ldflags "-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o ./bin/myn ./cmd/myn
```
