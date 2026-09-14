package nodejs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/rhczz/dshctl/internal/run"
)

// installNVM creates an nvm-style release under home and returns the node path.
func installNVM(t *testing.T, home, version string) string {
	t.Helper()
	return writeNode(t, filepath.Join(home, ".nvm", "versions", "node", "v"+version, "bin", nodeBinaryName))
}

// installFNM creates an fnm-style release, which nests the runtime one level
// deeper than nvm does.
func installFNM(t *testing.T, home, version string) string {
	t.Helper()
	return writeNode(t, filepath.Join(home, ".local", "share", "fnm", "node-versions", "v"+version, "installation", "bin", nodeBinaryName))
}

// installFNMLegacy creates the older fnm layout.
func installFNMLegacy(t *testing.T, home, version string) string {
	t.Helper()
	return writeNode(t, filepath.Join(home, ".local", "share", "fnm", "node-versions", "v"+version, "bin", nodeBinaryName))
}

// writeNode creates an executable file and returns its path.
func writeNode(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// resolver returns a resolver whose PATH lookup always fails, so only the
// version managers can answer.
func resolver() *Resolver {
	return &Resolver{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Glob:     filepath.Glob,
		Stat:     os.Stat,
	}
}

// TestResolvePrefersThePinnedNVMRelease pins the ordinary configuration.
func TestResolvePrefersThePinnedNVMRelease(t *testing.T) {
	home := t.TempDir()
	want := installNVM(t, home, "24.20.0")
	installNVM(t, home, "24.21.0")

	got, err := resolver().Resolve(Preferences{Version: "24.20.0", Home: home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != want {
		t.Fatalf("NodePath = %q, want %q", got.NodePath, want)
	}
	if got.Version != "24.20.0" || got.Source != SourceNVM {
		t.Fatalf("resolved = %+v", got)
	}
	if got.BinDir != filepath.Dir(want) {
		t.Fatalf("BinDir = %q, want %q", got.BinDir, filepath.Dir(want))
	}
}

// TestResolveFindsFNMLayouts is the regression test for probing <version>/bin
// instead of fnm's <version>/installation/bin, which made the whole fnm branch
// dead code.
func TestResolveFindsFNMLayouts(t *testing.T) {
	cases := []struct {
		name    string
		install func(*testing.T, string, string) string
	}{
		{"current layout", installFNM},
		{"legacy layout", installFNMLegacy},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			home := t.TempDir()
			want := testCase.install(t, home, "24.20.0")

			got, err := resolver().Resolve(Preferences{Version: "24.20.0", Home: home})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.NodePath != want || got.Source != SourceFNM {
				t.Fatalf("resolved = %+v, want the fnm installation at %q", got, want)
			}
		})
	}
}

// TestResolveFindsTheNVMWindowsLayout pins the layout nvm-windows uses: the
// runtime sits directly under the version directory, without a bin/ level. A
// search that only understood the Unix layout found nothing here and reported
// "no Node installed" for a working installation.
func TestResolveFindsTheNVMWindowsLayout(t *testing.T) {
	home := t.TempDir()
	want := writeNode(t, filepath.Join(home, "AppData", "Roaming", "nvm", "v24.20.0", nodeBinaryName))

	got, err := resolver().Resolve(Preferences{Version: "24.20.0", Home: home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != want || got.Source != SourceNVM {
		t.Fatalf("resolved = %+v, want the nvm-windows installation at %q", got, want)
	}
}

// TestResolveLatestPicksTheNewestRelease pins the "latest" selector across both
// version managers.
func TestResolveLatestPicksTheNewestRelease(t *testing.T) {
	home := t.TempDir()
	installNVM(t, home, "22.19.0")
	newest := installFNM(t, home, "24.21.0")
	installNVM(t, home, "24.20.0")

	for _, requested := range []string{"", "latest", "LATEST"} {
		got, err := resolver().Resolve(Preferences{Version: requested, Home: home})
		if err != nil {
			t.Fatalf("Resolve(%q): %v", requested, err)
		}
		if got.NodePath != newest {
			t.Fatalf("Resolve(%q) = %q, want %q", requested, got.NodePath, newest)
		}
	}
}

// TestResolveAcceptsAnEquivalentVersionString pins that "24.20" and "v24.20.0"
// name the same release instead of failing an exact string comparison.
func TestResolveAcceptsAnEquivalentVersionString(t *testing.T) {
	home := t.TempDir()
	want := installNVM(t, home, "24.20.0")
	for _, requested := range []string{"24.20.0", "v24.20.0", "24.20"} {
		got, err := resolver().Resolve(Preferences{Version: requested, Home: home})
		if err != nil {
			t.Fatalf("Resolve(%q): %v", requested, err)
		}
		if got.NodePath != want {
			t.Fatalf("Resolve(%q) = %q, want %q", requested, got.NodePath, want)
		}
	}
}

// TestResolveRefusesAMissingPinnedRelease pins that a pin is never satisfied by
// an unrelated release.
func TestResolveRefusesAMissingPinnedRelease(t *testing.T) {
	home := t.TempDir()
	installNVM(t, home, "24.20.0")

	_, err := resolver().Resolve(Preferences{Version: "24.99.0", Home: home})
	if err == nil {
		t.Fatal("expected an error for a release that is not installed")
	}
	if !contains(err.Error(), "24.99.0") {
		t.Fatalf("error = %v, want it to name the requested release", err)
	}
}

// TestResolveIgnoresUnusableInstallations pins that a version directory without
// a runnable node binary, or with a directory where the binary belongs, is not a
// candidate.
func TestResolveIgnoresUnusableInstallations(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".nvm", "versions", "node")
	// A release directory with no bin/node at all.
	if err := os.MkdirAll(filepath.Join(root, "v24.20.0", "bin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A release directory whose node is a directory.
	if err := os.MkdirAll(filepath.Join(root, "v24.21.0", "bin", nodeBinaryName), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A directory whose name is not a version.
	if err := os.MkdirAll(filepath.Join(root, "current", "bin"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	want := installNVM(t, home, "24.22.0")

	got, err := resolver().Resolve(Preferences{Home: home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != want {
		t.Fatalf("NodePath = %q, want %q", got.NodePath, want)
	}
}

// TestResolveFallsBackToPATH pins the last resort, and that the caller can learn
// the version it actually got.
func TestResolveFallsBackToPATH(t *testing.T) {
	fallback := &Resolver{
		LookPath: func(name string) (string, error) {
			if name == "node" {
				return "/usr/local/bin/node", nil
			}
			return "", errors.New("not found")
		},
		Glob: filepath.Glob,
		Stat: os.Stat,
	}
	// With a pin that is not installed anywhere, the resolve fails...
	if _, err := fallback.Resolve(Preferences{Version: "24.99.0", Home: t.TempDir()}); err == nil {
		t.Fatal("a missing pinned release must fail")
	}
	// ...while "latest" accepts what PATH offers.
	got, err := fallback.Resolve(Preferences{Home: t.TempDir()})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Source != SourcePath || got.NodePath != "/usr/local/bin/node" {
		t.Fatalf("resolved = %+v", got)
	}
	if got.Version != "" {
		t.Fatalf("a PATH hit has no version until it is asked, got %q", got.Version)
	}
}

// TestResolveWithoutAnyNode pins the failure message.
func TestResolveWithoutAnyNode(t *testing.T) {
	_, err := resolver().Resolve(Preferences{Version: "24.20.0", Home: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !contains(err.Error(), "nvm") || !contains(err.Error(), "fnm") {
		t.Fatalf("error = %v, want it to name where it looked", err)
	}
}

// TestInfoReadsTheVersionAndReportsAMismatch pins the PATH fallback: the binary
// is asked for its release, and a release that does not satisfy the request is
// reported rather than silently used.
func TestInfoReadsTheVersionAndReportsAMismatch(t *testing.T) {
	stub := fakeOutput{stdout: "v22.19.0\n"}
	resolver := &Resolver{Output: stub}

	got := resolver.Info(context.Background(), Installation{NodePath: "/usr/bin/node", Source: SourcePath}, "24.20.0")
	if got.Version != "22.19.0" {
		t.Fatalf("Version = %q, want 22.19.0", got.Version)
	}
	if got.Requested != "24.20.0" {
		t.Fatalf("Requested = %q, want the release that was asked for", got.Requested)
	}

	// A satisfying version reports no mismatch.
	stub.stdout = "v24.20.0\n"
	got = (&Resolver{Output: stub}).Info(context.Background(), Installation{NodePath: "/usr/bin/node"}, "24.20")
	if got.Requested != "" {
		t.Fatalf("Requested = %q, want empty when the release satisfies the pin", got.Requested)
	}

	// "latest" never reports a mismatch.
	got = (&Resolver{Output: stub}).Info(context.Background(), Installation{NodePath: "/usr/bin/node"}, Latest)
	if got.Requested != "" {
		t.Fatalf("Requested = %q, want empty for latest", got.Requested)
	}
}

// TestInfoKeepsAKnownVersion pins that a version implied by a directory name is
// not re-read from the binary.
func TestInfoKeepsAKnownVersion(t *testing.T) {
	stub := fakeOutput{stdout: "v99.0.0\n"}
	got := (&Resolver{Output: stub}).Info(context.Background(), Installation{Version: "24.20.0", NodePath: "/nvm/node"}, "24.20.0")
	if got.Version != "24.20.0" {
		t.Fatalf("Version = %q, want the directory name", got.Version)
	}
}

// TestInfoToleratesAnUnrunnableNode pins that a failed probe leaves the version
// unknown instead of failing the operation.
func TestInfoToleratesAnUnrunnableNode(t *testing.T) {
	stub := fakeOutput{err: errors.New("boom")}
	got := (&Resolver{Output: stub}).Info(context.Background(), Installation{NodePath: "/usr/bin/node"}, "24.20.0")
	if got.Version != "" {
		t.Fatalf("Version = %q, want empty", got.Version)
	}
	if got.Requested != "" {
		t.Fatalf("Requested = %q, want empty when the version is unknown", got.Requested)
	}
}

// fakeOutput answers captured output.
type fakeOutput struct {
	stdout string
	err    error
}

// Output implements run.Outputer.
func (f fakeOutput) Output(context.Context, run.Command) (string, error) {
	return f.stdout, f.err
}

// Capture implements run.Capturer.
func (f fakeOutput) Capture(context.Context, run.Command) run.Result {
	return run.Result{Stdout: f.stdout, Err: f.err}
}

// TestCompareAndMatches pins the version arithmetic.
func TestCompareAndMatches(t *testing.T) {
	cases := []struct {
		left, right string
		want        int
	}{
		{"24.20.0", "24.20.0", 0},
		{"24.20.0", "24.12.0", 1},
		{"24.12.0", "24.20.0", -1},
		{"24.20", "24.20.0", 0},
		{"v24.20.0", "24.20.0", 0},
		{"24.20.0-rc.1", "24.20.0", 0},
		{"24.20.0+build5", "24.20.0", 0},
		{"22.19.0", "24.0.0", -1},
		{"24.0.0", "24.0.1", -1},
		{"24.0.10", "24.0.9", 1},
		{"garbage", "0.0.0", 0},
	}
	for _, testCase := range cases {
		if got := Compare(testCase.left, testCase.right); got != testCase.want {
			t.Fatalf("Compare(%q, %q) = %d, want %d", testCase.left, testCase.right, got, testCase.want)
		}
	}
	if !Matches("24.20.0", "24.20") || Matches("24.20.0", "24.21") {
		t.Fatal("Matches does not agree with Compare")
	}
	if Matches("", "24.20") || Matches("24.20", "") {
		t.Fatal("an empty version never matches")
	}
}

// TestParseVersion pins the `node -v` reader.
func TestParseVersion(t *testing.T) {
	cases := map[string]string{
		"v24.20.0\n":   "24.20.0",
		"24.20.0":      "24.20.0",
		"  v22.19.0  ": "22.19.0",
		"v24.20.0 \n":  "24.20.0",
		"":             "",
		"v":            "",
	}
	for input, want := range cases {
		if got := ParseVersion(input); got != want {
			t.Fatalf("ParseVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestAtLeast pins the minimum-version check, including the unknown case.
func TestAtLeast(t *testing.T) {
	cases := []struct {
		version string
		minimum string
		want    bool
	}{
		{"24.20.0", "24.12.0", true},
		{"24.12.0", "24.12.0", true},
		{"22.19.0", "24.12.0", false},
		{"", "24.12.0", false},
	}
	for _, testCase := range cases {
		if got := (Installation{Version: testCase.version}).AtLeast(testCase.minimum); got != testCase.want {
			t.Fatalf("AtLeast(%q, %q) = %v, want %v", testCase.version, testCase.minimum, got, testCase.want)
		}
	}
}

// TestIsExecutable pins the platform's notion of a runnable binary.
func TestIsExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		if !isExecutable(0o644) {
			t.Fatal("Windows has no execute bit, so any regular file counts")
		}
		return
	}
	if isExecutable(0o644) {
		t.Fatal("a file without an execute bit is not runnable")
	}
	if !isExecutable(0o755) {
		t.Fatal("a file with an execute bit is runnable")
	}
}

// contains reports whether haystack holds needle.
func contains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestNodeBinaryNameMatchesThePlatform pins the executable name itself: the
// layout tests share the constant with the implementation, so only this
// assertion would catch a Windows build that looked for "node" instead of
// "node.exe".
func TestNodeBinaryNameMatchesThePlatform(t *testing.T) {
	if runtime.GOOS == "windows" {
		if nodeBinaryName != "node.exe" {
			t.Fatalf("nodeBinaryName = %q on Windows, want node.exe", nodeBinaryName)
		}
		return
	}
	if nodeBinaryName != "node" {
		t.Fatalf("nodeBinaryName = %q, want node", nodeBinaryName)
	}
}
