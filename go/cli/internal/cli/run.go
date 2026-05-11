package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
	"github.com/spf13/cobra"
)

const (
	defaultStdioIdleAfter = 30 * time.Minute
	stdioOutputDrainGrace = 200 * time.Millisecond
)

type runOptions struct {
	stdio         bool
	idleAfterText string
}

func newRunCommand() *cobra.Command {
	opts := runOptions{
		idleAfterText: defaultStdioIdleAfter.String(),
	}
	cmd := &cobra.Command{
		Use:          "run [--stdio] [--idle-after duration] -- <command...>",
		Short:        "Run a command with an Idle Lease",
		Args:         cobra.ArbitraryArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRunCommand(cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), opts, args, runDeps{})
		},
	}
	cmd.Flags().BoolVar(&opts.stdio, "stdio", false, "run the command under a terminal-backed Stdio Lease")
	cmd.Flags().StringVar(&opts.idleAfterText, "idle-after", defaultStdioIdleAfter.String(), "idle window for stdio activity")
	return cmd
}

type runDeps struct {
	stdinIsTerminal  func(io.Reader) bool
	stdoutIsTerminal func(io.Writer) bool
	runStdio         func(stdioRunRequest) error
}

type stdioRunRequest struct {
	Command   []string
	IdleAfter time.Duration
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
}

func runRunCommand(stdin io.Reader, stdout io.Writer, stderr io.Writer, opts runOptions, args []string, deps runDeps) error {
	if deps.stdinIsTerminal == nil {
		deps.stdinIsTerminal = readerIsTerminal
	}
	if deps.stdoutIsTerminal == nil {
		deps.stdoutIsTerminal = writerIsTerminal
	}
	if deps.runStdio == nil {
		deps.runStdio = runStdioCommand
	}

	if !opts.stdio {
		return fmt.Errorf("non-stdio command leases are not implemented yet; pass --stdio to run an interactive stdio command")
	}

	idleAfterText := opts.idleAfterText
	if idleAfterText == "" {
		idleAfterText = defaultStdioIdleAfter.String()
	}
	idleAfter, err := time.ParseDuration(idleAfterText)
	if err != nil {
		return fmt.Errorf("parse idle-after %q: %w", idleAfterText, err)
	}
	if idleAfter <= 0 {
		return fmt.Errorf("idle-after must be greater than zero")
	}
	if len(args) == 0 {
		return fmt.Errorf("missing command after --")
	}
	if !deps.stdinIsTerminal(stdin) {
		return fmt.Errorf("me run --stdio requires terminal-backed stdin")
	}
	if !deps.stdoutIsTerminal(stdout) {
		return fmt.Errorf("me run --stdio requires terminal-backed stdout")
	}

	return deps.runStdio(stdioRunRequest{
		Command:   append([]string(nil), args...),
		IdleAfter: idleAfter,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
	})
}

func readerIsTerminal(r io.Reader) bool {
	file, ok := r.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

func writerIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

func runStdioCommand(req stdioRunRequest) error {
	if len(req.Command) == 0 {
		return fmt.Errorf("missing command after --")
	}
	if runtime.GOOS == "windows" {
		return fmt.Errorf("stdio PTY is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	leaseSession, err := newStdioLeaseSession(req)
	if err != nil {
		return err
	}

	child := exec.Command(req.Command[0], req.Command[1:]...)
	ptmx, err := pty.Start(child)
	if err != nil {
		if errors.Is(err, pty.ErrUnsupported) {
			return fmt.Errorf("stdio PTY is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
		}
		return fmt.Errorf("start stdio PTY command %q: %w", req.Command[0], err)
	}

	if err := leaseSession.start(child.Process.Pid, stdioProcessGroup(child.Process.Pid)); err != nil {
		_ = ptmx.Close()
		_ = child.Process.Kill()
		_ = child.Wait()
		return fmt.Errorf("create stdio idle lease: %w", err)
	}

	restoreTerminal, err := prepareStdioTerminal(req.Stdin, ptmx)
	if err != nil {
		_ = leaseSession.close()
		_ = ptmx.Close()
		_ = child.Process.Kill()
		_ = child.Wait()
		return fmt.Errorf("prepare stdio terminal: %w", err)
	}

	outputDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(stdioActivityWriter{
			dst:    req.Stdout,
			record: leaseSession.recordOutput,
		}, ptmx)
		if isIgnorablePTYCopyError(err) {
			err = nil
		}
		outputDone <- err
	}()

	go func() {
		_, _ = io.Copy(stdioActivityWriter{
			dst:    ptmx,
			record: leaseSession.recordInput,
		}, req.Stdin)
	}()

	waitErr := child.Wait()
	outputErr := waitForStdioPTYOutput(ptmx, outputDone)
	_ = ptmx.Close()
	restoreErr := restoreTerminal()
	cleanupErr := leaseSession.close()

	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return commandExitError{code: exitCodeFromExitError(exitErr)}
		}
		return fmt.Errorf("wait for stdio command %q: %w", req.Command[0], waitErr)
	}
	if outputErr != nil {
		return fmt.Errorf("copy PTY output: %w", outputErr)
	}
	if restoreErr != nil {
		return restoreErr
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	return nil
}

func waitForStdioPTYOutput(ptmx *os.File, outputDone <-chan error) error {
	timer := time.NewTimer(stdioOutputDrainGrace)
	defer timer.Stop()

	select {
	case err := <-outputDone:
		return err
	case <-timer.C:
		_ = ptmx.Close()
		return <-outputDone
	}
}

func isIgnorablePTYCopyError(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrClosed) {
		return true
	}
	message := err.Error()
	return strings.Contains(message, "input/output error") || strings.Contains(message, "file already closed")
}

type stdioActivityWriter struct {
	dst    io.Writer
	record func(time.Time) error
}

func (writer stdioActivityWriter) Write(p []byte) (int, error) {
	n, err := writer.dst.Write(p)
	if n > 0 && writer.record != nil {
		if recordErr := writer.record(time.Now().UTC()); recordErr != nil && err == nil {
			return n, recordErr
		}
	}
	return n, err
}

type commandExitError struct {
	code int
}

func (err commandExitError) Error() string {
	return fmt.Sprintf("command exited with status %d", err.code)
}

func CommandExitCode(err error) (int, bool) {
	var exitErr commandExitError
	if errors.As(err, &exitErr) {
		return exitErr.code, true
	}
	return 0, false
}

func fallbackExitCode(err *exec.ExitError) int {
	code := err.ExitCode()
	if code >= 0 {
		return code
	}
	return 1
}
