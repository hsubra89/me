package main

import (
	"fmt"
	"os"

	"github.com/hsubra89/myn/go/cli/internal/cli"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	root := cli.NewRootCommand(cli.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	})

	if err := root.Execute(); err != nil {
		if code, ok := cli.CommandExitCode(err); ok {
			os.Exit(code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
