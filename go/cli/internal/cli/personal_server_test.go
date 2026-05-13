package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRunConfigureReportsExistingPersonalServer(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
		pricing: fakePersonalServerPricing("ipv4", "ash", "0.60"),
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
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
	if !strings.Contains(err.Error(), "Personal Server Configuration references missing server 123456; rerun `myn configure` interactively to clear it") {
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
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
		pricing: fakePersonalServerPricing("ipv4", "ash", "0.60"),
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
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
		pricing: fakePersonalServerPricing("ipv4", "ash", "0.60"),
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
		"Firewall: myn-personal-server (inbound SSH and Mosh UDP 60000-61000 over IPv4 and IPv6)",
		"Public network: IPv4 and IPv6 enabled",
		"Remote project root: ~/Remote Projects",
		"Install plan:",
		"System services:",
		"- security updates and unattended security upgrades",
		"- Mosh Access",
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
		"Maximum monthly price: 19.10 EUR gross",
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
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		},
		pricingErr: errors.New("pricing unavailable"),
		images: []personalServerImage{
			{Name: "ubuntu-24.04", Type: "system", Status: "available", OSFlavor: "ubuntu", OSVersion: "24.04", Architecture: "x86"},
		},
		createdServer: personalServerCloudServer{
			ID:   654321,
			IPv4: "203.0.113.55",
			IPv6: "2001:db8::55",
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		inputs:    []string{"harish", "harish-personal-server"},
		passwords: []string{"server-secret", "server-secret"},
		confirms:  []bool{true},
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
			userHomeDir: func() (string, error) {
				return home, nil
			},
			runSSH: newSuccessfulPersonalServerSSHRunner().Run,
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
	if !strings.Contains(out.String(), "Personal Server created: server 654321.") {
		t.Fatalf("expected unavailable pricing to still permit creation, got %q", out.String())
	}
}

func TestRunConfigureCreatesHetznerResourcesAndSavesPersonalServer(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "Remote Projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		},
		images: []personalServerImage{
			{Name: "ubuntu-22.04", Type: "system", Status: "available", OSFlavor: "ubuntu", OSVersion: "22.04", Architecture: "x86"},
			{Name: "ubuntu-24.04", Type: "system", Status: "available", OSFlavor: "ubuntu", OSVersion: "24.04", Architecture: "x86"},
			{Name: "ubuntu-26.04-deprecated", Type: "system", Status: "available", OSFlavor: "ubuntu", OSVersion: "26.04", Architecture: "x86", Deprecated: true},
			{Name: "debian-13", Type: "system", Status: "available", OSFlavor: "debian", OSVersion: "13", Architecture: "x86"},
		},
		createdServer: personalServerCloudServer{
			ID:   654321,
			IPv4: "203.0.113.55",
			IPv6: "2001:db8::55",
		},
	}
	var savedAfterWait bool
	gate := personalServerProvisioningGate{
		newCloudClient: func(token string) personalServerCloudClient {
			if token != "existing-token" {
				t.Fatalf("token mismatch: %q", token)
			}
			return cloud
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		saveConfig: func(path string, cfg appConfig) error {
			if !cloud.waitedActions {
				t.Fatal("Personal Server Configuration was saved before Hetzner create actions were waited")
			}
			savedAfterWait = true
			return saveAppConfig(path, cfg)
		},
		runSSH: newSuccessfulPersonalServerSSHRunner().Run,
		currentUsername: func() string {
			return "harish"
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		inputs:    []string{"harish", "harish-personal-server"},
		passwords: []string{"server-secret", "server-secret"},
		confirms:  []bool{true},
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "Remote Projects",
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

	if !savedAfterWait {
		t.Fatal("Personal Server Configuration was not saved by the provisioning gate")
	}
	if got := cloud.serverNames; !reflect.DeepEqual(got, []string{"harish-personal-server"}) {
		t.Fatalf("server name checks mismatch: want %v, got %v", []string{"harish-personal-server"}, got)
	}
	if got := cloud.createdFirewall; got.Name != "myn-personal-server" {
		t.Fatalf("created firewall name mismatch: %#v", got)
	}
	if got, want := cloud.createdFirewall.Labels, personalServerResourceLabels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("created firewall labels mismatch: want %v, got %v", want, got)
	}
	if got, want := cloud.createdFirewall.Rules, []personalServerFirewallRule{
		{Direction: "in", Protocol: "tcp", Port: "22", SourceIPs: []string{"0.0.0.0/0", "::/0"}},
		{Direction: "in", Protocol: "udp", Port: "60000-61000", SourceIPs: []string{"0.0.0.0/0", "::/0"}},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("created firewall rules mismatch: want %#v, got %#v", want, got)
	}
	if got, want := cloud.createdSSHKey.PublicKey, strings.TrimSpace(identity.PublicLine); got != want {
		t.Fatalf("created SSH key public key mismatch: want %q, got %q", want, got)
	}
	if got, want := cloud.createdSSHKey.Name, "myn-personal-server-"+strings.ReplaceAll(identity.HetznerFingerprint, ":", ""); got != want {
		t.Fatalf("created SSH key name mismatch: want %q, got %q", want, got)
	}
	if got, want := cloud.sshKeyFingerprints, []string{identity.HetznerFingerprint}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SSH key fingerprint lookup mismatch: want %v, got %v", want, got)
	}
	if got, want := cloud.createdSSHKey.Labels, personalServerResourceLabels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("created SSH key labels mismatch: want %v, got %v", want, got)
	}
	create := cloud.serverCreateRequest
	if got, want := create.Name, "harish-personal-server"; got != want {
		t.Fatalf("server create name mismatch: want %q, got %q", want, got)
	}
	if got, want := create.LocationName, "ash"; got != want {
		t.Fatalf("server create Location mismatch: want %q, got %q", want, got)
	}
	if got, want := create.ServerTypeName, "cx32"; got != want {
		t.Fatalf("server create Server Type mismatch: want %q, got %q", want, got)
	}
	if got, want := create.ImageName, "ubuntu-24.04"; got != want {
		t.Fatalf("server create image mismatch: want %q, got %q", want, got)
	}
	if !create.EnableIPv4 || !create.EnableIPv6 {
		t.Fatalf("server create should enable IPv4 and IPv6, got %#v", create)
	}
	if got, want := create.Labels, personalServerResourceLabels(); !reflect.DeepEqual(got, want) {
		t.Fatalf("server create labels mismatch: want %v, got %v", want, got)
	}
	if got, want := create.SSHKeyID, 3001; got != want {
		t.Fatalf("server create SSH key mismatch: want %d, got %d", want, got)
	}
	if got, want := create.FirewallID, 2001; got != want {
		t.Fatalf("server create firewall mismatch: want %d, got %d", want, got)
	}
	if !strings.Contains(create.UserData, "#cloud-config\n") || !strings.Contains(create.UserData, "MYN_REMOTE_PROJECT_ROOT='/home/harish/Remote Projects'") {
		t.Fatalf("server create should include rendered Personal Server Bootstrap cloud-init, got %q", create.UserData)
	}
	if got, want := cloud.waitedActionIDs, []int{9001}; !reflect.DeepEqual(got, want) {
		t.Fatalf("waited actions mismatch: want %v, got %v", want, got)
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.PersonalServer, (personalServerConfig{ServerID: 654321, IPv4: "203.0.113.55", IPv6: "2001:db8::55"}); got != want {
		t.Fatalf("saved Personal Server Configuration mismatch: want %#v, got %#v", want, got)
	}
	if !strings.Contains(out.String(), "Personal Server created: server 654321.") {
		t.Fatalf("expected created output, got %q", out.String())
	}
}

func TestRunConfigurePollsBootstrapAndReportsAccess(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "Remote Projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		},
		images: []personalServerImage{
			{Name: "ubuntu-24.04", Type: "system", Status: "available", OSFlavor: "ubuntu", OSVersion: "24.04", Architecture: "x86"},
		},
		createdServer: personalServerCloudServer{
			ID:   654321,
			IPv4: "203.0.113.55",
			IPv6: "2001:db8::55",
		},
	}
	ssh := &fakePersonalServerSSHRunner{
		outputs: []string{
			"ready\n",
			`{
  "status": "success",
  "timestamp": "2026-05-10T12:00:00Z",
  "rebootRequired": true,
  "toolVersions": {
    "docker": "Docker version 28.1.0",
    "mosh": "mosh-server (mosh 1.4.0)",
    "node": "v24.0.0"
  },
  "partialFailures": ["Claude Code install failed"]
}`,
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		inputs:    []string{"harish", "harish-personal-server"},
		passwords: []string{"server-secret", "server-secret"},
		confirms:  []bool{true},
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:          "Remote Projects",
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
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter:     prompter,
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return cloud
			},
			userHomeDir: func() (string, error) {
				return home, nil
			},
			runSSH: ssh.Run,
			currentUsername: func() string {
				return "harish"
			},
		},
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if got, want := ssh.calls, []personalServerSSHCall{
		{identityFile: identity.PrivatePath, user: "root", host: "203.0.113.55", command: "true"},
		{identityFile: identity.PrivatePath, user: "root", host: "203.0.113.55", command: "cat /var/lib/myn/personal-server-bootstrap.json"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SSH calls mismatch: want %#v, got %#v", want, got)
	}
	output := out.String()
	for _, want := range []string{
		"Personal Server created: server 654321.",
		"Personal Server bootstrap completed.",
		"Bootstrap timestamp: 2026-05-10T12:00:00Z",
		"Reboot required: true",
		"Installed tool versions:",
		"- docker: Docker version 28.1.0",
		"- mosh: mosh-server (mosh 1.4.0)",
		"- node: v24.0.0",
		"Partial bootstrap failures:",
		"- Claude Code install failed",
		"SSH commands:",
		"- user IPv4: ssh -i ~/.ssh/id_ed25519 harish@203.0.113.55",
		"- root IPv4: ssh -i ~/.ssh/id_ed25519 root@203.0.113.55",
		"- user IPv6: ssh -i ~/.ssh/id_ed25519 harish@[2001:db8::55]",
		"- root IPv6: ssh -i ~/.ssh/id_ed25519 root@[2001:db8::55]",
		"Mosh commands:",
		"- user IPv4: mosh --ssh=\"ssh -i ~/.ssh/id_ed25519\" harish@203.0.113.55",
		"- user IPv6: mosh --ssh=\"ssh -i ~/.ssh/id_ed25519\" harish@2001:db8::55",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got %q", want, output)
		}
	}
}

func TestRunConfigureToleratesTemporarySSHDisconnectsDuringBootstrap(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	cloud := successfulPersonalServerCloudClient()
	ssh := &fakePersonalServerSSHRunner{
		errors: []error{
			errors.New("connection refused"),
			nil,
			errors.New("connection reset during reboot"),
			nil,
		},
		outputs: []string{
			"ready\n",
			`{"status":"success","timestamp":"2026-05-10T12:00:00Z","toolVersions":{"docker":"Docker version 28.1.0"}}`,
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
			inputs:    []string{"harish", "harish-personal-server"},
			passwords: []string{"server-secret", "server-secret"},
			confirms:  []bool{true},
		},
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return cloud
			},
			userHomeDir: func() (string, error) {
				return home, nil
			},
			runSSH: ssh.Run,
			sleep: func(context.Context, time.Duration) error {
				return nil
			},
			currentUsername: func() string {
				return "harish"
			},
		},
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if got, want := ssh.calls, []personalServerSSHCall{
		{identityFile: identity.PrivatePath, user: "root", host: "203.0.113.55", command: "true"},
		{identityFile: identity.PrivatePath, user: "root", host: "203.0.113.55", command: "true"},
		{identityFile: identity.PrivatePath, user: "root", host: "203.0.113.55", command: "cat /var/lib/myn/personal-server-bootstrap.json"},
		{identityFile: identity.PrivatePath, user: "root", host: "203.0.113.55", command: "cat /var/lib/myn/personal-server-bootstrap.json"},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SSH calls mismatch: want %#v, got %#v", want, got)
	}
	if !strings.Contains(out.String(), "Personal Server bootstrap completed.") {
		t.Fatalf("expected bootstrap completion output, got %q", out.String())
	}
}

func TestRunConfigureReportsBootstrapFailureButKeepsSavedServer(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	ssh := &fakePersonalServerSSHRunner{
		outputs: []string{
			"ready\n",
			`{"status":"failed","timestamp":"2026-05-10T12:00:00Z","failure":"apt-get upgrade (exit 1)","partialFailures":["Codex install failed"]}`,
		},
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
		prompter: &fakeConfigurePrompter{
			canPrompt: true,
			inputs:    []string{"harish", "harish-personal-server"},
			passwords: []string{"server-secret", "server-secret"},
			confirms:  []bool{true},
		},
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return successfulPersonalServerCloudClient()
			},
			userHomeDir: func() (string, error) {
				return home, nil
			},
			runSSH: ssh.Run,
			currentUsername: func() string {
				return "harish"
			},
		},
	})
	if err == nil {
		t.Fatal("expected bootstrap failure error")
	}
	if !strings.Contains(err.Error(), "Personal Server Bootstrap failed: apt-get upgrade (exit 1)") {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.PersonalServer, (personalServerConfig{ServerID: 654321, IPv4: "203.0.113.55", IPv6: "2001:db8::55"}); got != want {
		t.Fatalf("saved Personal Server Configuration mismatch: want %#v, got %#v", want, got)
	}
	output := out.String()
	for _, want := range []string{
		"Personal Server bootstrap failed.",
		"Bootstrap failure: apt-get upgrade (exit 1)",
		"Partial bootstrap failures:",
		"- Codex install failed",
		"SSH commands:",
		"- root IPv4: ssh -i ~/.ssh/id_ed25519 root@203.0.113.55",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got %q", want, output)
		}
	}
	if strings.Contains(output, "Mosh commands:") {
		t.Fatalf("bootstrap failure should not print Mosh commands, got %q", output)
	}
}

func TestRunConfigureReportsBootstrapTimeoutButKeepsSavedServer(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	ssh := &fakePersonalServerSSHRunner{
		errors:  []error{nil, errors.New("marker not ready")},
		outputs: []string{"ready\n"},
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
		prompter: &fakeConfigurePrompter{
			canPrompt: true,
			inputs:    []string{"harish", "harish-personal-server"},
			passwords: []string{"server-secret", "server-secret"},
			confirms:  []bool{true},
		},
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return successfulPersonalServerCloudClient()
			},
			userHomeDir: func() (string, error) {
				return home, nil
			},
			runSSH:           ssh.Run,
			bootstrapTimeout: time.Nanosecond,
			sleep: func(ctx context.Context, _ time.Duration) error {
				<-ctx.Done()
				return ctx.Err()
			},
			currentUsername: func() string {
				return "harish"
			},
		},
	})
	if err == nil {
		t.Fatal("expected bootstrap timeout error")
	}
	if !strings.Contains(err.Error(), "timed out waiting for Personal Server Bootstrap marker") {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.PersonalServer, (personalServerConfig{ServerID: 654321, IPv4: "203.0.113.55", IPv6: "2001:db8::55"}); got != want {
		t.Fatalf("saved Personal Server Configuration mismatch: want %#v, got %#v", want, got)
	}
	output := out.String()
	for _, want := range []string{
		"Personal Server bootstrap failed: timed out waiting for Personal Server Bootstrap marker",
		"SSH commands:",
		"- user IPv4: ssh -i ~/.ssh/id_ed25519 harish@203.0.113.55",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected output to contain %q, got %q", want, output)
		}
	}
	if strings.Contains(output, "Mosh commands:") {
		t.Fatalf("bootstrap timeout should not print Mosh commands, got %q", output)
	}
}

func TestRunConfigureCancellationBeforeServerCreationDoesNotSavePersonalServer(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cloud := successfulPersonalServerCloudClient()
	cloud.failOnCanceledContext = true

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		ctx: ctx,
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter:     &fakeConfigurePrompter{canPrompt: true},
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return cloud
			},
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if len(cloud.contexts) == 0 {
		t.Fatal("expected Hetzner calls to receive command context")
	}
	if cloud.serverCreateRequest.Name != "" {
		t.Fatalf("cancellation before creation should not create a server, got request %#v", cloud.serverCreateRequest)
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.PersonalServer.isZero() {
		t.Fatalf("cancellation before creation should not save Personal Server Configuration, got %#v", cfg.PersonalServer)
	}
}

func TestRunConfigureCancellationAfterServerCreationKeepsSavedServer(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cloud := successfulPersonalServerCloudClient()
	cloud.failOnCanceledContext = true
	cloud.cancelAfterCreateServer = cancel

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		ctx: ctx,
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter: &fakeConfigurePrompter{
			canPrompt: true,
			inputs:    []string{"harish", "harish-personal-server"},
			passwords: []string{"server-secret", "server-secret"},
			confirms:  []bool{true},
		},
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return cloud
			},
			userHomeDir: func() (string, error) {
				return home, nil
			},
			currentUsername: func() string {
				return "harish"
			},
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if cloud.serverCreateRequest.Name == "" {
		t.Fatal("expected cancellation to happen after server creation")
	}
	if cloud.createdFirewall.ID == 0 || cloud.createdSSHKey.ID == 0 {
		t.Fatalf("supporting resources should be left in place on cancellation, got firewall=%#v sshKey=%#v", cloud.createdFirewall, cloud.createdSSHKey)
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.PersonalServer, (personalServerConfig{ServerID: 654321, IPv4: "203.0.113.55", IPv6: "2001:db8::55"}); got != want {
		t.Fatalf("cancellation after creation should preserve Personal Server Configuration: want %#v, got %#v", want, got)
	}
}

func TestRunConfigureRootSSHPollingRespectsCancellationAndKeepsSavedServer(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sshCalls := 0

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		ctx: ctx,
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter: &fakeConfigurePrompter{
			canPrompt: true,
			inputs:    []string{"harish", "harish-personal-server"},
			passwords: []string{"server-secret", "server-secret"},
			confirms:  []bool{true},
		},
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return successfulPersonalServerCloudClient()
			},
			userHomeDir: func() (string, error) {
				return home, nil
			},
			runSSH: func(context.Context, string, string, string, string) (string, error) {
				sshCalls++
				cancel()
				return "", errors.New("connection refused")
			},
			currentUsername: func() string {
				return "harish"
			},
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if sshCalls != 1 {
		t.Fatalf("expected one root SSH attempt before cancellation, got %d", sshCalls)
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.PersonalServer, (personalServerConfig{ServerID: 654321, IPv4: "203.0.113.55", IPv6: "2001:db8::55"}); got != want {
		t.Fatalf("root SSH cancellation should preserve Personal Server Configuration: want %#v, got %#v", want, got)
	}
}

func TestRunConfigureBootstrapMarkerPollingRespectsCancellationAndKeepsSavedServer(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sshCalls := 0

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identity.PrivatePath,
		sshIdentityFileSet: true,
	}, configureDeps{
		ctx: ctx,
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter: &fakeConfigurePrompter{
			canPrompt: true,
			inputs:    []string{"harish", "harish-personal-server"},
			passwords: []string{"server-secret", "server-secret"},
			confirms:  []bool{true},
		},
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return successfulPersonalServerCloudClient()
			},
			userHomeDir: func() (string, error) {
				return home, nil
			},
			runSSH: func(_ context.Context, _ string, _ string, _ string, command string) (string, error) {
				sshCalls++
				if command == "true" {
					return "ready\n", nil
				}
				cancel()
				return "", errors.New("marker not ready")
			},
			currentUsername: func() string {
				return "harish"
			},
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation error, got %v", err)
	}
	if sshCalls != 2 {
		t.Fatalf("expected root SSH and one marker poll before cancellation, got %d", sshCalls)
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.PersonalServer, (personalServerConfig{ServerID: 654321, IPv4: "203.0.113.55", IPv6: "2001:db8::55"}); got != want {
		t.Fatalf("marker polling cancellation should preserve Personal Server Configuration: want %#v, got %#v", want, got)
	}
}

func TestRunConfigureFailsWhenPersonalServerNameAlreadyExists(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		},
		serversByName: map[string]personalServerCloudServer{
			"harish-personal-server": {ID: 111111, Name: "harish-personal-server"},
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		inputs:    []string{"harish", "harish-personal-server"},
		passwords: []string{"server-secret", "server-secret"},
		confirms:  []bool{true},
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
		prompter:     prompter,
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return cloud
			},
			userHomeDir: func() (string, error) {
				return home, nil
			},
			currentUsername: func() string {
				return "harish"
			},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate server name error")
	}
	if !strings.Contains(err.Error(), `Personal Server name "harish-personal-server" already exists in Hetzner`) {
		t.Fatalf("unexpected error: %v", err)
	}
	if cloud.createdFirewall.ID != 0 || cloud.createdSSHKey.ID != 0 || cloud.serverCreateRequest.Name != "" {
		t.Fatalf("duplicate name should stop before creating resources, got firewall=%#v sshKey=%#v request=%#v", cloud.createdFirewall, cloud.createdSSHKey, cloud.serverCreateRequest)
	}
	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !cfg.PersonalServer.isZero() {
		t.Fatalf("duplicate name should not save Personal Server Configuration, got %#v", cfg.PersonalServer)
	}
}

func TestRunConfigureFailsWhenNoUbuntuSystemImageIsAvailable(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		},
		images: []personalServerImage{
			{Name: "ubuntu-24.04", Type: "system", Status: "available", OSFlavor: "ubuntu", OSVersion: "24.04", Architecture: "x86", Deprecated: true},
			{Name: "debian-13", Type: "system", Status: "available", OSFlavor: "debian", OSVersion: "13", Architecture: "x86"},
			{Name: "ubuntu-arm", Type: "system", Status: "available", OSFlavor: "ubuntu", OSVersion: "24.04", Architecture: "arm"},
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		inputs:    []string{"harish", "harish-personal-server"},
		passwords: []string{"server-secret", "server-secret"},
		confirms:  []bool{true},
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
		prompter:     prompter,
		personalServerProvisioner: personalServerProvisioningGate{
			newCloudClient: func(string) personalServerCloudClient {
				return cloud
			},
			userHomeDir: func() (string, error) {
				return home, nil
			},
			currentUsername: func() string {
				return "harish"
			},
		},
	})
	if err == nil {
		t.Fatal("expected missing Ubuntu image error")
	}
	if !strings.Contains(err.Error(), "no non-deprecated Ubuntu system image is available") {
		t.Fatalf("unexpected error: %v", err)
	}
	if cloud.createdFirewall.ID != 0 || cloud.createdSSHKey.ID != 0 || cloud.serverCreateRequest.Name != "" {
		t.Fatalf("missing image should stop before creating resources, got firewall=%#v sshKey=%#v request=%#v", cloud.createdFirewall, cloud.createdSSHKey, cloud.serverCreateRequest)
	}
}

func TestRunConfigureReusesExistingFirewallAndSSHKey(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	existingFirewall := personalServerFirewall{
		ID:   2222,
		Name: "myn-personal-server",
		Rules: []personalServerFirewallRule{
			{Direction: "in", Protocol: "tcp", Port: "2222", SourceIPs: []string{"198.51.100.0/24"}},
		},
	}
	existingSSHKey := personalServerSSHKey{
		ID:          3333,
		Name:        "existing-key",
		Fingerprint: identity.HetznerFingerprint,
		PublicKey:   strings.TrimSpace(identity.PublicLine),
	}
	cloud := &fakePersonalServerCloudClient{
		locations: []personalServerLocation{{Name: "ash", Description: "Ashburn, VA, USA"}},
		serverTypes: []personalServerType{
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		},
		images: []personalServerImage{
			{Name: "ubuntu-24.04", Type: "system", Status: "available", OSFlavor: "ubuntu", OSVersion: "24.04", Architecture: "x86"},
		},
		firewallsByName: map[string]personalServerFirewall{
			"myn-personal-server": existingFirewall,
		},
		sshKeysByFingerprint: map[string]personalServerSSHKey{
			identity.HetznerFingerprint: existingSSHKey,
		},
		createdServer: personalServerCloudServer{
			ID:   654321,
			IPv4: "203.0.113.55",
			IPv6: "2001:db8::55",
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		inputs:    []string{"harish", "harish-personal-server"},
		passwords: []string{"server-secret", "server-secret"},
		confirms:  []bool{true},
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
			userHomeDir: func() (string, error) {
				return home, nil
			},
			runSSH: newSuccessfulPersonalServerSSHRunner().Run,
			currentUsername: func() string {
				return "harish"
			},
		},
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if cloud.createdFirewall.ID != 0 {
		t.Fatalf("expected existing firewall to be reused, created %#v", cloud.createdFirewall)
	}
	if got, want := cloud.firewallsByName["myn-personal-server"].Rules, existingFirewall.Rules; !reflect.DeepEqual(got, want) {
		t.Fatalf("existing firewall rules should be left untouched: want %#v, got %#v", want, got)
	}
	if cloud.createdSSHKey.ID != 0 {
		t.Fatalf("expected existing SSH key to be reused, created %#v", cloud.createdSSHKey)
	}
	if got, want := cloud.serverCreateRequest.FirewallID, existingFirewall.ID; got != want {
		t.Fatalf("server create firewall mismatch: want %d, got %d", want, got)
	}
	if got, want := cloud.serverCreateRequest.SSHKeyID, existingSSHKey.ID; got != want {
		t.Fatalf("server create SSH key mismatch: want %d, got %d", want, got)
	}
	if !strings.Contains(out.String(), "Firewall: myn-personal-server (existing rules reused unchanged; Mosh may require inbound UDP 60000-61000)") {
		t.Fatalf("expected existing firewall caveat in output, got %q", out.String())
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
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
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
	servers                 map[int]personalServerCloudServer
	serversByName           map[string]personalServerCloudServer
	serverIDs               []int
	serverNames             []string
	contexts                []context.Context
	failOnCanceledContext   bool
	locations               []personalServerLocation
	serverTypes             []personalServerType
	pricing                 personalServerPricing
	pricingErr              error
	images                  []personalServerImage
	firewallsByName         map[string]personalServerFirewall
	createdFirewall         personalServerFirewall
	sshKeysByFingerprint    map[string]personalServerSSHKey
	sshKeyFingerprints      []string
	createdSSHKey           personalServerSSHKey
	serverCreateRequest     personalServerCreateServerRequest
	createdServer           personalServerCloudServer
	cancelAfterCreateServer func()
	waitedActions           bool
	waitedActionIDs         []int
	listLocations           int
	listServerTypes         int
	listPricing             int
}

func successfulPersonalServerCloudClient() *fakePersonalServerCloudClient {
	return &fakePersonalServerCloudClient{
		locations: []personalServerLocation{{Name: "ash", Description: "Ashburn, VA, USA"}},
		serverTypes: []personalServerType{
			fakePersonalServerType("cx32", "shared", "x86", false, 4, 8, 80, "local", "ash", true, false, "18.50"),
		},
		pricing: fakePersonalServerPricing("ipv4", "ash", "0.60"),
		images: []personalServerImage{
			{Name: "ubuntu-24.04", Type: "system", Status: "available", OSFlavor: "ubuntu", OSVersion: "24.04", Architecture: "x86"},
		},
		createdServer: personalServerCloudServer{
			ID:   654321,
			IPv4: "203.0.113.55",
			IPv6: "2001:db8::55",
		},
	}
}

type personalServerSSHCall struct {
	identityFile string
	user         string
	host         string
	command      string
}

type fakePersonalServerSSHRunner struct {
	outputs []string
	errors  []error
	calls   []personalServerSSHCall
}

func newSuccessfulPersonalServerSSHRunner() *fakePersonalServerSSHRunner {
	return &fakePersonalServerSSHRunner{
		outputs: []string{
			"ready\n",
			`{"status":"success","timestamp":"2026-05-10T12:00:00Z","rebootRequired":false,"toolVersions":{"docker":"Docker version 28.1.0"}}`,
		},
	}
}

func (r *fakePersonalServerSSHRunner) Run(_ context.Context, identityFile string, user string, host string, command string) (string, error) {
	r.calls = append(r.calls, personalServerSSHCall{
		identityFile: identityFile,
		user:         user,
		host:         host,
		command:      command,
	})
	if len(r.errors) > 0 {
		err := r.errors[0]
		r.errors = r.errors[1:]
		if err != nil {
			return "", err
		}
	}
	if len(r.outputs) == 0 {
		return "", nil
	}
	output := r.outputs[0]
	r.outputs = r.outputs[1:]
	return output, nil
}

func (c *fakePersonalServerCloudClient) recordContext(ctx context.Context) error {
	c.contexts = append(c.contexts, ctx)
	if c.failOnCanceledContext {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (c *fakePersonalServerCloudClient) ServerByID(ctx context.Context, id int) (personalServerCloudServer, bool, error) {
	if err := c.recordContext(ctx); err != nil {
		return personalServerCloudServer{}, false, err
	}
	c.serverIDs = append(c.serverIDs, id)
	server, ok := c.servers[id]
	return server, ok, nil
}

func (c *fakePersonalServerCloudClient) ServerByName(ctx context.Context, name string) (personalServerCloudServer, bool, error) {
	if err := c.recordContext(ctx); err != nil {
		return personalServerCloudServer{}, false, err
	}
	c.serverNames = append(c.serverNames, name)
	server, ok := c.serversByName[name]
	return server, ok, nil
}

func (c *fakePersonalServerCloudClient) Locations(ctx context.Context) ([]personalServerLocation, error) {
	if err := c.recordContext(ctx); err != nil {
		return nil, err
	}
	c.listLocations++
	return c.locations, nil
}

func (c *fakePersonalServerCloudClient) ServerTypes(ctx context.Context) ([]personalServerType, error) {
	if err := c.recordContext(ctx); err != nil {
		return nil, err
	}
	c.listServerTypes++
	return c.serverTypes, nil
}

func (c *fakePersonalServerCloudClient) Pricing(ctx context.Context) (personalServerPricing, error) {
	if err := c.recordContext(ctx); err != nil {
		return personalServerPricing{}, err
	}
	c.listPricing++
	if c.pricingErr != nil {
		return personalServerPricing{}, c.pricingErr
	}
	return c.pricing, nil
}

func (c *fakePersonalServerCloudClient) Images(ctx context.Context) ([]personalServerImage, error) {
	if err := c.recordContext(ctx); err != nil {
		return nil, err
	}
	return c.images, nil
}

func (c *fakePersonalServerCloudClient) FirewallByName(ctx context.Context, name string) (personalServerFirewall, bool, error) {
	if err := c.recordContext(ctx); err != nil {
		return personalServerFirewall{}, false, err
	}
	firewall, ok := c.firewallsByName[name]
	return firewall, ok, nil
}

func (c *fakePersonalServerCloudClient) CreateFirewall(ctx context.Context, firewall personalServerFirewall) (personalServerFirewall, []personalServerAction, error) {
	if err := c.recordContext(ctx); err != nil {
		return personalServerFirewall{}, nil, err
	}
	if firewall.ID == 0 {
		firewall.ID = 2001
	}
	c.createdFirewall = firewall
	return firewall, nil, nil
}

func (c *fakePersonalServerCloudClient) SSHKeyByFingerprint(ctx context.Context, fingerprint string) (personalServerSSHKey, bool, error) {
	if err := c.recordContext(ctx); err != nil {
		return personalServerSSHKey{}, false, err
	}
	c.sshKeyFingerprints = append(c.sshKeyFingerprints, fingerprint)
	sshKey, ok := c.sshKeysByFingerprint[fingerprint]
	return sshKey, ok, nil
}

func (c *fakePersonalServerCloudClient) CreateSSHKey(ctx context.Context, sshKey personalServerSSHKey) (personalServerSSHKey, error) {
	if err := c.recordContext(ctx); err != nil {
		return personalServerSSHKey{}, err
	}
	if sshKey.ID == 0 {
		sshKey.ID = 3001
	}
	c.createdSSHKey = sshKey
	return sshKey, nil
}

func (c *fakePersonalServerCloudClient) CreateServer(ctx context.Context, request personalServerCreateServerRequest) (personalServerCloudServer, []personalServerAction, error) {
	if err := c.recordContext(ctx); err != nil {
		return personalServerCloudServer{}, nil, err
	}
	c.serverCreateRequest = request
	if c.cancelAfterCreateServer != nil {
		c.cancelAfterCreateServer()
	}
	return c.createdServer, []personalServerAction{{ID: 9001}}, nil
}

func (c *fakePersonalServerCloudClient) WaitActions(ctx context.Context, actions []personalServerAction) error {
	if err := c.recordContext(ctx); err != nil {
		return err
	}
	if len(actions) == 0 {
		return nil
	}
	c.waitedActions = true
	for _, action := range actions {
		c.waitedActionIDs = append(c.waitedActionIDs, action.ID)
	}
	return nil
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

func fakePersonalServerPricing(ipType string, location string, monthlyGrossEUR string) personalServerPricing {
	return personalServerPricing{
		PrimaryIPs: []personalServerPrimaryIPPricing{
			{
				Type: ipType,
				Pricings: []personalServerPrimaryIPLocationPricing{
					{
						LocationName:    location,
						MonthlyGrossEUR: monthlyGrossEUR,
					},
				},
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
