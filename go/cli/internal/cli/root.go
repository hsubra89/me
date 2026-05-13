package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

func NewRootCommand(info BuildInfo) *cobra.Command {
	return newRootCommand(info, rootDeps{})
}

type rootDeps struct {
	connect connectDeps
}

func newRootCommand(info BuildInfo, deps rootDeps) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "myn",
		Short:         "Provision and operate your personal development environment",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	cmd.AddCommand(newAuthCommand())
	cmd.AddCommand(newConnectCommand(deps.connect))
	cmd.AddCommand(newConfigureCommand())
	cmd.AddCommand(newIdleCommand())
	cmd.AddCommand(newRunCommand())
	cmd.AddCommand(newVersionCommand(info))

	return cmd
}

func newVersionCommand(info BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printVersion(cmd.OutOrStdout(), info)
			return nil
		},
	}
}

func printVersion(w io.Writer, info BuildInfo) {
	fmt.Fprintf(w, "myn %s\ncommit: %s\ndate: %s\n", info.Version, info.Commit, info.Date)
}
