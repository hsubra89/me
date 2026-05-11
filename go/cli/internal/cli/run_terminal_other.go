//go:build !unix

package cli

import (
	"io"
	"os"
)

func prepareStdioTerminal(io.Reader, *os.File) (func() error, error) {
	return func() error { return nil }, nil
}
