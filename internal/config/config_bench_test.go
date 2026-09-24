package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rhczz/dshctl/internal/paths"
)

// BenchmarkLoadColdHome is the per-invocation floor: every command resolves the
// settings document once, and a cold run pays the file read plus the JSON
// decode plus every field's validation. This is a guard, not a tuning tool —
// if resolution ever starts scanning directories or re-reading the document per
// field, a machine with a slow home mount would feel it on every command.
func BenchmarkLoadCold(b *testing.B) {
	root := b.TempDir()
	getenv := func(key string) string {
		switch key {
		case paths.EnvHarnessHome:
			return root
		case paths.EnvStateDir:
			return filepath.Join(root, "state")
		}
		return os.Getenv(key)
	}
	// The document does not exist: the cold path is what a fresh checkout pays,
	// and it is also the path that must never touch the network.
	b.ResetTimer()
	for b.Loop() {
		if _, err := Load(getenv, Overrides{}); err != nil {
			b.Fatalf("Load: %v", err)
		}
	}
}

// BenchmarkLoadWarmDocument is the steady state: the document exists with the
// fields a long-lived installation has written (repoDir, nodeVersion), and the
// file is small enough to stay under the 64 KiB ceiling.
func BenchmarkLoadWarm(b *testing.B) {
	root := b.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		b.Fatalf("mkdir: %v", err)
	}
	document := `{"repoDir": "` + root + `/deepseek-harness", "port": 3080,
		"nodeVersion": "24.20.0", "startTimeoutSeconds": 90,
		"stopTimeoutSeconds": 15, "lockTimeoutSeconds": 10,
		"logRotateBytes": 4194304, "logLevel": "info"}`
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(document), 0o600); err != nil {
		b.Fatalf("write document: %v", err)
	}
	getenv := func(key string) string {
		if key == paths.EnvHarnessHome {
			return root
		}
		return os.Getenv(key)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := Load(getenv, Overrides{}); err != nil {
			b.Fatalf("Load: %v", err)
		}
	}
}
