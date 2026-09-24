package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rhczz/dshctl/internal/paths"
)

// benchEnv answers the environment keys the benchmarks set and nothing else:
// an operator's own DSHCTL_* variables must not decide what a benchmark
// measures, the way they must not decide what a command does.
func benchEnv(root string) paths.Getenv {
	stateDir := filepath.Join(root, "state")
	return func(key string) string {
		switch key {
		case paths.EnvHarnessHome:
			return root
		case paths.EnvStateDir:
			return stateDir
		default:
			return ""
		}
	}
}

// BenchmarkLoadCold is the per-invocation floor: every command resolves the
// settings document once, and a cold run pays the resolution without a document
// to read. This is a guard, not a tuning tool — if resolution ever starts
// scanning directories or re-reading per field, a machine with a slow home
// mount would feel it on every command.
func BenchmarkLoadCold(b *testing.B) {
	getenv := benchEnv(b.TempDir())
	b.ResetTimer()
	for b.Loop() {
		if _, err := Load(getenv, Overrides{}); err != nil {
			b.Fatalf("Load: %v", err)
		}
	}
}

// BenchmarkLoadWarm is the steady state of a long-lived installation: the
// document exists where resolution looks for it, so every run pays the file
// read, the JSON decode and every field's layering. This is the run the
// document-reading path can regress in.
func BenchmarkLoadWarm(b *testing.B) {
	root := b.TempDir()
	document := `{ "repoDir": "` + root + `/deepseek-harness", "port": 3080,
		"nodeVersion": "24.20.0", "startTimeoutSeconds": 90,
		"stopTimeoutSeconds": 15, "lockTimeoutSeconds": 10,
		"logRotateBytes": 4194304, "logLevel": "info" }`
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		b.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(document), 0o600); err != nil {
		b.Fatalf("write document: %v", err)
	}
	getenv := benchEnv(root)
	b.ResetTimer()
	for b.Loop() {
		if _, err := Load(getenv, Overrides{}); err != nil {
			b.Fatalf("Load: %v", err)
		}
	}
}
