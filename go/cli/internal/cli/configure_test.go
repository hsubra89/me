package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureCommandFlagsNormalizeAndPersist(t *testing.T) {
	home := t.TempDir()
	localRoot := filepath.Join(home, "Code Projects")
	mkdirAll(t, localRoot)
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "cli@host", 0o600)

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
		"--ssh-identity-file", identity.PrivatePath,
	})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute configure: %v", err)
	}

	const want = "Saved configuration.\nLocal project root: ~/Code Projects\nRemote project root: ~/Remote Projects\nSSH identity: ~/.ssh/id_ed25519\nPersonal Server creation skipped: configure is running non-interactively.\n"
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
	if got, want := cfg.SSH.IdentityFile, ".ssh/id_ed25519"; got != want {
		t.Fatalf("SSH identity mismatch: want %q, got %q", want, got)
	}
}

func TestRunConfigurePromptsWithExistingAndInferredDefaults(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "work"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "work@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Projects: projectsConfig{
			LocalRoot:  "missing",
			RemoteRoot: "servers/projects",
		},
		SSH: sshConfig{IdentityFile: identity.Relative},
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
		sshPublicKey: testSSHPublicKeyFunc(identity),
		sshAgentList: testSSHAgentListFunc(identity),
		prompter:     prompter,
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
	if got, want := cfg.SSH.IdentityFile, ".ssh/id_ed25519"; got != want {
		t.Fatalf("SSH identity mismatch: want %q, got %q", want, got)
	}
}

func TestRunConfigureDefaultsRemoteRootToSelectedLocalRoot(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "Code Projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "code@host", 0o600)
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
		sshPublicKey: testSSHPublicKeyFunc(identity),
		sshAgentList: testSSHAgentListFunc(identity),
		prompter:     prompter,
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
	assertSavedSSHIdentity(t, configPath, ".ssh/id_ed25519")
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

func TestRunConfigureNonInteractiveUsesExistingSSHIdentity(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
		SSH: sshConfig{IdentityFile: identity.Relative},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:     "projects",
		localRootSet:  true,
		remoteRoot:    "projects",
		remoteRootSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		prompter:     &fakeConfigurePrompter{canPrompt: false},
	})
	if err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if !strings.Contains(out.String(), "Personal Server creation skipped: configure is running non-interactively.") {
		t.Fatalf("expected non-interactive Personal Server skip, got %q", out.String())
	}
	assertSavedProjectsConfig(t, configPath, "projects", "projects")
	assertSavedSSHIdentity(t, configPath, ".ssh/id_ed25519")
}

func TestRunConfigureSkipsPersonalServerWhenHetznerCredentialsMissing(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		SSH: sshConfig{IdentityFile: identity.Relative},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:     "projects",
		localRootSet:  true,
		remoteRoot:    "projects",
		remoteRootSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		sshAgentList: testSSHAgentListFunc(identity),
		prompter:     &fakeConfigurePrompter{canPrompt: true},
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if !strings.Contains(out.String(), "Personal Server creation skipped: Hetzner Credentials are not configured. Run `me auth hetzner` first.") {
		t.Fatalf("expected missing credentials skip, got %q", out.String())
	}
	assertSavedProjectsConfig(t, configPath, "projects", "projects")
	assertSavedSSHIdentity(t, configPath, ".ssh/id_ed25519")
}

func TestRunConfigureSavesLocalConfigBeforePersonalServerBranch(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")

	called := false
	var out bytes.Buffer
	if err := runConfigure(&out, configureOptions{
		localRoot:     "projects",
		localRootSet:  true,
		remoteRoot:    "remote projects",
		remoteRootSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		sshAgentList: testSSHAgentListFunc(identity),
		prompter:     &fakeConfigurePrompter{canPrompt: true},
		personalServerProvisioner: personalServerProvisionerFunc(func(_ context.Context, _ io.Writer, _ string, _ appConfig, _ configurePrompter) error {
			called = true
			saved, err := loadAppConfig(configPath)
			if err != nil {
				t.Fatalf("load saved config inside Personal Server branch: %v", err)
			}
			if got, want := saved.Projects.LocalRoot, "projects"; got != want {
				t.Fatalf("saved local root before Personal Server branch: want %q, got %q", want, got)
			}
			if got, want := saved.Projects.RemoteRoot, "remote projects"; got != want {
				t.Fatalf("saved remote root before Personal Server branch: want %q, got %q", want, got)
			}
			if got, want := saved.SSH.IdentityFile, ".ssh/id_ed25519"; got != want {
				t.Fatalf("saved SSH identity before Personal Server branch: want %q, got %q", want, got)
			}
			return nil
		}),
	}); err != nil {
		t.Fatalf("run configure: %v", err)
	}
	if !called {
		t.Fatal("Personal Server branch was not called")
	}
}

func TestRunConfigureNonInteractiveMissingSSHIdentitySavesRootsAndFails(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	configPath := filepath.Join(t.TempDir(), "me", "config.json")

	provisionerCalled := false
	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:     "projects",
		localRootSet:  true,
		remoteRoot:    "projects",
		remoteRootSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		prompter: &fakeConfigurePrompter{canPrompt: false},
		personalServerProvisioner: personalServerProvisionerFunc(func(_ context.Context, _ io.Writer, _ string, _ appConfig, _ configurePrompter) error {
			provisionerCalled = true
			return nil
		}),
	})
	if err == nil {
		t.Fatal("expected missing SSH identity error")
	}
	if !strings.Contains(err.Error(), "pass --ssh-identity-file") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "SSH identity: not configured") {
		t.Fatalf("expected not configured output, got %q", out.String())
	}
	if provisionerCalled {
		t.Fatal("Personal Server provisioner should not run when SSH configuration fails")
	}

	assertSavedProjectsConfig(t, configPath, "projects", "projects")
	assertSavedSSHIdentity(t, configPath, "")
}

func TestRunConfigureInvalidFlaggedSSHIdentityDoesNotProvisionWithStaleIdentity(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "existing@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		Auth: authConfig{
			Hetzner: hetznerConfig{Token: "existing-token"},
		},
		SSH: sshConfig{IdentityFile: identity.Relative},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	provisionerCalled := false
	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    filepath.Join(home, ".ssh", "missing"),
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		prompter: &fakeConfigurePrompter{canPrompt: true},
		personalServerProvisioner: personalServerProvisionerFunc(func(_ context.Context, _ io.Writer, _ string, _ appConfig, _ configurePrompter) error {
			provisionerCalled = true
			return nil
		}),
	})
	if err == nil {
		t.Fatal("expected invalid SSH identity error")
	}
	if provisionerCalled {
		t.Fatal("Personal Server provisioner should not run with stale SSH identity after SSH setup fails")
	}

	assertSavedProjectsConfig(t, configPath, "projects", "projects")
	assertSavedSSHIdentity(t, configPath, identity.Relative)
}

func TestRunConfigureInteractiveGeneratesSSHIdentityWithFallbackAndAgentPrompt(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	writeTestFile(t, filepath.Join(home, ".ssh", "id_ed25519.pub"), "ssh-rsa ZmFrZS1yc2E= old@host")

	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	prompter := &fakeConfigurePrompter{
		canPrompt:     true,
		sshSelections: []int{0},
		confirms:      []bool{true, true},
		passwords:     []string{"secret"},
	}
	var generatedPath, generatedPassphrase, generatedComment, addedPath string
	generatedIdentity := testSSHIdentity{
		Relative:    ".ssh/id_me_25519",
		PrivatePath: filepath.Join(home, ".ssh", "id_me_25519"),
		PublicPath:  filepath.Join(home, ".ssh", "id_me_25519.pub"),
		PublicLine:  testSSHPublicKeyLine("generated@host"),
	}
	generatedIdentity.PublicKey, _ = parseSSHPublicKey(generatedIdentity.PublicLine)
	generatedIdentity.Fingerprint, _ = sshPublicKeyFingerprint(generatedIdentity.PublicKey)

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:     "projects",
		localRootSet:  true,
		remoteRoot:    "projects",
		remoteRootSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(generatedIdentity),
		generateSSHKeyPair: func(path string, passphrase string, comment string) error {
			generatedPath = path
			generatedPassphrase = passphrase
			generatedComment = comment
			writeTestFile(t, path, "private")
			writeTestFile(t, path+".pub", generatedIdentity.PublicLine)
			return nil
		},
		sshAgentList: func() (string, error) {
			return "", errors.New("no identities")
		},
		sshAgentAdd: func(path string) error {
			addedPath = path
			return nil
		},
		hostname: func() (string, error) {
			return "box", nil
		},
		currentUsername: func() string {
			return "harish"
		},
		prompter: prompter,
	})
	if err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if generatedPath != filepath.Join(home, ".ssh", "id_me_25519") {
		t.Fatalf("generated path mismatch: %q", generatedPath)
	}
	if generatedPassphrase != "secret" {
		t.Fatalf("passphrase mismatch: %q", generatedPassphrase)
	}
	if generatedComment != "harish@box" {
		t.Fatalf("comment mismatch: %q", generatedComment)
	}
	if addedPath != generatedPath {
		t.Fatalf("agent add path mismatch: want %q, got %q", generatedPath, addedPath)
	}
	assertSavedSSHIdentity(t, configPath, ".ssh/id_me_25519")
}

func TestRunConfigureInteractiveWarnsWhenRecoveredGeneratedSSHIdentityHasBroadPermissions(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "recovered@host", 0o644)
	if err := os.Remove(identity.PublicPath); err != nil {
		t.Fatalf("remove public key: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	prompter := &fakeConfigurePrompter{
		canPrompt:     true,
		sshSelections: []int{0},
	}

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:     "projects",
		localRootSet:  true,
		remoteRoot:    "projects",
		remoteRootSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		sshAgentList: testSSHAgentListFunc(identity),
		prompter:     prompter,
	})
	if err != nil {
		t.Fatalf("run configure: %v", err)
	}

	if !strings.Contains(out.String(), "Warning: SSH identity ~/.ssh/id_ed25519 permissions are broader than recommended.") {
		t.Fatalf("expected permission warning, got %q", out.String())
	}
	assertSavedSSHIdentity(t, configPath, ".ssh/id_ed25519")
}

func TestRunConfigureInteractiveWarnsWhenAgentAddFails(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identity := seedTestSSHIdentity(t, home, ".ssh/id_ed25519", "agent@host", 0o600)
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	prompter := &fakeConfigurePrompter{
		canPrompt: true,
		confirms:  []bool{true},
	}

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:     "projects",
		localRootSet:  true,
		remoteRoot:    "projects",
		remoteRootSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: testSSHPublicKeyFunc(identity),
		sshAgentList: func() (string, error) {
			return "", errors.New("agent unavailable")
		},
		sshAgentAdd: func(string) error {
			return errors.New("agent add failed")
		},
		prompter: prompter,
	})
	if err != nil {
		t.Fatalf("run configure: %v", err)
	}
	if !strings.Contains(out.String(), "Warning: could not add SSH identity ~/.ssh/id_ed25519 to ssh-agent: agent add failed") {
		t.Fatalf("expected agent warning, got %q", out.String())
	}
	assertSavedSSHIdentity(t, configPath, ".ssh/id_ed25519")
}

func TestRunConfigureRegeneratesMissingPublicKeyForFlaggedIdentity(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	identityPath := filepath.Join(home, ".ssh", "id_ed25519")
	writeTestFile(t, identityPath, "private")
	publicLine := testSSHPublicKeyLine("recovered@host")
	configPath := filepath.Join(t.TempDir(), "me", "config.json")

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:          "projects",
		localRootSet:       true,
		remoteRoot:         "projects",
		remoteRootSet:      true,
		sshIdentityFile:    identityPath,
		sshIdentityFileSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		sshPublicKey: func(string) (string, error) {
			return publicLine, nil
		},
		prompter: &fakeConfigurePrompter{canPrompt: false},
	})
	if err != nil {
		t.Fatalf("run configure: %v", err)
	}

	data, err := os.ReadFile(identityPath + ".pub")
	if err != nil {
		t.Fatalf("read regenerated public key: %v", err)
	}
	if strings.TrimSpace(string(data)) != publicLine {
		t.Fatalf("public key mismatch: %q", string(data))
	}
	assertSavedSSHIdentity(t, configPath, ".ssh/id_ed25519")
}

func TestRunConfigureInteractiveDeclinesGenerationSavesRootsAndClearsInvalidSSH(t *testing.T) {
	home := t.TempDir()
	mkdirAll(t, filepath.Join(home, "projects"))
	configPath := filepath.Join(t.TempDir(), "me", "config.json")
	if err := saveAppConfig(configPath, appConfig{
		SSH: sshConfig{IdentityFile: ".ssh/missing"},
	}); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	prompter := &fakeConfigurePrompter{
		canPrompt:     true,
		sshSelections: []int{0},
		confirms:      []bool{false},
	}

	var out bytes.Buffer
	err := runConfigure(&out, configureOptions{
		localRoot:     "projects",
		localRootSet:  true,
		remoteRoot:    "projects",
		remoteRootSet: true,
	}, configureDeps{
		appConfigPath: func() (string, error) {
			return configPath, nil
		},
		userHomeDir: func() (string, error) {
			return home, nil
		},
		prompter: prompter,
	})
	if err == nil {
		t.Fatal("expected declined generation error")
	}
	if !strings.Contains(out.String(), "SSH identity: not configured") {
		t.Fatalf("expected not configured output, got %q", out.String())
	}

	assertSavedProjectsConfig(t, configPath, "projects", "projects")
	assertSavedSSHIdentity(t, configPath, "")
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

func TestNormalizeSSHIdentityFile(t *testing.T) {
	home := t.TempDir()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{name: "relative", input: ".ssh/id_ed25519", want: ".ssh/id_ed25519"},
		{name: "home shorthand", input: "~/.ssh/id_ed25519", want: ".ssh/id_ed25519"},
		{name: "absolute", input: filepath.Join(home, ".ssh", "id_ed25519"), want: ".ssh/id_ed25519"},
		{name: "public key path", input: ".ssh/id_ed25519.pub", wantErr: "must be the private key path"},
		{name: "escapes home", input: "../id_ed25519", wantErr: "must be a subdirectory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _, err := normalizeSSHIdentityFile(tt.input, home, os.Stat)
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
				t.Fatalf("normalize SSH identity: %v", err)
			}
			if got != tt.want {
				t.Fatalf("identity mismatch: want %q, got %q", tt.want, got)
			}
		})
	}
}

type configurePromptCall struct {
	title        string
	defaultValue string
}

type sshSelectCall struct {
	choices  []sshIdentityPromptChoice
	selected int
}

type fakeConfigurePrompter struct {
	canPrompt            bool
	inputs               []string
	passwords            []string
	confirms             []bool
	sshSelections        []int
	locationSelections   []int
	serverTypeSelections []int
	calls                []configurePromptCall
	confirmCalls         []string
	passwordCalls        []string
	sshCalls             []sshSelectCall
	locationCalls        []personalServerLocationSelectCall
	serverTypeCalls      []personalServerTypeSelectCall
}

func (p *fakeConfigurePrompter) CanPrompt() bool {
	return p.canPrompt
}

func (p *fakeConfigurePrompter) Confirm(title string, affirmative bool) (bool, error) {
	p.confirmCalls = append(p.confirmCalls, title)
	if len(p.confirms) == 0 {
		return affirmative, nil
	}
	value := p.confirms[0]
	p.confirms = p.confirms[1:]
	return value, nil
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

func (p *fakeConfigurePrompter) Password(title string) (string, error) {
	p.passwordCalls = append(p.passwordCalls, title)
	if len(p.passwords) == 0 {
		return "", nil
	}
	value := p.passwords[0]
	p.passwords = p.passwords[1:]
	return value, nil
}

func (p *fakeConfigurePrompter) SelectSSHIdentity(choices []sshIdentityPromptChoice, selected int) (sshIdentityPromptChoice, error) {
	p.sshCalls = append(p.sshCalls, sshSelectCall{choices: choices, selected: selected})
	index := selected
	if len(p.sshSelections) > 0 {
		index = p.sshSelections[0]
		p.sshSelections = p.sshSelections[1:]
	}
	if index < 0 || index >= len(choices) {
		return sshIdentityPromptChoice{}, errors.New("SSH selection index out of range")
	}
	return choices[index], nil
}

func (p *fakeConfigurePrompter) SelectPersonalServerLocation(choices []personalServerLocationChoice, selected int) (personalServerLocationChoice, error) {
	p.locationCalls = append(p.locationCalls, personalServerLocationSelectCall{choices: choices, selected: selected})
	index := selected
	if len(p.locationSelections) > 0 {
		index = p.locationSelections[0]
		p.locationSelections = p.locationSelections[1:]
	}
	if index < 0 || index >= len(choices) {
		return personalServerLocationChoice{}, errors.New("Location selection index out of range")
	}
	return choices[index], nil
}

func (p *fakeConfigurePrompter) SelectPersonalServerType(choices []personalServerTypeChoice, selected int) (personalServerTypeChoice, error) {
	p.serverTypeCalls = append(p.serverTypeCalls, personalServerTypeSelectCall{choices: choices, selected: selected})
	index := selected
	if len(p.serverTypeSelections) > 0 {
		index = p.serverTypeSelections[0]
		p.serverTypeSelections = p.serverTypeSelections[1:]
	}
	if index < 0 || index >= len(choices) {
		return personalServerTypeChoice{}, errors.New("Server Type selection index out of range")
	}
	return choices[index], nil
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

func assertSavedSSHIdentity(t *testing.T, configPath string, identityFile string) {
	t.Helper()

	cfg, err := loadAppConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if got := cfg.SSH.IdentityFile; got != identityFile {
		t.Fatalf("SSH identity mismatch: want %q, got %q", identityFile, got)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
}

type testSSHIdentity struct {
	Relative           string
	PrivatePath        string
	PublicPath         string
	PublicLine         string
	PublicKey          sshPublicKey
	Fingerprint        string
	HetznerFingerprint string
}

func seedTestSSHIdentity(t *testing.T, home string, relative string, comment string, mode os.FileMode) testSSHIdentity {
	t.Helper()

	identity := testSSHIdentity{
		Relative:    filepath.ToSlash(relative),
		PrivatePath: filepath.Join(home, filepath.FromSlash(relative)),
		PublicLine:  testSSHPublicKeyLine(comment),
	}
	identity.PublicPath = identity.PrivatePath + ".pub"
	publicKey, err := parseSSHPublicKey(identity.PublicLine)
	if err != nil {
		t.Fatalf("parse test public key: %v", err)
	}
	identity.PublicKey = publicKey
	fingerprint, err := sshPublicKeyFingerprint(publicKey)
	if err != nil {
		t.Fatalf("fingerprint test public key: %v", err)
	}
	identity.Fingerprint = fingerprint
	hetznerFingerprint, err := sshPublicKeyHetznerFingerprint(publicKey)
	if err != nil {
		t.Fatalf("Hetzner fingerprint test public key: %v", err)
	}
	identity.HetznerFingerprint = hetznerFingerprint

	writeTestFile(t, identity.PrivatePath, "private")
	if err := os.Chmod(identity.PrivatePath, mode); err != nil {
		t.Fatalf("chmod private key: %v", err)
	}
	writeTestFile(t, identity.PublicPath, identity.PublicLine)
	return identity
}

func testSSHPublicKeyLine(comment string) string {
	return "ssh-ed25519 ZmFrZS1lZDI1NTE5LWtleQ== " + comment
}

func testSSHPublicKeyFunc(identities ...testSSHIdentity) func(string) (string, error) {
	return func(path string) (string, error) {
		for _, identity := range identities {
			if path == identity.PrivatePath {
				return identity.PublicLine, nil
			}
		}
		return "", errors.New("unknown private key")
	}
}

func testSSHAgentListFunc(identities ...testSSHIdentity) func() (string, error) {
	return func() (string, error) {
		var lines []string
		for _, identity := range identities {
			lines = append(lines, "256 "+identity.Fingerprint+" "+identity.PublicKey.Comment+" (ED25519)")
		}
		return strings.Join(lines, "\n"), nil
	}
}
