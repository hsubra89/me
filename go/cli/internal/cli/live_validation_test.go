package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
)

const (
	liveValidationEnabledEnv     = "MYN_LIVE_HETZNER"
	liveValidationAPIKeyEnv      = "HETZNER_API_KEY"
	liveValidationLocationEnv    = "MYN_LIVE_HETZNER_LOCATION"
	liveValidationServerTypeEnv  = "MYN_LIVE_HETZNER_SERVER_TYPE"
	liveValidationBootstrapLimit = 5 * time.Minute
	liveValidationTestTimeout    = 30 * time.Minute
	liveValidationUser           = "melive"
	liveValidationRemoteRoot     = "Remote Projects"
	liveValidationGitName        = "Myn Live Validation"
	liveValidationGitEmail       = "myn-live@example.invalid"
)

func TestLoadLiveValidationEnvFileReadsHetznerAPIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env.local")
	if err := os.WriteFile(path, []byte(`
# local live validation secrets
IGNORED=value
HETZNER_API_KEY=' live-token '
`), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	env, err := loadLiveValidationEnvFile(path)
	if err != nil {
		t.Fatalf("load env file: %v", err)
	}
	token, err := liveValidationHetznerAPIKey(env)
	if err != nil {
		t.Fatalf("read token: %v", err)
	}
	if token != "live-token" {
		t.Fatalf("token mismatch: want %q, got %q", "live-token", token)
	}
}

func TestLoadLiveValidationEnvFileReportsMissingFile(t *testing.T) {
	_, err := loadLiveValidationEnvFile(filepath.Join(t.TempDir(), ".env.local"))
	if err == nil {
		t.Fatal("expected missing .env.local error")
	}
	if !strings.Contains(err.Error(), "live validation requires .env.local") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLiveValidationHetznerAPIKeyReportsMissingToken(t *testing.T) {
	_, err := liveValidationHetznerAPIKey(map[string]string{"OTHER": "value"})
	if err == nil {
		t.Fatal("expected missing HETZNER_API_KEY error")
	}
	if !strings.Contains(err.Error(), "HETZNER_API_KEY") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLivePersonalServerProvisioning(t *testing.T) {
	if os.Getenv(liveValidationEnabledEnv) != "1" {
		t.Skipf("set %s=1 to run live Hetzner Personal Server validation", liveValidationEnabledEnv)
	}

	repoRoot, err := findLiveValidationRepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	env, err := loadLiveValidationEnvFile(filepath.Join(repoRoot, ".env.local"))
	if err != nil {
		t.Skip(err.Error())
	}
	token, err := liveValidationHetznerAPIKey(env)
	if err != nil {
		t.Skip(err.Error())
	}

	ctx, cancel := context.WithTimeout(context.Background(), liveValidationTestTimeout)
	defer cancel()

	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, liveValidationRemoteRoot), 0o700); err != nil {
		t.Fatalf("create isolated local root: %v", err)
	}
	identityPath := filepath.Join(home, ".ssh", "id_ed25519")
	generateLiveValidationSSHKey(t, identityPath)
	knownHostsPath := filepath.Join(home, ".ssh", "known_hosts")
	publicKeyLine := liveValidationSSHPublicKey(t, identityPath)
	publicKey, err := parseSSHPublicKey(publicKeyLine)
	if err != nil {
		t.Fatalf("parse generated SSH public key: %v", err)
	}
	sshKeyFingerprint, err := sshPublicKeyHetznerFingerprint(publicKey)
	if err != nil {
		t.Fatalf("compute Hetzner SSH key fingerprint: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	serverName := liveValidationServerName(t)
	client := liveValidationHcloudClient(token)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cleanupCancel()
		cleanupLiveValidationServer(cleanupCtx, t, client, configPath, serverName)
	})

	beforeFirewall, _, err := client.Firewall.GetByName(ctx, personalServerFirewallName)
	if err != nil {
		t.Fatalf("check existing live Personal Server Firewall: %v", err)
	}
	firewallExisted := beforeFirewall != nil
	beforeSSHKey, _, err := client.SSHKey.GetByFingerprint(ctx, sshKeyFingerprint)
	if err != nil {
		t.Fatalf("check existing live Personal Server SSH Key: %v", err)
	}
	sshKeyExisted := beforeSSHKey != nil

	var authOut bytes.Buffer
	if err := runHetznerAuth(ctx, &authOut, hetznerAuthOptions{token: token}, hetznerAuthDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		env: func(string) string {
			return ""
		},
		validateToken: newHetznerValidator("").validate,
	}); err != nil {
		t.Fatalf("save isolated Hetzner Credentials: %v", err)
	}
	if strings.Contains(authOut.String(), token) {
		t.Fatal("Hetzner API key was printed during live auth")
	}

	prompter := &liveValidationConfigurePrompter{
		locationName:   strings.TrimSpace(env[liveValidationLocationEnv]),
		serverTypeName: strings.TrimSpace(env[liveValidationServerTypeEnv]),
		serverName:     serverName,
		password:       "myn-live-validation-password",
	}
	var out bytes.Buffer
	err = runConfigure(&out, configureOptions{
		localRoot:          liveValidationRemoteRoot,
		localRootSet:       true,
		remoteRoot:         liveValidationRemoteRoot,
		remoteRootSet:      true,
		sshIdentityFile:    identityPath,
		sshIdentityFileSet: true,
	}, configureDeps{
		ctx: ctx,
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		prompter: prompter,
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(gotToken string) personalServerCloudClient {
				if gotToken != token {
					t.Fatalf("live configure used unexpected Hetzner token")
				}
				return newHcloudPersonalServerCloudClient(gotToken, "")
			},
			userHomeDir: func() (string, error) {
				return home, nil
			},
			runSSH:           liveValidationSSHRunner(knownHostsPath),
			bootstrapTimeout: liveValidationBootstrapLimit,
			currentUsername: func() string {
				return liveValidationUser
			},
			gitConfigValue: func(_ personalServerGitConfigScope, key string) (string, bool) {
				switch key {
				case "user.name":
					return liveValidationGitName, true
				case "user.email":
					return liveValidationGitEmail, true
				default:
					return "", false
				}
			},
		},
	})
	if strings.Contains(out.String(), token) {
		t.Fatal("Hetzner API key was printed during live configure")
	}

	cfg, loadErr := loadAppConfig(configPath)
	if loadErr != nil {
		t.Fatalf("load isolated config: %v", loadErr)
	}
	if err != nil {
		t.Fatalf("live configure failed: %v\n%s", err, out.String())
	}
	assertLiveValidationConfig(t, cfg)

	server, _, err := client.Server.GetByID(ctx, int64(cfg.PersonalServer.ServerID))
	if err != nil {
		t.Fatalf("load live validation server: %v", err)
	}
	if server == nil {
		t.Fatalf("live validation server %d was not found after configure", cfg.PersonalServer.ServerID)
	}
	assertLiveValidationServer(t, server, cfg, serverName, prompter.selectedLocationName, prompter.selectedServerTypeName)
	assertLiveValidationFirewall(t, ctx, client, beforeFirewall, firewallExisted)
	assertLiveValidationSSHKey(t, ctx, client, beforeSSHKey, sshKeyExisted, sshKeyFingerprint, publicKeyLine)
	assertLiveValidationBootstrap(t, ctx, identityPath, knownHostsPath, cfg.PersonalServer.IPv4)
	assertLiveValidationRemoteSetup(t, ctx, identityPath, knownHostsPath, cfg.PersonalServer.IPv4)
}

func loadLiveValidationEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("live validation requires .env.local at %s with %s", path, liveValidationAPIKeyEnv)
	}
	if err != nil {
		return nil, fmt.Errorf("read live validation .env.local: %w", err)
	}

	env := make(map[string]string)
	for lineNumber, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("parse live validation .env.local line %d: expected KEY=VALUE", lineNumber+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return nil, fmt.Errorf("parse live validation .env.local line %d: key is empty", lineNumber+1)
		}
		if len(value) >= 2 {
			quote := value[0]
			if (quote == '\'' || quote == '"') && value[len(value)-1] == quote {
				value = value[1 : len(value)-1]
			}
		}
		env[key] = value
	}
	return env, nil
}

func liveValidationHetznerAPIKey(env map[string]string) (string, error) {
	token := strings.TrimSpace(env[liveValidationAPIKeyEnv])
	if token == "" {
		return "", fmt.Errorf("live validation requires %s in .env.local", liveValidationAPIKeyEnv)
	}
	return token, nil
}

func findLiveValidationRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("find working directory: %w", err)
	}
	for {
		if fileExists(filepath.Join(dir, "prd.md")) && fileExists(filepath.Join(dir, "go", "cli", "go.mod")) {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repository root from %s", dir)
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func generateLiveValidationSSHKey(t *testing.T, identityPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(identityPath), 0o700); err != nil {
		t.Fatalf("create isolated ~/.ssh: %v", err)
	}
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-N", "", "-C", "myn-live-validation", "-f", identityPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate live validation SSH key: %v", commandOutputError("ssh-keygen", output, err))
	}
	if err := os.Chmod(identityPath, 0o600); err != nil {
		t.Fatalf("secure live validation SSH key: %v", err)
	}
	if err := os.Chmod(identityPath+".pub", 0o644); err != nil {
		t.Fatalf("secure live validation SSH public key: %v", err)
	}
}

func liveValidationSSHPublicKey(t *testing.T, identityPath string) string {
	t.Helper()
	output, err := exec.Command("ssh-keygen", "-y", "-f", identityPath).CombinedOutput()
	if err != nil {
		t.Fatalf("read live validation SSH public key: %v", commandOutputError("ssh-keygen -y", output, err))
	}
	return strings.TrimSpace(string(output))
}

func liveValidationServerName(t *testing.T) string {
	t.Helper()
	var randomBytes [3]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		t.Fatalf("generate live validation server suffix: %v", err)
	}
	return fmt.Sprintf("myn-live-%s-%s", time.Now().UTC().Format("20060102-150405"), hex.EncodeToString(randomBytes[:]))
}

func liveValidationHcloudClient(token string) *hcloud.Client {
	return hcloud.NewClient(
		hcloud.WithToken(token),
		hcloud.WithPollInterval(5*time.Second),
	)
}

func liveValidationSSHRunner(knownHostsPath string) personalServerSSHRunner {
	return func(ctx context.Context, identityFile string, user string, host string, command string) (string, error) {
		if err := os.MkdirAll(filepath.Dir(knownHostsPath), 0o700); err != nil {
			return "", fmt.Errorf("create isolated SSH known_hosts directory: %w", err)
		}
		sshHost := user + "@" + personalServerSSHCommandHost(host)
		cmd := exec.CommandContext(ctx, "ssh",
			"-o", "BatchMode=yes",
			"-o", "StrictHostKeyChecking=accept-new",
			"-o", "UserKnownHostsFile="+knownHostsPath,
			"-o", "ConnectTimeout=10",
			"-i", identityFile,
			sshHost,
			command,
		)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", commandOutputError("ssh", output, err)
		}
		return string(output), nil
	}
}

type liveValidationConfigurePrompter struct {
	locationName           string
	serverTypeName         string
	serverName             string
	password               string
	selectedLocationName   string
	selectedServerTypeName string
}

func (p *liveValidationConfigurePrompter) CanPrompt() bool {
	return true
}

func (p *liveValidationConfigurePrompter) Confirm(title string, affirmative bool) (bool, error) {
	if title == "Create Personal Server?" {
		return true, nil
	}
	return affirmative, nil
}

func (p *liveValidationConfigurePrompter) Input(title string, defaultValue string, validate func(string) error) (string, error) {
	value := defaultValue
	switch title {
	case "Personal Server User":
		value = liveValidationUser
	case "Personal Server name":
		value = p.serverName
	}
	if validate != nil {
		if err := validate(value); err != nil {
			return "", err
		}
	}
	return value, nil
}

func (p *liveValidationConfigurePrompter) Password(string) (string, error) {
	return p.password, nil
}

func (p *liveValidationConfigurePrompter) SelectSSHIdentity(choices []sshIdentityPromptChoice, selected int) (sshIdentityPromptChoice, error) {
	if selected < 0 || selected >= len(choices) {
		return sshIdentityPromptChoice{}, fmt.Errorf("SSH selection index out of range")
	}
	return choices[selected], nil
}

func (p *liveValidationConfigurePrompter) SelectPersonalServerLocation(choices []personalServerLocationChoice, selected int) (personalServerLocationChoice, error) {
	index := selected
	if p.locationName != "" {
		index = -1
		for i, choice := range choices {
			if choice.Location.Name == p.locationName {
				index = i
				break
			}
		}
	}
	if index < 0 || index >= len(choices) {
		return personalServerLocationChoice{}, fmt.Errorf("live validation Location %q was not offered", p.locationName)
	}
	p.selectedLocationName = choices[index].Location.Name
	return choices[index], nil
}

func (p *liveValidationConfigurePrompter) SelectPersonalServerType(choices []personalServerTypeChoice, selected int) (personalServerTypeChoice, error) {
	index := selected
	if p.serverTypeName != "" {
		index = -1
		for i, choice := range choices {
			if choice.ServerType.Name == p.serverTypeName {
				index = i
				break
			}
		}
	} else if len(choices) > 0 {
		index = cheapestLiveValidationServerTypeChoice(choices, p.selectedLocationName)
	}
	if index < 0 || index >= len(choices) {
		return personalServerTypeChoice{}, fmt.Errorf("live validation Server Type %q was not offered", p.serverTypeName)
	}
	p.selectedServerTypeName = choices[index].ServerType.Name
	return choices[index], nil
}

func cheapestLiveValidationServerTypeChoice(choices []personalServerTypeChoice, locationName string) int {
	selected := 0
	selectedPrice, selectedPriced := personalServerTypeMonthlyGross(choices[selected].ServerType, locationName)
	for index := 1; index < len(choices); index++ {
		price, priced := personalServerTypeMonthlyGross(choices[index].ServerType, locationName)
		switch {
		case priced && !selectedPriced:
			selected = index
			selectedPrice = price
			selectedPriced = true
		case priced && selectedPriced && price < selectedPrice:
			selected = index
			selectedPrice = price
		case priced == selectedPriced && price == selectedPrice && choices[index].ServerType.Name < choices[selected].ServerType.Name:
			selected = index
		}
	}
	return selected
}

func cleanupLiveValidationServer(ctx context.Context, t *testing.T, client *hcloud.Client, configPath string, serverName string) {
	t.Helper()
	serverIDs := make(map[int64]struct{})
	if cfg, err := loadAppConfig(configPath); err == nil && cfg.PersonalServer.ServerID != 0 {
		serverIDs[int64(cfg.PersonalServer.ServerID)] = struct{}{}
	}
	if strings.TrimSpace(serverName) != "" {
		server, _, err := client.Server.GetByName(ctx, serverName)
		if err != nil {
			t.Errorf("find live validation server by name for cleanup: %v", err)
		} else if server != nil {
			serverIDs[server.ID] = struct{}{}
		}
	}

	for id := range serverIDs {
		server, _, err := client.Server.GetByID(ctx, id)
		if err != nil {
			t.Errorf("find live validation server %d for cleanup: %v", id, err)
			continue
		}
		if server == nil {
			continue
		}
		result, _, err := client.Server.DeleteWithResult(ctx, server)
		if err != nil {
			t.Errorf("delete live validation server %d: %v", id, err)
			continue
		}
		if result != nil && result.Action != nil {
			if err := client.Action.WaitFor(ctx, result.Action); err != nil {
				t.Errorf("wait for live validation server %d deletion: %v", id, err)
				continue
			}
		}
		t.Logf("deleted live validation server %d (%s)", id, server.Name)
	}
}

func assertLiveValidationConfig(t *testing.T, cfg appConfig) {
	t.Helper()
	if cfg.PersonalServer.ServerID == 0 {
		t.Fatal("live validation did not save Personal Server serverID")
	}
	if got := strings.TrimSpace(cfg.PersonalServer.User); got != liveValidationUser {
		t.Fatalf("live validation Personal Server User mismatch: want %q, got %q", liveValidationUser, got)
	}
	if strings.TrimSpace(cfg.PersonalServer.IPv4) == "" {
		t.Fatal("live validation did not save Personal Server IPv4")
	}
	if strings.TrimSpace(cfg.PersonalServer.IPv6) == "" {
		t.Fatal("live validation did not save Personal Server IPv6")
	}
}

func assertLiveValidationServer(t *testing.T, server *hcloud.Server, cfg appConfig, serverName string, locationName string, serverTypeName string) {
	t.Helper()
	if server.Name != serverName {
		t.Fatalf("live validation server name mismatch: want %q, got %q", serverName, server.Name)
	}
	if locationName != "" && (server.Location == nil || server.Location.Name != locationName) {
		t.Fatalf("live validation Location mismatch: want %q, got %#v", locationName, server.Location)
	}
	if serverTypeName != "" && (server.ServerType == nil || server.ServerType.Name != serverTypeName) {
		t.Fatalf("live validation Server Type mismatch: want %q, got %#v", serverTypeName, server.ServerType)
	}
	assertLiveValidationLabels(t, "server", server.Labels)
	if server.PublicNet.IPv4.IsUnspecified() || server.PublicNet.IPv4.IP.String() != cfg.PersonalServer.IPv4 {
		t.Fatalf("live validation server IPv4 mismatch: config=%q live=%q", cfg.PersonalServer.IPv4, server.PublicNet.IPv4.IP.String())
	}
	if server.PublicNet.IPv6.IsUnspecified() || server.PublicNet.IPv6.IP.String() != cfg.PersonalServer.IPv6 {
		t.Fatalf("live validation server IPv6 mismatch: config=%q live=%q", cfg.PersonalServer.IPv6, server.PublicNet.IPv6.IP.String())
	}
}

func assertLiveValidationFirewall(t *testing.T, ctx context.Context, client *hcloud.Client, before *hcloud.Firewall, existed bool) {
	t.Helper()
	after, _, err := client.Firewall.GetByName(ctx, personalServerFirewallName)
	if err != nil {
		t.Fatalf("load live Personal Server Firewall after configure: %v", err)
	}
	if after == nil {
		t.Fatal("live Personal Server Firewall was not found after configure")
	}
	if existed {
		if before == nil || before.ID != after.ID {
			t.Fatalf("live validation should reuse existing firewall: before=%#v after=%#v", before, after)
		}
		if !sameFirewallRules(before.Rules, after.Rules) {
			t.Fatalf("live validation changed existing firewall rules: before=%v after=%v", firewallRuleSummaries(before.Rules), firewallRuleSummaries(after.Rules))
		}
		t.Logf("reused existing Personal Server Firewall %d and left rules untouched", after.ID)
		return
	}

	assertLiveValidationLabels(t, "firewall", after.Labels)
	if got, want := firewallRuleSummaries(after.Rules), []string{
		"in tcp 22 0.0.0.0/0,::/0",
		"in udp 60000-61000 0.0.0.0/0,::/0",
	}; !equalStringSlices(got, want) {
		t.Fatalf("new live Personal Server Firewall rules mismatch: want %v, got %v", want, got)
	}
	t.Logf("created Personal Server Firewall %d and intentionally left it in place", after.ID)
}

func assertLiveValidationSSHKey(t *testing.T, ctx context.Context, client *hcloud.Client, before *hcloud.SSHKey, existed bool, fingerprint string, publicKeyLine string) {
	t.Helper()
	after, _, err := client.SSHKey.GetByFingerprint(ctx, fingerprint)
	if err != nil {
		t.Fatalf("load live Personal Server SSH Key after configure: %v", err)
	}
	if after == nil {
		t.Fatal("live Personal Server SSH Key was not found after configure")
	}
	if existed {
		if before == nil || before.ID != after.ID {
			t.Fatalf("live validation should reuse existing SSH key: before=%#v after=%#v", before, after)
		}
		t.Logf("reused existing Personal Server SSH Key %d", after.ID)
		return
	}

	assertLiveValidationLabels(t, "ssh key", after.Labels)
	if strings.TrimSpace(after.PublicKey) != strings.TrimSpace(publicKeyLine) {
		t.Fatal("created live Personal Server SSH Key public key does not match isolated identity")
	}
	t.Logf("created Personal Server SSH Key %d and intentionally left it in place", after.ID)
}

func assertLiveValidationBootstrap(t *testing.T, ctx context.Context, identityPath string, knownHostsPath string, host string) {
	t.Helper()
	markerOutput := liveValidationSSH(t, ctx, identityPath, knownHostsPath, "root", host, "cat "+personalServerBootstrapMarkerPath)
	marker, err := parsePersonalServerBootstrapMarker(markerOutput)
	if err != nil {
		t.Fatalf("parse live Personal Server Bootstrap marker: %v", err)
	}
	if strings.ToLower(marker.Status) != "success" {
		t.Fatalf("live Personal Server Bootstrap marker was not successful: %#v", marker)
	}

	requiredTools := []string{"docker", "dockerCompose", "brew", "tmux", "jq", "git", "gh", "rustup", "go", "nvm", "node", "npm"}
	for _, tool := range requiredTools {
		if strings.TrimSpace(marker.ToolVersions[tool]) == "" {
			t.Fatalf("live Personal Server Bootstrap marker is missing %s version: %#v", tool, marker.ToolVersions)
		}
	}
	for _, optionalAgent := range []string{"codex", "claude"} {
		if strings.TrimSpace(marker.ToolVersions[optionalAgent]) == "" {
			t.Logf("live Personal Server Bootstrap marker has no %s version; partial failures: %v", optionalAgent, marker.PartialFailures)
		}
	}
	if len(marker.PartialFailures) > 0 {
		t.Logf("live Personal Server Bootstrap reported partial failures: %v", marker.PartialFailures)
	}
}

func assertLiveValidationRemoteSetup(t *testing.T, ctx context.Context, identityPath string, knownHostsPath string, host string) {
	t.Helper()
	liveValidationSSH(t, ctx, identityPath, knownHostsPath, "root", host, "true")
	if got := strings.TrimSpace(liveValidationSSH(t, ctx, identityPath, knownHostsPath, liveValidationUser, host, "id -un")); got != liveValidationUser {
		t.Fatalf("live Personal Server User SSH mismatch: want %q, got %q", liveValidationUser, got)
	}

	passwd := strings.TrimSpace(liveValidationSSH(t, ctx, identityPath, knownHostsPath, "root", host, "getent passwd "+shellQuote(liveValidationUser)+" | cut -d: -f1,7"))
	if passwd != liveValidationUser+":/bin/bash" {
		t.Fatalf("live Personal Server User passwd entry mismatch: %q", passwd)
	}
	groups := strings.Fields(liveValidationSSH(t, ctx, identityPath, knownHostsPath, "root", host, "id -nG "+shellQuote(liveValidationUser)))
	for _, group := range []string{"sudo", "docker"} {
		if !containsString(groups, group) {
			t.Fatalf("live Personal Server User missing %s group: %v", group, groups)
		}
	}

	remoteRoot := "/home/" + liveValidationUser + "/" + liveValidationRemoteRoot
	statCommand := "stat -c '%U:%G:%F' " + shellQuote(remoteRoot)
	if got := strings.TrimSpace(liveValidationSSH(t, ctx, identityPath, knownHostsPath, "root", host, statCommand)); got != liveValidationUser+":"+liveValidationUser+":directory" {
		t.Fatalf("live remote project root mismatch: %q", got)
	}

	liveValidationSSH(t, ctx, identityPath, knownHostsPath, liveValidationUser, host, "docker --version && docker compose version")
	nvmOutput := strings.Fields(liveValidationSSH(t, ctx, identityPath, knownHostsPath, liveValidationUser, host, "source /etc/profile.d/myn-personal-server.sh && nvm version default && node --version"))
	if len(nvmOutput) < 2 || nvmOutput[0] == "N/A" || nvmOutput[0] != nvmOutput[1] {
		t.Fatalf("live nvm default mismatch: %v", nvmOutput)
	}
	if got := strings.TrimSpace(liveValidationSSH(t, ctx, identityPath, knownHostsPath, liveValidationUser, host, "/home/linuxbrew/.linuxbrew/bin/git config --global user.name")); got != liveValidationGitName {
		t.Fatalf("live Git user.name mismatch: want %q, got %q", liveValidationGitName, got)
	}
	if got := strings.TrimSpace(liveValidationSSH(t, ctx, identityPath, knownHostsPath, liveValidationUser, host, "/home/linuxbrew/.linuxbrew/bin/git config --global user.email")); got != liveValidationGitEmail {
		t.Fatalf("live Git user.email mismatch: want %q, got %q", liveValidationGitEmail, got)
	}
}

func liveValidationSSH(t *testing.T, ctx context.Context, identityPath string, knownHostsPath string, user string, host string, command string) string {
	t.Helper()
	output, err := liveValidationSSHRunner(knownHostsPath)(ctx, identityPath, user, host, command)
	if err != nil {
		t.Fatalf("run live SSH command as %s: %v", user, err)
	}
	return output
}

func assertLiveValidationLabels(t *testing.T, resource string, labels map[string]string) {
	t.Helper()
	for key, value := range personalServerResourceLabels() {
		if labels[key] != value {
			t.Fatalf("live %s label %s mismatch: want %q, got %q", resource, key, value, labels[key])
		}
	}
}

func sameFirewallRules(left []hcloud.FirewallRule, right []hcloud.FirewallRule) bool {
	return equalStringSlices(firewallRuleSummaries(left), firewallRuleSummaries(right))
}

func firewallRuleSummaries(rules []hcloud.FirewallRule) []string {
	summaries := make([]string, 0, len(rules))
	for _, rule := range rules {
		sources := make([]string, 0, len(rule.SourceIPs))
		for _, source := range rule.SourceIPs {
			sources = append(sources, source.String())
		}
		sort.Strings(sources)
		summaries = append(summaries, fmt.Sprintf("%s %s %s %s", rule.Direction, rule.Protocol, stringValue(rule.Port), strings.Join(sources, ",")))
	}
	sort.Strings(summaries)
	return summaries
}

func equalStringSlices(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
