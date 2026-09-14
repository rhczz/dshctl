package nodejs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rhczz/dshctl/internal/paths"
)

// TestCompareReadsEverySegmentAsANumber pins that a version is compared
// segment by segment as an integer, not as text and not only at the first
// position. A textual comparison puts 24.9 above 24.10, which would make
// "latest" pick an older release and make a pinned request match the wrong one.
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
// Nobody validates a version before comparing it — the resolver compares
// directory names and a path lookup's output — so the function has to answer
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

// TestCompareIsATotalPreorder pins the three properties every caller relies on:
// a version equals itself, the order is antisymmetric, and it is transitive.
//
// Sorting releases to find "latest" and asking whether a candidate satisfies a
// pin are both built on Compare, and a comparison that is not a preorder makes
// sort.SliceStable produce an order that depends on the input order — a bug that
// shows up as "latest picked 24.20 yesterday and 24.21 today".
func TestCompareIsATotalPreorder(t *testing.T) {
	corpus := []string{
		"", "0", "v", "v-v", "v0", "24", "24.20", "24.20.0", "v24.20.0",
		"24.20.x", "24.20.0-rc.1", "24.20.0+build.5", "garbage",
		"99999999999999999999", "24.20.9", "24.20.10", "10.0.0", "9.9.9", " 24.20.0 ",
	}
	for _, left := range corpus {
		if got := Compare(left, left); got != 0 {
			t.Errorf("Compare(%q, %q) = %d, want 0", left, left, got)
		}
		for _, right := range corpus {
			forward, backward := Compare(left, right), Compare(right, left)
			if forward != -backward {
				t.Errorf("Compare(%q, %q) = %d but Compare(%q, %q) = %d, want the opposite sign",
					left, right, forward, right, left, backward)
			}
			for _, third := range corpus {
				if forward <= 0 && Compare(right, third) <= 0 && Compare(left, third) > 0 {
					t.Errorf("Compare is not transitive: %q <= %q <= %q but %q > %q",
						left, right, third, left, third)
				}
			}
		}
	}
}

// TestParseVersionReadsTheFirstToken pins `node -v` parsing.
//
// The command prints one line, and the value that comes back is compared against
// a pin, so anything after the version — a newline, a warning on the same line, a
// second line — must not become part of it.
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

// TestResolveFindsEveryFnMRootAndLayout pins the four shapes an fnm installation
// can have on disk: the current layout that nests the runtime under
// installation/, the older layout that puts it directly under the version, the
// Windows layout that has no bin/ level at all, and each of the three
// directories fnm uses for its releases.
//
// Only one of these was exercised before. Leaving the others untested is how the
// fnm branch became dead code once already: a search that probes
// <version>/bin/node finds nothing on a current fnm installation and reports
// "no Node installed" for a perfectly good one.
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
				home := t.TempDir()
				want := installFnMUnder(t, filepath.Join(home, root.relative), version, layout.relative)

				got, err := resolver().Resolve(Preferences{Version: version, Home: home})
				if err != nil {
					t.Fatalf("Resolve: %v", err)
				}
				if got.NodePath != want || got.Source != SourceFNM {
					t.Fatalf("resolved = %+v, want the fnm installation at %q", got, want)
				}
				if got.Version != version || got.BinDir != filepath.Dir(want) {
					t.Fatalf("resolved = %+v, want version %q and bin dir %q", got, version, filepath.Dir(want))
				}
			})
		}
	}
}

// TestResolveWithAnEmptyHomeNeverGlobsARelativePath pins what an unset home must
// not do.
//
// Every root the resolver searches is built by joining the home directory, and
// filepath.Join("", …) produces a path relative to the process's working
// directory rather than an empty one. A resolver handed an empty home therefore
// searches the caller's working directory for .nvm/versions/node/* and
// AppData/Roaming/nvm/* — a lookup whose answer depends on where dshctl happened
// to be started, and which can pick up a node binary out of an unrelated
// directory tree.
func TestResolveWithAnEmptyHomeNeverGlobsARelativePath(t *testing.T) {
	var patterns []string
	recording := &Resolver{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Glob: func(pattern string) ([]string, error) {
			patterns = append(patterns, pattern)
			return nil, nil
		},
		Stat: os.Stat,
	}

	if _, err := recording.Resolve(Preferences{Home: ""}); err == nil {
		t.Fatal("with no home and nothing on PATH the resolve must fail")
	}
	if len(patterns) == 0 {
		t.Fatal("the resolver never looked for a managed installation")
	}
	for _, pattern := range patterns {
		if !filepath.IsAbs(pattern) {
			t.Errorf("Resolve searched the relative path %q: an empty Home must not make the resolver read the working directory", pattern)
		}
	}
}

// TestResolveFillsInAnEmptyHomeFromThePlatform pins what an unset Home means.
//
// The version managers live in the operating system's home directory, so a
// preference that names none is filled in from the platform rather than left
// empty: leaving it empty is what produced the relative patterns above. The
// throwaway HOME here is the only home the test process can see, so the
// installation that comes back is the one the fallback resolved.
func TestResolveFillsInAnEmptyHomeFromThePlatform(t *testing.T) {
	home := t.TempDir()
	// os.UserHomeDir reads $HOME on Unix and %USERPROFILE% on Windows.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	want := installNVM(t, home, "24.20.0")
	got, err := resolver().Resolve(Preferences{Home: ""})
	if err != nil {
		t.Fatalf("Resolve with no home: %v", err)
	}
	if got.NodePath != want || got.Source != SourceNVM || got.Version != "24.20.0" {
		t.Fatalf("resolved = %+v, want the nvm installation at %q", got, want)
	}
}

// TestResolveSkipsABrokenNodeSymlinkAndFollowsAWorkingOne pins how a symlinked
// binary is judged.
//
// A version directory whose node is a dangling symlink looks installed — the
// directory is there, the name is there — but running it fails, so it must not
// be offered as an installation. A symlink to a real executable is an ordinary
// setup (a version manager that shares one runtime, a hand-made link), and
// refusing it would report "no Node installed" for a working one.
func TestResolveSkipsABrokenNodeSymlinkAndFollowsAWorkingOne(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".nvm", "versions", "node")

	brokenDir := filepath.Join(root, "v24.20.0", "bin")
	if err := os.MkdirAll(brokenDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(filepath.Join(home, "deleted-runtime"), filepath.Join(brokenDir, nodeBinaryName)); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	real := writeNode(t, filepath.Join(home, "store", "node-24.21.0", nodeBinaryName))
	linkedDir := filepath.Join(root, "v24.21.0", "bin")
	if err := os.MkdirAll(linkedDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	linked := filepath.Join(linkedDir, nodeBinaryName)
	if err := os.Symlink(real, linked); err != nil {
		t.Fatalf("symlink to a real binary: %v", err)
	}

	got, err := resolver().Resolve(Preferences{Home: home})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.NodePath != linked || got.Version != "24.21.0" || got.Source != SourceNVM {
		t.Fatalf("resolved = %+v, want the symlinked installation at %q", got, linked)
	}

	// A dangling link never satisfies a pin for its version.
	if _, err := resolver().Resolve(Preferences{Version: "24.20.0", Home: home}); err == nil {
		t.Fatal("a version directory holding only a broken symlink must not satisfy a pin")
	}
}

// TestResolveMissingReleaseErrorMessageIsExact pins the operator-facing text for
// a pin that is not installed.
//
// The message is a remedy, not just a report: an operator who pinned a release
// reads it to learn that they can install it, change the setting, or fall back
// to PATH. Substring assertions let a rewording drop one of the three options
// while the tests stay green.
func TestResolveMissingReleaseErrorMessageIsExact(t *testing.T) {
	_, err := resolver().Resolve(Preferences{Version: "24.99.0", Home: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error for a release that is not installed")
	}
	want := "找不到 Node 24.99.0(已查找 nvm 与 fnm 的安装目录)\n" +
		"可选: 安装该版本;把配置里的 nodeVersion 改成已安装的版本或 \"latest\";" +
		"或临时用环境变量 " + paths.EnvNodeVersion + "=latest 使用 PATH 中最新的 Node"
	if err.Error() != want {
		t.Fatalf("error =\n%q\nwant\n%q", err.Error(), want)
	}
}

// TestResolveWithoutAnyNodeMessageIsExact pins the same contract for the case
// where nothing was found anywhere.
func TestResolveWithoutAnyNodeMessageIsExact(t *testing.T) {
	_, err := resolver().Resolve(Preferences{Home: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error when no Node exists at all")
	}
	want := "找不到可用的 node(已查找 nvm、fnm 与 PATH): 请安装 Node 或把它加入 PATH"
	if err.Error() != want {
		t.Fatalf("error =\n%q\nwant\n%q", err.Error(), want)
	}
}
