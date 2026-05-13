package cli

import (
	"bytes"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	cmd := NewRootCommand(BuildInfo{
		Version: "1.2.3",
		Commit:  "abc123",
		Date:    "2026-05-10",
	})
	cmd.SetArgs([]string{"version"})

	var out bytes.Buffer
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute version command: %v", err)
	}

	const want = "myn 1.2.3\ncommit: abc123\ndate: 2026-05-10\n"
	if got := out.String(); got != want {
		t.Fatalf("version output mismatch:\nwant %q\ngot  %q", want, got)
	}
}
