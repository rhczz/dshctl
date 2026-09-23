//go:build unix

package config

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/rhczz/dshctl/internal/paths"
)

// TestLoadRejectsAFIFOConfig pins the hang guard: a FIFO at the config path
// would block os.ReadFile forever, so the metadata check refuses it before any
// byte is read.
func TestLoadRejectsAFIFOConfig(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := syscall.Mkfifo(filepath.Join(stateDir, "config.json"), 0o600); err != nil {
		t.Skipf("FIFOs are unavailable: %v", err)
	}

	_, err := Load(env{
		paths.EnvHarnessHome: filepath.Join(root, "harness"),
		paths.EnvStateDir:    stateDir,
	}.Getenv, Overrides{})
	if err == nil || !strings.Contains(err.Error(), "is not a regular file") {
		t.Fatalf("Load with a FIFO config = %v, want a non-regular report", err)
	}
}
