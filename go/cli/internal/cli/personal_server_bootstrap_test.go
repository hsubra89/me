package cli

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v2"
)

func TestRenderPersonalServerBootstrapCloudInit(t *testing.T) {
	const sshPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAITestKey personal@local"

	rendered, err := renderPersonalServerBootstrapCloudInit(personalServerBootstrapInput{
		User:              "harish",
		PasswordHash:      "$6$abcdefghijklmnop$hashed",
		SSHPublicKey:      sshPublicKey,
		RemoteProjectRoot: "Remote Projects",
		GitIdentity: personalServerGitIdentity{
			Name: "Harish Subramanian",
		},
		ToolPlan: personalServerBootstrapToolPlan{
			HomebrewTools: []string{"tmux", "jq", "git", "gh", "rustup", "go", "nvm"},
			CodingAgents:  []personalServerCodingAgent{personalServerCodingAgentCodex, personalServerCodingAgentClaudeCode},
		},
	})
	if err != nil {
		t.Fatalf("render cloud-init: %v", err)
	}
	if !strings.HasPrefix(rendered, "#cloud-config\n") {
		t.Fatalf("cloud-init should start with #cloud-config, got %q", rendered)
	}

	parsed := parseBootstrapCloudInit(t, rendered)
	if !parsed.PackageUpdate || !parsed.PackageUpgrade || !parsed.PackageRebootIfRequired {
		t.Fatalf("security updates and reboot-on-required should be enabled, got %#v", parsed)
	}
	if parsed.DisableRoot {
		t.Fatal("root SSH should remain enabled")
	}
	if parsed.SSHPwAuth {
		t.Fatal("password SSH authentication should remain disabled")
	}

	root := parsed.user("root")
	if !containsString(root.SSHAuthorizedKeys, sshPublicKey) {
		t.Fatalf("root should authorize configured SSH key, got %#v", root.SSHAuthorizedKeys)
	}

	user := parsed.user("harish")
	if got, want := user.Shell, "/bin/bash"; got != want {
		t.Fatalf("user shell mismatch: want %q, got %q", want, got)
	}
	for _, want := range []string{"sudo", "docker"} {
		if !strings.Contains(user.Groups, want) {
			t.Fatalf("user groups should include %q, got %q", want, user.Groups)
		}
	}
	if got, want := user.Sudo, "ALL=(ALL:ALL) ALL"; got != want {
		t.Fatalf("sudo policy mismatch: want %q, got %q", want, got)
	}
	if user.LockPasswd == nil || *user.LockPasswd {
		t.Fatalf("user password should be unlocked for sudo, got %#v", user.LockPasswd)
	}
	if got, want := user.Passwd, "$6$abcdefghijklmnop$hashed"; got != want {
		t.Fatalf("password hash mismatch: want %q, got %q", want, got)
	}
	if !containsString(user.SSHAuthorizedKeys, sshPublicKey) {
		t.Fatalf("user should authorize configured SSH key, got %#v", user.SSHAuthorizedKeys)
	}

	script := parsed.bootstrapScript()
	for _, want := range []string{
		"MYN_REMOTE_PROJECT_ROOT='/home/harish/Remote Projects'",
		"export MYN_USER='harish'",
		"install -d -o \"$MYN_USER\" -g \"$MYN_USER\" \"$MYN_REMOTE_PROJECT_ROOT\"",
		"install -m 0644 -o \"$MYN_USER\" -g \"$MYN_USER\" /dev/null \"/home/$MYN_USER/.tmux.conf\"",
		"cat >\"/home/$MYN_USER/.tmux.conf\" <<'TMUXCONF'\n" + personalServerTmuxProfile + "TMUXCONF\n",
		"chown \"$MYN_USER:$MYN_USER\" \"/home/$MYN_USER/.tmux.conf\"",
		"chmod 0644 \"/home/$MYN_USER/.tmux.conf\"",
		"apt-get install -y ca-certificates curl gnupg lsb-release unattended-upgrades apt-transport-https build-essential procps file git sudo mosh",
		"systemctl enable --now unattended-upgrades",
		"APT::Periodic::Unattended-Upgrade \"1\";",
		"https://download.docker.com/linux/ubuntu",
		"docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin",
		"brew install \"${MYN_HOMEBREW_TOOLS[@]}\"",
		"nvm install --lts",
		"nvm alias default 'lts/*'",
		"npm install -g @openai/codex",
		"curl -fsSL https://claude.ai/install.sh | bash",
		"git config --global user.name 'Harish Subramanian'",
		"MYN_SKIPPED_GIT_IDENTITY=( 'user.email' )",
		"trap mark_failed ERR",
		"write_marker \"failed\"",
		"MYN_PARTIAL_FAILURES+=(\"Codex install failed\")",
		"MYN_PARTIAL_FAILURES+=(\"Claude Code install failed\")",
		"\"status\"",
		"\"timestamp\"",
		"\"rebootRequired\"",
		"\"toolVersions\"",
		"myn_user = os.environ.get(\"MYN_USER\", \"\")",
		"\"sudo\", \"-H\", \"-u\", myn_user",
		"\"mosh\": [\"mosh-server\", \"--version\"]",
		"\"brew\": user_command([\"/home/linuxbrew/.linuxbrew/bin/brew\", \"--version\"])",
		"\"node\": user_shell(\"source /etc/profile.d/myn-personal-server.sh >/dev/null 2>&1; node --version\")",
		"\"partialFailures\"",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("bootstrap script should contain %q:\n%s", want, script)
		}
	}

	for _, forbidden := range []string{
		"rustup default",
		"rustup toolchain install",
		"git clone",
		"gh auth login",
		"brew install mosh",
		"$6$abcdefghijklmnop$hashed",
	} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("bootstrap script should not contain %q:\n%s", forbidden, script)
		}
	}
}

type parsedBootstrapCloudInit struct {
	PackageUpdate           bool                 `yaml:"package_update"`
	PackageUpgrade          bool                 `yaml:"package_upgrade"`
	PackageRebootIfRequired bool                 `yaml:"package_reboot_if_required"`
	DisableRoot             bool                 `yaml:"disable_root"`
	SSHPwAuth               bool                 `yaml:"ssh_pwauth"`
	Users                   []bootstrapCloudUser `yaml:"users"`
	WriteFiles              []bootstrapWriteFile `yaml:"write_files"`
}

type bootstrapCloudUser struct {
	Name              string   `yaml:"name"`
	Shell             string   `yaml:"shell"`
	Groups            string   `yaml:"groups"`
	Sudo              string   `yaml:"sudo"`
	LockPasswd        *bool    `yaml:"lock_passwd"`
	Passwd            string   `yaml:"passwd"`
	SSHAuthorizedKeys []string `yaml:"ssh_authorized_keys"`
}

type bootstrapWriteFile struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
}

func parseBootstrapCloudInit(t *testing.T, rendered string) parsedBootstrapCloudInit {
	t.Helper()

	var parsed parsedBootstrapCloudInit
	if err := yaml.Unmarshal([]byte(rendered), &parsed); err != nil {
		t.Fatalf("parse cloud-init YAML: %v\n%s", err, rendered)
	}
	return parsed
}

func (parsed parsedBootstrapCloudInit) user(name string) bootstrapCloudUser {
	for _, user := range parsed.Users {
		if user.Name == name {
			return user
		}
	}
	return bootstrapCloudUser{}
}

func (parsed parsedBootstrapCloudInit) bootstrapScript() string {
	for _, file := range parsed.WriteFiles {
		if file.Path == "/usr/local/sbin/myn-personal-server-bootstrap.sh" {
			return file.Content
		}
	}
	return ""
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
