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

## Test

```sh
just test
# or
go test ./...
```

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
