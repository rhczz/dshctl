package nodejs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// This file holds the adversarial half of the suite: the arithmetic the whole
// package rests on, and every on-disk shape a version manager can produce.

// TestCompareReadsEverySegmentAsANumber pins that a version is compared
// segment by segment as an integer, not as text and not only at the first
// position. A textual comparison puts 24.9 above 24.10, which would make a
// request match the wrong release and the gate judge it wrongly.
func TestCompareReadsEverySegmentAsANumber(t *testing.T) {
	cases := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{"a two-digit major beats a one-digit major", "10.0.0", "9.0.0", 1},
		{"a one-digit major loses to a two-digit major", "9.0.0", "10.0.0", -1},
		{"a two-digit minor beats a one-digit minor", "24.10.0", "24.2.0", 1},
		{"a one-digit minor loses to a two-digit minor", "24.2.0", "24.10.0", -1},
		{"a two-digit patch beats a one-digit patch", "24.20.10", "24.20.9", 1},
		{"a one-digit patch loses to a two-digit patch", "24.20.9", "24.20.10", -1},
		{"a three-digit patch beats a two-digit patch", "24.20.100", "24.20.99", 1},
		{"a longer version with a zero segment is equal", "24.20.0.0", "24.20", 0},
		{"the floor boundary is inclusive", "24.12.0", "24.12.0", 0},
		{"one patch below the floor is below it", "24.11.999", "24.12.0", -1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Compare(testCase.left, testCase.right); got != testCase.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d", testCase.left, testCase.right, got, testCase.want)
			}
		})
	}
}

// TestCompareTreatsUnknownSegmentsAsZero pins the arithmetic's tolerance for the
// strings that reach it: an empty version, a bare "v", a pre-release suffix, a
// segment that is not a number, and a segment too large for int64.
//
// Nobody validates a release before comparing it — the resolver compares
// directory names and a probe's output — so the function has to answer
// something for all of them and never panic. A segment that cannot be read
// counts as zero, which makes every such string compare as 0.x.y; the pins here
// record that so a change to the rule is a visible decision rather than a
// surprise.
func TestCompareTreatsUnknownSegmentsAsZero(t *testing.T) {
	cases := []struct {
		name  string
		left  string
		right string
		want  int
	}{
		{"two empty versions", "", "", 0},
		{"an empty version equals a zero version", "", "0", 0},
		{"an empty version equals garbage", "", "garbage", 0},
		{"a bare v with a stray segment", "v-v", "0", 0},
		{"a non-numeric segment counts as zero", "24.20.x", "24.20", 0},
		{"a non-numeric segment is below a real one", "24.20.x", "24.20.1", -1},
		{"a non-numeric minor counts as zero", "24.x.9", "24.0.9", 0},
		{"a segment that overflows int64 counts as zero", "99999999999999999999", "0", 0},
		{"a segment that overflows int64 is below one", "99999999999999999999", "1", -1},
		{"a pre-release suffix is ignored", "24.20.0-rc.1", "24.20.0", 0},
		{"two different pre-release suffixes are equal", "24.20.0-rc.1", "24.20.0-rc.2", 0},
		{"build metadata is ignored", "24.20.0+build5", "24.20.0-alpha", 0},
		{"surrounding whitespace is trimmed", " 24.20.0 ", "24.20", 0},
		{"a capital V is not a version prefix", "V24.20.0", "24.20.0", -1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Compare(testCase.left, testCase.right); got != testCase.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d", testCase.left, testCase.right, got, testCase.want)
			}
			if got := Compare(testCase.right, testCase.left); got != -testCase.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d (the reverse of the pinned pair)",
					testCase.right, testCase.left, got, -testCase.want)
			}
		})
	}
}

// TestMatchesAgreesWithCompare pins the equality predicate the resolver uses to
// decide whether a release satisfies a request.
func TestMatchesAgreesWithCompare(t *testing.T) {
	if !Matches("24.20.0", "24.20") || !Matches("v24.20.0", "24.20.0") {
		t.Fatal("an equivalent release must match")
	}
	if Matches("24.20.0", "24.21") || Matches("24.20.0", "24.20.1") {
		t.Fatal("a different release must not match")
	}
	if Matches("", "24.20") || Matches("24.20", "") || Matches("  ", "24.20") {
		t.Fatal("an empty release never matches")
	}
}

// TestParseVersionReadsTheFirstToken pins the `node -v` reader on the values the
// probe tests cannot express through a whole resolution.
func TestParseVersionReadsTheFirstToken(t *testing.T) {
	cases := map[string]string{
		"v24.20.0\r\n":         "24.20.0",
		"v24.20.0\n":           "24.20.0",
		"v24.20.0\nv22.19.0\n": "24.20.0",
		"v24.20.0 extra":       "24.20.0",
		"v24.20.0\ttrailing":   "24.20.0",
		"\n\n":                 "",
		"   \t ":               "",
		"v":                    "",
		"v24.20.0-rc.1\n":      "24.20.0-rc.1",
		"V24.20.0\n":           "V24.20.0",
	}
	for input, want := range cases {
		if got := ParseVersion(input); got != want {
			t.Fatalf("ParseVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

// installFnMUnder creates one fnm release below an explicit node-versions root.
//
// The roots differ per platform, and the two layouts differ per fnm generation,
// so the caller names both rather than relying on the layout this machine
// happens to have.
func installFnMUnder(t *testing.T, root, version, layout string) string {
	t.Helper()
	return writeNode(t, filepath.Join(root, "v"+version, filepath.FromSlash(layout), nodeBinaryName))
}

// TestResolveFindsEveryFnMRootAndLayout pins the shapes an fnm installation can
// have on disk: the current layout that nests the runtime under installation/,
// the older layout that puts it directly under the version, the Windows layout
// that has no bin/ level at all, and each of the three directories fnm uses for
// its releases.
//
// A search that probes <version>/bin/node finds nothing on a current fnm
// installation and reports "no Node installed" for a perfectly good one.
func TestResolveFindsEveryFnMRootAndLayout(t *testing.T) {
	roots := []struct {
		name     string
		relative string
	}{
		{"linux data directory", filepath.Join(".local", "share", "fnm", "node-versions")},
		{"macOS application support", filepath.Join("Library", "Application Support", "fnm", "node-versions")},
		{"windows roaming app data", filepath.Join("AppData", "Roaming", "fnm", "node-versions")},
	}
	layouts := []struct {
		name     string
		relative string
	}{
		{"current layout", filepath.Join("installation", "bin")},
		{"older layout", "bin"},
		{"windows layout", "."},
	}
	const version = "24.20.0"
	for _, root := range roots {
		for _, layout := range layouts {
			t.Run(root.name+", "+layout.name, func(t *testing.T) {
				m := newMachine(t)
				want := installFnMUnder(t, filepath.Join(m.home, root.relative), version, layout.relative)

				got, err := m.resolver().Resolve(context.Background(), Preferences{Version: version, Home: m.home})
				if err != nil {
					t.Fatalf("Resolve: %v", err)
				}
				if got.NodePath != want || got.Source != SourceFNM {
					t.Fatalf("resolved = %+v, want the fnm installation at %q", got, want)
				}
				if got.Version != version || got.BinDir != filepath.Dir(want) {
					t.Fatalf("resolved = %+v, want version %q and bin dir %q", got, version, filepath.Dir(want))
				}
				// The release is implied by the directory name, so no process
				// may have been started to learn it.
				m.wantProbes(t)
			})
		}
	}
}

// TestResolveFindsTheNVMWindowsLayout pins the layout nvm-windows uses: the
// runtime sits directly under the version directory, without a bin/ level. A
// search that only understood the Unix layout found nothing here and reported
// "no Node installed" for a working installation.
func TestResolveFindsTheNVMWindowsLayout(t *testing.T) {
	m := newMachine(t)
	want := installNVMWindows(t, m.home, "24.20.0")

	got, err := m.resolver().Resolve(context.Background(), Preferences{Version: "24.20.0", Home: m.home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != want || got.Source != SourceNVM {
		t.Fatalf("resolved = %+v, want the nvm-windows installation at %q", got, want)
	}
}

// TestResolveIgnoresUnusableManagerEntries pins that a version directory without
// a runnable binary, or with a directory where the binary belongs, or with a
// name that is not a release, is not a candidate — and does not stop the scan.
func TestResolveIgnoresUnusableManagerEntries(t *testing.T) {
	m := newMachine(t)
	root := filepath.Join(m.home, ".nvm", "versions", "node")
	// A release directory with no bin/node at all.
	if err := os.MkdirAll(filepath.Join(root, "v24.20.0", "bin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A release directory whose node is a directory.
	if err := os.MkdirAll(filepath.Join(root, "v24.21.0", "bin", nodeBinaryName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A directory whose name is not a release, holding a perfectly good binary:
	// the release is implied by the name, so a name that carries none makes the
	// entry unusable however runnable the binary is.
	writeNode(t, filepath.Join(root, "current", "bin", nodeBinaryName))
	// A regular file where a release directory belongs.
	if err := os.WriteFile(filepath.Join(root, "v24.23.0"), []byte("not a directory"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	want := installNVM(t, m.home, "24.22.0")

	got, err := m.resolver().Resolve(context.Background(), Preferences{Version: "24.22.0", Home: m.home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != want {
		t.Fatalf("NodePath = %q, want %q", got.NodePath, want)
	}
}

// TestManagerScanIsNewestFirst pins the order the observations and any future
// selection rely on: the scan reports releases from the highest down, so the
// first entry of an empty request is the newest one.
func TestManagerScanIsNewestFirst(t *testing.T) {
	m := newMachine(t)
	installNVM(t, m.home, "24.9.0")
	installFNM(t, m.home, "26.1.0")
	installNVM(t, m.home, "24.20.0")

	candidates := m.resolver().installed(m.home)
	if len(candidates) != 3 {
		t.Fatalf("candidates = %+v, want three", candidates)
	}
	want := []string{"26.1.0", "24.20.0", "24.9.0"}
	for index, version := range want {
		if candidates[index].version != version {
			t.Fatalf("candidates = %+v, want %v", candidates, want)
		}
	}
}

// TestResolveSkipsABrokenManagerSymlinkAndFollowsAWorkingOne pins how a
// symlinked binary is judged.
//
// A version directory whose node is a dangling symlink looks installed — the
// directory is there, the name is there — but running it fails, so it must not
// be offered as an installation. A symlink to a real executable is an ordinary
// setup (a version manager that shares one runtime, a hand-made link), and
// refusing it would report "no Node installed" for a working one.
func TestResolveSkipsABrokenManagerSymlinkAndFollowsAWorkingOne(t *testing.T) {
	m := newMachine(t)
	root := filepath.Join(m.home, ".nvm", "versions", "node")

	brokenDir := filepath.Join(root, "v24.20.0", "bin")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(filepath.Join(m.home, "deleted-runtime"), filepath.Join(brokenDir, nodeBinaryName)); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	real := writeNode(t, filepath.Join(m.home, "store", "node-24.21.0", nodeBinaryName))
	linkedDir := filepath.Join(root, "v24.21.0", "bin")
	if err := os.MkdirAll(linkedDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	linked := filepath.Join(linkedDir, nodeBinaryName)
	if err := os.Symlink(real, linked); err != nil {
		t.Fatalf("symlink to a real binary: %v", err)
	}

	got, err := m.resolver().Resolve(context.Background(), Preferences{Version: "24.21.0", Home: m.home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != linked || got.Version != "24.21.0" || got.Source != SourceNVM {
		t.Fatalf("resolved = %+v, want the symlinked installation at %q", got, linked)
	}

	// A dangling link never satisfies a request for its version.
	if _, err := m.resolver().Resolve(context.Background(), Preferences{Version: "24.20.0", Home: m.home}); err == nil {
		t.Fatal("a version directory holding only a broken symlink must not satisfy a request")
	}
}

// TestResolveReportsWhereItLookedWhenNothingIsInstalled pins that the failure
// for a requested release carries observations for both places, even when one of
// them had nothing at all: "we looked at your version managers and at PATH" is
// the difference between a report and a mystery.
func TestResolveReportsWhereItLookedWhenNothingIsInstalled(t *testing.T) {
	m := newMachine(t)

	_, err := m.resolver().Resolve(context.Background(), Preferences{Version: "24.20.0", Home: m.home})
	failure := wantFailure(t, err)
	if len(failure.Observations) != 2 {
		t.Fatalf("observations = %+v, want the managers and PATH", failure.Observations)
	}
	for _, observation := range failure.Observations {
		if observation.Detail == "" {
			t.Fatalf("observation = %+v, want a reason it had nothing", observation)
		}
	}
	if failure.Observations[0].Source != SourceManagers {
		t.Fatalf("observations = %+v, want the manager summary first", failure.Observations)
	}
}

// TestResolveReportsAFailureWithoutObservationsSafely pins the degenerate input:
// a failure that carries no observations must still render, because a panic in
// the failure path turns a reported problem into a crash.
func TestResolveReportsAFailureWithoutObservationsSafely(t *testing.T) {
	got := Describe(&Failure{Requested: "24.20.0"}, gateMinimum, gateTested)
	if got == "" {
		t.Fatal("Describe returned nothing for a failure with no observations")
	}
	if !contains(got, "24.20.0") {
		t.Fatalf("Describe() = %q, want the request named", got)
	}
}

// TestMajorOfReadsTheLeadingSegment pins the helper the gate's "verified major
// version" rule is built on.
func TestMajorOfReadsTheLeadingSegment(t *testing.T) {
	cases := map[string]int{
		"24.20.0":     24,
		"v24.20.0":    24,
		"25.0.0":      25,
		"":            0,
		"garbage":     0,
		"24":          24,
		"24.20.0-rc1": 24,
	}
	for input, want := range cases {
		if got := majorOf(input); got != want {
			t.Fatalf("majorOf(%q) = %d, want %d", input, got, want)
		}
	}
}

// TestResolverDefaultsToTheRealMachine pins that a zero Resolver is usable: the
// nil host lookups fall back to the platform's, so a caller that constructs one
// without wiring gets a working resolver rather than a panic.
//
// PATH is emptied first: the point is what the fallbacks are, and a machine that
// happens to have a node installed would otherwise decide the outcome.
func TestResolverDefaultsToTheRealMachine(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	empty := &Resolver{}
	if _, err := empty.lookPath("definitely-not-a-real-tool-name"); err == nil {
		t.Fatal("lookPath must reach the real PATH")
	}
	if _, err := empty.glob(filepath.Join(t.TempDir(), "*")); err != nil {
		t.Fatalf("glob: %v", err)
	}
	if _, err := empty.stat(t.TempDir()); err != nil {
		t.Fatalf("stat: %v", err)
	}
	if empty.output() == nil {
		t.Fatal("output must fall back to the real runner")
	}
	// A resolver with no home at all still answers: it fails rather than
	// searching the working directory.
	if _, err := empty.Resolve(context.Background(), Preferences{}); err == nil {
		t.Fatal("a resolver with nothing installed must fail")
	}
}

// TestFailureCarriesItsCause pins that the underlying probe failure survives
// wrapping, so a caller can log the operating system's own words.
func TestFailureCarriesItsCause(t *testing.T) {
	cause := errors.New("boom")
	failure := &Failure{Err: cause}
	if !errors.Is(failure, cause) {
		t.Fatal("Failure must unwrap to the cause it carries")
	}
	if failure.Error() == "" {
		t.Fatal("Failure must render something on its own")
	}
}
