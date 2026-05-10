package cli

import (
	"os"
	"testing"
)

func TestIsTerminalRejectsNonTerminalCharacterDevice(t *testing.T) {
	file, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open null device: %v", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		t.Fatalf("stat null device: %v", err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("%s is not reported as a character device on this platform", os.DevNull)
	}

	if isTerminal(file) {
		t.Fatalf("%s should not be detected as an interactive terminal", os.DevNull)
	}
}
