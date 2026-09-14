package cli

import (
	"os"
	"testing"
)

// TestMain makes the hermeticity of this package's tests an enforced property
// rather than a convention.
//
// Every test in this package points HOME and the state directory at a
// throwaway directory, so the run must leave the operator's own home untouched.
// If a test ever forgets one of those overrides and writes to a real location,
// the assertion below fails here instead of silently polluting a developer's
// machine or a CI runner.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "dshctl-cli-home-")
	if err != nil {
		panic("cannot create a throwaway home: " + err.Error())
	}

	previous, hadPrevious := os.LookupEnv("HOME")
	if err := os.Setenv("HOME", home); err != nil {
		panic("cannot redirect HOME: " + err.Error())
	}
	code := m.Run()
	// The compiled test binary is removed here rather than by a deferred call
	// inside a test: os.Exit skips defers, and a leaked binary in the system
	// temporary directory is state this suite must not create.
	cleanupBuiltBinary()
	if hadPrevious {
		_ = os.Setenv("HOME", previous)
	} else {
		_ = os.Unsetenv("HOME")
	}

	// ~/.dsh is the only place dshctl may create outside a temp directory.
	if _, err := os.Stat(home + "/.dsh"); err == nil {
		panic("a cli test created state under a real home directory: " + home + "/.dsh")
	}
	// The throwaway home is removed before exiting: a deferred call would never
	// run, because os.Exit skips defers, and would leak a directory into the
	// system temp on every test run.
	if err := os.RemoveAll(home); err != nil {
		panic("cannot remove the throwaway home: " + err.Error())
	}
	os.Exit(code)
}
