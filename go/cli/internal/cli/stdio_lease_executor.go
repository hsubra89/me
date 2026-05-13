package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

const (
	defaultStdioIdleAfter = 30 * time.Minute
	stdioOutputDrainGrace = 200 * time.Millisecond
)

type stdioLeaseExecutionRequest struct {
	Command       []string
	IdleAfterText string
	Stdin         io.Reader
	Stdout        io.Writer
}

type stdioLeaseExecution struct {
	Command   []string
	IdleAfter time.Duration
	Stdin     io.Reader
	Stdout    io.Writer
}

type stdioLeaseExecutor struct {
	stdinIsTerminal  func(io.Reader) bool
	stdoutIsTerminal func(io.Writer) bool
	newLeaseSession  func(stdioLeaseExecution) (*stdioLeaseSession, error)
	runExecution     func(stdioLeaseExecution) error
	now              func() time.Time
}

func (executor stdioLeaseExecutor) Run(req stdioLeaseExecutionRequest) error {
	execution, err := executor.validate(req)
	if err != nil {
		return err
	}
	if executor.runExecution != nil {
		return executor.runExecution(execution)
	}
	return executor.runStdioLeaseExecution(execution)
}

func (executor stdioLeaseExecutor) validate(req stdioLeaseExecutionRequest) (stdioLeaseExecution, error) {
	idleAfterText := req.IdleAfterText
	if idleAfterText == "" {
		idleAfterText = defaultStdioIdleAfter.String()
	}
	idleAfter, err := time.ParseDuration(idleAfterText)
	if err != nil {
		return stdioLeaseExecution{}, fmt.Errorf("parse idle-after %q: %w", idleAfterText, err)
	}
	if idleAfter <= 0 {
		return stdioLeaseExecution{}, fmt.Errorf("idle-after must be greater than zero")
	}
	if len(req.Command) == 0 {
		return stdioLeaseExecution{}, fmt.Errorf("missing command after --")
	}
	if !executor.stdinTerminal(req.Stdin) {
		return stdioLeaseExecution{}, fmt.Errorf("myn run --stdio requires terminal-backed stdin")
	}
	if !executor.stdoutTerminal(req.Stdout) {
		return stdioLeaseExecution{}, fmt.Errorf("myn run --stdio requires terminal-backed stdout")
	}

	return stdioLeaseExecution{
		Command:   append([]string(nil), req.Command...),
		IdleAfter: idleAfter,
		Stdin:     req.Stdin,
		Stdout:    req.Stdout,
	}, nil
}

func (executor stdioLeaseExecutor) runStdioLeaseExecution(req stdioLeaseExecution) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("stdio PTY is unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	leaseSession, err := executor.createLeaseSession(req)
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
			now:    executor.stdioNow,
		}, ptmx)
		if isIgnorablePTYCopyError(err) {
			err = nil
		}
		outputDone <- err
	}()

	inputWriter := newStoppableStdioActivityWriter(ptmx, leaseSession.recordInput, executor.stdioNow)
	go func() {
		_, _ = io.Copy(inputWriter, req.Stdin)
	}()

	waitErr := child.Wait()
	inputWriter.stopAccepting()
	restoreErr := restoreTerminal()
	outputErr := waitForStdioPTYOutput(ptmx, outputDone)
	_ = ptmx.Close()
	inputWriter.wait()
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

func (executor stdioLeaseExecutor) createLeaseSession(req stdioLeaseExecution) (*stdioLeaseSession, error) {
	if executor.newLeaseSession != nil {
		return executor.newLeaseSession(req)
	}
	return newStdioLeaseSession(req)
}

func (executor stdioLeaseExecutor) stdinTerminal(stdin io.Reader) bool {
	if executor.stdinIsTerminal != nil {
		return executor.stdinIsTerminal(stdin)
	}
	return readerIsTerminal(stdin)
}

func (executor stdioLeaseExecutor) stdoutTerminal(stdout io.Writer) bool {
	if executor.stdoutIsTerminal != nil {
		return executor.stdoutIsTerminal(stdout)
	}
	return writerIsTerminal(stdout)
}

func (executor stdioLeaseExecutor) stdioNow() time.Time {
	if executor.now != nil {
		return executor.now().UTC()
	}
	return time.Now().UTC()
}

func readerIsTerminal(r io.Reader) bool {
	file, ok := r.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

func writerIsTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(file.Fd())
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
	now    func() time.Time
}

func (writer stdioActivityWriter) Write(p []byte) (int, error) {
	n, err := writer.dst.Write(p)
	if n > 0 && writer.record != nil {
		now := time.Now().UTC()
		if writer.now != nil {
			now = writer.now().UTC()
		}
		if recordErr := writer.record(now); recordErr != nil && err == nil {
			return n, recordErr
		}
	}
	return n, err
}

type stoppableStdioActivityWriter struct {
	writer  stdioActivityWriter
	mu      sync.Mutex
	cond    *sync.Cond
	stopped bool
	active  int
}

func newStoppableStdioActivityWriter(dst io.Writer, record func(time.Time) error, now func() time.Time) *stoppableStdioActivityWriter {
	writer := &stoppableStdioActivityWriter{
		writer: stdioActivityWriter{
			dst:    dst,
			record: record,
			now:    now,
		},
	}
	writer.cond = sync.NewCond(&writer.mu)
	return writer
}

func (writer *stoppableStdioActivityWriter) Write(p []byte) (int, error) {
	writer.mu.Lock()
	if writer.stopped {
		writer.mu.Unlock()
		return 0, os.ErrClosed
	}
	writer.active++
	writer.mu.Unlock()

	n, err := writer.writer.Write(p)

	writer.mu.Lock()
	writer.active--
	if writer.active == 0 {
		writer.cond.Broadcast()
	}
	writer.mu.Unlock()

	return n, err
}

func (writer *stoppableStdioActivityWriter) stop() {
	writer.stopAccepting()
	writer.wait()
}

func (writer *stoppableStdioActivityWriter) stopAccepting() {
	writer.mu.Lock()
	writer.stopped = true
	writer.mu.Unlock()
}

func (writer *stoppableStdioActivityWriter) wait() {
	writer.mu.Lock()
	for writer.active > 0 {
		writer.cond.Wait()
	}
	writer.mu.Unlock()
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
