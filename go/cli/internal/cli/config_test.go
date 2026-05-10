package cli

import (
	"os"
	"path/filepath"
	"testing"
)

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
