package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
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

	cloud := &fakePersonalServerCloudClient{}
	gate := personalServerProvisioningGate{
		newCloudClient: func(string) personalServerCloudClient {
			return cloud
		},
	}
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
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
		sshPublicKey:              testSSHPublicKeyFunc(identity),
		prompter:                  prompter,
		personalServerProvisioner: gate,
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if got, want := prompter.confirmCalls, []string{"Clear stale Personal Server Configuration for missing server 123456?"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("confirm calls mismatch: want %v, got %v", want, got)
	}
	output := out.String()
	for _, want := range []string{
		"Personal Server Configuration references missing server 123456.",
		"Cleared stale Personal Server Configuration.",
		"Personal Server provisioning prerequisites are ready.",
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
		prompter:     &fakeConfigurePrompter{canPrompt: true},
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
	servers   map[int]personalServerCloudServer
	serverIDs []int
}

func (c *fakePersonalServerCloudClient) ServerByID(_ context.Context, id int) (personalServerCloudServer, bool, error) {
	c.serverIDs = append(c.serverIDs, id)
	server, ok := c.servers[id]
	return server, ok, nil
}
