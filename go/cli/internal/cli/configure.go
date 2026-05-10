package cli

import (
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
	localRoot     string
	localRootSet  bool
	remoteRoot    string
	remoteRootSet bool
}

type configureDeps struct {
	appConfigPath func() (string, error)
	userHomeDir   func() (string, error)
	workingDir    func() (string, error)
	gitRoot       func(string) (string, error)
	stat          func(string) (os.FileInfo, error)
	prompter      configurePrompter
}

type configurePrompter interface {
	CanPrompt() bool
	Input(title string, defaultValue string, validate func(string) error) (string, error)
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
			return runConfigure(cmd.OutOrStdout(), opts, defaultConfigureDeps())
		},
	}

	cmd.Flags().StringVar(&opts.localRoot, "local-root", "", "Local project root under your home directory")
	cmd.Flags().StringVar(&opts.remoteRoot, "remote-root", "", "Remote project root under the remote home directory")

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
		prompter:    huhPrompter{},
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
	if err := saveAppConfig(appConfigPath, cfg); err != nil {
		return err
	}

	fmt.Fprintln(out, "Saved configuration.")
	fmt.Fprintf(out, "Local project root: ~/%s\n", localRoot)
	fmt.Fprintf(out, "Remote project root: ~/%s\n", remoteRoot)
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

func fillConfigureDeps(deps configureDeps) configureDeps {
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
	if deps.prompter == nil {
		deps.prompter = huhPrompter{}
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
