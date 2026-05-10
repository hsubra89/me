package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureCommandFlagsNormalizeAndPersist(t *testing.T) {
	home := t.TempDir()
	localRoot := filepath.Join(home, "Code Projects")
	mkdirAll(t, localRoot)

	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("ME_CONFIG", configPath)

	var out bytes.Buffer
	cmd := NewRootCommand(BuildInfo{})
	cmd.SetArgs([]string{
		"configure",
		"--local-root", localRoot + string(filepath.Separator),
		"--remote-root", "~/Remote Projects/",
	})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute configure: %v", err)
	}

	const want = "Saved configuration.\nLocal project root: ~/Code Projects\nRemote project root: ~/Remote Projects\n"
	if got := out.String(); got != want {
		t.Fatalf("output mismatch:\nwant %q\ngot  %q", want, got)
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.Projects.LocalRoot, "Code Projects"; got != want {
		t.Fatalf("local root mismatch: want %q, got %q", want, got)
	}
	if got, want := cfg.Projects.RemoteRoot, "Remote Projects"; got != want {
		t.Fatalf("remote root mismatch: want %q, got %q", want, got)
	}
	if got, want := cfg.Auth.Hetzner.Token, "existing-token"; got != want {
		t.Fatalf("auth token mismatch: want %q, got %q", want, got)
	}
}

func TestRunConfigurePromptsWithExistingAndInferredDefaults(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "work"))
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Projects: projectsConfig{
			LocalRoot:  "missing",
			RemoteRoot: "servers/projects",
		},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	prompter := &fakeConfigurePrompter{canPrompt: true}
	deps := configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		workingDir: func() (string, error) {
			return filepath.Join(home, "work", "me"), nil
		},
		gitRoot: func(string) (string, error) {
			return filepath.Join(home, "work", "me"), nil
		},
		prompter: prompter,
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{}, deps); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if len(prompter.calls) != 2 {
		t.Fatalf("prompt count mismatch: %d", len(prompter.calls))
	}
	if got, want := prompter.calls[0], (configurePromptCall{title: "Local project root", defaultValue: "work"}); got != want {
		t.Fatalf("local prompt mismatch: want %#v, got %#v", want, got)
	}
	if got, want := prompter.calls[1], (configurePromptCall{title: "Remote project root", defaultValue: "servers/projects"}); got != want {
		t.Fatalf("remote prompt mismatch: want %#v, got %#v", want, got)
	}

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got, want := cfg.Projects.LocalRoot, "work"; got != want {
		t.Fatalf("local root mismatch: want %q, got %q", want, got)
	}
	if got, want := cfg.Projects.RemoteRoot, "servers/projects"; got != want {
		t.Fatalf("remote root mismatch: want %q, got %q", want, got)
	}
}

func TestRunConfigureDefaultsRemoteRootToSelectedLocalRoot(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "Code Projects"))
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		inputs:    []string{"Code Projects"},
	}

	deps := configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		workingDir: func() (string, error) {
			return home, nil
		},
		gitRoot: func(string) (string, error) {
			return "", errors.New("not a git checkout")
		},
		prompter: prompter,
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{}, deps); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if len(prompter.calls) != 2 {
		t.Fatalf("prompt count mismatch: %d", len(prompter.calls))
	}
	if got, want := prompter.calls[0].defaultValue, ""; got != want {
		t.Fatalf("local default mismatch: want %q, got %q", want, got)
	}
	if got, want := prompter.calls[1].defaultValue, "Code Projects"; got != want {
		t.Fatalf("remote default mismatch: want %q, got %q", want, got)
	}

	assertSavedProjectsConfig(t, configPath, "Code Projects", "Code Projects")
}

func TestRunConfigureRequiresTerminalForMissingValues(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	configPath := filepath.Join(t.TempDir(), "me", "config.json")

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:    "projects",
		localRootSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		prompter: &fakeConfigurePrompter{canPrompt: false},
	})

	if err == nil {
		t.Fatal("expected non-interactive error")
	}
	if !strings.Contains(err.Error(), "interactive configuration requires a terminal; pass --local-root and --remote-root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunConfigureRejectsInvalidFlagPaths(t *testing.T) {
	home := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "me", "config.json")

	tests := []struct {
		name string
		opts configureOptions
		want string
	}{
		{
			name: "missing local directory",
			opts: configureOptions{
				localRoot:     "missing",
				localRootSet:  true,
				remoteRoot:    "projects",
				remoteRootSet: true,
			},
			want: "local project root must be an existing directory",
		},
		{
			name: "absolute remote path",
			opts: configureOptions{
				localRoot:     "projects",
				localRootSet:  true,
				remoteRoot:    "/home/harish/projects",
				remoteRootSet: true,
			},
			want: "remote project root must be relative to the remote home directory",
		},
	}

	mkdirAll(t, filepath.Join(home, "projects"))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := runConfigure(&out, tt.opts, configureDeps{
				appConfigPath: func() (string, error) {
					return configPath, nil
				},
				userHomeDir: func() (string, error) {
					return home, nil
				},
				prompter: &fakeConfigurePrompter{canPrompt: false},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("unexpected error: want %q in %q", tt.want, err.Error())
			}
		})
	}
}

func TestNormalizeLocalProjectRoot(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	mkdirAll(t, filepath.Join(home, "src"))
	mkdirAll(t, filepath.Join(home, ".local", "projects"))
	writeTestFile(t, filepath.Join(home, "notes.txt"), "not a directory")

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "relative", input: "projects", want: "projects"},
		{name: "home shorthand", input: "~/projects/", want: "projects"},
		{name: "absolute", input: filepath.Join(home, "projects"), want: "projects"},
		{name: "cleans segments", input: "projects/../src", want: "src"},
		{name: "hidden directory", input: ".local/projects", want: ".local/projects"},
		{name: "home itself shorthand", input: "~", wantErr: "must be a subdirectory"},
		{name: "home itself absolute", input: home, wantErr: "must be a subdirectory"},
		{name: "escapes home", input: "../projects", wantErr: "must be a subdirectory"},
		{name: "unsupported tilde", input: "~other/projects", wantErr: "must use ~ or ~/"},
		{name: "file", input: "notes.txt", wantErr: "must be an existing directory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeLocalProjectRoot(tt.input, home, os.Stat)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("unexpected error: want %q in %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("normalize local root: %v", err)
			}
			if got != tt.want {
				t.Fatalf("root mismatch: want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestNormalizeRemoteProjectRoot(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "relative", input: "projects", want: "projects"},
		{name: "home shorthand", input: "~/projects/", want: "projects"},
		{name: "cleans segments", input: "projects/../src", want: "src"},
		{name: "spaces", input: "Code Projects", want: "Code Projects"},
		{name: "home itself shorthand", input: "~", wantErr: "must be a subdirectory"},
		{name: "home itself relative", input: ".", wantErr: "must be a subdirectory"},
		{name: "escapes home", input: "../projects", wantErr: "must be a subdirectory"},
		{name: "absolute", input: "/home/harish/projects", wantErr: "must be relative"},
		{name: "unsupported tilde", input: "~other/projects", wantErr: "must use ~ or ~/"},
		{name: "backslash", input: `projects\me`, wantErr: "must use slash separators"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeRemoteProjectRoot(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("unexpected error: want %q in %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("normalize remote root: %v", err)
			}
			if got != tt.want {
				t.Fatalf("root mismatch: want %q, got %q", tt.want, got)
			}
		})
	}
}

type configurePromptCall struct {
	title        string
	defaultValue string
}

type fakeConfigurePrompter struct {
	canPrompt bool
	inputs    []string
	calls     []configurePromptCall
}

func (p *fakeConfigurePrompter) CanPrompt() bool {
	return p.canPrompt
}

func (p *fakeConfigurePrompter) Input(title string, defaultValue string, validate func(string) error) (string, error) {
	p.calls = append(p.calls, configurePromptCall{title: title, defaultValue: defaultValue})

	value := defaultValue
	if len(p.inputs) > 0 {
		value = p.inputs[0]
		p.inputs = p.inputs[1:]
	}
	if validate != nil {
		if err := validate(value); err != nil {
			return "", err
		}
	}
	return value, nil
}

func assertSavedProjectsConfig(t *testing.T, configPath string, localRoot string, remoteRoot string) {
	t.Helper()

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.Projects.LocalRoot; got != localRoot {
		t.Fatalf("local root mismatch: want %q, got %q", localRoot, got)
	}
	if got := cfg.Projects.RemoteRoot; got != remoteRoot {
		t.Fatalf("remote root mismatch: want %q, got %q", remoteRoot, got)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
}
