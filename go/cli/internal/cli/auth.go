package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

type hetznerAuthOptions struct {
	token             string
	fromHcloudContext string
}

type hetznerAuthDeps struct {
	appConfigPath    func() (string, error)
	hcloudConfigPath func() (string, error)
	env              func(string) string
	validateToken    func(context.Context, string) error
	prompter         hetznerAuthPrompter
}

func newAuthCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
	}

	cmd.AddCommand(newHetznerAuthCommand())

	return cmd
}

func newHetznerAuthCommand() *cobra.Command {
	var opts hetznerAuthOptions

	cmd := &cobra.Command{
		Use:   "hetzner",
		Short: "Configure Hetzner Cloud authentication",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHetznerAuth(cmd.Context(), cmd.OutOrStdout(), opts, defaultHetznerAuthDeps())
		},
	}

	cmd.Flags().StringVar(&opts.token, "token", "", "Hetzner API token to validate and save")
	cmd.Flags().StringVar(&opts.fromHcloudContext, "from-hcloud-context", "", "Import a token from a named hcloud context")

	return cmd
}

func defaultHetznerAuthDeps() hetznerAuthDeps {
	env := os.Getenv
	validator := newHetznerValidator(env("HCLOUD_ENDPOINT"))

	return hetznerAuthDeps{
		appConfigPath: func() (string, error) {
			return defaultAppConfigPath(env)
		},
		hcloudConfigPath: func() (string, error) {
			return defaultHcloudConfigPath(env)
		},
		env:           env,
		validateToken: validator.validate,
		prompter:      huhPrompter{},
	}
}

func runHetznerAuth(ctx context.Context, out io.Writer, opts hetznerAuthOptions, deps hetznerAuthDeps) error {
	deps = fillHetznerAuthDeps(deps)

	if opts.token != "" && opts.fromHcloudContext != "" {
		return fmt.Errorf("--token and --from-hcloud-context cannot be used together")
	}

	appConfigPath, err := deps.appConfigPath()
	if err != nil {
		return err
	}

	if opts.token != "" {
		return validateSaveAndReport(ctx, out, appConfigPath, deps, strings.TrimSpace(opts.token), "")
	}

	if opts.fromHcloudContext != "" {
		return importHcloudContextByName(ctx, out, appConfigPath, opts.fromHcloudContext, deps)
	}

	if token := strings.TrimSpace(deps.env("HCLOUD_TOKEN")); token != "" {
		return validateSaveAndReport(ctx, out, appConfigPath, deps, token, "")
	}

	cfg, err := loadAppConfig(appConfigPath)
	if err != nil {
		return err
	}

	if token := strings.TrimSpace(cfg.Auth.Hetzner.Token); token != "" {
		if err := deps.validateToken(ctx, token); err == nil {
			if !deps.prompter.CanPrompt() {
				fmt.Fprintln(out, "Hetzner authentication is already configured.")
				return nil
			}

			keep, err := deps.prompter.Confirm("Hetzner authentication is already configured. Keep existing credentials?", true)
			if err != nil {
				return err
			}
			if keep {
				fmt.Fprintln(out, "Hetzner authentication is already configured.")
				return nil
			}
		} else {
			reportExistingTokenFailure(out, err)
		}
	}

	if imported, err := maybeImportHcloudContext(ctx, out, appConfigPath, deps); err != nil {
		return err
	} else if imported {
		return nil
	}

	if !deps.prompter.CanPrompt() {
		return fmt.Errorf("interactive Hetzner authentication requires a terminal; pass --token, set HCLOUD_TOKEN, or use --from-hcloud-context")
	}

	token, err := deps.prompter.InputToken()
	if err != nil {
		return err
	}
	return validateSaveAndReport(ctx, out, appConfigPath, deps, strings.TrimSpace(token), "")
}

func fillHetznerAuthDeps(deps hetznerAuthDeps) hetznerAuthDeps {
	if deps.env == nil {
		deps.env = os.Getenv
	}
	if deps.appConfigPath == nil {
		deps.appConfigPath = func() (string, error) {
			return defaultAppConfigPath(deps.env)
		}
	}
	if deps.hcloudConfigPath == nil {
		deps.hcloudConfigPath = func() (string, error) {
			return defaultHcloudConfigPath(deps.env)
		}
	}
	if deps.validateToken == nil {
		validator := newHetznerValidator(deps.env("HCLOUD_ENDPOINT"))
		deps.validateToken = validator.validate
	}
	if deps.prompter == nil {
		deps.prompter = huhPrompter{}
	}
	return deps
}

func importHcloudContextByName(ctx context.Context, out io.Writer, appConfigPath string, name string, deps hetznerAuthDeps) error {
	hcloudConfigPath, err := deps.hcloudConfigPath()
	if err != nil {
		return err
	}

	candidates, err := loadHcloudTokenCandidates(hcloudConfigPath)
	if err != nil {
		return err
	}

	candidate, ok := findHcloudTokenCandidate(candidates, name)
	if !ok {
		return fmt.Errorf("hcloud context %q was not found", name)
	}

	return validateSaveAndReport(ctx, out, appConfigPath, deps, candidate.Token, candidate.Name)
}

func maybeImportHcloudContext(ctx context.Context, out io.Writer, appConfigPath string, deps hetznerAuthDeps) (bool, error) {
	hcloudConfigPath, err := deps.hcloudConfigPath()
	if err != nil {
		fmt.Fprintf(out, "Could not check hcloud config: %v\n", err)
		return false, nil
	}

	candidates, err := loadHcloudTokenCandidates(hcloudConfigPath)
	if err != nil {
		fmt.Fprintf(out, "Could not check hcloud config: %v\n", err)
		return false, nil
	}
	if len(candidates) == 0 {
		return false, nil
	}

	if !deps.prompter.CanPrompt() {
		return false, nil
	}

	candidate, selected, err := chooseHcloudContext(candidates, deps.prompter)
	if err != nil {
		return false, err
	}
	if !selected {
		return false, nil
	}

	if err := deps.validateToken(ctx, candidate.Token); err != nil {
		reportHcloudTokenFailure(out, candidate.Name, err)
		return false, nil
	}

	if err := saveHetznerToken(appConfigPath, candidate.Token); err != nil {
		return false, err
	}
	fmt.Fprintf(out, "Saved Hetzner credentials from hcloud context %q.\n", candidate.Name)
	return true, nil
}

func chooseHcloudContext(candidates []hcloudTokenCandidate, prompter hetznerAuthPrompter) (hcloudTokenCandidate, bool, error) {
	if len(candidates) == 1 {
		candidate := candidates[0]
		use, err := prompter.Confirm(fmt.Sprintf("Use hcloud context %q token?", candidate.Name), true)
		if err != nil {
			return hcloudTokenCandidate{}, false, err
		}
		if !use {
			return hcloudTokenCandidate{}, false, nil
		}
		return candidate, true, nil
	}

	return prompter.SelectHcloudContext(candidates)
}

func validateSaveAndReport(ctx context.Context, out io.Writer, appConfigPath string, deps hetznerAuthDeps, token string, hcloudContextName string) error {
	if err := deps.validateToken(ctx, token); err != nil {
		return fmt.Errorf("Hetzner token validation failed: %s", err)
	}

	if err := saveHetznerToken(appConfigPath, token); err != nil {
		return err
	}

	if hcloudContextName != "" {
		fmt.Fprintf(out, "Saved Hetzner credentials from hcloud context %q.\n", hcloudContextName)
	} else {
		fmt.Fprintln(out, "Saved Hetzner credentials.")
	}

	return nil
}

func saveHetznerToken(path string, token string) error {
	cfg, err := loadAppConfig(path)
	if err != nil {
		return err
	}

	cfg.Auth.Hetzner.Token = token
	return saveAppConfig(path, cfg)
}

func reportExistingTokenFailure(out io.Writer, err error) {
	if validationTimedOut(err) {
		fmt.Fprintln(out, "Tried the existing Hetzner token, but it did not validate within 4s.")
		return
	}
	fmt.Fprintln(out, "Tried the existing Hetzner token, but it did not validate.")
}

func reportHcloudTokenFailure(out io.Writer, contextName string, err error) {
	if validationTimedOut(err) {
		fmt.Fprintf(out, "Tried the hcloud context %q token, but it did not validate within 4s.\n", contextName)
		return
	}
	fmt.Fprintf(out, "Tried the hcloud context %q token, but it did not validate.\n", contextName)
}
