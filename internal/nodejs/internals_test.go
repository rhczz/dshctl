package nodejs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This file closes the remaining branches of the package: the guards that keep a
// scan from reaching outside the home directory, the readers that turn directory
// names into releases, and the rendering of a failure. They are unit cases
// rather than decision-table rows because they are the machinery beneath the
// rows, and the package is expected to be covered statement by statement.

// TestNewResolverUsesTheRealMachine pins the constructor every caller wires in.
func TestNewResolverUsesTheRealMachine(t *testing.T) {
	resolver := NewResolver()
	if resolver.LookPath == nil || resolver.Glob == nil || resolver.Stat == nil || resolver.Output == nil {
		t.Fatalf("NewResolver() = %+v, want every host lookup wired", resolver)
	}
}

// TestVersionFromPath pins the reader of installation directory names, including
// the shapes that carry no release at all.
func TestVersionFromPath(t *testing.T) {
	cases := map[string]string{
		filepath.Join("/h", ".nvm", "versions", "node", "v24.20.0"): "24.20.0",
		filepath.Join("/h", ".nvm", "versions", "node", "24.20.0"):  "24.20.0",
		filepath.Join("/h", "fnm", "v24.20.0", "installation"):      "24.20.0",
		filepath.Join("/h", ".nvm", "versions", "node", "current"):  "",
		filepath.Join("/h", ".nvm", "versions", "node", "v"):        "",
		"/": "",
		".": "",
		"":  "",
	}
	for dir, want := range cases {
		if got := versionFromPath(dir); got != want {
			t.Fatalf("versionFromPath(%q) = %q, want %q", dir, got, want)
		}
	}
}

// TestCollectWithoutARoot pins the guard that keeps a scan inside a real
// directory: an empty root is not expanded, because filepath.Join("", "…")
// would turn it into a path relative to the working directory.
func TestCollectWithoutARoot(t *testing.T) {
	resolver := &Resolver{Glob: filepath.Glob, Stat: os.Stat}
	if got := resolver.collect("", "*", SourceNVM); got != nil {
		t.Fatalf("collect(\"\", …) = %+v, want nothing", got)
	}
}

// TestCollectSkipsARootThatCannotBeExpanded pins that a pattern the platform
// refuses (a malformed character class, a permission problem) drops that root
// instead of failing the whole resolution: the other managers may still hold the
// release.
func TestCollectSkipsARootThatCannotBeExpanded(t *testing.T) {
	resolver := &Resolver{
		Glob: func(string) ([]string, error) { return nil, errors.New("bad pattern") },
		Stat: os.Stat,
	}
	if got := resolver.collect("/some/root", "*", SourceNVM); got != nil {
		t.Fatalf("collect = %+v, want nothing", got)
	}
}

// TestHomeWithoutAPlatformHome pins the last resort of the home lookup: when the
// platform cannot name a home directory, the scan is skipped rather than pointed
// at whatever directory dshctl was started in.
func TestHomeWithoutAPlatformHome(t *testing.T) {
	// os.UserHomeDir reads $HOME on Unix and %USERPROFILE% on Windows.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	var patterns []string
	resolver := &Resolver{
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
		Glob: func(pattern string) ([]string, error) {
			patterns = append(patterns, pattern)
			return nil, nil
		},
		Stat: os.Stat,
	}
	if _, err := resolver.Resolve(context.Background(), Preferences{Version: "24.20.0"}); err == nil {
		t.Fatal("with no home and nothing on PATH the resolve must fail")
	}
	if len(patterns) != 0 {
		t.Fatalf("searched %v, want no search without a home", patterns)
	}
}

// TestResolveReportsABrokenPATHNodeForARequestedRelease pins the requested-release
// half of an unreadable PATH node: the report still carries both observations,
// so the operator sees that the managers were searched as well.
func TestResolveReportsABrokenPATHNodeForARequestedRelease(t *testing.T) {
	m := newMachine(t)
	binary := filepath.Join(m.home, "usr", "bin", nodeBinaryName)
	m.pathHit = binary
	m.answers[versionProbe] = probeAnswer{err: errors.New("permission denied")}

	_, err := m.resolver().Resolve(context.Background(), Preferences{Version: "24.20.0", Home: m.home})
	failure := wantFailure(t, err)
	if failure.Requested != "24.20.0" {
		t.Fatalf("Requested = %q, want the release that was asked for", failure.Requested)
	}
	if failure.Err == nil || !strings.Contains(failure.Err.Error(), "permission denied") {
		t.Fatalf("Err = %v, want the probe's own failure", failure.Err)
	}
	if len(failure.Observations) != 2 {
		t.Fatalf("observations = %+v, want the managers and PATH", failure.Observations)
	}
	if !strings.Contains(failure.Observations[1].Detail, "permission denied") {
		t.Fatalf("PATH observation = %+v, want the reason", failure.Observations[1])
	}
}

// TestFailureErrorRendersEveryShape pins the compact summary that reaches logs
// and wrapped errors, in all three shapes a failure can have.
func TestFailureErrorRendersEveryShape(t *testing.T) {
	cases := []struct {
		name    string
		failure *Failure
		want    string
	}{
		{"a request that is installed nowhere", &Failure{Requested: "24.20.0"}, "24.20.0"},
		{"a binary that cannot be used", &Failure{Err: errors.New("boom")}, "boom"},
		{"nothing at all", &Failure{}, "PATH"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := testCase.failure.Error(); !strings.Contains(got, testCase.want) {
				t.Fatalf("Error() = %q, want it to contain %q", got, testCase.want)
			}
		})
	}
}

// TestObservationLabels pins how a source is named in the operator-facing text.
func TestObservationLabels(t *testing.T) {
	for source, want := range map[string]string{
		SourcePath:     "PATH",
		SourceManagers: "nvm/fnm",
		SourceNVM:      "nvm",
		SourceFNM:      "fnm",
	} {
		if got := observationLabel(source); got != want {
			t.Fatalf("observationLabel(%q) = %q, want %q", source, got, want)
		}
	}
	if got := originLabel(SourcePath); got != "PATH" {
		t.Fatalf("originLabel(%q) = %q, want PATH", SourcePath, got)
	}
	if got := originLabel(SourceNVM); got != "nvm" {
		t.Fatalf("originLabel(%q) = %q, want nvm", SourceNVM, got)
	}
}

// TestAssessOnAnInstallationWithoutAPath pins the degenerate input: the verdict
// must not depend on the binary being named, because the gate's answer is about
// the release.
func TestAssessOnAnInstallationWithoutAPath(t *testing.T) {
	verdict := Assess(Installation{Version: "24.20.0"}, gateMinimum, gateTested)
	if verdict.Status != Supported {
		t.Fatalf("Status = %q, want %q", verdict.Status, Supported)
	}
}

// TestMajorOfToleratesAnything pins that the major reader answers for the
// strings the gate may be handed, rather than panicking on a release that has no
// segments at all.
func TestMajorOfToleratesAnything(t *testing.T) {
	for _, version := range []string{"", ".", "..", "v", "-", "+"} {
		if got := majorOf(version); got != 0 {
			t.Fatalf("majorOf(%q) = %d, want 0", version, got)
		}
	}
}

// TestProbeTimeoutIsABound pins that the probe window is a short, stated bound
// rather than something a caller can extend indefinitely.
func TestProbeTimeoutIsABound(t *testing.T) {
	if probeTimeout <= 0 {
		t.Fatalf("probeTimeout = %s, want a positive bound", probeTimeout)
	}
	if probeTimeout > 30*time.Second {
		t.Fatalf("probeTimeout = %s, want a bound a preflight can afford", probeTimeout)
	}
}
