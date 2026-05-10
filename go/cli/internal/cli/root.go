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
	cmd := &cobra.Command{
		Use:          "me",
		Short:        "Personal command-line tools",
		SilenceUsage: true,
	}

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
	fmt.Fprintf(w, "me %s\ncommit: %s\ndate: %s\n", info.Version, info.Commit, info.Date)
}
