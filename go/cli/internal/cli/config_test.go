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
    "ipv4": "203.0.113.10",
    "ipv6": "2001:db8::1"
  }
}
`
	if got := string(data); got != want {
		t.Fatalf("saved config mismatch:\nwant %s\ngot  %s", want, got)
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
