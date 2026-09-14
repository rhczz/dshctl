package paths

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// env is a map-backed environment lookup.
type env map[string]string

// Getenv implements Getenv.
func (e env) Getenv(key string) string { return e[key] }

// homeEnvKey names the variable os.UserHomeDir reads on this platform, so a
// test can take the home away the same way a stripped environment would.
func homeEnvKey() string {
	if runtime.GOOS == "windows" {
		return "USERPROFILE"
	}
	return "HOME"
}

// TestHome pins that the home directory is absolute.
func TestHome(t *testing.T) {
	home, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	if !filepath.IsAbs(home) {
		t.Fatalf("Home = %q, want an absolute path", home)
	}
}

// TestResolveRejectsRelativePaths pins the rule that keeps the operation lock
// from following the current directory.
func TestResolveRejectsRelativePaths(t *testing.T) {
	for _, raw := range []string{"", "   ", "relstate", "./state", "../state", "~someone/state"} {
		if _, err := Resolve(raw); !errors.Is(err, ErrNotAbsolute) {
			t.Fatalf("Resolve(%q) = %v, want ErrNotAbsolute", raw, err)
		}
	}
}

// platformAbsolute renders a platform-absolute path from slash-separated
// components, so a case about absolute paths holds on every OS instead of only
// on the one whose separator the author happened to use.
func platformAbsolute(segments ...string) string {
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	return filepath.Join(append([]string{root}, segments...)...)
}

// TestResolveExpandsAndCleans pins the accepted forms.
func TestResolveExpandsAndCleans(t *testing.T) {
	home, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	cases := map[string]string{
		platformAbsolute("absolute", "path"):                     platformAbsolute("absolute", "path"),
		platformAbsolute("absolute", ".", "path", "..", "other"): platformAbsolute("absolute", "other"),
		"~":            home,
		"~/":           home,
		"~/state":      filepath.Join(home, "state"),
		"~\\state":     filepath.Join(home, "state"),
		"  ~/padded  ": filepath.Join(home, "padded"),
	}
	for raw, want := range cases {
		got, err := Resolve(raw)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", raw, err)
		}
		if got != want {
			t.Fatalf("Resolve(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestHarnessHomePrecedence pins $DSH_HOME over the operating-system home.
func TestHarnessHomePrecedence(t *testing.T) {
	custom := platformAbsolute("custom", "harness")
	got, err := HarnessHome(env{EnvHarnessHome: custom}.Getenv)
	if err != nil || got != custom {
		t.Fatalf("HarnessHome = (%q, %v), want (%q, nil)", got, err, custom)
	}
	home, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	got, err = HarnessHome(env{}.Getenv)
	if err != nil || got != filepath.Join(home, HarnessDirName) {
		t.Fatalf("HarnessHome = (%q, %v)", got, err)
	}
}

// TestStateDirAndConfigFilePrecedence pins the resolution chain.
func TestStateDirAndConfigFilePrecedence(t *testing.T) {
	customState := platformAbsolute("custom", "state")
	customHarness := platformAbsolute("custom", "harness")
	customConfig := platformAbsolute("custom", "config.json")

	dir, err := StateDir(env{EnvStateDir: customState}.Getenv)
	if err != nil || dir != customState {
		t.Fatalf("StateDir = (%q, %v)", dir, err)
	}
	dir, err = StateDir(env{EnvHarnessHome: customHarness}.Getenv)
	if err != nil || dir != filepath.Join(customHarness, StateDirName) {
		t.Fatalf("StateDir = (%q, %v)", dir, err)
	}
	file, err := ConfigFile(env{EnvConfigFile: customConfig}.Getenv)
	if err != nil || file != customConfig {
		t.Fatalf("ConfigFile = (%q, %v)", file, err)
	}
	file, err = ConfigFile(env{EnvStateDir: customState}.Getenv)
	if err != nil || file != filepath.Join(customState, "config.json") {
		t.Fatalf("ConfigFile = (%q, %v)", file, err)
	}
	if _, err := StateDir(env{EnvStateDir: "relstate"}.Getenv); err == nil {
		t.Fatal("a relative state directory must be rejected")
	}
}

// TestProbes pins the small filesystem checks, including the distinction
// between a regular file and a directory.
func TestProbes(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "nested", "state")
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if !IsDir(dir) || !Exists(dir) {
		t.Fatal("a created directory must be visible to the probes")
	}
	file := filepath.Join(dir, "config.json")
	if err := os.WriteFile(file, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if IsDir(file) {
		t.Fatal("a regular file is not a directory")
	}
	if !IsRegularFile(file) {
		t.Fatal("a regular file must be recognised")
	}
	if IsRegularFile(dir) {
		t.Fatal("a directory is not a regular file")
	}
	if Exists(filepath.Join(dir, "absent")) {
		t.Fatal("a missing path must not exist")
	}

	// Exists does not follow a final symlink; IsRegularFile therefore reports a
	// symlink as "not a regular file", which is what provision relies on.
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(file, link); err != nil {
		// Windows without developer mode refuses to create symlinks. Skipping
		// loudly keeps the assertions below from disappearing silently, which an
		// "if err == nil" guard around them would have done.
		t.Skipf("symlinks are unavailable: %v", err)
	}
	if !Exists(link) {
		t.Fatal("a symlink exists")
	}
	if IsRegularFile(link) {
		t.Fatal("a symlink must not be reported as a regular file")
	}
}

// TestExpandHomeLeavesOtherFormsAlone pins that an unsupported form is not
// silently rewritten into something surprising.
func TestExpandHomeLeavesOtherFormsAlone(t *testing.T) {
	for _, raw := range []string{"/abs", "rel", "~user/x", ""} {
		got, err := expandHome(raw)
		if err != nil {
			t.Fatalf("expandHome(%q): %v", raw, err)
		}
		if got != raw {
			t.Fatalf("expandHome(%q) = %q, want it unchanged", raw, got)
		}
	}
}

// TestEnsureDirReportsAFileInTheWay pins the ENOTDIR failure: the parent path
// exists, but as a regular file.
//
// The error has to name the directory that could not be created, and the file
// occupying its place must survive: provisioning reports the conflict instead
// of repairing it, because that file is the operator's.
func TestEnsureDirReportsAFileInTheWay(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "state")
	if err := os.WriteFile(parent, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err := EnsureDir(filepath.Join(parent, "dshctl"))
	if err == nil {
		t.Fatal("EnsureDir must report a directory it cannot create")
	}
	if !strings.Contains(err.Error(), parent) {
		t.Fatalf("error = %v, want it to name %s", err, parent)
	}
	if !IsRegularFile(parent) {
		t.Fatal("the occupying file did not survive")
	}
}

// TestHomeRejectsAnUnusableEnvironment pins that Home never invents a home.
//
// A relative or missing home would move every file dshctl owns — the state
// directory, the runtime record, the operation lock — under whatever directory
// the process happens to run in, which is the quiet failure this package exists
// to prevent. Reporting the environment problem is the only safe answer.
func TestHomeRejectsAnUnusableEnvironment(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"unset", ""},
		{"blank", "   "},
		{"relative", "relative/home"},
		{"bare name", "home"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(homeEnvKey(), testCase.value)
			home, err := Home()
			if err == nil {
				t.Fatalf("Home = %q, want an error for %q", home, testCase.value)
			}
			if home != "" {
				t.Fatalf("Home = %q alongside an error, want no value", home)
			}
		})
	}
}

// TestResolvePropagatesAFailedHomeExpansion pins that a "~" path is neither
// kept literal nor treated as a relative path when the home is unavailable.
//
// The failure has to come from the expansion. Reporting ErrNotAbsolute would
// send the operator looking at the path they typed, when the value that is
// missing is the home directory in their environment.
func TestResolvePropagatesAFailedHomeExpansion(t *testing.T) {
	t.Setenv(homeEnvKey(), "")
	for _, raw := range []string{"~", "~/", "~/state", `~\state`} {
		got, err := Resolve(raw)
		if err == nil {
			t.Fatalf("Resolve(%q) = %q, want an error when the home is unavailable", raw, got)
		}
		if got != "" {
			t.Fatalf("Resolve(%q) = %q alongside an error, want no path", raw, got)
		}
		if errors.Is(err, ErrNotAbsolute) {
			t.Fatalf("Resolve(%q) error = %v, want the home failure, not a relative-path report", raw, err)
		}
		if !strings.Contains(err.Error(), "~") {
			t.Fatalf("Resolve(%q) error = %v, want it to name the value being expanded", raw, err)
		}
	}
}

// TestWhitespaceOnlyEnvironmentFallsThroughToTheDefault pins that a blank
// override counts as unset.
//
// Trimming before deciding is what makes a stray space harmless: taken
// literally, "  " is a relative path, which Resolve rejects — and a typo in a
// shell profile would then break every command instead of being ignored.
func TestWhitespaceOnlyEnvironmentFallsThroughToTheDefault(t *testing.T) {
	home, err := Home()
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	defaultHarness := filepath.Join(home, HarnessDirName)

	harness, err := HarnessHome(env{EnvHarnessHome: "   "}.Getenv)
	if err != nil || harness != defaultHarness {
		t.Fatalf("HarnessHome = (%q, %v), want (%q, nil)", harness, err, defaultHarness)
	}
	stateDir, err := StateDir(env{EnvStateDir: " \t\n "}.Getenv)
	if err != nil || stateDir != filepath.Join(defaultHarness, StateDirName) {
		t.Fatalf("StateDir = (%q, %v), want the default state directory", stateDir, err)
	}
	configFile, err := ConfigFile(env{EnvConfigFile: " "}.Getenv)
	if err != nil || configFile != filepath.Join(defaultHarness, StateDirName, "config.json") {
		t.Fatalf("ConfigFile = (%q, %v), want the default config file", configFile, err)
	}
}

// TestErrNotAbsoluteMentionsThePath pins that the operator can see which value
// was rejected.
func TestErrNotAbsoluteMentionsThePath(t *testing.T) {
	_, err := Resolve("relstate")
	if err == nil || !strings.Contains(err.Error(), "relstate") {
		t.Fatalf("error = %v, want it to name the rejected value", err)
	}
}
