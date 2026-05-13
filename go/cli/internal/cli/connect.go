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

type connectDeps struct {
	ctx              context.Context
	appConfigPath    func() (string, error)
	loadConfig       func(string) (appConfig, error)
	userHomeDir      func() (string, error)
	workingDir       func() (string, error)
	stat             func(string) (os.FileInfo, error)
	stdinIsTerminal  func(io.Reader) bool
	stdoutIsTerminal func(io.Writer) bool
	runProcess       func(context.Context, connectProcessRequest) error
}

type connectProcessRequest struct {
	Command []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

type connectPlan struct {
	sshUser           string
	sshHost           string
	sshIdentityPath   string
	remotePath        string
	remoteProjectRoot string
	tmuxSessionName   string
}

func newConnectCommand(deps connectDeps) *cobra.Command {
	return &cobra.Command{
		Use:     "connect",
		Aliases: []string{"c"},
		Short:   "Connect to your Personal Server",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConnectCommand(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), args, deps)
		},
	}
}

func runConnectCommand(stdin io.Reader, stdout io.Writer, stderr io.Writer, args []string, deps connectDeps) error {
	if len(args) != 0 {
		return fmt.Errorf("myn connect accepts no path arguments")
	}
	deps = fillConnectDeps(deps)

	configPath, err := deps.appConfigPath()
	if err != nil {
		return err
	}
	cfg, err := deps.loadConfig(configPath)
	if err != nil {
		return err
	}
	home, err := deps.userHomeDir()
	if err != nil {
		return fmt.Errorf("find user home directory: %w", err)
	}

	plan, err := planPersonalServerConnection(cfg, home, deps)
	if err != nil {
		return err
	}
	if !deps.stdinIsTerminal(stdin) {
		return fmt.Errorf("myn connect requires terminal-backed stdin")
	}
	if !deps.stdoutIsTerminal(stdout) {
		return fmt.Errorf("myn connect requires terminal-backed stdout")
	}

	return deps.runProcess(deps.ctx, connectProcessRequest{
		Command: connectSSHCommand(plan),
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
	})
}

func fillConnectDeps(deps connectDeps) connectDeps {
	if deps.ctx == nil {
		deps.ctx = context.Background()
	}
	if deps.appConfigPath == nil {
		env := os.Getenv
		deps.appConfigPath = func() (string, error) {
			return defaultAppConfigPath(env)
		}
	}
	if deps.loadConfig == nil {
		deps.loadConfig = loadAppConfig
	}
	if deps.userHomeDir == nil {
		deps.userHomeDir = os.UserHomeDir
	}
	if deps.workingDir == nil {
		deps.workingDir = os.Getwd
	}
	if deps.stat == nil {
		deps.stat = os.Stat
	}
	if deps.stdinIsTerminal == nil {
		deps.stdinIsTerminal = readerIsTerminal
	}
	if deps.stdoutIsTerminal == nil {
		deps.stdoutIsTerminal = writerIsTerminal
	}
	if deps.runProcess == nil {
		deps.runProcess = runConnectProcess
	}
	return deps
}

func planPersonalServerConnection(cfg appConfig, home string, deps connectDeps) (connectPlan, error) {
	if strings.TrimSpace(cfg.Projects.LocalRoot) == "" {
		return connectPlan{}, fmt.Errorf("local project root is not configured; run `myn configure`")
	}
	if strings.TrimSpace(cfg.Projects.RemoteRoot) == "" {
		return connectPlan{}, fmt.Errorf("remote project root is not configured; run `myn configure`")
	}
	if strings.TrimSpace(cfg.SSH.IdentityFile) == "" {
		return connectPlan{}, fmt.Errorf("SSH identity is not configured; run `myn configure`")
	}
	connectionState, connectionConfig := cfg.PersonalServer.connectionConfigState()
	if connectionState != personalServerConnectionConfigReady {
		return connectPlan{}, connectionState.validationError()
	}

	localRootPath, err := localProjectRootPath(cfg.Projects.LocalRoot, home)
	if err != nil {
		return connectPlan{}, err
	}
	if err := validateExistingDirectory(deps.stat, localRootPath, "local project root"); err != nil {
		return connectPlan{}, err
	}

	_, identityPath, err := normalizeSSHIdentityFile(cfg.SSH.IdentityFile, home, deps.stat)
	if err != nil {
		return connectPlan{}, err
	}
	if err := validateExistingRegularFile(deps.stat, identityPath, "SSH identity file"); err != nil {
		return connectPlan{}, err
	}

	cwd, err := deps.workingDir()
	if err != nil {
		return connectPlan{}, fmt.Errorf("find current working directory: %w", err)
	}
	localRelativePath, err := localPathRelativeToProjectRoot(localRootPath, cwd)
	if err != nil {
		return connectPlan{}, err
	}

	remoteRoot, err := normalizeRemoteProjectRoot(cfg.Projects.RemoteRoot)
	if err != nil {
		return connectPlan{}, err
	}
	remotePath := remoteRoot
	remoteProjectRoot := remoteRoot
	if localRelativePath != "." {
		remoteRelativePath := filepath.ToSlash(localRelativePath)
		remotePath = path.Join(remoteRoot, remoteRelativePath)
		remoteProjectRoot = path.Join(remoteRoot, strings.Split(remoteRelativePath, "/")[0])
	}

	return connectPlan{
		sshUser:           connectionConfig.User,
		sshHost:           connectionConfig.Host,
		sshIdentityPath:   identityPath,
		remotePath:        remotePath,
		remoteProjectRoot: remoteProjectRoot,
		tmuxSessionName:   connectTmuxSessionName(remoteProjectRoot),
	}, nil
}

func localPathRelativeToProjectRoot(localRootPath string, cwd string) (string, error) {
	localRootPath = filepath.Clean(localRootPath)
	cwd = filepath.Clean(cwd)

	relative, err := filepath.Rel(localRootPath, cwd)
	if err != nil {
		return "", fmt.Errorf("current working directory %q is outside configured local project root %q", cwd, localRootPath)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("current working directory %q is outside configured local project root %q", cwd, localRootPath)
	}

	return relative, nil
}

func validateExistingDirectory(stat func(string) (os.FileInfo, error), value string, name string) error {
	info, err := stat(value)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s must be an existing directory", name)
		}
		return fmt.Errorf("check %s: %w", name, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s must be an existing directory", name)
	}
	return nil
}

func validateExistingRegularFile(stat func(string) (os.FileInfo, error), value string, name string) error {
	info, err := stat(value)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s does not exist", name)
		}
		return fmt.Errorf("check %s: %w", name, err)
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return fmt.Errorf("%s must be a regular file", name)
	}
	return nil
}

func connectSSHCommand(plan connectPlan) []string {
	command := personalServerSSHCommandArgs(
		plan.sshIdentityPath,
		plan.sshUser,
		plan.sshHost,
		"-t",
		"-o", "StrictHostKeyChecking=accept-new",
	)
	return append(command, "bash", "-lc", shellQuote(connectRemoteHandoffCommand(plan)))
}

func connectRemoteHandoffCommand(plan connectPlan) string {
	sessionName := plan.tmuxSessionName
	if sessionName == "" {
		sessionName = connectTmuxSessionName(plan.remoteProjectRoot)
	}
	sessionNameArg := shellQuote(sessionName)
	sessionTarget := shellQuote("=" + sessionName)
	remotePath := remoteHomePathExpression(plan.remotePath)
	remoteProjectRoot := remoteHomePathExpression(plan.remoteProjectRoot)

	lines := []string{
		"if tmux has-session -t " + sessionTarget + " 2>/dev/null; then",
		"  exec tmux attach-session -t " + sessionTarget,
		"fi",
		`start_dir="$HOME"`,
		"if [ -d " + remotePath + " ]; then",
		"  start_dir=" + remotePath,
	}
	if remoteProjectRoot != remotePath {
		lines = append(lines,
			"elif [ -d "+remoteProjectRoot+" ]; then",
			"  start_dir="+remoteProjectRoot,
		)
	}
	lines = append(lines,
		"fi",
		`exec tmux new-session -s `+sessionNameArg+` -c "$start_dir"`,
	)
	return strings.Join(lines, "\n")
}

func connectTmuxSessionName(remoteProjectRoot string) string {
	var normalized strings.Builder
	lastWasSeparator := false
	for _, r := range strings.ToLower(remoteProjectRoot) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			normalized.WriteRune(r)
			lastWasSeparator = false
			continue
		}
		if normalized.Len() > 0 && !lastWasSeparator {
			normalized.WriteByte('-')
			lastWasSeparator = true
		}
	}

	value := strings.Trim(normalized.String(), "-")
	if value == "" {
		return "myn-project"
	}
	return "myn-" + value
}

func remoteHomePathExpression(remoteRoot string) string {
	remoteRoot = path.Clean(remoteRoot)
	if remoteRoot == "." {
		return `"$HOME"`
	}
	return `"$HOME"/` + shellQuote(remoteRoot)
}

func runConnectProcess(ctx context.Context, req connectProcessRequest) error {
	if len(req.Command) == 0 {
		return fmt.Errorf("missing process command")
	}

	cmd := exec.CommandContext(ctx, req.Command[0], req.Command[1:]...)
	cmd.Stdin = req.Stdin
	cmd.Stdout = req.Stdout
	cmd.Stderr = req.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return commandExitError{code: exitCodeFromExitError(exitErr)}
		}
		return fmt.Errorf("run %s: %w", req.Command[0], err)
	}
	return nil
}
