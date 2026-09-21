package service

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rhczz/dshctl/internal/config"
	"github.com/rhczz/dshctl/internal/repo"
)

// The paths below are a contract with the checkout, not an implementation
// detail: the build writes its marker where start and doctor look for it, and
// the checkout ships its workspace marker where the repository check expects it.
// Three packages name these files — config, repo and service — and when two of
// them drift apart the failure is silent: a successful build is reported as
// missing, or a real checkout is refused.
//
// The literals are pinned on purpose. Changing one is a coordinated change with
// the repository's own scripts, and this test is what makes that visible.

// TestTheBuildMarkerIsTheFileTheBuildWrites pins the build marker's name and the
// three ways it is derived.
func TestTheBuildMarkerIsTheFileTheBuildWrites(t *testing.T) {
	const wantRelative = ".dsh-build/client-build-environment.json"
	if buildRecordRel != wantRelative {
		t.Fatalf("the service builds the marker path from %q, want %q", buildRecordRel, wantRelative)
	}

	settings := config.Default(t.TempDir())
	want := filepath.Join(settings.RepoDir, ".dsh-build", "client-build-environment.json")
	if got := settings.BuildRecordPath(); got != want {
		t.Fatalf("config.Settings.BuildRecordPath() = %q, want %q", got, want)
	}
	checkout := repo.Repo{Dir: settings.RepoDir, BuildRecordRel: buildRecordRel}
	if got := checkout.BuildRecordPath(); got != settings.BuildRecordPath() {
		t.Fatalf("the repository looks for the marker at %q while the settings name %q",
			got, settings.BuildRecordPath())
	}
}

// TestTheCheckoutMarkersAreTheFilesTheRepositoryShips pins the two markers that
// decide whether a directory is the managed checkout, and that a directory
// carrying both is accepted by the same code that reports it missing.
func TestTheCheckoutMarkersAreTheFilesTheRepositoryShips(t *testing.T) {
	settings := config.Default(t.TempDir())
	if got := filepath.Base(settings.RepoManifest()); got != "package.json" {
		t.Fatalf("the checkout marker is %q, want package.json", got)
	}
	if got := filepath.Base(settings.RepoWorkspaceManifest()); got != "pnpm-workspace.yaml" {
		t.Fatalf("the workspace marker is %q, want pnpm-workspace.yaml", got)
	}

	directory := t.TempDir()
	writeFile(t, filepath.Join(directory, config.ServerManifestRel), "{}")
	writeFile(t, filepath.Join(directory, config.WorkspaceManifestRel), "packages:\n  - packages/*\n")
	checkout := repo.Repo{
		Dir:                  directory,
		ManifestRel:          config.ServerManifestRel,
		WorkspaceManifestRel: config.WorkspaceManifestRel,
	}
	if !checkout.IsServerCheckout() {
		t.Fatalf("%s carries both markers but was not recognized as a checkout", directory)
	}
}

// TestTheRecordGlobFindsRecordsInAnAwkwardDirectory pins that the pattern the
// cross-port guard uses still names this port's record when the state directory
// itself contains characters a glob treats specially.
//
// The guard exists to keep a build from stopping a server that belongs to
// another port. A pattern that matches nothing makes it silently blind, which is
// how a per-port record was overwritten once already.
func TestTheRecordGlobFindsRecordsInAnAwkwardDirectory(t *testing.T) {
	names := []string{"state", "state[1]", "state%dx"}
	if runtime.GOOS != "windows" {
		// A Windows file name cannot contain "*" or "?", so a directory that
		// exercises them cannot be created there. The escaping of those two
		// characters is still pinned on Windows by
		// config.TestQuoteGlobMatchesTheLiteralName, which builds the pattern
		// directly instead of going through the filesystem.
		names = append(names, "state*all", "state?x")
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), name)
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatalf("create %s: %v", root, err)
			}
			settings := config.Default(t.TempDir())
			settings.StateDir = root
			settings.Port = 3080

			pattern := config.StateFileGlob(settings.StateDir)
			matches, err := filepath.Glob(pattern)
			if err != nil {
				t.Fatalf("the record glob %q is not a valid pattern: %v", pattern, err)
			}
			if len(matches) != 0 {
				t.Fatalf("an empty state directory matched %v", matches)
			}
			writeFile(t, settings.StateFile(), "{}")
			matches, err = filepath.Glob(pattern)
			if err != nil {
				t.Fatalf("glob %q: %v", pattern, err)
			}
			if len(matches) != 1 || matches[0] != settings.StateFile() {
				t.Fatalf("glob %q = %v, want exactly %q", pattern, matches, settings.StateFile())
			}
		})
	}
}

// TestTheDerivedPathsSitWhereTheyBelong pins the address, the record and the
// lock every human-facing message and every guard is built from.
//
// config.Default only fills the values that need no filesystem — the repository
// default and the timeouts — so the state directory is set here the way Load
// sets it, and the test asserts the derivation rather than the default.
func TestTheDerivedPathsSitWhereTheyBelong(t *testing.T) {
	settings := config.Default(t.TempDir())
	settings.StateDir = t.TempDir()
	settings.Port = 4321

	if got, want := settings.URL(), "http://127.0.0.1:4321"; got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
	if got := filepath.Dir(settings.StateFile()); got != settings.StateDir {
		t.Fatalf("the record lives in %q, not in the state directory %q", got, settings.StateDir)
	}
	if got := filepath.Dir(settings.LockFile()); got != settings.StateDir {
		t.Fatalf("the lock lives in %q, not beside the state it guards (%q)", got, settings.StateDir)
	}
	if got := filepath.Base(settings.LockFile()); got != config.LockFileName {
		t.Fatalf("the lock file is %q, want %q", got, config.LockFileName)
	}
	// Two ports in one state directory must not share a record: that is the
	// shape that once left a running server unmanageable.
	other := settings
	other.Port = 4322
	if settings.StateFile() == other.StateFile() {
		t.Fatalf("ports %d and %d share the record %q", settings.Port, other.Port, settings.StateFile())
	}
}
