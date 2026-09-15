//go:build unix

package config

// Permission-based tests live in a Unix-only file: they need a directory mode
// that actually stops the process from reading a file, which Windows does not
// have.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/exitcode"
)

// TestAnUnreadableConfigFileIsReportedAsAFailure pins the documented
// classification of a config file the process may not read.
//
// readFile distinguishes a malformed document — a usage error that names the
// field and exits 2 — from a file it could not read at all, which its doc
// comment calls "the failure it is": nothing the operator wrote is wrong, the
// file simply could not be looked at, and exit 1 is what that means. Its sibling
// branches for a non-regular file and for an oversized file already return a
// plain error; the unreadable branch does not, so the same condition is reported
// with two different statuses depending on which step noticed it.
func TestAnUnreadableConfigFileIsReportedAsAFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this test needs")
	}
	environment, stateDir := configDocument(t, `{"port": 4123}`)
	configPath := filepath.Join(stateDir, ConfigFileName)
	if err := os.Chmod(configPath, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(configPath, 0o600) })

	_, err := Load(environment.Getenv, Overrides{})
	if err == nil {
		t.Fatal("an unreadable config file must be reported")
	}
	if !strings.Contains(err.Error(), "无法读取配置文件") {
		t.Fatalf("error = %q, want it to identify the read failure", err)
	}
	// readFile's own comment: a file that cannot be read at all is "reported as
	// the failure it is rather than as a bad setting", so exit 1, not 2.
	if code := exitcode.Of(err); code != exitcode.Failure {
		t.Fatalf("exit code = %d (%v), want %d (Failure): an unreadable file is not a bad setting",
			code, err, exitcode.Failure)
	}
}

// TestWritingBackIntoAReadOnlyStateDirectoryIsReported pins what a start does
// when it cannot record the release it used: the failure has to reach the
// caller, which reports it as a warning rather than as a failed start. The
// service is already running by then, and a version that could not be written
// down is not a reason to stop it.
func TestWritingBackIntoAReadOnlyStateDirectoryIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits this test needs")
	}
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	settings := Default(fixtureHome())
	settings.StateDir = stateDir
	settings.ConfigPath = filepath.Join(stateDir, "config.json")
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })

	_, err := settings.RecordRuntime("", "24.20.0")
	if err == nil {
		t.Fatal("writing into a directory that cannot be written must be reported")
	}
	if !strings.Contains(err.Error(), "config.json") && !strings.Contains(err.Error(), "state") {
		t.Fatalf("error = %v, want it to name what could not be written", err)
	}
}
