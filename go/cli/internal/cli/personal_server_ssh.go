package cli

import (
	"fmt"
	"strings"
)

func personalServerSSHHost(ipv4 string, ipv6 string) string {
	if host := strings.TrimSpace(ipv4); host != "" {
		return host
	}
	return strings.TrimSpace(ipv6)
}

func personalServerSSHCommandArgs(identityFile string, user string, host string, options ...string) []string {
	args := append([]string{"ssh"}, options...)
	args = append(args,
		"-i", identityFile,
		"-l", strings.TrimSpace(user),
		strings.TrimSpace(host),
	)
	return args
}

func personalServerSSHCommandText(identityFile string, user string, host string) string {
	return fmt.Sprintf("ssh -i %s -l %s %s", identityFile, strings.TrimSpace(user), strings.TrimSpace(host))
}
