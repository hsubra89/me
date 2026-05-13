package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAuthHetznerTokenFlagValidatesAndPersists(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config", "myn", "config.json")
	server := newHetznerValidationServer(t, map[string]testHetznerTokenPermission{
		"flag-token": testHetznerReadWrite,
	})

	t.Setenv("MYN_CONFIG", configPath)
	t.Setenv("HCLOUD_ENDPOINT", server.URL)

	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"auth", "hetzner", "--token", "flag-token"})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute auth hetzner: %v", err)
	}

	if got, want := out.String(), "Saved Hetzner credentials.\n"; got != want {
		t.Fatalf("output mismatch:\nwant %q\ngot  %q", want, got)
	}

	assertSavedHetznerToken(t, configPath, "flag-token")
	assertFileMode(t, configPath, 0o600)
	assertFileMode(t, filepath.Dir(configPath), 0o700)
}

func TestAuthHetznerTokenFlagRejectsInvalidToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	server := newHetznerValidationServer(t, map[string]testHetznerTokenPermission{
		"good-token": testHetznerReadWrite,
	})

	t.Setenv("MYN_CONFIG", configPath)
	t.Setenv("HCLOUD_ENDPOINT", server.URL)

	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"auth", "hetzner", "--token", "bad-token"})
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected invalid token error")
	}
	if !strings.Contains(err.Error(), "Hetzner token validation failed: API rejected the token") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Fatalf("config should not exist after invalid token, stat err: %v", statErr)
	}
}

func TestAuthHetznerTokenFlagRejectsReadOnlyToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	server := newHetznerValidationServer(t, map[string]testHetznerTokenPermission{
		"read-only-token": testHetznerReadOnly,
	})

	t.Setenv("MYN_CONFIG", configPath)
	t.Setenv("HCLOUD_ENDPOINT", server.URL)

	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"auth", "hetzner", "--token", "read-only-token"})
	cmd.SetOut(&out)

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected read-only token error")
	}
	if !strings.Contains(err.Error(), "Hetzner token validation failed: token is read-only; create a Read & Write Hetzner token") {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, statErr := os.Stat(configPath); !os.IsNotExist(statErr) {
		t.Fatalf("config should not exist after read-only token, stat err: %v", statErr)
	}
}

func TestAuthHetznerImportsNamedHcloudContext(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	hcloudConfigPath := filepath.Join(t.TempDir(), "hcloud", "cli.toml")
	server := newHetznerValidationServer(t, map[string]testHetznerTokenPermission{
		"staging-token": testHetznerReadWrite,
	})

	writeTestFile(t, hcloudConfigPath, `
active_context = "prod"

[[contexts]]
  name = "prod"
  token = "prod-token"

[[contexts]]
  name = "staging"
  token = "staging-token"
`)

	t.Setenv("MYN_CONFIG", configPath)
	t.Setenv("HCLOUD_CONFIG", hcloudConfigPath)
	t.Setenv("HCLOUD_ENDPOINT", server.URL)

	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{"auth", "hetzner", "--from-hcloud-context", "staging"})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute auth hetzner: %v", err)
	}

	if got, want := out.String(), "Saved Hetzner credentials from hcloud context \"staging\".\n"; got != want {
		t.Fatalf("output mismatch:\nwant %q\ngot  %q", want, got)
	}
	assertSavedHetznerToken(t, configPath, "staging-token")
}

func TestAuthHetznerPickerImportsSelectedHcloudContext(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	hcloudConfigPath := filepath.Join(t.TempDir(), "hcloud", "cli.toml")
	writeTestFile(t, hcloudConfigPath, `
active_context = "prod"

[[contexts]]
  name = "prod"
  token = "prod-token"

[[contexts]]
  name = "staging"
  token = "staging-token"
`)

	prompter := &fakeHetznerPrompter{
		canPrompt:       true,
		selectContextAt: 1,
	}
	var validated []string
	deps := hetznerAuthDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		hcloudConfigPath: func() (string, error) {
			return hcloudConfigPath, nil
		},
		validateToken: func(_ context.Context, token string) error {
			validated = append(validated, token)
			if token != "staging-token" {
				return fmt.Errorf("unexpected token %q", token)
			}
			return nil
		},
		prompter: prompter,
	}

	var out bytes.Buffer
	if err := runHetznerAuth(context.Background(), &out, hetznerAuthOptions{}, deps); err != nil {
		t.Fatalf("run auth hetzner: %v", err)
	}

	if got, want := out.String(), "Saved Hetzner credentials from hcloud context \"staging\".\n"; got != want {
		t.Fatalf("output mismatch:\nwant %q\ngot  %q", want, got)
	}
	if !slices.Equal(validated, []string{"staging-token"}) {
		t.Fatalf("validated tokens mismatch: %v", validated)
	}
	assertSavedHetznerToken(t, configPath, "staging-token")
}

func TestAuthHetznerReportsExistingTokenTimeoutAndPromptsForNewToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveHetznerToken(configPath, "old-token"); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	prompter := &fakeHetznerPrompter{
		canPrompt:  true,
		inputToken: "new-token",
	}
	deps := hetznerAuthDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		hcloudConfigPath: func() (string, error) {
			return filepath.Join(t.TempDir(), "missing-hcloud.toml"), nil
		},
		validateToken: func(_ context.Context, token string) error {
			if token == "old-token" {
				return hetznerValidationError{reason: "did not validate within 4s", timeout: true}
			}
			if token == "new-token" {
				return nil
			}
			return fmt.Errorf("unexpected token %q", token)
		},
		prompter: prompter,
	}

	var out bytes.Buffer
	if err := runHetznerAuth(context.Background(), &out, hetznerAuthOptions{}, deps); err != nil {
		t.Fatalf("run auth hetzner: %v", err)
	}

	const want = "Tried the existing Hetzner token, but it did not validate within 4s.\nSaved Hetzner credentials.\n"
	if got := out.String(); got != want {
		t.Fatalf("output mismatch:\nwant %q\ngot  %q", want, got)
	}
	assertSavedHetznerToken(t, configPath, "new-token")
}

func TestAuthHetznerReportsHcloudTokenTimeoutAndPromptsForNewToken(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	hcloudConfigPath := filepath.Join(t.TempDir(), "hcloud", "cli.toml")
	writeTestFile(t, hcloudConfigPath, `
active_context = "warptech"

[[contexts]]
  name = "warptech"
  token = "hcloud-token"
`)

	prompter := &fakeHetznerPrompter{
		canPrompt: true,
		confirmResults: []bool{
			true,
		},
		inputToken: "new-token",
	}
	deps := hetznerAuthDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		hcloudConfigPath: func() (string, error) {
			return hcloudConfigPath, nil
		},
		validateToken: func(_ context.Context, token string) error {
			if token == "hcloud-token" {
				return hetznerValidationError{reason: "did not validate within 4s", timeout: true}
			}
			if token == "new-token" {
				return nil
			}
			return fmt.Errorf("unexpected token %q", token)
		},
		prompter: prompter,
	}

	var out bytes.Buffer
	if err := runHetznerAuth(context.Background(), &out, hetznerAuthOptions{}, deps); err != nil {
		t.Fatalf("run auth hetzner: %v", err)
	}

	const want = "Tried the hcloud context \"warptech\" token, but it did not validate within 4s.\nSaved Hetzner credentials.\n"
	if got := out.String(); got != want {
		t.Fatalf("output mismatch:\nwant %q\ngot  %q", want, got)
	}
	assertSavedHetznerToken(t, configPath, "new-token")
}

func TestAuthHetznerKeepsValidExistingTokenWithoutTerminal(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "myn", "config.json")
	if err := saveHetznerToken(configPath, "existing-token"); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	deps := hetznerAuthDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		validateToken: func(_ context.Context, token string) error {
			if token != "existing-token" {
				return fmt.Errorf("unexpected token %q", token)
			}
			return nil
		},
		prompter: &fakeHetznerPrompter{
			canPrompt: false,
		},
	}

	var out bytes.Buffer
	if err := runHetznerAuth(context.Background(), &out, hetznerAuthOptions{}, deps); err != nil {
		t.Fatalf("run auth hetzner: %v", err)
	}

	if got, want := out.String(), "Hetzner authentication is already configured.\n"; got != want {
		t.Fatalf("output mismatch:\nwant %q\ngot  %q", want, got)
	}
	assertSavedHetznerToken(t, configPath, "existing-token")
}

type fakeHetznerPrompter struct {
	canPrompt       bool
	confirmResults  []bool
	selectContextAt int
	inputToken      string
}

func (p *fakeHetznerPrompter) CanPrompt() bool {
	return p.canPrompt
}

func (p *fakeHetznerPrompter) Confirm(_ string, affirmative bool) (bool, error) {
	if len(p.confirmResults) == 0 {
		return affirmative, nil
	}

	result := p.confirmResults[0]
	p.confirmResults = p.confirmResults[1:]
	return result, nil
}

func (p *fakeHetznerPrompter) SelectHcloudContext(candidates []hcloudTokenCandidate) (hcloudTokenCandidate, bool, error) {
	if p.selectContextAt < 0 || p.selectContextAt >= len(candidates) {
		return hcloudTokenCandidate{}, false, nil
	}
	return candidates[p.selectContextAt], true, nil
}

func (p *fakeHetznerPrompter) InputToken() (string, error) {
	return p.inputToken, nil
}

type testHetznerTokenPermission string

const (
	testHetznerReadOnly  testHetznerTokenPermission = "read"
	testHetznerReadWrite testHetznerTokenPermission = "read-write"
)

func newHetznerValidationServer(t *testing.T, tokens map[string]testHetznerTokenPermission) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != hetznerReadValidationPath && r.URL.Path != hetznerWriteValidationPath {
			t.Errorf("unexpected validation path: %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == hetznerReadValidationPath && r.Method != http.MethodGet {
			t.Errorf("unexpected read validation method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == hetznerWriteValidationPath && r.Method != http.MethodDelete {
			t.Errorf("unexpected write validation method: %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		permission, ok := tokens[token]
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		if r.URL.Path == hetznerWriteValidationPath {
			if permission != testHetznerReadWrite {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"locations":[]}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func assertSavedHetznerToken(t *testing.T, configPath string, want string) {
	t.Helper()

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Auth.Hetzner.Token; got != want {
		t.Fatalf("saved token mismatch: want %q, got %q", want, got)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode mismatch: want %v, got %v", path, want, got)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create parent dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
