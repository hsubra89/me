//go:build unix

package cli

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/charmbracelet/x/term"
	"github.com/creack/pty"
)

func prepareStdioTerminal(stdin io.Reader, childPTY *os.File) (func() error, error) {
	userTTY, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(userTTY.Fd()) {
		return func() error { return nil }, nil
	}

	oldState, err := term.MakeRaw(userTTY.Fd())
	if err != nil {
		return nil, fmt.Errorf("enable raw terminal mode: %w", err)
	}

	stopResizeForwarding, err := startStdioResizeForwarding(userTTY, childPTY)
	if err != nil {
		_ = term.Restore(userTTY.Fd(), oldState)
		return nil, fmt.Errorf("start terminal resize forwarding: %w", err)
	}

	return func() error {
		stopResizeForwarding()
		if err := term.Restore(userTTY.Fd(), oldState); err != nil {
			return fmt.Errorf("restore stdio terminal mode: %w", err)
		}
		return nil
	}, nil
}

func startStdioResizeForwarding(source *os.File, target *os.File) (func(), error) {
	events := make(chan os.Signal, 1)
	signal.Notify(events, syscall.SIGWINCH)
	return startStdioResizeForwardingWithEvents(source, target, events, func() {
		signal.Stop(events)
	})
}

func startStdioResizeForwardingWithEvents(source *os.File, target *os.File, events <-chan os.Signal, stopNotify func()) (func(), error) {
	if err := copyTerminalSize(source, target); err != nil {
		if stopNotify != nil {
			stopNotify()
		}
		return nil, err
	}

	done := make(chan struct{})
	go func() {
		for {
			select {
			case _, ok := <-events:
				if !ok {
					return
				}
				_ = copyTerminalSize(source, target)
			case <-done:
				return
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			if stopNotify != nil {
				stopNotify()
			}
			close(done)
		})
	}, nil
}

func copyTerminalSize(source *os.File, target *os.File) error {
	return pty.InheritSize(source, target)
}
