# me

A small Cobra-based Go CLI under `go/cli`.

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
go run ./cmd/me version
```

## Local Dev Launcher

```sh
../../scripts/mount-me-cli
me version
```

This creates `~/.local/bin/me` as a launcher for this checkout. It runs
`go run ./cmd/me`, so local source changes are picked up automatically. Use
`../../scripts/mount-me-cli --unmount` to remove it.

## Configure

```sh
go run ./cmd/me configure
```

The command configures project roots for this machine and the remote machine,
plus the Ed25519 SSH identity that future `me`-provisioned servers should
trust. It stores normalized home-relative paths in the `me` config:

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
`~/.ssh/id_ed25519` first and fall back to `~/.ssh/id_me_25519` when the
default path is occupied. If an existing private key is missing its `.pub`
file, `configure` regenerates it with `ssh-keygen -y`.

Generated keys prompt once for a masked passphrase, which may be empty, and use
`<user>@<hostname>` as the key comment. When the selected key is not already
loaded in `ssh-agent`, `configure` asks whether to add it; this is recommended
for remote server access. Agent-add failures are warnings and do not block
config saving.

Non-interactive options:

```sh
go run ./cmd/me configure \
  --local-root projects \
  --remote-root projects \
  --ssh-identity-file ~/.ssh/id_ed25519
```

Non-interactive configuration accepts an existing `ssh.identityFile` from the
config or a `--ssh-identity-file` value. It does not generate SSH keys.
Non-interactive `configure` also never creates a Personal Server.

## Hetzner Authentication

```sh
go run ./cmd/me auth hetzner
```

The command validates that a Hetzner Cloud API token is a Read & Write token and
saves it in the `me` config. It first checks `/locations`, then probes
`DELETE /ssh_keys/0` against a non-existent key so no key is created. By
default the config path is
`${XDG_CONFIG_HOME}/me/config.json` or the platform equivalent from Go's
`os.UserConfigDir`; set `ME_CONFIG` to override it.

Non-interactive options:

```sh
go run ./cmd/me auth hetzner --token "$HCLOUD_TOKEN"
go run ./cmd/me auth hetzner --from-hcloud-context warptech
```

When no token is supplied, the command checks an existing `me` token first, then
looks for hcloud contexts in `~/.config/hcloud/cli.toml` or `HCLOUD_CONFIG`.
Set `HCLOUD_ENDPOINT` to override the validation endpoint.

## Personal Server Provisioning

Run `me auth hetzner` before provisioning a Personal Server. `me configure`
uses saved Hetzner Credentials; it does not collect or import a Hetzner token
inside the configure flow.

Interactive `me configure` configures the local project root, remote project
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
stores only the created server identity and assigned addresses:

```json
{
  "personalServer": {
    "serverID": 123456,
    "ipv4": "203.0.113.10",
    "ipv6": "2001:db8::1"
  }
}
```

Location, Server Type, server name, Personal Server User, password, install
choices, and pricing are transient provisioning inputs. They are not saved as
desired state.

During creation, `configure` prompts for a Hetzner Location, then lists eligible
non-deprecated x86_64 Server Types available in that Location. The Location
selector defaults to `ash` when Hetzner offers it, otherwise the first Location
sorted by code. Server Type options show dedicated/shared compute, vCPU, RAM,
disk, storage type, and API name. Prices are not shown in the Server Type
selector; the final confirmation shows one maximum monthly gross EUR price when
pricing is available, or clearly says the price is unavailable.

Before creating cloud resources, `configure` shows an install plan grouped as:

- System services: security updates, unattended security upgrades, Docker
  Engine, Docker Compose, the Personal Server User, SSH access, and the remote
  project root.
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
marker. A created server ID and assigned IP addresses are saved even if
bootstrap fails or times out, so the billable server can be inspected.

When provisioning finishes, `configure` prints SSH commands for the Personal
Server User and root over IPv4 and IPv6, IPv4 first. Each command includes
`-i` with the configured SSH identity.

`me` creates or reuses the `me-personal-server` firewall and a Hetzner SSH key
resource for the configured SSH identity. A newly created firewall allows
inbound SSH from all IPv4 and IPv6 sources only. Existing firewall rules are
not reset, and supporting resources are not automatically cleaned up on server
creation, cancellation, or bootstrap failure.

Personal Server provisioning does not create SSH config aliases, clone or sync
projects, copy dotfiles, copy GitHub credentials, authenticate `gh`, or install
a Rust toolchain.

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
ME_LIVE_HETZNER=1 go test ./internal/cli -run TestLivePersonalServerProvisioning -count=1 -timeout=30m -v
```

The test uses isolated temporary `me` config, home, known_hosts, and SSH identity
values. It creates a uniquely named `me-live-*` server through the real
configure provisioning path, waits for bootstrap, verifies SSH/user/tool setup,
then deletes the test server on success or failure. It may create and intentionally
leave behind the reusable `me-personal-server` firewall and matching SSH key,
which is the product behavior for supporting resources. By default it chooses
the cheapest eligible offered Server Type in the selected Location to limit
prorated test cost; set `ME_LIVE_HETZNER_LOCATION` or
`ME_LIVE_HETZNER_SERVER_TYPE` in `.env.local` to force a specific choice.

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
go build -o ./bin/me ./cmd/me
```

## Release Metadata

```sh
just release 0.1.0
# or
mkdir -p ./bin
go build \
  -ldflags "-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o ./bin/me ./cmd/me
```
