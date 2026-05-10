package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

type configureOptions struct {
	localRoot          string
	localRootSet       bool
	remoteRoot         string
	remoteRootSet      bool
	sshIdentityFile    string
	sshIdentityFileSet bool
}

type configureDeps struct {
	ctx                       context.Context
	appConfigPath             func() (string, error)
	userHomeDir               func() (string, error)
	workingDir                func() (string, error)
	gitRoot                   func(string) (string, error)
	stat                      func(string) (os.FileInfo, error)
	readDir                   func(string) ([]os.DirEntry, error)
	readFile                  func(string) ([]byte, error)
	writeFile                 func(string, []byte, os.FileMode) error
	mkdirAll                  func(string, os.FileMode) error
	chmod                     func(string, os.FileMode) error
	sshPublicKey              func(string) (string, error)
	generateSSHKeyPair        func(string, string, string) error
	sshAgentList              func() (string, error)
	sshAgentAdd               func(string) error
	hostname                  func() (string, error)
	currentUsername           func() string
	prompter                  configurePrompter
	personalServerProvisioner personalServerProvisioner
}

type configurePrompter interface {
	CanPrompt() bool
	Confirm(title string, affirmative bool) (bool, error)
	Input(title string, defaultValue string, validate func(string) error) (string, error)
	Password(title string) (string, error)
	SelectSSHIdentity(choices []sshIdentityPromptChoice, selected int) (sshIdentityPromptChoice, error)
	SelectPersonalServerLocation(choices []personalServerLocationChoice, selected int) (personalServerLocationChoice, error)
	SelectPersonalServerType(choices []personalServerTypeChoice, selected int) (personalServerTypeChoice, error)
}

func newConfigureCommand() *cobra.Command {
	var opts configureOptions

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Configure project roots",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.localRootSet = cmd.Flags().Changed("local-root")
			opts.remoteRootSet = cmd.Flags().Changed("remote-root")
			opts.sshIdentityFileSet = cmd.Flags().Changed("ssh-identity-file")
			deps := defaultConfigureDeps()
			deps.ctx = cmd.Context()
			return runConfigure(cmd.OutOrStdout(), opts, deps)
		},
	}

	cmd.Flags().StringVar(&opts.localRoot, "local-root", "", "Local project root under your home directory")
	cmd.Flags().StringVar(&opts.remoteRoot, "remote-root", "", "Remote project root under the remote home directory")
	cmd.Flags().StringVar(&opts.sshIdentityFile, "ssh-identity-file", "", "Existing Ed25519 private key path to use for remote SSH")

	return cmd
}

func defaultConfigureDeps() configureDeps {
	env := os.Getenv

	return configureDeps{
		appConfigPath: func() (string, error) {
			return defaultAppConfigPath(env)
		},
		userHomeDir: os.UserHomeDir,
		workingDir:  os.Getwd,
		gitRoot:     gitRootFromWorkingDir,
		stat:        os.Stat,
		readDir:     os.ReadDir,
		readFile:    os.ReadFile,
		writeFile:   os.WriteFile,
		mkdirAll:    os.MkdirAll,
		chmod:       os.Chmod,
		sshPublicKey: func(identityPath string) (string, error) {
			output, err := exec.Command("ssh-keygen", "-y", "-f", identityPath).CombinedOutput()
			if err != nil {
				return "", commandOutputError("ssh-keygen -y", output, err)
			}
			return string(output), nil
		},
		generateSSHKeyPair: func(identityPath string, passphrase string, comment string) error {
			output, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", passphrase, "-C", comment, "-f", identityPath).CombinedOutput()
			if err != nil {
				return commandOutputError("ssh-keygen", output, err)
			}
			return nil
		},
		sshAgentList: func() (string, error) {
			output, err := exec.Command("ssh-add", "-l", "-E", "sha256").CombinedOutput()
			if err != nil {
				return string(output), err
			}
			return string(output), nil
		},
		sshAgentAdd: func(identityPath string) error {
			cmd := exec.Command("ssh-add", identityPath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
		hostname:        os.Hostname,
		currentUsername: currentOSUsername,
		prompter:        huhPrompter{},
	}
}

func runConfigure(out io.Writer, opts configureOptions, deps configureDeps) error {
	deps = fillConfigureDeps(deps)

	appConfigPath, err := deps.appConfigPath()
	if err != nil {
		return err
	}

	cfg, err := loadAppConfig(appConfigPath)
	if err != nil {
		return err
	}

	home, err := deps.userHomeDir()
	if err != nil {
		return fmt.Errorf("find user home directory: %w", err)
	}

	localRoot, err := configureLocalRoot(opts, cfg, home, deps)
	if err != nil {
		return err
	}

	remoteRoot, err := configureRemoteRoot(opts, cfg, localRoot, deps)
	if err != nil {
		return err
	}

	cfg.Projects.LocalRoot = localRoot
	cfg.Projects.RemoteRoot = remoteRoot

	sshResult, sshErr := configureSSHIdentity(opts, cfg, home, deps)
	if sshErr == nil {
		cfg.SSH.IdentityFile = sshResult.identityFile
	} else if sshResult.clearIdentity {
		cfg.SSH.IdentityFile = ""
	}

	if err := saveAppConfig(appConfigPath, cfg); err != nil {
		return err
	}

	for _, message := range sshResult.messages {
		fmt.Fprintln(out, message)
	}
	fmt.Fprintln(out, "Saved configuration.")
	fmt.Fprintf(out, "Local project root: ~/%s\n", localRoot)
	fmt.Fprintf(out, "Remote project root: ~/%s\n", remoteRoot)
	if cfg.SSH.IdentityFile != "" {
		fmt.Fprintf(out, "SSH identity: ~/%s\n", cfg.SSH.IdentityFile)
	} else {
		fmt.Fprintln(out, "SSH identity: not configured")
	}
	if sshErr != nil {
		return sshErr
	}
	if personalServerErr := deps.personalServerProvisioner.Configure(deps.ctx, out, appConfigPath, cfg, deps.prompter); personalServerErr != nil {
		return personalServerErr
	}
	return nil
}

func configureLocalRoot(opts configureOptions, cfg appConfig, home string, deps configureDeps) (string, error) {
	if opts.localRootSet {
		return normalizeLocalProjectRoot(opts.localRoot, home, deps.stat)
	}

	defaultValue := validLocalRootDefault(cfg.Projects.LocalRoot, home, deps.stat)
	if defaultValue == "" {
		defaultValue = inferLocalRootDefault(home, deps)
	}

	input, err := promptConfigureValue(deps, "Local project root", defaultValue, func(value string) error {
		_, err := normalizeLocalProjectRoot(value, home, deps.stat)
		return err
	})
	if err != nil {
		return "", err
	}
	return normalizeLocalProjectRoot(input, home, deps.stat)
}

func configureRemoteRoot(opts configureOptions, cfg appConfig, localRoot string, deps configureDeps) (string, error) {
	if opts.remoteRootSet {
		return normalizeRemoteProjectRoot(opts.remoteRoot)
	}

	defaultValue := validRemoteRootDefault(cfg.Projects.RemoteRoot)
	if defaultValue == "" {
		defaultValue = localRoot
	}

	input, err := promptConfigureValue(deps, "Remote project root", defaultValue, func(value string) error {
		_, err := normalizeRemoteProjectRoot(value)
		return err
	})
	if err != nil {
		return "", err
	}
	return normalizeRemoteProjectRoot(input)
}

type configureSSHIdentityResult struct {
	identityFile  string
	messages      []string
	clearIdentity bool
}

func configureSSHIdentity(opts configureOptions, cfg appConfig, home string, deps configureDeps) (configureSSHIdentityResult, error) {
	if opts.sshIdentityFileSet {
		candidate, err := loadSSHIdentity(opts.sshIdentityFile, home, deps)
		if err != nil {
			return configureSSHIdentityResult{}, err
		}
		return configureSSHIdentityResult{
			identityFile: candidate.IdentityFile,
			messages:     nonEmptyMessages(candidate.Warning),
		}, nil
	}

	var current sshIdentityCandidate
	currentConfigured := strings.TrimSpace(cfg.SSH.IdentityFile) != ""
	currentValid := false
	if currentConfigured {
		if candidate, err := loadSSHIdentity(cfg.SSH.IdentityFile, home, deps); err == nil {
			current = candidate
			currentValid = true
		}
	}

	if !deps.prompter.CanPrompt() {
		if currentValid {
			return configureSSHIdentityResult{
				identityFile: current.IdentityFile,
				messages:     nonEmptyMessages(current.Warning),
			}, nil
		}
		return configureSSHIdentityResult{clearIdentity: true}, sshIdentityNotConfiguredError{
			reason: "SSH identity is not configured; pass --ssh-identity-file",
		}
	}

	candidates, err := discoverSSHIdentities(home, deps)
	if err != nil {
		return configureSSHIdentityResult{clearIdentity: currentConfigured && !currentValid}, err
	}
	if currentValid {
		candidates = append([]sshIdentityCandidate{current}, candidates...)
	}

	choices := dedupeSSHIdentityChoices(candidates)
	selected := selectedSSHIdentityChoice(choices, current)
	choices = append(choices, sshIdentityPromptChoice{
		Label:    generateSSHIdentityChoiceText,
		Generate: true,
	})

	choice, err := deps.prompter.SelectSSHIdentity(choices, selected)
	if err != nil {
		return configureSSHIdentityResult{clearIdentity: currentConfigured && !currentValid}, err
	}

	var candidate sshIdentityCandidate
	var messages []string
	if choice.Generate {
		generated, err := configureGeneratedSSHIdentity(home, deps)
		if err != nil {
			return configureSSHIdentityResult{clearIdentity: currentConfigured && !currentValid}, err
		}
		candidate = generated
	} else {
		candidate, err = loadSSHIdentity(choice.Identity.IdentityFile, home, deps)
		if err != nil {
			return configureSSHIdentityResult{clearIdentity: currentConfigured && !currentValid}, err
		}
	}
	messages = append(messages, candidate.Warning)

	agentMessages, err := maybeAddSSHIdentityToAgent(candidate, deps)
	if err != nil {
		return configureSSHIdentityResult{clearIdentity: currentConfigured && !currentValid}, err
	}
	messages = append(messages, agentMessages...)

	return configureSSHIdentityResult{
		identityFile: candidate.IdentityFile,
		messages:     nonEmptyMessages(messages...),
	}, nil
}

func selectedSSHIdentityChoice(choices []sshIdentityPromptChoice, current sshIdentityCandidate) int {
	if current.IdentityFile != "" {
		for index, choice := range choices {
			if choice.Identity.IdentityFile == current.IdentityFile {
				return index
			}
		}
	}
	for index, choice := range choices {
		if choice.Identity.IdentityFile == primaryGeneratedSSHIdentity {
			return index
		}
	}
	return 0
}

func configureGeneratedSSHIdentity(home string, deps configureDeps) (sshIdentityCandidate, error) {
	target, err := generatedSSHIdentityTarget(home, deps)
	if err != nil {
		return sshIdentityCandidate{}, err
	}
	if target.Recovered != nil {
		return *target.Recovered, nil
	}

	generate, err := deps.prompter.Confirm(fmt.Sprintf("Generate new Ed25519 SSH keypair at ~/%s?", target.IdentityFile), true)
	if err != nil {
		return sshIdentityCandidate{}, err
	}
	if !generate {
		return sshIdentityCandidate{}, sshIdentityNotConfiguredError{reason: "SSH identity was not configured"}
	}

	passphrase, err := deps.prompter.Password("SSH key passphrase (empty allowed)")
	if err != nil {
		return sshIdentityCandidate{}, err
	}

	sshDir := filepath.Dir(target.IdentityPath)
	if err := deps.mkdirAll(sshDir, 0o700); err != nil {
		return sshIdentityCandidate{}, fmt.Errorf("create ~/.ssh: %w", err)
	}
	if err := deps.chmod(sshDir, 0o700); err != nil {
		return sshIdentityCandidate{}, fmt.Errorf("secure ~/.ssh: %w", err)
	}

	if err := deps.generateSSHKeyPair(target.IdentityPath, passphrase, generatedSSHKeyComment(deps)); err != nil {
		return sshIdentityCandidate{}, err
	}
	if err := deps.chmod(target.IdentityPath, 0o600); err != nil {
		return sshIdentityCandidate{}, fmt.Errorf("secure SSH identity: %w", err)
	}
	if err := deps.chmod(target.IdentityPath+".pub", 0o644); err != nil {
		return sshIdentityCandidate{}, fmt.Errorf("secure SSH public key: %w", err)
	}

	return loadSSHIdentity(target.IdentityFile, home, deps)
}

func maybeAddSSHIdentityToAgent(candidate sshIdentityCandidate, deps configureDeps) ([]string, error) {
	if candidate.Fingerprint != "" {
		if output, err := deps.sshAgentList(); err == nil && sshAgentHasFingerprint(output, candidate.Fingerprint) {
			return nil, nil
		}
	}

	add, err := deps.prompter.Confirm(fmt.Sprintf("Add SSH identity ~/%s to ssh-agent? Recommended for remote server access.", candidate.IdentityFile), true)
	if err != nil {
		return nil, err
	}
	if !add {
		return []string{"SSH identity was not added to ssh-agent."}, nil
	}

	if err := deps.sshAgentAdd(candidate.IdentityPath); err != nil {
		return []string{fmt.Sprintf("Warning: could not add SSH identity ~/%s to ssh-agent: %v", candidate.IdentityFile, err)}, nil
	}
	return nil, nil
}

func sshAgentHasFingerprint(output string, fingerprint string) bool {
	return strings.Contains(output, fingerprint)
}

func nonEmptyMessages(messages ...string) []string {
	var out []string
	for _, message := range messages {
		if strings.TrimSpace(message) != "" {
			out = append(out, message)
		}
	}
	return out
}

func fillConfigureDeps(deps configureDeps) configureDeps {
	if deps.ctx == nil {
		deps.ctx = context.Background()
	}
	if deps.appConfigPath == nil {
		env := os.Getenv
		deps.appConfigPath = func() (string, error) {
			return defaultAppConfigPath(env)
		}
	}
	if deps.userHomeDir == nil {
		deps.userHomeDir = os.UserHomeDir
	}
	if deps.workingDir == nil {
		deps.workingDir = os.Getwd
	}
	if deps.gitRoot == nil {
		deps.gitRoot = gitRootFromWorkingDir
	}
	if deps.stat == nil {
		deps.stat = os.Stat
	}
	if deps.readDir == nil {
		deps.readDir = os.ReadDir
	}
	if deps.readFile == nil {
		deps.readFile = os.ReadFile
	}
	if deps.writeFile == nil {
		deps.writeFile = os.WriteFile
	}
	if deps.mkdirAll == nil {
		deps.mkdirAll = os.MkdirAll
	}
	if deps.chmod == nil {
		deps.chmod = os.Chmod
	}
	if deps.sshPublicKey == nil {
		deps.sshPublicKey = func(identityPath string) (string, error) {
			output, err := exec.Command("ssh-keygen", "-y", "-f", identityPath).CombinedOutput()
			if err != nil {
				return "", commandOutputError("ssh-keygen -y", output, err)
			}
			return string(output), nil
		}
	}
	if deps.generateSSHKeyPair == nil {
		deps.generateSSHKeyPair = func(identityPath string, passphrase string, comment string) error {
			output, err := exec.Command("ssh-keygen", "-t", "ed25519", "-N", passphrase, "-C", comment, "-f", identityPath).CombinedOutput()
			if err != nil {
				return commandOutputError("ssh-keygen", output, err)
			}
			return nil
		}
	}
	if deps.sshAgentList == nil {
		deps.sshAgentList = func() (string, error) {
			output, err := exec.Command("ssh-add", "-l", "-E", "sha256").CombinedOutput()
			if err != nil {
				return string(output), err
			}
			return string(output), nil
		}
	}
	if deps.sshAgentAdd == nil {
		deps.sshAgentAdd = func(identityPath string) error {
			cmd := exec.Command("ssh-add", identityPath)
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		}
	}
	if deps.hostname == nil {
		deps.hostname = os.Hostname
	}
	if deps.currentUsername == nil {
		deps.currentUsername = currentOSUsername
	}
	if deps.prompter == nil {
		deps.prompter = huhPrompter{}
	}
	if deps.personalServerProvisioner == nil {
		deps.personalServerProvisioner = personalServerProvisioningGate{
			userHomeDir:     deps.userHomeDir,
			stat:            deps.stat,
			readFile:        deps.readFile,
			writeFile:       deps.writeFile,
			chmod:           deps.chmod,
			sshPublicKey:    deps.sshPublicKey,
			currentUsername: deps.currentUsername,
		}
	}
	return deps
}

func promptConfigureValue(deps configureDeps, title string, defaultValue string, validate func(string) error) (string, error) {
	if !deps.prompter.CanPrompt() {
		return "", fmt.Errorf("interactive configuration requires a terminal; pass --local-root and --remote-root")
	}

	return deps.prompter.Input(title, defaultValue, validate)
}

func validLocalRootDefault(value string, home string, stat func(string) (os.FileInfo, error)) string {
	normalized, err := normalizeLocalProjectRoot(value, home, stat)
	if err != nil {
		return ""
	}
	return normalized
}

func validRemoteRootDefault(value string) string {
	normalized, err := normalizeRemoteProjectRoot(value)
	if err != nil {
		return ""
	}
	return normalized
}

func inferLocalRootDefault(home string, deps configureDeps) string {
	cwd, err := deps.workingDir()
	if err == nil {
		if gitRoot, err := deps.gitRoot(cwd); err == nil && gitRoot != "" {
			if normalized, err := normalizeLocalProjectRoot(filepath.Dir(gitRoot), home, deps.stat); err == nil {
				return normalized
			}
		}
	}

	if normalized, err := normalizeLocalProjectRoot(filepath.Join(home, "projects"), home, deps.stat); err == nil {
		return normalized
	}

	return ""
}

func gitRootFromWorkingDir(cwd string) (string, error) {
	output, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}

	root := strings.TrimSpace(string(output))
	if root == "" {
		return "", fmt.Errorf("git returned an empty project root")
	}
	return root, nil
}

func commandOutputError(command string, output []byte, err error) error {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return fmt.Errorf("%s: %w", command, err)
	}
	return fmt.Errorf("%s: %w: %s", command, err, message)
}

func normalizeLocalProjectRoot(input string, home string, stat func(string) (os.FileInfo, error)) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", fmt.Errorf("local project root is required")
	}
	if stat == nil {
		stat = os.Stat
	}

	candidate, err := localProjectRootPath(value, home)
	if err != nil {
		return "", err
	}

	relative, err := relativeSubdirectory(candidate, home, "local project root")
	if err != nil {
		return "", err
	}

	info, err := stat(candidate)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("local project root must be an existing directory")
		}
		return "", fmt.Errorf("check local project root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("local project root must be an existing directory")
	}

	return filepath.ToSlash(relative), nil
}

func localProjectRootPath(value string, home string) (string, error) {
	home = filepath.Clean(home)

	switch {
	case value == "~":
		return home, nil
	case strings.HasPrefix(value, "~/"):
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(value, "~/"))), nil
	case strings.HasPrefix(value, "~"):
		return "", fmt.Errorf("local project root must use ~ or ~/ to reference home")
	case filepath.IsAbs(value):
		return filepath.Clean(value), nil
	default:
		return filepath.Clean(filepath.Join(home, value)), nil
	}
}

func normalizeRemoteProjectRoot(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", fmt.Errorf("remote project root is required")
	}

	switch {
	case value == "~":
		value = "."
	case strings.HasPrefix(value, "~/"):
		value = strings.TrimPrefix(value, "~/")
	case strings.HasPrefix(value, "~"):
		return "", fmt.Errorf("remote project root must use ~ or ~/ to reference home")
	case strings.HasPrefix(value, "/"):
		return "", fmt.Errorf("remote project root must be relative to the remote home directory")
	}

	if strings.Contains(value, "\\") {
		return "", fmt.Errorf("remote project root must use slash separators")
	}

	normalized := path.Clean(value)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") || path.IsAbs(normalized) {
		return "", fmt.Errorf("remote project root must be a subdirectory of the remote home directory")
	}

	return normalized, nil
}

func relativeSubdirectory(candidate string, home string, name string) (string, error) {
	home = filepath.Clean(home)
	candidate = filepath.Clean(candidate)

	relative, err := filepath.Rel(home, candidate)
	if err != nil {
		return "", fmt.Errorf("%s must be a subdirectory of your home directory", name)
	}
	if relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%s must be a subdirectory of your home directory", name)
	}

	return relative, nil
}
