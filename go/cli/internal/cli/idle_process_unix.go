//go:build unix

package cli

import (
	"errors"

	"golang.org/x/sys/unix"
)

func idleProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}
