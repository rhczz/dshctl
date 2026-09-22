package nodejs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rhczz/dshctl/internal/run"
)

// The decision table this file pins is the R section of the resolution matrix:
// one case per row, all of them listed in matrix_completeness_test.go so a row
// cannot be dropped silently.

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

// installNVMWindows creates the layout nvm-windows uses.
func installNVMWindows(t *testing.T, home, version string) string {
	t.Helper()
	return writeNode(t, filepath.Join(home, "AppData", "Roaming", "nvm", "v"+version, nodeBinaryName))
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

// Probe argument keys: the two questions the resolver asks a node binary.
const (
	versionProbe  = "-v"
	execPathProbe = "-p process.execPath"
)

// machine is a fictional host: a home that may hold version managers, a PATH
// that may or may not hold a node, and a node binary that answers — or refuses
// to answer — the two questions the resolver asks it.
type machine struct {
	t       *testing.T
	home    string
	pathHit string
	answers map[string]probeAnswer
	probed  []string
	globs   []string
}

// probeAnswer is what the fictional node binary prints, or why it fails.
type probeAnswer struct {
	stdout string
	err    error
}

// newMachine returns a machine whose PATH holds no node and whose home is empty.
func newMachine(t *testing.T) *machine {
	t.Helper()
	return &machine{t: t, home: t.TempDir(), answers: map[string]probeAnswer{}}
}

// nodeOnPATH makes `node` resolve to path and answer with version, as a direct
// installation would: the binary is the interpreter that runs.
func (m *machine) nodeOnPATH(path, version string) {
	m.pathHit = path
	m.answers[versionProbe] = probeAnswer{stdout: "v" + version + "\n"}
	m.answers[execPathProbe] = probeAnswer{stdout: path + "\n"}
}

// forwardingNodeOnPATH makes `node` resolve to a shim that forwards to real.
func (m *machine) forwardingNodeOnPATH(shim, real, version string) {
	m.pathHit = shim
	m.answers[versionProbe] = probeAnswer{stdout: "v" + version + "\n"}
	m.answers[execPathProbe] = probeAnswer{stdout: real + "\n"}
}

// installNVM installs a release under the machine's home.
func (m *machine) installNVM(version string) string { return installNVM(m.t, m.home, version) }

// installFNM installs a release under the machine's home.
func (m *machine) installFNM(version string) string { return installFNM(m.t, m.home, version) }

// resolver wires the machine into a Resolver.
func (m *machine) resolver() *Resolver {
	return &Resolver{
		LookPath: func(name string) (string, error) {
			if name != "node" {
				return "", os.ErrNotExist
			}
			if m.pathHit == "" {
				return "", os.ErrNotExist
			}
			return m.pathHit, nil
		},
		Glob: func(pattern string) ([]string, error) {
			m.globs = append(m.globs, pattern)
			return filepath.Glob(pattern)
		},
		Stat:   os.Stat,
		Output: m,
	}
}

// Output implements run.Outputer, recording every probe.
func (m *machine) Output(_ context.Context, cmd run.Command) (string, error) {
	key := strings.Join(cmd.Args, " ")
	m.probed = append(m.probed, key)
	answer, ok := m.answers[key]
	if !ok {
		return "", fmt.Errorf("unexpected probe: %s %s", cmd.Name, key)
	}
	return answer.stdout, answer.err
}

// probedKeys returns the argument keys the fictional node was asked, in order.
func (m *machine) probedKeys() []string { return append([]string(nil), m.probed...) }

// wantProbes fails the test unless exactly the given probes were made.
func (m *machine) wantProbes(t *testing.T, want ...string) {
	t.Helper()
	got := m.probedKeys()
	if len(got) != len(want) {
		t.Fatalf("probe = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("probe = %v, want %v", got, want)
		}
	}
}

// resolveRows is the R section of the decision table: one case per row.
var resolveRows = map[string]func(*testing.T){
	"R1":  testResolveR1DiscoversFromPATH,
	"R2":  testResolveR2ReportsAMissingPATHNode,
	"R3":  testResolveR3ReportsAnUnusablePATHNode,
	"R4":  testResolveR4ReturnsWhateverPATHServes,
	"R5":  testResolveR5FindsARequestedReleaseOnPATH,
	"R6":  testResolveR6LetsTheManagersAnswerWithoutProbing,
	"R7":  testResolveR7FindsARequestedReleaseInFNM,
	"R8":  testResolveR8ReportsAMissingRequestedRelease,
	"R9":  testResolveR9PicksTheRequestedReleaseExactly,
	"R10": testResolveR10FollowsAForwardingShim,
	"R11": testResolveR11ToleratesAFailedExecPathProbe,
	"R12": testResolveR12ReportsARequestThatNamesNothing,
	"R13": testResolveR13NeverGlobsARelativePathWithoutAHome,
}

// TestResolveMatrix runs every row of the resolution table.
func TestResolveMatrix(t *testing.T) {
	for id, run := range resolveRows {
		t.Run(id, func(t *testing.T) { run(t) })
	}
}

// TestResolveR1DiscoversFromPATH pins the default path: nothing was asked for,
// so the runtime is what PATH serves, and its release is read from the binary.
func testResolveR1DiscoversFromPATH(t *testing.T) {
	m := newMachine(t)
	binary := filepath.Join(m.home, "usr", "bin", nodeBinaryName)
	m.nodeOnPATH(binary, "24.20.0")

	got, err := m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := Installation{
		Version:  "24.20.0",
		NodePath: binary,
		BinDir:   filepath.Dir(binary),
		Source:   SourcePath,
	}
	if got != want {
		t.Fatalf("resolved = %+v, want %+v", got, want)
	}
	m.wantProbes(t, versionProbe, execPathProbe)
}

// TestResolveR2ReportsAMissingPATHNode pins the failure that carries the
// observation instead of a message: PATH had nothing to offer.
func testResolveR2ReportsAMissingPATHNode(t *testing.T) {
	m := newMachine(t)

	_, err := m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
	failure := wantFailure(t, err)
	if failure.Requested != "" {
		t.Fatalf("Requested = %q, want empty for a discovery", failure.Requested)
	}
	if len(failure.Observations) != 1 {
		t.Fatalf("observations = %+v, want exactly one", failure.Observations)
	}
	if failure.Observations[0].Source != SourcePath {
		t.Fatalf("observation = %+v, want the PATH observation", failure.Observations[0])
	}
	if failure.Err != nil {
		t.Fatalf("Err = %v, want nil: nothing was there to fail", failure.Err)
	}
	m.wantProbes(t)
}

// TestResolveR3ReportsAnUnusablePATHNode pins what a node that cannot say what
// it is means: the failure names the path and carries the reason, so the
// operator learns which binary is broken instead of reading "no Node".
func testResolveR3ReportsAnUnusablePATHNode(t *testing.T) {
	m := newMachine(t)
	binary := filepath.Join(m.home, "usr", "bin", nodeBinaryName)
	m.pathHit = binary
	m.answers[versionProbe] = probeAnswer{err: errors.New("exit status 1")}

	_, err := m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
	failure := wantFailure(t, err)
	if failure.Err == nil || !strings.Contains(failure.Err.Error(), "exit status 1") {
		t.Fatalf("Err = %v, want the probe's own failure", failure.Err)
	}
	observation := failure.Observations[0]
	if observation.Path != binary {
		t.Fatalf("observation = %+v, want it to name %q", observation, binary)
	}
}

// TestResolveR4ReturnsWhateverPATHServes pins that the resolver reports what it
// found: the minimum-version gate is Assess's decision, not the resolver's, so
// there is exactly one place where a release is refused.
func testResolveR4ReturnsWhateverPATHServes(t *testing.T) {
	m := newMachine(t)
	binary := filepath.Join(m.home, "usr", "bin", nodeBinaryName)
	m.nodeOnPATH(binary, "22.14.0")

	got, err := m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Version != "22.14.0" {
		t.Fatalf("Version = %q, want the release PATH serves", got.Version)
	}
}

// TestResolveR5FindsARequestedReleaseOnPATH pins the case the version-manager
// scan alone used to miss: a release installed by something else — Homebrew, n,
// the official installer — that is exactly the one asked for.
func testResolveR5FindsARequestedReleaseOnPATH(t *testing.T) {
	m := newMachine(t)
	binary := filepath.Join(m.home, "opt", "homebrew", "bin", nodeBinaryName)
	m.nodeOnPATH(binary, "24.20.0")

	got, err := m.resolver().Resolve(context.Background(), Preferences{Version: "24.20.0", Home: m.home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != binary || got.Version != "24.20.0" || got.Source != SourcePath {
		t.Fatalf("resolved = %+v, want the PATH installation at %q", got, binary)
	}
}

// TestResolveR6LetsTheManagersAnswerWithoutProbing pins two things at once: a
// requested release that a version manager holds wins over a different node on
// PATH, and finding it costs no process at all — the release is implied by the
// directory name.
func testResolveR6LetsTheManagersAnswerWithoutProbing(t *testing.T) {
	m := newMachine(t)
	want := m.installNVM("24.20.0")
	m.installNVM("24.21.0")
	// PATH serves a different release entirely: it must not be consulted.
	m.nodeOnPATH(filepath.Join(m.home, "usr", "bin", nodeBinaryName), "22.14.0")

	got, err := m.resolver().Resolve(context.Background(), Preferences{Version: "24.20.0", Home: m.home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != want || got.Version != "24.20.0" || got.Source != SourceNVM {
		t.Fatalf("resolved = %+v, want the nvm installation at %q", got, want)
	}
	m.wantProbes(t)
}

// TestResolveR7FindsARequestedReleaseInFNM pins the fnm half of the manager
// scan: a release that only fnm holds is found, and no process is started.
func testResolveR7FindsARequestedReleaseInFNM(t *testing.T) {
	m := newMachine(t)
	want := m.installFNM("24.20.0")

	got, err := m.resolver().Resolve(context.Background(), Preferences{Version: "24.20.0", Home: m.home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != want || got.Source != SourceFNM || got.Version != "24.20.0" {
		t.Fatalf("resolved = %+v, want the fnm installation at %q", got, want)
	}
	m.wantProbes(t)
}

// TestResolveR8ReportsAMissingRequestedRelease pins the report of a release that
// is installed nowhere: it names the release and shows what PATH had instead,
// because "22.14.0 is on your PATH" is the fact the operator needs to see.
func testResolveR8ReportsAMissingRequestedRelease(t *testing.T) {
	m := newMachine(t)
	m.installNVM("24.19.0")
	m.nodeOnPATH(filepath.Join(m.home, "usr", "bin", nodeBinaryName), "22.14.0")

	_, err := m.resolver().Resolve(context.Background(), Preferences{Version: "24.20.0", Home: m.home})
	failure := wantFailure(t, err)
	if failure.Requested != "24.20.0" {
		t.Fatalf("Requested = %q, want the release that was asked for", failure.Requested)
	}
	if len(failure.Observations) != 2 {
		t.Fatalf("observations = %+v, want the managers and PATH", failure.Observations)
	}
	if failure.Observations[0].Source != SourceManagers || !strings.Contains(failure.Observations[0].Detail, "24.19.0") {
		t.Fatalf("managers observation = %+v, want it to name the newest release found", failure.Observations[0])
	}
	if failure.Observations[1].Version != "22.14.0" {
		t.Fatalf("PATH observation = %+v, want the release PATH serves", failure.Observations[1])
	}
}

// TestResolveR9PicksTheRequestedReleaseExactly pins that a request is satisfied
// by that release and not by a neighbouring one.
func testResolveR9PicksTheRequestedReleaseExactly(t *testing.T) {
	m := newMachine(t)
	m.installNVM("24.20.0")
	want := m.installNVM("24.19.0")
	m.installNVM("24.21.0")

	for _, requested := range []string{"24.19.0", "24.19", "v24.19.0", " 24.19.0 "} {
		got, err := m.resolver().Resolve(context.Background(), Preferences{Version: requested, Home: m.home})
		if err != nil {
			t.Fatalf("Resolve(%q): %v", requested, err)
		}
		if got.NodePath != want || got.Version != "24.19.0" {
			t.Fatalf("Resolve(%q) = %+v, want the 24.19.0 installation at %q", requested, got, want)
		}
	}
}

// TestResolveR10FollowsAForwardingShim pins that the runtime handed to the
// server is the interpreter, not the forwarder: a shim resolves by its own rules
// at exec time, so the directory put in front of PATH must be the real one.
func testResolveR10FollowsAForwardingShim(t *testing.T) {
	m := newMachine(t)
	shim := filepath.Join(m.home, ".asdf", "shims", nodeBinaryName)
	real := filepath.Join(m.home, ".asdf", "installs", "nodejs", "24.20.0", "bin", nodeBinaryName)
	m.forwardingNodeOnPATH(shim, real, "24.20.0")

	got, err := m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != real || got.BinDir != filepath.Dir(real) {
		t.Fatalf("resolved = %+v, want the real interpreter at %q", got, real)
	}
	if !got.ViaShim {
		t.Fatalf("resolved = %+v, want ViaShim reported", got)
	}
}

// TestResolveR11ToleratesAFailedExecPathProbe pins the degradation: a binary
// that cannot be asked where it lives is still usable, and the directory of the
// PATH hit is the best available answer.
func testResolveR11ToleratesAFailedExecPathProbe(t *testing.T) {
	m := newMachine(t)
	binary := filepath.Join(m.home, "usr", "bin", nodeBinaryName)
	m.nodeOnPATH(binary, "24.20.0")
	m.answers[execPathProbe] = probeAnswer{err: errors.New("boom")}

	got, err := m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != binary || got.BinDir != filepath.Dir(binary) || got.ViaShim {
		t.Fatalf("resolved = %+v, want the PATH hit at %q with no shim reported", got, binary)
	}
}

// TestResolveR12ReportsARequestThatNamesNothing pins that a value which is not a
// release is reported as a request that cannot be satisfied rather than being
// silently reinterpreted.
func testResolveR12ReportsARequestThatNamesNothing(t *testing.T) {
	for _, requested := range []string{"latest", "abc", "v"} {
		m := newMachine(t)
		m.nodeOnPATH(filepath.Join(m.home, "usr", "bin", nodeBinaryName), "24.20.0")

		_, err := m.resolver().Resolve(context.Background(), Preferences{Version: requested, Home: m.home})
		failure := wantFailure(t, err)
		if failure.Requested == "" {
			t.Fatalf("Resolve(%q) reported a discovery failure, want the request named", requested)
		}
	}
}

// TestResolveR13NeverGlobsARelativePathWithoutAHome pins what an unset home must
// not do.
//
// Every root the resolver searches is built by joining the home directory, and
// filepath.Join("", …) produces a path relative to the process's working
// directory rather than an empty one. A resolver handed an empty home therefore
// searches the caller's working directory for .nvm/versions/node/* — a lookup
// whose answer depends on where dshctl happened to be started.
func testResolveR13NeverGlobsARelativePathWithoutAHome(t *testing.T) {
	var patterns []string
	recording := &Resolver{
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
		Glob: func(pattern string) ([]string, error) {
			patterns = append(patterns, pattern)
			return nil, nil
		},
		Stat: os.Stat,
	}

	if _, err := recording.Resolve(context.Background(), Preferences{Home: ""}); err == nil {
		t.Fatal("with no home and nothing on PATH the resolve must fail")
	}
	for _, pattern := range patterns {
		if !filepath.IsAbs(pattern) {
			t.Errorf("Resolve searched the relative path %q: an empty Home must not make the resolver read the working directory", pattern)
		}
	}
}

// wantFailure asserts that a resolution was refused, and returns the refusal so
// a row can assert on what it carries.
func wantFailure(t *testing.T, failure *Failure) *Failure {
	t.Helper()
	if failure == nil {
		t.Fatal("expected a resolution failure")
	}
	return failure
}

// TestResolveFillsInAnEmptyHomeFromThePlatform pins what an unset Home means.
//
// The version managers live in the operating system's home directory, so a
// preference that names none is filled in from the platform rather than left
// empty. The throwaway HOME here is the only home the test process can see, so
// the installation that comes back is the one the fallback resolved.
func TestResolveFillsInAnEmptyHomeFromThePlatform(t *testing.T) {
	home := t.TempDir()
	// os.UserHomeDir reads $HOME on Unix and %USERPROFILE% on Windows.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	want := installNVM(t, home, "24.20.0")
	got, err := newMachineWithHome(t, home).resolver().Resolve(
		context.Background(), Preferences{Version: "24.20.0", Home: ""})
	if err != nil {
		t.Fatalf("Resolve with no home: %v", err)
	}
	if got.NodePath != want || got.Source != SourceNVM || got.Version != "24.20.0" {
		t.Fatalf("resolved = %+v, want the nvm installation at %q", got, want)
	}
}

// newMachineWithHome returns a machine whose home is the given directory rather
// than a fresh temporary one.
func newMachineWithHome(t *testing.T, home string) *machine {
	t.Helper()
	return &machine{t: t, home: home, answers: map[string]probeAnswer{}}
}

// TestResolvedInstallationsAreWellFormed pins the invariants every caller leans
// on: a resolved installation names a release, an absolute binary of a known
// origin, and the directory that binary lives in.
func TestResolvedInstallationsAreWellFormed(t *testing.T) {
	cases := map[string]func(*testing.T) (Installation, *Failure){
		"from PATH": func(t *testing.T) (Installation, *Failure) {
			m := newMachine(t)
			m.nodeOnPATH(filepath.Join(m.home, "usr", "bin", nodeBinaryName), "24.20.0")
			return m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
		},
		"through a shim": func(t *testing.T) (Installation, *Failure) {
			m := newMachine(t)
			m.forwardingNodeOnPATH(
				filepath.Join(m.home, "shims", nodeBinaryName),
				filepath.Join(m.home, "real", "bin", nodeBinaryName),
				"24.20.0")
			return m.resolver().Resolve(context.Background(), Preferences{Home: m.home})
		},
		"from nvm": func(t *testing.T) (Installation, *Failure) {
			m := newMachine(t)
			m.installNVM("24.20.0")
			return m.resolver().Resolve(context.Background(), Preferences{Version: "24.20.0", Home: m.home})
		},
	}
	for name, resolve := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := resolve(t)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Version == "" {
				t.Error("Version is empty: a resolution must name the release it will run")
			}
			if !filepath.IsAbs(got.NodePath) || !filepath.IsAbs(got.BinDir) {
				t.Errorf("resolved = %+v, want absolute paths", got)
			}
			if got.BinDir != filepath.Dir(got.NodePath) {
				t.Errorf("BinDir = %q, want the directory of %q", got.BinDir, got.NodePath)
			}
			switch got.Source {
			case SourceNVM, SourceFNM, SourcePath:
			default:
				t.Errorf("Source = %q, want one of the documented origins", got.Source)
			}
			if got.Source != SourcePath && got.ViaShim {
				t.Errorf("resolved = %+v, want ViaShim only for a PATH hit", got)
			}
		})
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

// contains reports whether haystack holds needle.
func contains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
