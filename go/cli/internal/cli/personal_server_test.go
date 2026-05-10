package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestRunConfigureReportsExistingPersonalServer(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
		PersonalServer: personalServerConfig{
			ServerID: 123456,
			IPv4:     "203.0.113.10",
			IPv6:     "2001:db8::1",
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cloud := &fakePersonalServerCloudClient{
		servers: map[int]personalServerCloudServer{
			123456: {
				ID:   123456,
				IPv4: "198.51.100.24",
				IPv6: "2001:db8::24",
			},
		},
	}
	gate := personalServerProvisioningGate{
		newCloudClient: func(token string) personalServerCloudClient {
			if token != "existing-token" {
				t.Fatalf("token mismatch: %q", token)
			}
			return cloud
		},
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey:              testSSHPublicKeyFunc(identity),
		prompter:                  &fakeConfigurePrompter{canPrompt: true},
		personalServerProvisioner: gate,
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if got, want := cloud.serverIDs, []int{123456}; !reflect.DeepEqual(got, want) {
		t.Fatalf("verified server IDs mismatch: want %v, got %v", want, got)
	}
	output := out.String()
	for _, want := range []string{
		"Personal Server already configured: server 123456 exists.",
		"Saved addresses: IPv4 203.0.113.10, IPv6 2001:db8::1",
		"Current addresses: IPv4 198.51.100.24, IPv6 2001:db8::24",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got %q", want, output)
		}
	}
	if strings.Contains(output, "Personal Server provisioning prerequisites are ready.") {
		t.Fatalf("existing server should skip creation path, got %q", output)
	}
}

func TestRunConfigureClearsStalePersonalServerConfigurationWhenInteractive(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
		PersonalServer: personalServerConfig{
			ServerID: 123456,
			IPv4:     "203.0.113.10",
			IPv6:     "2001:db8::1",
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cloud := &fakePersonalServerCloudClient{
		locations: []personalServerLocation{
			{Name: "ash", Description: "Ashburn, VA, USA"},
		},
		serverTypes: []personalServerType{
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		},
	}
	gate := personalServerProvisioningGate{
		newCloudClient: func(string) personalServerCloudClient {
			return cloud
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		confirms:  []bool{true},
		passwords: []string{"server-secret", "server-secret"},
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey:              testSSHPublicKeyFunc(identity),
		prompter:                  prompter,
		personalServerProvisioner: gate,
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if got, want := prompter.confirmCalls, []string{
		"Clear stale Personal Server Configuration for missing server 123456?",
		"Create Personal Server?",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("confirm calls mismatch: want %v, got %v", want, got)
	}
	output := out.String()
	for _, want := range []string{
		"Personal Server Configuration references missing server 123456.",
		"Cleared stale Personal Server Configuration.",
		"Personal Server provisioning prerequisites are ready.",
		"Personal Server creation declined. No cloud resources were created.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got %q", want, output)
		}
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.PersonalServer.isZero() {
		t.Fatalf("Personal Server Configuration should be cleared, got %#v", cfg.PersonalServer)
	}
}

func TestRunConfigureFailsForStalePersonalServerConfigurationWhenNonInteractive(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
		PersonalServer: personalServerConfig{
			ServerID: 123456,
			IPv4:     "203.0.113.10",
			IPv6:     "2001:db8::1",
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter:     &fakeConfigurePrompter{canPrompt: false},
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return &fakePersonalServerCloudClient{}
			},
		},
	})
	if err == nil {
		t.Fatal("expected stale Personal Server Configuration error")
	}
	if !strings.Contains(err.Error(), "Personal Server Configuration references missing server 123456; rerun `me configure` interactively to clear it") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Personal Server Configuration references missing server 123456.") {
		t.Fatalf("expected missing server output, got %q", out.String())
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.PersonalServer, (personalServerConfig{ServerID: 123456, IPv4: "203.0.113.10", IPv6: "2001:db8::1"}); got != want {
		t.Fatalf("Personal Server Configuration should be preserved: want %#v, got %#v", want, got)
	}
}

func TestRunConfigureDoesNotAutoAdoptPersonalServerWithoutSavedConfiguration(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cloud := &fakePersonalServerCloudClient{
		servers: map[int]personalServerCloudServer{
			123456: {
				ID:   123456,
				IPv4: "198.51.100.24",
				IPv6: "2001:db8::24",
			},
		},
		locations: []personalServerLocation{
			{Name: "ash", Description: "Ashburn, VA, USA"},
		},
		serverTypes: []personalServerType{
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		},
	}
	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter: &fakeConfigurePrompter{
			canPrompt: true,
			passwords: []string{"server-secret", "server-secret"},
		},
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return cloud
			},
		},
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if len(cloud.serverIDs) != 0 {
		t.Fatalf("expected no Hetzner lookup without saved Personal Server Configuration, got %v", cloud.serverIDs)
	}
	if !strings.Contains(out.String(), "Personal Server provisioning prerequisites are ready.") {
		t.Fatalf("expected ready output, got %q", out.String())
	}
}

func TestRunConfigurePreviewsLocationAndEligibleServerTypes(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cloud := &fakePersonalServerCloudClient{
		locations: []personalServerLocation{
			{Name: "fsn1", Description: "Falkenstein, Germany"},
			{Name: "hil", Description: "Hillsboro, OR, USA"},
			{Name: "ash", Description: "Ashburn, VA, USA"},
		},
		serverTypes: []personalServerType{
			fakePersonalServerType("cpx41", "shared", "x86", false, 8, 16, 240, "local", "ash", true, false, "20.00"),
			fakePersonalServerType("ccx23", "dedicated", "x86", false, 4, 16, 160, "local", "ash", true, false, "22.00"),
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "ceph", "ash", true, false, "18.50"),
			fakePersonalServerType("cax21", "shared", "arm", false, 4, 8, 80, "local", "ash", true, false, "12.00"),
			fakePersonalServerType("old-x86", "shared", "x86", true, 2, 4, 40, "local", "ash", true, false, "8.00"),
			fakePersonalServerType("unavailable-x86", "shared", "x86", false, 2, 4, 40, "local", "ash", false, false, "8.00"),
			fakePersonalServerType("location-deprecated-x86", "shared", "x86", false, 2, 4, 40, "local", "ash", true, true, "8.00"),
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		passwords: []string{"server-secret", "server-secret"},
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter:     prompter,
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(token string) personalServerCloudClient {
				if token != "existing-token" {
					t.Fatalf("token mismatch: %q", token)
				}
				return cloud
			},
		},
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if len(prompter.locationCalls) != 1 {
		t.Fatalf("location prompt count mismatch: %d", len(prompter.locationCalls))
	}
	locationCall := prompter.locationCalls[0]
	if got, want := locationCall.selected, 0; got != want {
		t.Fatalf("Location default mismatch: want %d, got %d", want, got)
	}
	if got, want := personalServerLocationChoiceLabels(locationCall.choices), []string{
		"ash - Ashburn, VA, USA",
		"fsn1 - Falkenstein, Germany",
		"hil - Hillsboro, OR, USA",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Location choices mismatch: want %v, got %v", want, got)
	}

	if len(prompter.serverTypeCalls) != 1 {
		t.Fatalf("Server Type prompt count mismatch: %d", len(prompter.serverTypeCalls))
	}
	serverTypeCall := prompter.serverTypeCalls[0]
	if got, want := serverTypeCall.selected, 0; got != want {
		t.Fatalf("Server Type default mismatch: want %d, got %d", want, got)
	}
	if got, want := personalServerTypeChoiceLabels(serverTypeCall.choices), []string{
		"ccx23 - dedicated, 4 vCPU, 16 GB RAM, 160 GB local disk",
		"cpx41 - shared, 8 vCPU, 16 GB RAM, 240 GB local disk",
		"cx32 - shared, 4 vCPU, 8 GB RAM, 80 GB ceph disk",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Server Type choices mismatch: want %v, got %v", want, got)
	}
	for _, label := range personalServerTypeChoiceLabels(serverTypeCall.choices) {
		for _, forbidden := range []string{"EUR", "€", "20.00", "22.00", "18.50"} {
			if strings.Contains(label, forbidden) {
				t.Fatalf("Server Type selector label should not show price %q in %q", forbidden, label)
			}
		}
	}
	if got, want := prompter.confirmCalls, []string{"Create Personal Server?"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("confirm calls mismatch: want %v, got %v", want, got)
	}
	if !strings.Contains(out.String(), "Personal Server creation declined. No cloud resources were created.") {
		t.Fatalf("expected declined output, got %q", out.String())
	}
	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.PersonalServer.isZero() {
		t.Fatalf("declined preview should not save Personal Server Configuration, got %#v", cfg.PersonalServer)
	}
}

func TestRunConfigureCollectsPersonalServerCreationInputsAndDeclinesFinalConfirmation(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cloud := &fakePersonalServerCloudClient{
		locations: []personalServerLocation{
			{Name: "ash", Description: "Ashburn, VA, USA"},
		},
		serverTypes: []personalServerType{
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		},
	}
	var gitConfigCalls []string
	gate := personalServerProvisioningGate{
		newCloudClient: func(string) personalServerCloudClient {
			return cloud
		},
		currentUsername: func() string {
			return `ACME\Harish Subra`
		},
		gitConfigValue: func(scope personalServerGitConfigScope, key string) (string, bool) {
			gitConfigCalls = append(gitConfigCalls, string(scope)+":"+key)
			switch {
			case scope == personalServerGitConfigGlobal && key == "user.name":
				return "Global Name", true
			case scope == personalServerGitConfigLocal && key == "user.email":
				return "local@example.test", true
			default:
				return "", false
			}
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		inputs:    []string{"harish-subra", "harish-dev"},
		passwords: []string{"server-secret", "server-secret"},
		confirms:  []bool{false},
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "Remote Projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey:              testSSHPublicKeyFunc(identity),
		prompter:                  prompter,
		personalServerProvisioner: gate,
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if got, want := prompter.calls, []configurePromptCall{
		{title: "Personal Server User", defaultValue: "harish-subra"},
		{title: "Personal Server name", defaultValue: "harish-subra-personal-server"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("input prompts mismatch: want %#v, got %#v", want, got)
	}
	if got, want := prompter.passwordCalls, []string{
		"Personal Server User password",
		"Confirm Personal Server User password",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("password prompts mismatch: want %v, got %v", want, got)
	}
	if got, want := prompter.confirmCalls, []string{"Create Personal Server?"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("confirm calls mismatch: want %v, got %v", want, got)
	}
	if got, want := gitConfigCalls, []string{
		"global:user.name",
		"global:user.email",
		"local:user.email",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("git config calls mismatch: want %v, got %v", want, got)
	}

	output := out.String()
	for _, want := range []string{
		"Personal Server plan:",
		"Location: ash",
		"Server Type: cx32",
		"Server name: harish-dev",
		"Personal Server User: harish-subra",
		"SSH key: ~/.ssh/id_ed25519",
		"Firewall: me-personal-server (inbound SSH over IPv4 and IPv6)",
		"Public network: IPv4 and IPv6 enabled",
		"Remote project root: ~/Remote Projects",
		"Install plan:",
		"System services:",
		"- security updates and unattended security upgrades",
		"- Docker Engine and Docker Compose",
		"- Homebrew",
		"Homebrew tools:",
		"- tmux, jq, git, gh, rustup, go, nvm",
		"Coding agents:",
		"- Codex",
		"- Claude Code",
		"Git identity:",
		"- user.name: Global Name",
		"- user.email: local@example.test",
		"Maximum monthly price: 18.50 EUR gross",
		"Personal Server creation declined. No cloud resources were created.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got %q", want, output)
		}
	}
	for _, forbidden := range []string{"server-secret", "$6$"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("output should not reveal password material %q: %q", forbidden, output)
		}
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.PersonalServer.isZero() {
		t.Fatalf("declined confirmation should not save Personal Server Configuration, got %#v", cfg.PersonalServer)
	}
}

func TestRunConfigureFinalConfirmationReportsUnavailablePricing(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cloud := &fakePersonalServerCloudClient{
		locations: []personalServerLocation{{Name: "ash", Description: "Ashburn, VA, USA"}},
		serverTypes: []personalServerType{
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "not-a-price"),
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		inputs:    []string{"harish", "harish-personal-server"},
		passwords: []string{"server-secret", "server-secret"},
		confirms:  []bool{false},
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter:     prompter,
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return cloud
			},
			currentUsername: func() string {
				return "harish"
			},
		},
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if !strings.Contains(out.String(), "Maximum monthly price: unavailable") {
		t.Fatalf("expected unavailable pricing output, got %q", out.String())
	}
}

func TestCollectPersonalServerCreationInputsPromptsWhenUsernameCannotNormalize(t *testing.T) {
	gate := personalServerProvisioningGate{
		currentUsername: func() string {
			return "!!!"
		},
		gitConfigValue: func(personalServerGitConfigScope, string) (string, bool) {
			return "", false
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		inputs:    []string{"dev-user", "dev-user-personal-server"},
		passwords: []string{"server-secret", "server-secret"},
	}

	inputs, err := gate.collectPersonalServerCreationInputs(prompter)
	if err != nil {
		t.Fatalf("collect inputs: %v", err)
	}

	if got, want := prompter.calls, []configurePromptCall{
		{title: "Personal Server User", defaultValue: ""},
		{title: "Personal Server name", defaultValue: "dev-user-personal-server"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("input prompts mismatch: want %#v, got %#v", want, got)
	}
	if got, want := inputs.User, "dev-user"; got != want {
		t.Fatalf("user mismatch: want %q, got %q", want, got)
	}
	if got, want := inputs.ServerName, "dev-user-personal-server"; got != want {
		t.Fatalf("server name mismatch: want %q, got %q", want, got)
	}
	if inputs.GitIdentity.Name != "" || inputs.GitIdentity.Email != "" {
		t.Fatalf("expected missing Git identity values, got %#v", inputs.GitIdentity)
	}

	var out bytes.Buffer
	writePersonalServerCreationPlan(&out, personalServerCreationPlan{
		Location:          personalServerLocation{Name: "ash"},
		ServerType:        fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		User:              inputs.User,
		ServerName:        inputs.ServerName,
		GitIdentity:       inputs.GitIdentity,
		RemoteProjectRoot: "projects",
		SSHIdentityFile:   ".ssh/id_ed25519",
	})
	for _, want := range []string{
		"- user.name: skipped (not configured)",
		"- user.email: skipped (not configured)",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("expected output to contain %q, got %q", want, out.String())
		}
	}
}

func TestNormalizePersonalServerUser(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "uppercase", input: "HARISH", want: "harish"},
		{name: "spaces", input: "Harish Subra", want: "harish-subra"},
		{name: "domain prefix", input: `ACME\Harish.Subra`, want: "harish-subra"},
		{name: "path prefix", input: "/Users/Harish", want: "harish"},
		{name: "invalid characters", input: "harish@example.test", want: "harish-example-test"},
		{name: "leading digit", input: "9Harish", want: "user-9harish"},
		{name: "empty output", input: "!!!", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizePersonalServerUser(tt.input); got != tt.want {
				t.Fatalf("normalized user mismatch: want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestValidatePersonalServerName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "valid", input: "harish-personal-server"},
		{name: "uppercase", input: "Harish", wantErr: "lowercase"},
		{name: "underscore", input: "harish_server", wantErr: "lowercase"},
		{name: "leading hyphen", input: "-harish", wantErr: "start"},
		{name: "trailing hyphen", input: "harish-", wantErr: "end"},
		{name: "too long", input: strings.Repeat("a", 64), wantErr: "63 characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePersonalServerName(tt.input)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validate server name: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("unexpected error: want %q in %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestCollectPersonalServerPasswordHashRequiresNonEmptyConfirmedPassword(t *testing.T) {
	tests := []struct {
		name      string
		passwords []string
		wantErr   string
	}{
		{name: "empty", passwords: []string{"", ""}, wantErr: "password is required"},
		{name: "mismatch", passwords: []string{"secret", "different"}, wantErr: "password confirmation does not match"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prompter := &fakeConfigurePrompter{canPrompt: true, passwords: tt.passwords}
			_, err := collectPersonalServerPasswordHash(prompter)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("unexpected error: want %q in %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestHashPersonalServerPasswordUsesSHA512CryptWithRandomSalt(t *testing.T) {
	hash1, err := hashPersonalServerPassword("secret", strings.NewReader(strings.Repeat("\x01", 16)))
	if err != nil {
		t.Fatalf("hash first password: %v", err)
	}
	hash2, err := hashPersonalServerPassword("secret", strings.NewReader(strings.Repeat("\x02", 16)))
	if err != nil {
		t.Fatalf("hash second password: %v", err)
	}

	if hash1 == hash2 {
		t.Fatalf("expected randomized hashes to differ, got %q", hash1)
	}
	for _, hash := range []string{hash1, hash2} {
		if !regexp.MustCompile(`^\$6\$[./0-9A-Za-z]{16}\$[./0-9A-Za-z]+$`).MatchString(hash) {
			t.Fatalf("hash should use SHA-512 crypt format, got %q", hash)
		}
		if strings.Contains(hash, "secret") {
			t.Fatalf("hash should not contain plaintext password: %q", hash)
		}
	}
}

func TestRunConfigureLocationFallbackDefaultIsFirstSortedCode(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		passwords: []string{"server-secret", "server-secret"},
	}
	cloud := &fakePersonalServerCloudClient{
		locations: []personalServerLocation{
			{Name: "nbg1", Description: "Nuremberg, Germany"},
			{Name: "fsn1", Description: "Falkenstein, Germany"},
			{Name: "hil", Description: "Hillsboro, OR, USA"},
		},
		serverTypes: []personalServerType{
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "fsn1", true, false, "18.50"),
		},
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter:     prompter,
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return cloud
			},
		},
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if len(prompter.locationCalls) != 1 {
		t.Fatalf("location prompt count mismatch: %d", len(prompter.locationCalls))
	}
	if got, want := prompter.locationCalls[0].selected, 0; got != want {
		t.Fatalf("Location fallback default mismatch: want %d, got %d", want, got)
	}
	if got, want := prompter.locationCalls[0].choices[0].Location.Name, "fsn1"; got != want {
		t.Fatalf("Location fallback choice mismatch: want %q, got %q", want, got)
	}
}

func TestRunConfigureReturnsToLocationSelectionWhenNoServerTypesAreEligible(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	prompter := &fakeConfigurePrompter{
		canPrompt:          true,
		locationSelections: []int{0, 1},
		passwords:          []string{"server-secret", "server-secret"},
	}
	cloud := &fakePersonalServerCloudClient{
		locations: []personalServerLocation{
			{Name: "ash", Description: "Ashburn, VA, USA"},
			{Name: "fsn1", Description: "Falkenstein, Germany"},
		},
		serverTypes: []personalServerType{
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "fsn1", true, false, "18.50"),
		},
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter:     prompter,
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return cloud
			},
		},
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if len(prompter.locationCalls) != 2 {
		t.Fatalf("expected Location selector to be shown twice, got %d", len(prompter.locationCalls))
	}
	if len(prompter.serverTypeCalls) != 1 {
		t.Fatalf("Server Type prompt count mismatch: %d", len(prompter.serverTypeCalls))
	}
	if got, want := prompter.serverTypeCalls[0].choices[0].ServerType.Name, "cx32"; got != want {
		t.Fatalf("Server Type choice mismatch: want %q, got %q", want, got)
	}
	if !strings.Contains(out.String(), "No eligible Server Types are available in Location ash.") {
		t.Fatalf("expected no eligible Server Types output, got %q", out.String())
	}
}

func TestRunConfigureVerifiesPersonalServerWithHetznerEndpointOverride(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
		PersonalServer: personalServerConfig{
			ServerID: 123456,
			IPv4:     "203.0.113.10",
			IPv6:     "2001:db8::1",
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet {
			t.Errorf("method mismatch: %s", r.Method)
		}
		if r.URL.Path != "/servers/123456" {
			t.Errorf("path mismatch: %s", r.URL.Path)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer existing-token"; got != want {
			t.Errorf("authorization mismatch: want %q, got %q", want, got)
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
  "server": {
    "id": 123456,
    "name": "personal",
    "public_net": {
      "ipv4": {"ip": "198.51.100.24"},
      "ipv6": {"ip": "2001:db8::/64"}
    }
  }
}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("HCLOUD_ENDPOINT", server.URL)

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter:     &fakeConfigurePrompter{canPrompt: false},
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if requests != 1 {
		t.Fatalf("request count mismatch: want 1, got %d", requests)
	}
	if !strings.Contains(out.String(), "Current addresses: IPv4 198.51.100.24, IPv6 2001:db8::") {
		t.Fatalf("expected endpoint response in output, got %q", out.String())
	}
}

type fakePersonalServerCloudClient struct {
	servers         map[int]personalServerCloudServer
	serverIDs       []int
	locations       []personalServerLocation
	serverTypes     []personalServerType
	listLocations   int
	listServerTypes int
}

func (c *fakePersonalServerCloudClient) ServerByID(_ context.Context, id int) (personalServerCloudServer, bool, error) {
	c.serverIDs = append(c.serverIDs, id)
	server, ok := c.servers[id]
	return server, ok, nil
}

func (c *fakePersonalServerCloudClient) Locations(context.Context) ([]personalServerLocation, error) {
	c.listLocations++
	return c.locations, nil
}

func (c *fakePersonalServerCloudClient) ServerTypes(context.Context) ([]personalServerType, error) {
	c.listServerTypes++
	return c.serverTypes, nil
}

type personalServerLocationSelectCall struct {
	choices  []personalServerLocationChoice
	selected int
}

type personalServerTypeSelectCall struct {
	choices  []personalServerTypeChoice
	selected int
}

func fakePersonalServerType(name string, cpuType string, architecture string, deprecated bool, cores int, memoryGB float64, diskGB int, storageType string, location string, available bool, locationDeprecated bool, monthlyGrossEUR string) personalServerType {
	return personalServerType{
		Name:         name,
		CPUType:      cpuType,
		Architecture: architecture,
		Deprecated:   deprecated,
		Cores:        cores,
		MemoryGB:     memoryGB,
		DiskGB:       diskGB,
		StorageType:  storageType,
		Locations: []personalServerTypeLocation{
			{
				LocationName: location,
				Available:    available,
				Deprecated:   locationDeprecated,
			},
		},
		Pricings: []personalServerTypeLocationPricing{
			{
				LocationName:    location,
				MonthlyGrossEUR: monthlyGrossEUR,
			},
		},
	}
}

func personalServerLocationChoiceLabels(choices []personalServerLocationChoice) []string {
	labels := make([]string, 0, len(choices))
	for _, choice := range choices {
		labels = append(labels, choice.Label)
	}
	return labels
}

func personalServerTypeChoiceLabels(choices []personalServerTypeChoice) []string {
	labels := make([]string, 0, len(choices))
	for _, choice := range choices {
		labels = append(labels, choice.Label)
	}
	return labels
}
