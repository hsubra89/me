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
	prompter := &fakeConfigurePrompter{canPrompt: true}

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

	prompter := &fakeConfigurePrompter{canPrompt: true}
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
