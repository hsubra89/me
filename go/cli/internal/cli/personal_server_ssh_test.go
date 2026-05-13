package cli

import (
	"reflect"
	"testing"
)

func TestPersonalServerSSHHostSelectsIPv4BeforeIPv6(t *testing.T) {
	tests := []struct {
		name string
		ipv4 string
		ipv6 string
		want string
	}{
		{
			name: "prefers IPv4",
			ipv4: " 203.0.113.55 ",
			ipv6: "2001:db8::55",
			want: "203.0.113.55",
		},
		{
			name: "falls back to IPv6",
			ipv6: " 2001:db8::55 ",
			want: "2001:db8::55",
		},
		{
			name: "empty when no address",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := personalServerSSHHost(tt.ipv4, tt.ipv6); got != tt.want {
				t.Fatalf("SSH host mismatch: want %q, got %q", tt.want, got)
			}
		})
	}
}

func TestPersonalServerSSHCommandArgsPassLoginAndHostSeparately(t *testing.T) {
	got := personalServerSSHCommandArgs(
		"/home/harish/.ssh/id_ed25519",
		" harish ",
		" 2001:db8::55 ",
		"-o", "BatchMode=yes",
	)
	want := []string{
		"ssh",
		"-o", "BatchMode=yes",
		"-i", "/home/harish/.ssh/id_ed25519",
		"-l", "harish",
		"2001:db8::55",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SSH command args mismatch: want %#v, got %#v", want, got)
	}
}

func TestPersonalServerSSHCommandTextPassesLoginAndHostSeparately(t *testing.T) {
	got := personalServerSSHCommandText("~/.ssh/id_ed25519", "harish", "2001:db8::55")
	want := "ssh -i ~/.ssh/id_ed25519 -l harish 2001:db8::55"
	if got != want {
		t.Fatalf("SSH command text mismatch: want %q, got %q", want, got)
	}
}
