package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAppConfigOmitsEmptyPersonalServerConfiguration(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		PersonalServer: personalServerConfig{},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), "personalServer") {
		t.Fatalf("empty Personal Server Configuration should be omitted, got %s", data)
	}

	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("parse saved config: %v", err)
	}
	if len(saved) != 0 {
		t.Fatalf("empty config mismatch: %#v", saved)
	}
}

func TestSaveAppConfigPersistsPersonalServerIdentityOnly(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		PersonalServer: personalServerConfig{
			ServerID: 123456,
			User:     "harish",
			IPv4:     "203.0.113.10",
			IPv6:     "2001:db8::1",
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	const want = `{
  "personalServer": {
    "serverID": 123456,
    "user": "harish",
    "ipv4": "203.0.113.10",
    "ipv6": "2001:db8::1"
  }
}
`
	if got := string(data); got != want {
		t.Fatalf("saved config mismatch:\nwant %s\ngot  %s", want, got)
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.PersonalServer.User, "harish"; got != want {
		t.Fatalf("Personal Server User mismatch: want %q, got %q", want, got)
	}
}

func TestSaveAppConfigPreservesExistingParentDirectoryMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("create parent dir: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("chmod parent dir: %v", err)
	}

	configPath := filepath.Join(dir, "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "token"},
		},
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	assertFileMode(t, dir, 0o755)
	assertFileMode(t, configPath, 0o600)
}

func TestPersonalServerConfigurationClassifiesConnectionReadiness(t *testing.T) {
	tests := []struct {
		name      string
		config    personalServerConfig
		wantState personalServerConnectionConfigState
		want      personalServerConnectionConfig
	}{
		{
			name:      "absent",
			wantState: personalServerConnectionConfigAbsent,
		},
		{
			name: "missing server ID is incomplete",
			config: personalServerConfig{
				User: "harish",
				IPv4: "203.0.113.10",
			},
			wantState: personalServerConnectionConfigIncomplete,
		},
		{
			name: "missing Personal Server User is incomplete",
			config: personalServerConfig{
				ServerID: 123456,
				IPv4:     "203.0.113.10",
			},
			wantState: personalServerConnectionConfigIncomplete,
		},
		{
			name: "missing saved address",
			config: personalServerConfig{
				ServerID: 123456,
				User:     "harish",
			},
			wantState: personalServerConnectionConfigMissingAddress,
		},
		{
			name: "ready selects IPv4 before IPv6",
			config: personalServerConfig{
				ServerID: 123456,
				User:     " harish ",
				IPv4:     " 203.0.113.10 ",
				IPv6:     "2001:db8::1",
			},
			wantState: personalServerConnectionConfigReady,
			want: personalServerConnectionConfig{
				User: "harish",
				Host: "203.0.113.10",
			},
		},
		{
			name: "ready falls back to IPv6",
			config: personalServerConfig{
				ServerID: 123456,
				User:     "harish",
				IPv6:     " 2001:db8::1 ",
			},
			wantState: personalServerConnectionConfigReady,
			want: personalServerConnectionConfig{
				User: "harish",
				Host: "2001:db8::1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotState, got := tt.config.connectionConfigState()
			if gotState != tt.wantState {
				t.Fatalf("state mismatch: want %v, got %v", tt.wantState, gotState)
			}
			if got != tt.want {
				t.Fatalf("connection config mismatch: want %#v, got %#v", tt.want, got)
			}
		})
	}
}
