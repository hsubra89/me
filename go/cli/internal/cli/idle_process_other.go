//go:build !unix

package cli

func idleProcessAlive(pid int) bool {
	return pid > 0
}
