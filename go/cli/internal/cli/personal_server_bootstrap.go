package cli

import (
	_ "embed"
	"fmt"
	"path"
	"strings"

	"go.yaml.in/yaml/v2"
)

const (
	personalServerBootstrapScriptPath     = "/usr/local/sbin/myn-personal-server-bootstrap.sh"
	personalServerSSHHardeningProfilePath = "/etc/ssh/sshd_config.d/20-myn-hardening.conf"
)

//go:embed personal_server_tmux.conf
var personalServerTmuxProfile string

type personalServerBootstrapInput struct {
	User              string
	PasswordHash      string
	SSHPublicKey      string
	RemoteProjectRoot string
	GitIdentity       personalServerGitIdentity
	ToolPlan          personalServerBootstrapToolPlan
}

type personalServerBootstrapToolPlan struct {
	HomebrewTools []string
	CodingAgents  []personalServerCodingAgent
}

type personalServerCodingAgent string

const (
	personalServerCodingAgentCodex      personalServerCodingAgent = "codex"
	personalServerCodingAgentClaudeCode personalServerCodingAgent = "claude-code"
)

func defaultPersonalServerBootstrapToolPlan() personalServerBootstrapToolPlan {
	return personalServerBootstrapToolPlan{
		HomebrewTools: []string{"tmux", "jq", "git", "gh", "rustup", "go", "nvm"},
		CodingAgents:  []personalServerCodingAgent{personalServerCodingAgentCodex, personalServerCodingAgentClaudeCode},
	}
}

type personalServerCloudInit struct {
	PackageUpdate           bool                               `yaml:"package_update"`
	PackageUpgrade          bool                               `yaml:"package_upgrade"`
	PackageRebootIfRequired bool                               `yaml:"package_reboot_if_required"`
	SSHPwAuth               bool                               `yaml:"ssh_pwauth"`
	DisableRoot             bool                               `yaml:"disable_root"`
	Groups                  []string                           `yaml:"groups,omitempty"`
	Users                   []personalServerCloudInitUser      `yaml:"users"`
	WriteFiles              []personalServerCloudInitWriteFile `yaml:"write_files"`
	RunCmd                  [][]string                         `yaml:"runcmd"`
}

type personalServerCloudInitUser struct {
	Name              string   `yaml:"name"`
	Shell             string   `yaml:"shell,omitempty"`
	Groups            string   `yaml:"groups,omitempty"`
	Sudo              string   `yaml:"sudo,omitempty"`
	LockPasswd        *bool    `yaml:"lock_passwd,omitempty"`
	Passwd            string   `yaml:"passwd,omitempty"`
	SSHAuthorizedKeys []string `yaml:"ssh_authorized_keys,omitempty"`
}

type personalServerCloudInitWriteFile struct {
	Path        string `yaml:"path"`
	Owner       string `yaml:"owner"`
	Permissions string `yaml:"permissions"`
	Content     string `yaml:"content"`
}

func renderPersonalServerBootstrapCloudInit(input personalServerBootstrapInput) (string, error) {
	if err := validatePersonalServerBootstrapInput(input); err != nil {
		return "", err
	}
	input.ToolPlan = completePersonalServerBootstrapToolPlan(input.ToolPlan)

	lockPassword := false
	config := personalServerCloudInit{
		PackageUpdate:           true,
		PackageUpgrade:          true,
		PackageRebootIfRequired: true,
		SSHPwAuth:               false,
		DisableRoot:             false,
		Groups:                  []string{"docker"},
		Users: []personalServerCloudInitUser{
			{
				Name:              "root",
				SSHAuthorizedKeys: []string{strings.TrimSpace(input.SSHPublicKey)},
			},
			{
				Name:              input.User,
				Shell:             "/bin/bash",
				Groups:            "sudo,docker",
				Sudo:              "ALL=(ALL:ALL) ALL",
				LockPasswd:        &lockPassword,
				Passwd:            input.PasswordHash,
				SSHAuthorizedKeys: []string{strings.TrimSpace(input.SSHPublicKey)},
			},
		},
		WriteFiles: []personalServerCloudInitWriteFile{
			{
				Path:        personalServerBootstrapScriptPath,
				Owner:       "root:root",
				Permissions: "0755",
				Content:     renderPersonalServerBootstrapScript(input),
			},
		},
		RunCmd: [][]string{{"/bin/bash", personalServerBootstrapScriptPath}},
	}

	data, err := yaml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("encode Personal Server Bootstrap cloud-init: %w", err)
	}
	return "#cloud-config\n" + string(data), nil
}

func validatePersonalServerBootstrapInput(input personalServerBootstrapInput) error {
	if err := validatePersonalServerUser(input.User); err != nil {
		return err
	}
	if strings.TrimSpace(input.PasswordHash) == "" {
		return fmt.Errorf("Personal Server User password hash is required")
	}
	if strings.TrimSpace(input.SSHPublicKey) == "" {
		return fmt.Errorf("SSH public key is required")
	}
	if _, err := normalizeRemoteProjectRoot(input.RemoteProjectRoot); err != nil {
		return err
	}
	for _, tool := range completePersonalServerBootstrapToolPlan(input.ToolPlan).HomebrewTools {
		if strings.TrimSpace(tool) == "" || strings.ContainsAny(tool, " \t\r\n") {
			return fmt.Errorf("Homebrew tool names must be non-empty single tokens")
		}
	}
	for _, agent := range completePersonalServerBootstrapToolPlan(input.ToolPlan).CodingAgents {
		switch agent {
		case personalServerCodingAgentCodex, personalServerCodingAgentClaudeCode:
		default:
			return fmt.Errorf("unsupported coding agent %q", agent)
		}
	}
	return nil
}

func completePersonalServerBootstrapToolPlan(plan personalServerBootstrapToolPlan) personalServerBootstrapToolPlan {
	defaults := defaultPersonalServerBootstrapToolPlan()
	if len(plan.HomebrewTools) == 0 {
		plan.HomebrewTools = defaults.HomebrewTools
	}
	if len(plan.CodingAgents) == 0 {
		plan.CodingAgents = defaults.CodingAgents
	}
	return plan
}

func renderPersonalServerBootstrapScript(input personalServerBootstrapInput) string {
	var b strings.Builder
	remoteRoot, _ := normalizeRemoteProjectRoot(input.RemoteProjectRoot)
	remoteRootPath := path.Join("/home", input.User, remoteRoot)
	skippedGitIdentity := skippedPersonalServerGitIdentity(input.GitIdentity)

	fmt.Fprintln(&b, "#!/usr/bin/env bash")
	fmt.Fprintln(&b, "set -Eeuo pipefail")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "export MYN_USER=%s\n", shellQuote(input.User))
	fmt.Fprintf(&b, "MYN_REMOTE_PROJECT_ROOT=%s\n", shellQuote(remoteRootPath))
	fmt.Fprintln(&b, "MYN_MARKER_DIR='/var/lib/myn'")
	fmt.Fprintln(&b, "MYN_MARKER=\"$MYN_MARKER_DIR/personal-server-bootstrap.json\"")
	fmt.Fprintf(&b, "MYN_SSH_HARDENING_PROFILE=%s\n", shellQuote(personalServerSSHHardeningProfilePath))
	fmt.Fprintln(&b, "MYN_REBOOT_REQUIRED='false'")
	fmt.Fprintf(&b, "MYN_HOMEBREW_TOOLS=(%s)\n", shellArray(input.ToolPlan.HomebrewTools))
	fmt.Fprintf(&b, "MYN_SKIPPED_GIT_IDENTITY=(%s)\n", shellArray(skippedGitIdentity))
	fmt.Fprintf(&b, "export MYN_SKIPPED_GIT_IDENTITY_TEXT=%s\n", shellQuote(strings.Join(skippedGitIdentity, ",")))
	fmt.Fprintln(&b, "MYN_PARTIAL_FAILURES=()")
	fmt.Fprintln(&b)
	writePersonalServerBootstrapFunctions(&b)
	fmt.Fprintln(&b)
	writePersonalServerBootstrapSteps(&b, input)

	return b.String()
}

func writePersonalServerBootstrapFunctions(b *strings.Builder) {
	fmt.Fprintln(b, `write_marker() {
  local status="$1"
  local failure="${2:-}"
  install -d -m 0755 "$MYN_MARKER_DIR"
  python3 - "$MYN_MARKER" "$status" "$failure" "$MYN_REBOOT_REQUIRED" "${MYN_PARTIAL_FAILURES[@]}" <<'PY'
import datetime
import json
import os
import subprocess
import sys

path = sys.argv[1]
status = sys.argv[2]
failure = sys.argv[3]
reboot_required = sys.argv[4] == "true"
partial_failures = sys.argv[5:]

def first_line(command):
    try:
        output = subprocess.check_output(command, stderr=subprocess.STDOUT, text=True, timeout=20)
    except Exception:
        return ""
    return output.splitlines()[0] if output.splitlines() else ""

myn_user = os.environ.get("MYN_USER", "")

def user_command(command):
    if not myn_user:
        return ["false"]
    return ["sudo", "-H", "-u", myn_user] + command

def user_shell(command):
    return user_command(["bash", "-lc", command])

tool_commands = {
    "docker": ["docker", "--version"],
    "dockerCompose": ["docker", "compose", "version"],
    "mosh": ["mosh-server", "--version"],
    "brew": user_command(["/home/linuxbrew/.linuxbrew/bin/brew", "--version"]),
    "tmux": ["/home/linuxbrew/.linuxbrew/bin/tmux", "-V"],
    "jq": ["/home/linuxbrew/.linuxbrew/bin/jq", "--version"],
    "git": ["/home/linuxbrew/.linuxbrew/bin/git", "--version"],
    "gh": ["/home/linuxbrew/.linuxbrew/bin/gh", "--version"],
    "rustup": ["/home/linuxbrew/.linuxbrew/bin/rustup", "--version"],
    "go": ["/home/linuxbrew/.linuxbrew/bin/go", "version"],
    "nvm": user_shell("source /etc/profile.d/myn-personal-server.sh >/dev/null 2>&1; nvm --version"),
    "node": user_shell("source /etc/profile.d/myn-personal-server.sh >/dev/null 2>&1; node --version"),
    "npm": user_shell("source /etc/profile.d/myn-personal-server.sh >/dev/null 2>&1; npm --version"),
    "codex": user_shell("source /etc/profile.d/myn-personal-server.sh >/dev/null 2>&1; codex --version"),
    "claude": user_shell("source /etc/profile.d/myn-personal-server.sh >/dev/null 2>&1; claude --version"),
}

payload = {
    "status": status,
    "timestamp": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "failure": failure,
    "rebootRequired": reboot_required,
    "toolVersions": {name: first_line(command) for name, command in tool_commands.items()},
    "partialFailures": partial_failures,
    "skippedGitIdentity": [item for item in os.environ.get("MYN_SKIPPED_GIT_IDENTITY_TEXT", "").split(",") if item],
}

with open(path, "w", encoding="utf-8") as marker:
    json.dump(payload, marker, indent=2, sort_keys=True)
    marker.write("\n")
os.chmod(path, 0o644)
PY
}

fail_ssh_hardening() {
  local reason="$1"
  local failure="Personal Server SSH Hardening Profile failed to apply: $reason"
  echo "$failure" >&2
  rm -f "$MYN_SSH_HARDENING_PROFILE" 2>/dev/null || true
  write_marker "failed" "$failure"
  exit 1
}

mark_failed() {
  local status="$?"
  local command="${BASH_COMMAND:-unknown command}"
  trap - ERR
  write_marker "failed" "$command (exit $status)"
  exit "$status"
}

run_as_user_shell() {
  sudo -H -u "$MYN_USER" bash -lc "$1"
}

brew() {
  sudo -H -u "$MYN_USER" /home/linuxbrew/.linuxbrew/bin/brew "$@"
}

trap mark_failed ERR`)
}

func writePersonalServerBootstrapSteps(b *strings.Builder, input personalServerBootstrapInput) {
	fmt.Fprintln(b, `export DEBIAN_FRONTEND=noninteractive

apt-get update
apt-get install -y ca-certificates curl gnupg lsb-release unattended-upgrades apt-transport-https build-essential procps file git sudo mosh
apt-get -y upgrade
systemctl enable --now unattended-upgrades
cat >/etc/apt/apt.conf.d/20auto-upgrades <<'APTCONF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APTCONF

install -d -o "$MYN_USER" -g "$MYN_USER" "$MYN_REMOTE_PROJECT_ROOT"
install -m 0644 -o "$MYN_USER" -g "$MYN_USER" /dev/null "/home/$MYN_USER/.tmux.conf"
cat >"/home/$MYN_USER/.tmux.conf" <<'TMUXCONF'`)
	fmt.Fprint(b, personalServerTmuxProfile)
	if !strings.HasSuffix(personalServerTmuxProfile, "\n") {
		fmt.Fprintln(b)
	}
	fmt.Fprintln(b, `TMUXCONF
chown "$MYN_USER:$MYN_USER" "/home/$MYN_USER/.tmux.conf"
chmod 0644 "/home/$MYN_USER/.tmux.conf"

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
. /etc/os-release
echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu ${VERSION_CODENAME} stable" > /etc/apt/sources.list.d/docker.list
apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
usermod -aG docker "$MYN_USER"

install -d -o "$MYN_USER" -g "$MYN_USER" /home/linuxbrew/.linuxbrew
if [ ! -x /home/linuxbrew/.linuxbrew/bin/brew ]; then
  sudo -H -u "$MYN_USER" env NONINTERACTIVE=1 bash -lc "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
fi
chown -R "$MYN_USER:$MYN_USER" /home/linuxbrew/.linuxbrew
cat >/etc/profile.d/myn-personal-server.sh <<'PROFILE'
export PATH="/home/linuxbrew/.linuxbrew/bin:/home/linuxbrew/.linuxbrew/sbin:$PATH"
eval "$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)"
export NVM_DIR="$HOME/.nvm"
if [ -s "$(/home/linuxbrew/.linuxbrew/bin/brew --prefix nvm 2>/dev/null)/nvm.sh" ]; then
  . "$(/home/linuxbrew/.linuxbrew/bin/brew --prefix nvm)/nvm.sh"
fi
PROFILE

brew update
brew install "${MYN_HOMEBREW_TOOLS[@]}"

run_as_user_shell "eval \"\$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)\" && mkdir -p \"\$HOME/.nvm\" && export NVM_DIR=\"\$HOME/.nvm\" && source \"\$(/home/linuxbrew/.linuxbrew/bin/brew --prefix nvm)/nvm.sh\" && nvm install --lts && nvm alias default 'lts/*' && nvm use default"`)

	if strings.TrimSpace(input.GitIdentity.Name) != "" {
		fmt.Fprintf(b, "sudo -H -u \"$MYN_USER\" /home/linuxbrew/.linuxbrew/bin/git config --global user.name %s\n", shellQuote(input.GitIdentity.Name))
	}
	if strings.TrimSpace(input.GitIdentity.Email) != "" {
		fmt.Fprintf(b, "sudo -H -u \"$MYN_USER\" /home/linuxbrew/.linuxbrew/bin/git config --global user.email %s\n", shellQuote(input.GitIdentity.Email))
	}

	for _, agent := range input.ToolPlan.CodingAgents {
		switch agent {
		case personalServerCodingAgentCodex:
			fmt.Fprintln(b, `if ! run_as_user_shell 'source /etc/profile.d/myn-personal-server.sh && npm install -g @openai/codex'; then
  MYN_PARTIAL_FAILURES+=("Codex install failed")
fi`)
		case personalServerCodingAgentClaudeCode:
			fmt.Fprintln(b, `if ! run_as_user_shell 'curl -fsSL https://claude.ai/install.sh | bash'; then
  MYN_PARTIAL_FAILURES+=("Claude Code install failed")
fi`)
		}
	}

	fmt.Fprint(b, `
if [ -f /var/run/reboot-required ]; then
  MYN_REBOOT_REQUIRED='true'
fi

if ! install -d -m 0755 /etc/ssh/sshd_config.d; then
  fail_ssh_hardening "could not create sshd_config.d"
fi
if ! cat >"$MYN_SSH_HARDENING_PROFILE" <<'SSHDHARDENING'
`)
	fmt.Fprint(b, renderPersonalServerSSHHardeningProfile(input.User))
	fmt.Fprintln(b, `SSHDHARDENING
then
  fail_ssh_hardening "could not write $MYN_SSH_HARDENING_PROFILE"
fi
if ! chmod 0644 "$MYN_SSH_HARDENING_PROFILE"; then
  fail_ssh_hardening "could not secure $MYN_SSH_HARDENING_PROFILE"
fi
if ! sshd -t; then
  fail_ssh_hardening "sshd configuration validation failed"
fi
if ! systemctl reload ssh; then
  if ! systemctl reload sshd; then
    fail_ssh_hardening "SSH service reload failed"
  fi
fi

write_marker "success" ""`)
}

func renderPersonalServerSSHHardeningProfile(user string) string {
	return fmt.Sprintf(`# Hardened SSH daemon settings for Myn Personal Servers.
# Installed by Personal Server Bootstrap after key-only user login works.

PermitRootLogin no
AllowUsers %s

PubkeyAuthentication yes
RequiredRSASize 3072
AuthenticationMethods publickey
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitEmptyPasswords no

StrictModes yes
HostbasedAuthentication no
IgnoreRhosts yes

LoginGraceTime 30
MaxAuthTries 3
MaxStartups 10:30:60
PerSourceMaxStartups 5

X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding local
GatewayPorts no
PermitTunnel no
PermitUserEnvironment no

LogLevel VERBOSE
DebianBanner no

ClientAliveInterval 300
ClientAliveCountMax 2
`, user)
}

func skippedPersonalServerGitIdentity(identity personalServerGitIdentity) []string {
	var skipped []string
	if strings.TrimSpace(identity.Name) == "" {
		skipped = append(skipped, "user.name")
	}
	if strings.TrimSpace(identity.Email) == "" {
		skipped = append(skipped, "user.email")
	}
	return skipped
}

func shellArray(values []string) string {
	if len(values) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, shellQuote(value))
	}
	return " " + strings.Join(quoted, " ") + " "
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
